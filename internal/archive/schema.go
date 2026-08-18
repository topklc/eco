package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion is the schema this build writes and expects. A file at a lower
// version is migrated on Open; a file at a higher one is refused, because a
// newer eco wrote it and this build cannot know what it changed.
const SchemaVersion = 1

// migration is one step up the ladder. Steps are applied in order, each in its
// own transaction together with the schema_version row that records it, so a
// crash mid-migration leaves the file at the last version that fully applied.
type migration struct {
	version int
	stmts   []string
}

// migrations is the whole ladder. Never edit a step that has shipped: append a
// new one. v2's schema change is migration{version: 2, ...} and needs no wipe.
var migrations = []migration{
	{version: 1, stmts: schemaV1},
}

// schemaV1 is blueprint §5.4, spelled as the blueprint spells it.
//
// Notes that are load-bearing rather than decorative:
//   - "rowid INTEGER PRIMARY KEY" names the true rowid, which is what the FTS5
//     external-content table joins against (content_rowid='rowid') and what the
//     pager uses as its tiebreaker anchor.
//   - msgid is UNIQUE and nullable. Every message without a server msgid is
//     written as NULL, never "", so those rows do not collide with each other.
//   - schema_version and meta are this package's bookkeeping, not §5.4's: the
//     first is the version table the migration ladder walks, the second holds
//     per-profile settings (retention) that belong with the data they govern.
var schemaV1 = []string{
	`CREATE TABLE messages(
  rowid INTEGER PRIMARY KEY,
  msgid TEXT UNIQUE,              -- dedupe + merge key + pagination anchor (ADR-3)
  buffer_folded TEXT NOT NULL,    -- fold() applied
  ts INTEGER NOT NULL,            -- server-time ms
  sender TEXT, account TEXT,
  kind INTEGER NOT NULL,          -- privmsg/notice/action/status/tombstone
  body TEXT, tags_json TEXT,
  pending INTEGER NOT NULL DEFAULT 0,
  redacted INTEGER NOT NULL DEFAULT 0
)`,
	`CREATE INDEX idx_messages_buffer_ts ON messages(buffer_folded, ts, rowid)`,

	// FTS5 external content: the index stores no copy of the text, it reads it
	// back out of messages by rowid. That halves the file and makes "delete the
	// row" and "delete the index entry" the same fact rather than two.
	`CREATE VIRTUAL TABLE messages_fts USING fts5(
  body, sender, buffer_folded,
  content='messages', content_rowid='rowid',
  tokenize="unicode61 remove_diacritics 2"
)`,
	// External content indexes are not maintained by SQLite. These three
	// triggers are the maintenance, and the delete form must pass the OLD
	// values: FTS5 needs them to find the tokens it wrote.
	`CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, body, sender, buffer_folded)
  VALUES (new.rowid, new.body, new.sender, new.buffer_folded);
END`,
	`CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body, sender, buffer_folded)
  VALUES ('delete', old.rowid, old.body, old.sender, old.buffer_folded);
END`,
	`CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body, sender, buffer_folded)
  VALUES ('delete', old.rowid, old.body, old.sender, old.buffer_folded);
  INSERT INTO messages_fts(rowid, body, sender, buffer_folded)
  VALUES (new.rowid, new.body, new.sender, new.buffer_folded);
END`,

	`CREATE TABLE buffers(
  name_folded TEXT PRIMARY KEY, display_name TEXT, kind INTEGER, key TEXT,
  last_msgid TEXT, read_marker_msgid TEXT, read_marker_ts INTEGER,
  notify_level INTEGER, logging INTEGER NOT NULL DEFAULT 1, pinned INTEGER
)`,
	`CREATE TABLE list_cache(channel TEXT PRIMARY KEY, users INTEGER, topic TEXT, fetched_at INTEGER)`,

	`CREATE TABLE schema_version(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
	`CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
}

// ErrSchemaTooNew reports a file written by a later build of eco.
var ErrSchemaTooNew = errors.New("archive: database was written by a newer version of eco")

// currentVersion reads the applied schema version, 0 for a file that has never
// been migrated (which includes an empty file that was just created).
func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// migrate walks a ladder from wherever the file is to its top. It is
// idempotent: on an up-to-date file it runs one SELECT and returns.
//
// The ladder is a parameter rather than a reference to the package variable so
// that a test can walk one with a second step on it, which is the only way to
// show — before v2 exists — that v2 lands as a migration and not as a wipe.
func migrate(ctx context.Context, db *sql.DB, now time.Time, ladder []migration) (from, to int, err error) {
	top := 0
	for _, m := range ladder {
		if m.version > top {
			top = m.version
		}
	}
	from, err = currentVersion(ctx, db)
	if err != nil {
		return 0, 0, fmt.Errorf("archive: reading schema version: %w", err)
	}
	if from > top {
		return from, from, fmt.Errorf("%w (file is v%d, this build speaks v%d)", ErrSchemaTooNew, from, top)
	}
	to = from
	for _, m := range ladder {
		if m.version <= from {
			continue
		}
		if err := applyMigration(ctx, db, m, now); err != nil {
			return from, to, fmt.Errorf("archive: migrating to v%d: %w", m.version, err)
		}
		to = m.version
	}
	return from, to, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op
	for _, stmt := range m.stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w; statement: %s", err, firstLine(stmt))
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_version(version, applied_at) VALUES(?, ?)`,
		m.version, now.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
