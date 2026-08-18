package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DefaultPageLimit is the page size Latest and Page use when asked for none.
const DefaultPageLimit = 100

// maxPageLimit caps a page. A caller asking for a million rows wants a search,
// not a page, and the timeline cannot show them anyway.
const maxPageLimit = 5000

// PageQuery asks for the messages immediately before an anchor: what scroll-up
// needs. The anchor is whichever of the two is set, BeforeRowID winning when
// both are — it is the cheaper answer and the one a previous page handed back.
//
// With no anchor at all the page is the newest one, which makes
// Page(ctx, PageQuery{Buffer: b}) and Latest(ctx, b, 0) the same request.
type PageQuery struct {
	Buffer      string // folded name
	BeforeRowID int64
	BeforeMsgID string
	Limit       int

	// ExcludeRedacted drops tombstones from the page. They are included by
	// default: a hole where a message was reads worse than "this was
	// deleted", and the body is gone from the row either way.
	ExcludeRedacted bool
}

func pageLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultPageLimit
	case n > maxPageLimit:
		return maxPageLimit
	}
	return n
}

// Latest returns the newest limit messages in a buffer, oldest first: what
// opening a buffer shows.
func (d *DB) Latest(ctx context.Context, folded string, limit int) ([]Message, error) {
	return d.Page(ctx, PageQuery{Buffer: folded, Limit: limit})
}

// Page returns up to Limit messages ending just before the anchor, ordered
// oldest first — the order they go on screen, so the caller prepends the slice
// as it is.
//
// Ordering and stability. Rows are ordered by (ts, rowid), never by ts alone.
// Two messages sharing a millisecond (a netsplit's worth of JOINs, a bridged
// relay, a server with second-resolution time) therefore have a total order
// that does not change between calls, and paging cannot skip or repeat one of
// them at a page boundary. That pair is exactly the index
// idx_messages_buffer_ts(buffer_folded, ts, rowid), so a page is a seek plus a
// walk of Limit rows however deep in history it is.
func (d *DB) Page(ctx context.Context, q PageQuery) ([]Message, error) {
	if q.Buffer == "" {
		return nil, errors.New("archive: Page with an empty buffer name")
	}
	limit := pageLimit(q.Limit)

	anchorTS, anchorRow, anchored, err := d.anchor(ctx, q)
	if err != nil {
		return nil, err
	}

	where := `m.buffer_folded = ?`
	args := []any{q.Buffer}
	if anchored {
		where += ` AND (m.ts < ? OR (m.ts = ? AND m.rowid < ?))`
		args = append(args, anchorTS, anchorTS, anchorRow)
	}
	if q.ExcludeRedacted {
		where += ` AND m.redacted = 0`
	}
	args = append(args, limit)

	rows, err := d.r.QueryContext(ctx, `
SELECT `+messageColumns+` FROM messages m
WHERE `+where+`
ORDER BY m.ts DESC, m.rowid DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("archive: page %q: %w", q.Buffer, err)
	}
	out, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("archive: page %q: %w", q.Buffer, err)
	}
	return reverse(out), nil
}

// anchor resolves a PageQuery's anchor to the (ts, rowid) pair the ordering is
// defined on. An unknown BeforeMsgID is not an error: the message is not in
// this archive (it was redacted, or it predates retention), and the honest
// answer is the newest page rather than an empty one.
func (d *DB) anchor(ctx context.Context, q PageQuery) (ts, rowid int64, ok bool, err error) {
	switch {
	case q.BeforeRowID > 0:
		err = d.r.QueryRowContext(ctx, `SELECT ts, rowid FROM messages WHERE rowid = ?`, q.BeforeRowID).Scan(&ts, &rowid)
	case q.BeforeMsgID != "":
		err = d.r.QueryRowContext(ctx, `SELECT ts, rowid FROM messages WHERE msgid = ?`, q.BeforeMsgID).Scan(&ts, &rowid)
	default:
		return 0, 0, false, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("archive: resolving page anchor: %w", err)
	}
	return ts, rowid, true, nil
}

// Around returns the context of one message: up to before messages older than
// it, the message itself, and up to after messages newer. It is what a search
// hit needs to become a place in the timeline.
func (d *DB) Around(ctx context.Context, rowID int64, before, after int) ([]Message, error) {
	var (
		ts     int64
		buffer string
	)
	err := d.r.QueryRowContext(ctx, `SELECT ts, buffer_folded FROM messages WHERE rowid = ?`, rowID).Scan(&ts, &buffer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("archive: around %d: %w", rowID, err)
	}

	older, err := d.Page(ctx, PageQuery{Buffer: buffer, BeforeRowID: rowID, Limit: before})
	if err != nil {
		return nil, err
	}
	rows, err := d.r.QueryContext(ctx, `
SELECT `+messageColumns+` FROM messages m
WHERE m.buffer_folded = ? AND (m.ts > ? OR (m.ts = ? AND m.rowid >= ?))
ORDER BY m.ts, m.rowid
LIMIT ?`, buffer, ts, ts, rowID, pageLimit(after)+1)
	if err != nil {
		return nil, fmt.Errorf("archive: around %d: %w", rowID, err)
	}
	newer, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("archive: around %d: %w", rowID, err)
	}
	return append(older, newer...), nil
}

// Count returns how many messages a buffer holds. "" counts the whole archive.
func (d *DB) Count(ctx context.Context, folded string) (int64, error) {
	var n int64
	var err error
	if folded == "" {
		err = d.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&n)
	} else {
		err = d.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE buffer_folded = ?`, folded).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("archive: count: %w", err)
	}
	return n, nil
}
