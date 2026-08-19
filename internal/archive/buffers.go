package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// KindUnknown marks a buffers row that the archive created for itself, because
// a message arrived for a buffer nothing had described yet. EnsureBuffer
// replaces it with the real kind the moment the app names one.
const KindUnknown = model.BufferKind(255)

// ServerBufferName is the key a network's status buffer is filed under.
//
// The store names that buffer "" (model.ServerBuffer), which cannot be a key
// here: "" is also what a caller who forgot to name a buffer passes, and the
// archive refuses that rather than filing MOTD under the same name as a bug.
// '*' is safe as the substitute - it is not a legal nick character and not a
// legal channel name - and it is what servers themselves use for "no target".
const ServerBufferName = "*"

// FoldedName maps a buffer id onto the archive's key space: the folded name,
// with the status buffer's empty name replaced by ServerBufferName. The
// network half of the id is the file the archive lives in, so it does not
// appear in the key.
func FoldedName(id model.BufferID) string {
	if id.Folded == "" {
		return ServerBufferName
	}
	return id.Folded
}

// BufferMeta is one row of the buffers table: a buffer's identity plus the
// settings that have to outlive the process.
type BufferMeta struct {
	Name    string // folded name - the key, never the display name
	Display string
	Kind    model.BufferKind
	Key     string // channel key (+k), so a rejoin does not have to ask

	LastMsgID string // newest msgid stored for this buffer; the CHATHISTORY AFTER anchor
	Marker    Marker // read marker (blueprint §4.4)

	// NotifyLevel is the wire value of state.NotifyLevel (0 default, 1 all,
	// 2 mentions, 3 mute). It is a number here so that storage does not
	// depend on the reducer; the values are append-only either way.
	NotifyLevel uint8

	Logging bool // false means this buffer is never written to disk
	Pinned  bool
}

// Marker is a read marker: where the user has read up to in a buffer.
// The msgid is the precise answer and the timestamp is the portable one, so
// both are kept - a server that only speaks timestamps still round-trips.
type Marker struct {
	MsgID string
	At    time.Time
}

// IsZero reports whether the marker points at nothing.
func (m Marker) IsZero() bool { return m.MsgID == "" && m.At.IsZero() }

// Equal compares two markers the way the column does: to the millisecond, and
// without caring about the location or the monotonic reading a caller's clock
// may have attached.
func (m Marker) Equal(o Marker) bool {
	return m.MsgID == o.MsgID && m.At.UnixMilli() == o.At.UnixMilli() && m.At.IsZero() == o.At.IsZero()
}

// normalize rounds a marker to what the ts column can hold, so a round trip
// through the disk returns the same value the caller was handed.
func (m Marker) normalize() Marker {
	if !m.At.IsZero() {
		m.At = time.UnixMilli(m.At.UnixMilli()).UTC()
	}
	return m
}

// EnsureBuffer creates or updates a buffer's identity, leaving every setting
// (logging, notify level, marker, pin) alone. Call it when a buffer opens.
//
// Kind is written only when it can be true. KindUnknown always means "leave
// what is stored alone", and so does a zero-valued Kind on anything but
// ServerBufferName: model.BufferServer is the zero value of its type, and only
// the status buffer can be a status buffer, so a caller that filled in a name
// and a display but not a kind cannot accidentally relabel a channel.
func (d *DB) EnsureBuffer(ctx context.Context, m BufferMeta) error {
	if m.Name == "" {
		return errors.New("archive: EnsureBuffer with an empty folded name")
	}
	display := m.Display
	if display == "" {
		display = m.Name
	}
	var kind any
	if m.Kind != KindUnknown && (m.Kind != model.BufferServer || m.Name == ServerBufferName) {
		kind = int64(m.Kind)
	}
	var key any
	if m.Key != "" {
		key = m.Key
	}
	_, err := d.w.ExecContext(ctx, `
INSERT INTO buffers(name_folded, display_name, kind, key, logging)
VALUES(?, ?, ?, ?, 1)
ON CONFLICT(name_folded) DO UPDATE SET
  display_name = excluded.display_name,
  kind         = COALESCE(excluded.kind, buffers.kind),
  key          = COALESCE(excluded.key, buffers.key)`,
		m.Name, display, kind, key)
	if err != nil {
		return fmt.Errorf("archive: ensure buffer %q: %w", m.Name, err)
	}
	return nil
}

const bufferColumns = `name_folded, display_name, kind, key, last_msgid, read_marker_msgid, read_marker_ts, notify_level, logging, pinned`

func scanBuffer(s scanner) (BufferMeta, error) {
	var (
		b        BufferMeta
		display  sql.NullString
		kind     sql.NullInt64
		key      sql.NullString
		last     sql.NullString
		markerID sql.NullString
		markerTS sql.NullInt64
		notify   sql.NullInt64
		logging  int64
		pinned   sql.NullInt64
	)
	if err := s.Scan(&b.Name, &display, &kind, &key, &last, &markerID, &markerTS, &notify, &logging, &pinned); err != nil {
		return BufferMeta{}, err
	}
	b.Display = display.String
	if b.Display == "" {
		b.Display = b.Name
	}
	b.Kind = KindUnknown
	if kind.Valid {
		b.Kind = model.BufferKind(kind.Int64)
	}
	b.Key = key.String
	b.LastMsgID = last.String
	b.Marker = Marker{MsgID: markerID.String}
	if markerTS.Valid && markerTS.Int64 != 0 {
		b.Marker.At = time.UnixMilli(markerTS.Int64).UTC()
	}
	b.NotifyLevel = uint8(notify.Int64)
	b.Logging = logging != 0
	b.Pinned = pinned.Valid && pinned.Int64 != 0
	return b, nil
}

// Buffer reads one buffer's row. ok is false when the archive has never heard
// of the buffer, which is not an error: it means "all defaults".
func (d *DB) Buffer(ctx context.Context, folded string) (BufferMeta, bool, error) {
	row := d.r.QueryRowContext(ctx, `SELECT `+bufferColumns+` FROM buffers WHERE name_folded = ?`, folded)
	b, err := scanBuffer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return BufferMeta{}, false, nil
	}
	if err != nil {
		return BufferMeta{}, false, fmt.Errorf("archive: read buffer %q: %w", folded, err)
	}
	return b, true, nil
}

// Buffers reads every buffer row, ordered by folded name so the result is
// stable for tests and for the rail.
func (d *DB) Buffers(ctx context.Context) ([]BufferMeta, error) {
	rows, err := d.r.QueryContext(ctx, `SELECT `+bufferColumns+` FROM buffers ORDER BY name_folded`)
	if err != nil {
		return nil, fmt.Errorf("archive: list buffers: %w", err)
	}
	defer rows.Close()
	var out []BufferMeta
	for rows.Next() {
		b, err := scanBuffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetLogging turns disk logging for one buffer on or off. Off is enforced at
// the Recorder, so a buffer with logging off never reaches a statement - and
// every Recorder currently writing to this archive is told before this
// returns, so the very next message honours it.
//
// Turning logging off does not delete what is already stored; that is Forget's
// job, and the two are separate because "stop writing" and "erase" are
// different requests.
func (d *DB) SetLogging(ctx context.Context, folded string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	if !on {
		// Tell the Recorders first when switching off: between the statement
		// and the notification, the wrong order would still be writing.
		d.tellRecordersLogging(folded, on)
	}
	_, err := d.w.ExecContext(ctx, `
INSERT INTO buffers(name_folded, display_name, logging) VALUES(?, ?, ?)
ON CONFLICT(name_folded) DO UPDATE SET logging = excluded.logging`, folded, folded, v)
	if err != nil {
		return fmt.Errorf("archive: set logging %q: %w", folded, err)
	}
	if on {
		d.tellRecordersLogging(folded, on)
	}
	return nil
}

// SetNotifyLevel stores a buffer's notification level (see BufferMeta).
func (d *DB) SetNotifyLevel(ctx context.Context, folded string, level uint8) error {
	_, err := d.w.ExecContext(ctx, `
INSERT INTO buffers(name_folded, display_name, notify_level) VALUES(?, ?, ?)
ON CONFLICT(name_folded) DO UPDATE SET notify_level = excluded.notify_level`, folded, folded, int64(level))
	if err != nil {
		return fmt.Errorf("archive: set notify level %q: %w", folded, err)
	}
	return nil
}

// SetPinned pins or unpins a buffer in the rail.
func (d *DB) SetPinned(ctx context.Context, folded string, pinned bool) error {
	v := 0
	if pinned {
		v = 1
	}
	_, err := d.w.ExecContext(ctx, `
INSERT INTO buffers(name_folded, display_name, pinned) VALUES(?, ?, ?)
ON CONFLICT(name_folded) DO UPDATE SET pinned = excluded.pinned`, folded, folded, v)
	if err != nil {
		return fmt.Errorf("archive: set pinned %q: %w", folded, err)
	}
	return nil
}

// LoggingOff returns the set of buffers with logging turned off. The Recorder
// loads it once at start and keeps it current from SetLogging; nothing else
// needs it.
func (d *DB) LoggingOff(ctx context.Context) (map[string]bool, error) {
	rows, err := d.r.QueryContext(ctx, `SELECT name_folded FROM buffers WHERE logging = 0`)
	if err != nil {
		return nil, fmt.Errorf("archive: read logging opt-outs: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// --- read markers ------------------------------------------------------------

// ReadMarker returns a buffer's read marker. A buffer with no marker returns
// the zero Marker, which means "nothing has been read".
func (d *DB) ReadMarker(ctx context.Context, folded string) (Marker, error) {
	var (
		id sql.NullString
		ts sql.NullInt64
	)
	err := d.r.QueryRowContext(ctx,
		`SELECT read_marker_msgid, read_marker_ts FROM buffers WHERE name_folded = ?`, folded).Scan(&id, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return Marker{}, nil
	}
	if err != nil {
		return Marker{}, fmt.Errorf("archive: read marker %q: %w", folded, err)
	}
	m := Marker{MsgID: id.String}
	if ts.Valid && ts.Int64 != 0 {
		m.At = time.UnixMilli(ts.Int64).UTC()
	}
	return m, nil
}

// SetReadMarker merges a marker into the stored one and returns the result
// with moved reporting whether anything changed.
//
// Conflict resolution is max(server, local) (ADR-3): markers only move
// forward, so a MARKREAD from a device that was behind - or a late local
// focus event - cannot un-read a buffer. Precisely:
//
//   - a strictly later timestamp wins, and takes its msgid with it;
//   - an equal timestamp keeps the stored position but fills in a msgid the
//     stored marker was missing, because a msgid is the sharper answer;
//   - an earlier timestamp changes nothing;
//   - a marker with no timestamp at all only ever fills in a missing msgid.
//
// This is deliberately the same rule the store applies in memory
// (state.buffer.setMarker), so the two cannot disagree about who is ahead.
func (d *DB) SetReadMarker(ctx context.Context, folded string, in Marker) (out Marker, moved bool, err error) {
	if folded == "" {
		return Marker{}, false, errors.New("archive: SetReadMarker with an empty folded name")
	}
	if in.IsZero() {
		cur, err := d.ReadMarker(ctx, folded)
		return cur, false, err
	}
	in = in.normalize()
	cur, err := d.ReadMarker(ctx, folded)
	if err != nil {
		return Marker{}, false, err
	}
	out = mergeMarker(cur, in)
	if out.Equal(cur) {
		// Still ensure the row exists, so a marker set on a buffer the
		// archive has never seen is not silently dropped.
		if cur.IsZero() {
			if err := d.writeMarker(ctx, folded, out); err != nil {
				return cur, false, err
			}
		}
		return cur, false, nil
	}
	if err := d.writeMarker(ctx, folded, out); err != nil {
		return cur, false, err
	}
	return out, true, nil
}

// mergeMarker is SetReadMarker's rule, extracted so a table can pin it.
func mergeMarker(cur, in Marker) Marker {
	if in.IsZero() {
		return cur
	}
	if cur.IsZero() {
		return in
	}
	switch {
	case in.At.IsZero():
		// No timestamp to compare: only ever add information.
		if cur.MsgID == "" && in.MsgID != "" {
			cur.MsgID = in.MsgID
		}
		return cur
	case cur.At.IsZero(), in.At.After(cur.At):
		return in
	case in.At.Equal(cur.At):
		if cur.MsgID == "" && in.MsgID != "" {
			cur.MsgID = in.MsgID
		}
		return cur
	default: // in.At.Before(cur.At)
		return cur
	}
}

func (d *DB) writeMarker(ctx context.Context, folded string, m Marker) error {
	var id any
	if m.MsgID != "" {
		id = m.MsgID
	}
	var ts any
	if !m.At.IsZero() {
		ts = m.At.UnixMilli()
	}
	_, err := d.w.ExecContext(ctx, `
INSERT INTO buffers(name_folded, display_name, read_marker_msgid, read_marker_ts)
VALUES(?, ?, ?, ?)
ON CONFLICT(name_folded) DO UPDATE SET
  read_marker_msgid = excluded.read_marker_msgid,
  read_marker_ts    = excluded.read_marker_ts`, folded, folded, id, ts)
	if err != nil {
		return fmt.Errorf("archive: write marker %q: %w", folded, err)
	}
	return nil
}

// LastMsgID is the newest msgid stored for a buffer: the anchor a reconnect
// hands to CHATHISTORY AFTER. It reads the buffers row the Recorder maintains,
// falling back to the messages index when nothing has written one yet.
func (d *DB) LastMsgID(ctx context.Context, folded string) (string, error) {
	var last sql.NullString
	err := d.r.QueryRowContext(ctx, `SELECT last_msgid FROM buffers WHERE name_folded = ?`, folded).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("archive: last msgid %q: %w", folded, err)
	}
	if last.Valid && last.String != "" {
		return last.String, nil
	}
	var fromMsgs sql.NullString
	err = d.r.QueryRowContext(ctx, `
SELECT msgid FROM messages
WHERE buffer_folded = ? AND msgid IS NOT NULL
ORDER BY ts DESC, rowid DESC LIMIT 1`, folded).Scan(&fromMsgs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("archive: last msgid %q: %w", folded, err)
	}
	return fromMsgs.String, nil
}
