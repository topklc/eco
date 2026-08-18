package archive

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// TestOpenCreatesTheSchemaBlueprintSpecifies pins §5.4 column by column. The
// schema is a contract with the next milestone and with every file already on
// a user's disk, so it is checked against the blueprint's spelling rather than
// against itself.
func TestOpenCreatesTheSchemaBlueprintSpecifies(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	tables := map[string][]string{
		"messages":   {"rowid", "msgid", "buffer_folded", "ts", "sender", "account", "kind", "body", "tags_json", "pending", "redacted"},
		"buffers":    {"name_folded", "display_name", "kind", "key", "last_msgid", "read_marker_msgid", "read_marker_ts", "notify_level", "logging", "pinned"},
		"list_cache": {"channel", "users", "topic", "fetched_at"},
	}
	for table, want := range tables {
		rows, err := db.r.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		var got []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			got = append(got, n)
		}
		rows.Close()
		if !equalStrings(got, want) {
			t.Errorf("%s columns = %v, want %v", table, got, want)
		}
	}

	// The index the pager depends on, by name and by columns.
	var idx string
	if err := db.r.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_messages_buffer_ts'`).Scan(&idx); err != nil {
		t.Fatalf("idx_messages_buffer_ts is missing: %v", err)
	}
	for _, col := range []string{"buffer_folded", "ts", "rowid"} {
		if !strings.Contains(idx, col) {
			t.Errorf("idx_messages_buffer_ts does not cover %s: %s", col, idx)
		}
	}

	// msgid is UNIQUE: the dedupe key of ADR-3.
	var uniq int
	if err := db.r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_index_list('messages') WHERE "unique" = 1`).Scan(&uniq); err != nil {
		t.Fatal(err)
	}
	if uniq == 0 {
		t.Error("messages.msgid is not UNIQUE, so nothing dedupes")
	}

	// FTS5 over the three columns, as external content on messages.
	var ftsSQL string
	if err := db.r.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE name='messages_fts'`).Scan(&ftsSQL); err != nil {
		t.Fatalf("messages_fts is missing: %v", err)
	}
	for _, want := range []string{"fts5", "body", "sender", "buffer_folded", "content='messages'", "content_rowid='rowid'"} {
		if !strings.Contains(ftsSQL, want) {
			t.Errorf("messages_fts is missing %q: %s", want, ftsSQL)
		}
	}
}

// TestOpenIsIdempotentAndVersioned pins the migration ladder: version 0 is an
// empty file, a second Open changes nothing, and the version table records
// what was applied so v2 can migrate instead of wiping.
func TestOpenIsIdempotentAndVersioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	ctx := context.Background()

	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	var v int
	if err := db.r.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", v, SchemaVersion)
	}
	var appliedAt int64
	if err := db.r.QueryRowContext(ctx, `SELECT applied_at FROM schema_version WHERE version = ?`, SchemaVersion).Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if appliedAt != epoch.UnixMilli() {
		t.Errorf("applied_at = %d, want the injected clock's %d", appliedAt, epoch.UnixMilli())
	}
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("#eco", "m1", at(1), "alice", "still here"))
	if err := r.Close(); err != nil {
		t.Fatalf("recorder Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer again.Close()
	var rows int
	if err := again.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != SchemaVersion {
		t.Errorf("schema_version has %d rows after reopening, want %d — a migration re-ran", rows, SchemaVersion)
	}
	got, err := again.Latest(ctx, "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "still here" {
		t.Errorf("after reopening, #eco = %v, want the one message back", bodies(got))
	}
}

// TestOpenRefusesAFutureSchema pins the forward-compatibility rule: a file
// written by a newer eco is refused, not downgraded and not wiped.
func TestOpenRefusesAFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.w.Exec(`INSERT INTO schema_version(version, applied_at) VALUES(?, ?)`,
		SchemaVersion+7, epoch.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path, Options{}); !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("Open of a newer file = %v, want ErrSchemaTooNew", err)
	}
}

// TestMigrateFromZeroIsWhatOpenDoes pins that the ladder starts at 0 rather
// than at "whatever Open happens to create", which is what will let v2 add a
// step instead of a wipe.
func TestMigrateFromZeroIsWhatOpenDoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bare.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testCtx(t)
	if err := raw.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := currentVersion(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("a file with no tables reports version %d, want 0", v)
	}
	from, to, err := migrate(ctx, raw, epoch, migrations)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if from != 0 || to != SchemaVersion {
		t.Errorf("migrate went %d → %d, want 0 → %d", from, to, SchemaVersion)
	}
	from, to, err = migrate(ctx, raw, epoch, migrations)
	if err != nil || from != SchemaVersion || to != SchemaVersion {
		t.Errorf("re-migrate went %d → %d (%v), want a no-op at %d", from, to, err, SchemaVersion)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestASecondMigrationKeepsTheMessages is the promise M9 makes to M12: when v2
// changes this schema it lands as another rung on the ladder, and the user's
// history is still there afterwards. It is testable today because migrate
// takes the ladder as an argument, so the test can walk one that has a second
// step on it.
func TestASecondMigrationKeepsTheMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	ctx := testCtx(t)

	// A v1 archive with history and a read marker in it.
	db := openTestDBAt(t, path)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "m1", at(1), "alice", "written by the v1 build"),
		msg("#eco", "m2", at(2), "bob", "and this one too"),
	)
	if _, _, err := db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m1", At: at(1)}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// What a future build's ladder looks like: the shipped steps, plus one.
	future := append(append([]migration(nil), migrations...), migration{
		version: SchemaVersion + 1,
		stmts: []string{
			`ALTER TABLE messages ADD COLUMN reply_to TEXT`,
			`CREATE INDEX idx_messages_reply ON messages(reply_to)`,
		},
	})

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	from, to, err := migrate(ctx, raw, epoch, future)
	if err != nil {
		t.Fatalf("migrating v1 → v2: %v", err)
	}
	if from != SchemaVersion || to != SchemaVersion+1 {
		t.Errorf("migrate went %d → %d, want %d → %d", from, to, SchemaVersion, SchemaVersion+1)
	}

	var bodies []string
	rows, err := raw.QueryContext(ctx, `SELECT body FROM messages ORDER BY ts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, b)
	}
	rows.Close()
	if !equalStrings(bodies, []string{"written by the v1 build", "and this one too"}) {
		t.Errorf("after the migration the archive holds %v — the upgrade was a wipe", bodies)
	}
	var markerID string
	if err := raw.QueryRowContext(ctx, `SELECT read_marker_msgid FROM buffers WHERE name_folded='#eco'`).Scan(&markerID); err != nil {
		t.Fatal(err)
	}
	if markerID != "m1" {
		t.Errorf("the read marker did not survive the migration: %q", markerID)
	}
	// The new column is really there, and the old rows have it as NULL.
	var withReply int
	if err := raw.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE reply_to IS NULL`).Scan(&withReply); err != nil {
		t.Fatalf("the v2 column is missing: %v", err)
	}
	if withReply != 2 {
		t.Errorf("%d of 2 pre-existing rows have the new column", withReply)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// And the build that only knows v1 now refuses the file rather than
	// pretending it understands it.
	if _, err := Open(path, Options{Clock: model.FixedClock(epoch)}); !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("a v1 build opening a v2 file = %v, want ErrSchemaTooNew", err)
	}
}

// TestOpenSetsWALAndBusyTimeout pins the pragmas the write-behind depends on:
// WAL is what lets a search read while the recorder writes, and the busy
// timeout is what makes a second eco on the same profile wait instead of fail.
func TestOpenSetsWALAndBusyTimeout(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	var mode string
	if err := db.r.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var busy int
	if err := db.w.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d, want a positive default", busy)
	}
	// The read pool really is read-only: a stray write must fail here rather
	// than turn into a second writer racing the Recorder.
	if _, err := db.r.ExecContext(ctx, `INSERT INTO buffers(name_folded) VALUES('#nope')`); err == nil {
		t.Error("the read pool accepted a write")
	}
}

// TestReadOnlyOpen pins that a read-only open neither creates nor migrates.
func TestReadOnlyOpen(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nothing.db")
	if _, err := Open(missing, Options{ReadOnly: true}); err == nil {
		t.Error("a read-only Open of a missing file succeeded")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a read-only Open created the file")
	}

	path := filepath.Join(dir, "ro.db")
	db := openTestDBAt(t, path)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("#eco", "m1", at(1), "alice", "hello"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := Open(path, Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only Open: %v", err)
	}
	defer ro.Close()
	got, err := ro.Latest(testCtx(t), "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("read-only archive returned %d messages, want 1", len(got))
	}
}

// TestDefaultPathRefusesTraversal pins that a profile id is treated as a
// filename, not as a path: a malicious or careless profile id must not be able
// to point the archive somewhere else (§5.6).
func TestDefaultPathRefusesTraversal(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	cases := []struct {
		name string
		id   string
		ok   bool
	}{
		{"plain", "libera", true},
		{"uuid-ish", "0f9a-2b31", true},
		{"empty", "", false},
		{"dot", ".", false},
		{"dotdot", "..", false},
		{"traversal", "../../etc/passwd", false},
		{"slash", "a/b", false},
		{"backslash", `a\b`, false},
		{"drive", "C:evil", false},
		{"hidden dotdot", "a..b", false},
		{"nul", "a\x00b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefaultPath(tc.id)
			if tc.ok != (err == nil) {
				t.Fatalf("DefaultPath(%q) = %q, %v", tc.id, got, err)
			}
			if !tc.ok {
				return
			}
			if !strings.HasSuffix(got, filepath.Join("eco", "archive", tc.id+".db")) {
				t.Errorf("DefaultPath(%q) = %q, want it under eco/archive", tc.id, got)
			}
		})
	}
}

// TestDataDirHonoursXDG pins the platform data dir: on Linux the archive is
// data, not configuration, so it follows XDG_DATA_HOME rather than
// os.UserConfigDir.
func TestDataDirHonoursXDG(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("XDG_DATA_HOME is not the platform convention here")
	}
	want := filepath.Join(t.TempDir(), "xdg-data")
	t.Setenv("XDG_DATA_HOME", want)
	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}

	t.Setenv("XDG_DATA_HOME", "relative/path")
	if _, err := DataDir(); err == nil {
		t.Error("a relative XDG_DATA_HOME was accepted")
	}

	t.Setenv("XDG_DATA_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	got, err = DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, ".local", "share") {
		t.Errorf("DataDir with no XDG_DATA_HOME = %q, want ~/.local/share", got)
	}
}

// TestOpenRejectsAQuestionMarkInThePath pins the one path the DSN cannot
// express, so it fails at Open with a sentence rather than by opening some
// other file.
func TestOpenRejectsAQuestionMarkInThePath(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "we?ird.db"), Options{}); err == nil {
		t.Error("a path containing '?' was accepted")
	}
}

// TestCloseIsIdempotent pins that shutdown paths can call Close twice.
func TestCloseIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "twice.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
