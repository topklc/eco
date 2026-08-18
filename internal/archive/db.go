package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/toprakkilic/eco/internal/model"

	_ "modernc.org/sqlite" // pure-Go driver: keeps the mingw cross-build honest
)

// Options configures Open. The zero value is the shipping configuration.
type Options struct {
	// Clock is the time source for retention sweeps and marker defaults.
	// nil reads the wall clock (model.Clock.Now).
	Clock model.Clock

	// BusyTimeout is how long a statement waits for another writer (another
	// eco instance on the same profile) before failing. 0 means 5s.
	BusyTimeout time.Duration

	// Readers caps the read connection pool. 0 means 4. Writes always use one
	// connection of their own, because WAL permits exactly one writer.
	Readers int

	// ReadOnly opens without creating or migrating. A file below SchemaVersion
	// is then an error rather than something to fix.
	ReadOnly bool

	// SearchWindow overrides how many matches a search ranks. 0 means the
	// package default, SearchWindow; a test lowers it to reach the windowed
	// behaviour without writing twenty thousand messages first.
	SearchWindow int
}

func (o Options) busyTimeout() time.Duration {
	if o.BusyTimeout <= 0 {
		return 5 * time.Second
	}
	return o.BusyTimeout
}

func (o Options) readers() int {
	if o.Readers <= 0 {
		return 4
	}
	return o.Readers
}

// DB is one profile's archive.
//
// It holds two pools over the same file. WAL gives one writer and any number
// of concurrent readers, so writes go through w (MaxOpenConns 1, BEGIN
// IMMEDIATE) and reads through r. That is what keeps a 1M-row search from
// standing between the Recorder and the disk.
type DB struct {
	path string
	w    *sql.DB // exactly one connection; every INSERT/UPDATE/DELETE
	r    *sql.DB // read pool
	opts Options
	now  model.Clock

	// mu guards closed only; no statement runs under it.
	mu     sync.Mutex
	closed bool

	// recMu guards recs: the Recorders writing to this file. They are here
	// so that turning a buffer's logging off takes effect immediately in
	// whichever Recorder is running, whichever call the caller reached for.
	// A privacy control that only works if you call the right one of two
	// methods is not a privacy control.
	recMu sync.Mutex
	recs  []*Recorder
}

func (d *DB) addRecorder(r *Recorder) {
	d.recMu.Lock()
	d.recs = append(d.recs, r)
	d.recMu.Unlock()
}

func (d *DB) removeRecorder(r *Recorder) {
	d.recMu.Lock()
	for i, cur := range d.recs {
		if cur == r {
			d.recs = append(d.recs[:i], d.recs[i+1:]...)
			break
		}
	}
	d.recMu.Unlock()
}

// tellRecordersLogging updates every live Recorder's copy of the opt-out.
func (d *DB) tellRecordersLogging(folded string, on bool) {
	d.recMu.Lock()
	recs := append([]*Recorder(nil), d.recs...)
	d.recMu.Unlock()
	for _, r := range recs {
		r.setLoggingCached(folded, on)
	}
}

// Open opens (creating if needed) the archive at path and migrates it to
// SchemaVersion. The parent directory is created.
func Open(path string, opts Options) (*DB, error) {
	if path == "" {
		return nil, errors.New("archive: empty path")
	}
	// The DSN is "<path>?<params>" rather than a file: URI on purpose: the
	// driver splits at the first '?' and hands the left half to SQLite
	// verbatim, so no path needs percent-escaping and Windows backslashes
	// survive. The one thing that cannot survive is a '?' in the path.
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("archive: path contains '?', which the sqlite DSN cannot express: %q", path)
	}
	if opts.ReadOnly {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("archive: read-only open: %w", err)
		}
	} else if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("archive: creating %s: %w", dir, err)
		}
	}

	d := &DB{path: path, opts: opts, now: opts.Clock}

	var err error
	// A read-only archive gets a read-only "write" pool too, so that a caller
	// who opened it read-only and then asks it to write gets an error rather
	// than a write.
	if d.w, err = sql.Open("sqlite", d.dsn(!opts.ReadOnly)); err != nil {
		return nil, fmt.Errorf("archive: open %s: %w", path, err)
	}
	d.w.SetMaxOpenConns(1)
	d.w.SetMaxIdleConns(1)
	d.w.SetConnMaxLifetime(0)

	if d.r, err = sql.Open("sqlite", d.dsn(false)); err != nil {
		d.w.Close()
		return nil, fmt.Errorf("archive: open %s: %w", path, err)
	}
	d.r.SetMaxOpenConns(opts.readers())
	d.r.SetMaxIdleConns(opts.readers())
	d.r.SetConnMaxLifetime(0)

	ctx := context.Background()
	first := d.w // the writer opens (and creates) the file
	if opts.ReadOnly {
		first = d.r
	}
	if err := first.PingContext(ctx); err != nil {
		d.closePools()
		return nil, fmt.Errorf("archive: open %s: %w", path, err)
	}

	if opts.ReadOnly {
		v, err := currentVersion(ctx, d.r)
		if err != nil {
			d.closePools()
			return nil, err
		}
		if v != SchemaVersion {
			d.closePools()
			return nil, fmt.Errorf("archive: read-only open of a v%d file, this build speaks v%d", v, SchemaVersion)
		}
		return d, nil
	}

	if _, _, err := migrate(ctx, d.w, d.now.Now(), migrations); err != nil {
		d.closePools()
		return nil, err
	}

	// Session-only retention means nothing survives a restart, so anything a
	// previous run left behind (it crashed, or it was killed) goes now rather
	// than at some later sweep.
	ret, err := d.Retention(ctx)
	if err != nil {
		d.closePools()
		return nil, err
	}
	if ret == RetainSession {
		if _, err := d.PurgeAll(ctx); err != nil {
			d.closePools()
			return nil, err
		}
	}
	return d, nil
}

// dsn builds the connection string. The writer takes BEGIN IMMEDIATE so two
// eco instances on one profile collide at BEGIN (where busy_timeout applies)
// instead of half-way through a batch (where it does not).
func (d *DB) dsn(writer bool) string {
	ms := d.opts.busyTimeout().Milliseconds()
	if ms < 1 {
		ms = 1
	}
	// These are the driver's validated shorthands, not raw _pragma strings:
	// the driver checks every value before it executes any of them, so a typo
	// fails the Open instead of half-converting the file.
	q := []string{
		fmt.Sprintf("_busy_timeout=%d", ms),
		"_journal_mode=WAL",
		// NORMAL is the WAL pairing: an OS crash or power cut can lose the
		// last transactions, it cannot corrupt the file. A process crash
		// (the one we test) loses nothing.
		"_synchronous=NORMAL",
		"_foreign_keys=1",
	}
	if writer {
		q = append(q, "_txlock=immediate")
	} else {
		// Belt and braces: the read pool is refused write access by SQLite
		// itself, so a stray write lands as an error here rather than as a
		// second writer fighting the Recorder for the file.
		q = append(q, "_query_only=1")
	}
	return d.path + "?" + strings.Join(q, "&")
}

// Path is where this archive lives.
func (d *DB) Path() string { return d.path }

// Close releases both pools. It is safe to call twice.
//
// Close any Recorder first: its Close is what flushes the queue, and a
// Recorder still writing to a closed archive can only report errors.
//
// Under session-only retention Close purges first: that is the policy's whole
// promise, and the next Open purges again in case this one never ran.
func (d *DB) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()

	var errs []error
	if !d.opts.ReadOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if ret, err := d.Retention(ctx); err == nil && ret == RetainSession {
			if _, err := d.PurgeAll(ctx); err != nil {
				errs = append(errs, err)
			}
		} else if err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	errs = append(errs, d.closePools())
	return errors.Join(errs...)
}

func (d *DB) closePools() error {
	var errs []error
	if d.r != nil {
		errs = append(errs, d.r.Close())
	}
	if d.w != nil {
		errs = append(errs, d.w.Close())
	}
	return errors.Join(errs...)
}

// --- meta key/value ----------------------------------------------------------

func (d *DB) meta(ctx context.Context, key string) (string, error) {
	var v string
	err := d.r.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (d *DB) setMeta(ctx context.Context, key, value string) error {
	_, err := d.w.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// --- paths -------------------------------------------------------------------

// DefaultPath is where profile's archive lives: <data>/eco/archive/<profile>.db
// on the platform's data directory (XDG_DATA_HOME or ~/.local/share on Linux
// and BSD, ~/Library/Application Support on macOS, %AppData% on Windows).
//
// The profile id is a filename, so it is checked rather than trusted: anything
// with a separator, a drive letter or a dot-dot in it is an error.
func DefaultPath(profileID string) (string, error) {
	if err := validProfileID(profileID); err != nil {
		return "", err
	}
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "eco", "archive", profileID+".db"), nil
}

func validProfileID(id string) error {
	switch {
	case id == "":
		return errors.New("archive: empty profile id")
	case id == "." || id == "..":
		return fmt.Errorf("archive: invalid profile id %q", id)
	case strings.ContainsAny(id, `/\:`):
		return fmt.Errorf("archive: profile id %q contains a path separator", id)
	case strings.Contains(id, ".."):
		return fmt.Errorf("archive: profile id %q contains ..", id)
	case strings.ContainsRune(id, 0):
		return fmt.Errorf("archive: profile id %q contains NUL", id)
	}
	return nil
}

// DataDir is the platform's per-user data directory. Go's os package has
// UserConfigDir and UserCacheDir but no UserDataDir, so this is that gap:
// an archive is data, not configuration, and on Linux the two differ.
func DataDir() (string, error) {
	switch runtime.GOOS {
	case "windows", "darwin", "plan9", "ios":
		// On these, config and data are the same directory by convention,
		// and os.UserConfigDir already knows the right one.
		return os.UserConfigDir()
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", errors.New("archive: XDG_DATA_HOME is set to a relative path")
		}
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}
