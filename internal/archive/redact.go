package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// Redact removes a message's content and leaves a tombstone in its place
// (ADR-3). It reports whether there was anything to redact.
//
// What "removes" means here, precisely:
//
//   - The row is DELETEd, which fires the FTS trigger, so the body leaves the
//     search index as well as the table. Nothing else in this package can
//     achieve that: an UPDATE to an empty body would leave the old tokens in
//     an external-content index pointing at a row that no longer contains
//     them, and the words would still be findable.
//   - A tombstone is inserted at the same rowid inside the same transaction,
//     carrying the original msgid, buffer, timestamp and sender but no body.
//     Same rowid means every pagination anchor that pointed at the message
//     still lands in the right place, and the conversation keeps its shape.
//   - Keeping the msgid is what makes redaction survive replay: when the
//     server sends that message again in a CHATHISTORY batch, the Recorder's
//     INSERT OR IGNORE collapses onto the tombstone instead of resurrecting
//     the text.
//
// by and reason are kept in the tombstone's tags (redact-by, redact-reason) so
// the UI can say who deleted it. Pass "" for either to omit it.
func (d *DB) Redact(ctx context.Context, msgID, by, reason string, at time.Time) (bool, error) {
	if msgID == "" {
		return false, errors.New("archive: Redact with an empty msgid")
	}
	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("archive: redact: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	var (
		rowid  int64
		buffer string
		ts     int64
		sender sql.NullString
		acct   sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT rowid, buffer_folded, ts, sender, account FROM messages WHERE msgid = ?`, msgID).
		Scan(&rowid, &buffer, &ts, &sender, &acct)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("archive: redact %q: %w", msgID, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE rowid = ?`, rowid); err != nil {
		return false, fmt.Errorf("archive: redact %q: %w", msgID, err)
	}

	tomb := Message{
		MsgID:    msgID,
		Buffer:   buffer,
		Sender:   sender.String,
		Account:  acct.String,
		Kind:     model.MsgTombstone,
		Redacted: true,
	}
	if !at.IsZero() {
		tomb.Tags = map[string]string{"redact-at": at.UTC().Format(time.RFC3339)}
	}
	if by != "" {
		tomb.Tags = withTag(tomb.Tags, "redact-by", by)
	}
	if reason != "" {
		tomb.Tags = withTag(tomb.Tags, "redact-reason", reason)
	}
	tags, err := tomb.tagsArg()
	if err != nil {
		tags = nil
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO messages(rowid, msgid, buffer_folded, ts, sender, account, kind, body, tags_json, pending, redacted)
VALUES(?, ?, ?, ?, ?, ?, ?, '', ?, 0, 1)`,
		rowid, msgID, buffer, ts, tomb.Sender, tomb.Account, int64(tomb.Kind), tags); err != nil {
		return false, fmt.Errorf("archive: redact %q: %w", msgID, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("archive: redact %q: %w", msgID, err)
	}
	return true, nil
}

func withTag(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

// Forget deletes a buffer's messages outright — no tombstones, nothing left in
// the index. This is the "delete this conversation" button, not redaction: it
// is what a user asking the archive to forget a buffer means, and the buffer's
// own settings row (logging, marker) survives so that "stop logging and erase
// what you have" is two calls with no gap between them.
//
// It returns the number of messages removed.
func (d *DB) Forget(ctx context.Context, folded string) (int64, error) {
	if folded == "" {
		return 0, errors.New("archive: Forget with an empty buffer name")
	}
	res, err := d.w.ExecContext(ctx, `DELETE FROM messages WHERE buffer_folded = ?`, folded)
	if err != nil {
		return 0, fmt.Errorf("archive: forget %q: %w", folded, err)
	}
	n, _ := res.RowsAffected()
	if _, err := d.w.ExecContext(ctx, `UPDATE buffers SET last_msgid = NULL WHERE name_folded = ?`, folded); err != nil {
		return n, fmt.Errorf("archive: forget %q: %w", folded, err)
	}
	return n, nil
}
