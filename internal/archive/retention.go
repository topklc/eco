package archive

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Retention is how long the archive keeps messages (blueprint §5.4, the
// privacy contract). The values are stored in the file, so they are
// append-only.
type Retention string

const (
	// RetainForever is the default: nothing is deleted by age.
	RetainForever Retention = "forever"
	Retain365     Retention = "365"
	Retain90      Retention = "90"
	Retain30      Retention = "30"

	// RetainSession keeps messages only for as long as the app is running.
	// Open purges what a previous run left and Close purges what this one
	// wrote, so nothing survives a restart. Messages are still written to
	// disk while the app runs — that is what makes scroll-up work in the
	// session — so a hard kill can leave rows behind until the next Open,
	// which is the first thing that runs. Say that out loud in the UI copy
	// rather than promising more than the disk can deliver.
	RetainSession Retention = "session"
)

// Days is how many days this policy keeps, or 0 for policies that are not
// measured in days (forever, session-only).
func (r Retention) Days() int {
	switch r {
	case Retain365:
		return 365
	case Retain90:
		return 90
	case Retain30:
		return 30
	}
	return 0
}

// Valid reports whether r is one of the five policies.
func (r Retention) Valid() bool {
	switch r {
	case RetainForever, Retain365, Retain90, Retain30, RetainSession:
		return true
	}
	return false
}

const metaRetention = "retention"

// Retention reads the file's retention policy. A file that has never been told
// one retains forever.
func (d *DB) Retention(ctx context.Context) (Retention, error) {
	v, err := d.meta(ctx, metaRetention)
	if err != nil {
		return RetainForever, fmt.Errorf("archive: read retention: %w", err)
	}
	if r := Retention(v); r.Valid() {
		return r, nil
	}
	return RetainForever, nil
}

// SetRetention stores the policy. It does not enforce it: call ApplyRetention
// for that, so that turning the dial and paying for it are separate moments and
// the caller decides when the pause happens.
func (d *DB) SetRetention(ctx context.Context, r Retention) error {
	if !r.Valid() {
		return fmt.Errorf("archive: unknown retention policy %q", string(r))
	}
	if err := d.setMeta(ctx, metaRetention, string(r)); err != nil {
		return fmt.Errorf("archive: set retention: %w", err)
	}
	return nil
}

// ApplyRetention enforces the stored policy and reports how many messages it
// deleted. It is safe to call as often as the app likes; on a forever archive
// it is one SELECT.
//
// Deleting rows fires the FTS trigger, so expired messages leave the search
// index too. The pages they occupied stay in the file until Vacuum runs — see
// there for why that is a separate decision.
func (d *DB) ApplyRetention(ctx context.Context) (int64, error) {
	r, err := d.Retention(ctx)
	if err != nil {
		return 0, err
	}
	switch r {
	case RetainForever:
		return 0, nil
	case RetainSession:
		return d.PurgeAll(ctx)
	}
	cutoff := d.now.Now().AddDate(0, 0, -r.Days())
	return d.PurgeBefore(ctx, cutoff)
}

// PurgeBefore deletes every message older than cutoff and returns how many.
func (d *DB) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := d.w.ExecContext(ctx, `DELETE FROM messages WHERE ts < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("archive: purge before %s: %w", cutoff.Format(time.RFC3339), err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if err := d.refreshLastMsgIDs(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

// PurgeAll deletes every message, leaving the buffers and their settings
// alone: the user asked to forget the conversation, not the channel list.
func (d *DB) PurgeAll(ctx context.Context) (int64, error) {
	res, err := d.w.ExecContext(ctx, `DELETE FROM messages`)
	if err != nil {
		return 0, fmt.Errorf("archive: purge: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := d.w.ExecContext(ctx, `UPDATE buffers SET last_msgid = NULL`); err != nil {
		return n, fmt.Errorf("archive: purge: %w", err)
	}
	return n, nil
}

// refreshLastMsgIDs repoints every buffer's CHATHISTORY anchor at what is
// still stored, so a purge cannot leave the client asking a server for
// messages after a msgid it has thrown away.
func (d *DB) refreshLastMsgIDs(ctx context.Context) error {
	_, err := d.w.ExecContext(ctx, `
UPDATE buffers SET last_msgid = (
  SELECT msgid FROM messages
  WHERE messages.buffer_folded = buffers.name_folded AND msgid IS NOT NULL
  ORDER BY ts DESC, rowid DESC LIMIT 1
)`)
	if err != nil {
		return fmt.Errorf("archive: refresh last msgids: %w", err)
	}
	return nil
}

// Vacuum rebuilds the file, returning the space deleted messages were holding
// to the filesystem, and truncates the WAL.
//
// It is a separate call because it is the expensive one: it copies the whole
// database, needs room for a second copy while it runs, and blocks writers for
// as long as it takes. Deleting is cheap and can happen on a timer; vacuuming
// is what you do after the user turns retention down, or on a quiet
// disconnect — not after every sweep.
func (d *DB) Vacuum(ctx context.Context) error {
	if _, err := d.w.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("archive: vacuum: %w", err)
	}
	if _, err := d.w.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("archive: wal checkpoint: %w", err)
	}
	return nil
}

// Optimize runs the FTS5 housekeeping that keeps search fast as the index
// grows, plus ANALYZE so the planner keeps choosing the buffer_ts index. Both
// are advisory: skipping them is slower, never wrong.
func (d *DB) Optimize(ctx context.Context) error {
	if _, err := d.w.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES('optimize')`); err != nil {
		return fmt.Errorf("archive: fts optimize: %w", err)
	}
	if _, err := d.w.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("archive: analyze: %w", err)
	}
	return nil
}

// SizeOnDisk is what the archive currently costs, in bytes: the database file
// plus the write-ahead log and shared-memory files that belong to it. It is
// what a settings page shows next to the retention dial, and what a test
// checks after a vacuum.
func (d *DB) SizeOnDisk() (int64, error) {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		fi, err := os.Stat(d.path + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue // -wal and -shm come and go
			}
			return 0, fmt.Errorf("archive: size: %w", err)
		}
		total += fi.Size()
	}
	return total, nil
}
