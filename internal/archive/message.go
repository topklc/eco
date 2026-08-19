package archive

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// Message is one row of the messages table.
//
// The archive is per network profile, so a message is keyed by the *folded*
// buffer name alone - model.BufferID's network half is the file it lives in.
// Time is stored to the millisecond (the schema's INTEGER ts), which is the
// resolution IRCv3 server-time carries anyway.
type Message struct {
	// RowID is set by reads and ignored by writes. It is the pagination
	// anchor and the identity a search hit hands back so the UI can jump.
	RowID int64

	// MsgID is the IRCv3 msgid. Empty means "this message has none": it is
	// stored as NULL, and such a row cannot be deduped (see the package doc).
	MsgID string

	Buffer  string // folded name, exactly as state keys it
	Time    time.Time
	Sender  string // display case, as the server sent it
	Account string

	Kind model.MessageKind
	Body string

	Tags map[string]string // stored as tags_json; nil and empty are the same thing

	Pending  model.PendingState
	Redacted bool
}

// FromModel projects the store's message onto an archive row. It is the one
// place the two shapes meet, so the wiring stage has nothing to decide - down
// to the status buffer's name, which is "" upstream and ServerBufferName here.
func FromModel(m model.Message) Message {
	return Message{
		MsgID:    m.MsgID,
		Buffer:   FoldedName(m.Buffer),
		Time:     m.Time,
		Sender:   m.Sender.Nick,
		Account:  m.Sender.Account,
		Kind:     m.Kind,
		Body:     m.Text,
		Tags:     m.Tags,
		Pending:  m.Pending,
		Redacted: m.Redacted,
	}
}

// unixMilli is the ts column's value. A zero time stores as 0 rather than as
// the year-1 nonsense UnixMilli would produce.
func (m Message) unixMilli() int64 {
	if m.Time.IsZero() {
		return 0
	}
	return m.Time.UnixMilli()
}

// msgidArg renders the msgid for binding: NULL when empty, so that two
// msgid-less messages do not collide on the UNIQUE index.
func (m Message) msgidArg() any {
	if m.MsgID == "" {
		return nil
	}
	return m.MsgID
}

// tagsArg renders Tags as JSON, or NULL when there are none. encoding/json
// sorts map keys, so the same tags always produce the same bytes.
func (m Message) tagsArg() (any, error) {
	if len(m.Tags) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m.Tags)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// messageColumns is the read projection, in scanMessage's order.
const messageColumns = `m.rowid, m.msgid, m.buffer_folded, m.ts, m.sender, m.account, m.kind, m.body, m.tags_json, m.pending, m.redacted`

// scanner is what sql.Row and sql.Rows have in common.
type scanner interface{ Scan(dest ...any) error }

func scanMessage(s scanner) (Message, error) {
	var (
		m       Message
		msgid   sql.NullString
		sender  sql.NullString
		account sql.NullString
		body    sql.NullString
		tags    sql.NullString
		ts      int64
		kind    int64
		pending int64
		red     int64
	)
	if err := s.Scan(&m.RowID, &msgid, &m.Buffer, &ts, &sender, &account, &kind, &body, &tags, &pending, &red); err != nil {
		return Message{}, err
	}
	m.MsgID = msgid.String
	m.Sender = sender.String
	m.Account = account.String
	m.Body = body.String
	m.Kind = model.MessageKind(kind)
	m.Pending = model.PendingState(pending)
	m.Redacted = red != 0
	if ts != 0 {
		m.Time = time.UnixMilli(ts).UTC()
	}
	if tags.Valid && tags.String != "" {
		if err := json.Unmarshal([]byte(tags.String), &m.Tags); err != nil {
			// A corrupt tags blob is not worth losing the message over: the
			// text is what the user came for.
			m.Tags = nil
		}
	}
	return m, nil
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// reverse flips a newest-first slice into the oldest-first order every reader
// of this package gets.
func reverse(ms []Message) []Message {
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
	return ms
}
