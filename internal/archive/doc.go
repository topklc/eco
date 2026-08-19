// Package archive is the client's memory on disk: one SQLite database per
// network profile holding every message, the per-buffer settings and read
// markers, and the LIST cache, so history and unread survive a restart.
//
// THREADING: an open DB is safe for concurrent use. Writes are serialised onto
// one connection (WAL allows one writer), reads come from a small pool, and the
// Recorder owns a goroutine of its own. Nothing here touches fyne, the network,
// a package-level variable or the wall clock it was not given (Options.Clock).
//
// THE SHAPE
//
//	live path:     state → Recorder.Record → (batch, write-behind) → messages
//	history path:  Latest / Page  → the (buffer_folded, ts, rowid) index
//	search path:   Search         → messages_fts (FTS5, external content)
//	markers:       ReadMarker / SetReadMarker, max(server, local)
//
// Record never blocks the caller and never fails loudly: it hands the message
// to a bounded queue and returns. What that costs is stated plainly - when the
// queue is full the message is dropped and counted in Stats.Dropped, because a
// chat client that stalls its UI to write a log line is the worse bug. Close
// flushes: every message Record accepted before Close is on disk when Close
// returns.
//
// # WHAT IS AUTHORITATIVE
//
// Dedupe collapses on msgid (ADR-3): the same msgid never occupies two rows,
// so replayed CHATHISTORY, a reconnect gap-fill and the live line are one
// message. A message with no msgid (a status line, or any message from a
// server that does not speak IRCv3) is stored with a NULL msgid and cannot be
// deduped - SQLite treats NULLs as distinct, which is exactly the wanted
// behaviour, and is why the empty string is never written to that column.
//
// A locally echoed message is *not* recorded until it resolves. It has no
// msgid until the labeled echo lands, so recording it early would write a row
// the echo could not collapse onto. The pending column still exists and is
// written, because a send that failed outright (model.PendingFailed) is worth
// keeping.
//
// PRIVACY (the contract ships with the archive, not after it)
//
//   - Per-buffer logging opt-out is honoured at the Recorder: a buffer with
//     logging off never reaches a statement, let alone the disk.
//   - Redact deletes the row and its FTS entry and leaves a tombstone at the
//     same rowid, so the conversation still reads and the pagination anchors
//     that pointed at it still work. The body is gone from both tables.
//   - Retention is forever / 365 / 90 / 30 days / session-only, with a vacuum
//     path that actually returns the pages to the filesystem.
//   - There is no encryption at rest in v2.0. That is a decision, not an
//     oversight (blueprint §5.6): the file is readable by anything running as
//     the user, and the shared-machine answer is the logging opt-out and the
//     session-only retention policy.
//
// # SCHEMA
//
// The tables are blueprint §5.4 verbatim; see schema.go. Migrations run from
// version 0 (an empty file) upward and record each step in schema_version, so
// the v2 schema change lands as another migration rather than a wipe. A file
// written by a newer eco is refused rather than downgraded.
package archive
