package model

import "time"

// MessageKind is what a message *is*, independent of how it renders. It maps
// 1:1 onto the archive's messages.kind column (blueprint §5.4), so the numeric
// values are part of the on-disk format: append, never renumber.
type MessageKind uint8

const (
	MsgPrivmsg   MessageKind = 0 // ordinary channel or query message
	MsgNotice    MessageKind = 1 // NOTICE
	MsgAction    MessageKind = 2 // CTCP ACTION (/me), Text has the ACTION wrapper stripped
	MsgStatus    MessageKind = 3 // client-generated: joins, parts, errors, numerics
	MsgTombstone MessageKind = 4 // what a redacted message leaves behind
)

// String returns the archive/debug name of the kind.
func (k MessageKind) String() string {
	switch k {
	case MsgPrivmsg:
		return "privmsg"
	case MsgNotice:
		return "notice"
	case MsgAction:
		return "action"
	case MsgStatus:
		return "status"
	case MsgTombstone:
		return "tombstone"
	}
	return "unknown"
}

// PendingState tracks a locally echoed message on its way to being confirmed.
// v1 matched outgoing text against the server echo (session.go pendingEcho);
// v2 attaches a label, renders the message immediately as PendingEcho, and
// resolves it in place when the labeled echo arrives with the real msgid and
// server time. The zero value is Delivered, which is correct for every
// inbound message.
type PendingState uint8

const (
	Delivered     PendingState = 0 // confirmed by the server, or inbound
	PendingEcho   PendingState = 1 // written to the wire, awaiting its labeled echo
	PendingFailed PendingState = 2 // the server answered the label with a FAIL
)

// String returns the debug name of the state.
func (p PendingState) String() string {
	switch p {
	case Delivered:
		return "delivered"
	case PendingEcho:
		return "pending"
	case PendingFailed:
		return "failed"
	}
	return "unknown"
}

// Sender identifies who produced a message or event. A zero Sender means the
// server itself spoke (numerics, ERROR, connection notices).
type Sender struct {
	Nick    string // display case exactly as the server sent it
	User    string
	Host    string
	Account string // account-tag / extended-join, "" when unknown or logged out
	Self    bool   // this is us, on this or another device of the same account
}

// IsZero reports whether the sender is the server rather than a user.
func (s Sender) IsZero() bool { return s.Nick == "" && s.User == "" && s.Host == "" }

// Message is the store's protocol-level record of one message. It is *not* a
// render-ready row: the UI projects it through internal/render (see Line).
type Message struct {
	ID     MessageID // stable local identity, assigned on arrival, never reused
	MsgID  string    // IRCv3 msgid; "" until a PendingEcho message resolves
	Label  string    // labeled-response correlation for our own sends
	Buffer BufferID

	Time       time.Time // server-time when the server sent one, else the client clock
	ServerTime bool      // true when Time came from a server-time tag

	Sender Sender
	Kind   MessageKind
	Text   string

	Tags map[string]string // message tags, read-only for consumers

	Pending   PendingState
	Highlight bool   // your nick was mentioned, or this is a query addressed to you
	Playback  bool   // arrived inside a chathistory batch, not live
	Redacted  bool   // a REDACT landed on it; Kind is then MsgTombstone
	Batch     string // enclosing batch ref, "" outside a batch
}

// Member is one occupant of a channel.
type Member struct {
	Nick     string // display case as the server gave it
	Folded   string // Fold(Nick) - the members map key, never Nick itself
	Prefixes string // rank-ordered prefix chars from PREFIX, e.g. "@+"
	Account  string
	RealName string
	Away     bool
}

// BufferKind distinguishes the three kinds of buffer. Like MessageKind these
// values reach the archive, so they are append-only.
type BufferKind uint8

const (
	BufferServer  BufferKind = 0 // the network's status buffer
	BufferChannel BufferKind = 1 // name starts with a CHANTYPES char
	BufferQuery   BufferKind = 2 // a private conversation with one nick
)

// String returns the archive/debug name of the kind.
func (k BufferKind) String() string {
	switch k {
	case BufferServer:
		return "server"
	case BufferChannel:
		return "channel"
	case BufferQuery:
		return "query"
	}
	return "unknown"
}

// BufferInfo is a buffer's identity and display metadata: everything the UI
// needs to put a row in the rail without asking the store a second question.
type BufferInfo struct {
	ID   BufferID
	Name string // display name, server-cased; may change, so it is data not a key
	Kind BufferKind
}

// ChannelInfo is one LIST result row.
type ChannelInfo struct {
	Name  string
	Users int
	Topic string
}

// TypingState is the draft/typing (TAGMSG +typing) value.
type TypingState uint8

const (
	TypingDone   TypingState = 0 // stopped typing / sent
	TypingActive TypingState = 1
	TypingPaused TypingState = 2
)

// String returns the wire spelling of the typing state.
func (t TypingState) String() string {
	switch t {
	case TypingActive:
		return "active"
	case TypingPaused:
		return "paused"
	case TypingDone:
		return "done"
	}
	return "unknown"
}

// ConnStatus is the connection state machine shared by irc, state and the UI.
type ConnStatus uint8

const (
	ConnDisconnected   ConnStatus = 0 // not connected; RetryIn says whether we will try again
	ConnConnecting     ConnStatus = 1 // dialing / TLS handshake
	ConnRegistering    ConnStatus = 2 // CAP negotiation, NICK/USER sent, pre-001
	ConnAuthenticating ConnStatus = 3 // SASL in flight; autojoin waits for this to settle
	ConnReady          ConnStatus = 4 // registered and authenticated; safe to JOIN
	ConnFailed         ConnStatus = 5 // terminal for this attempt; Err says why
)

// String returns the debug name of the status.
func (c ConnStatus) String() string {
	switch c {
	case ConnDisconnected:
		return "disconnected"
	case ConnConnecting:
		return "connecting"
	case ConnRegistering:
		return "registering"
	case ConnAuthenticating:
		return "authenticating"
	case ConnReady:
		return "ready"
	case ConnFailed:
		return "failed"
	}
	return "unknown"
}

// HistoryDirection says which way a CHATHISTORY request walked.
type HistoryDirection uint8

const (
	HistoryLatest HistoryDirection = 0 // CHATHISTORY LATEST
	HistoryBefore HistoryDirection = 1 // scroll-up backfill
	HistoryAfter  HistoryDirection = 2 // reconnect gap-fill
	HistoryAround HistoryDirection = 3 // jump-to-message context
)

// String returns the CHATHISTORY subcommand name.
func (d HistoryDirection) String() string {
	switch d {
	case HistoryLatest:
		return "latest"
	case HistoryBefore:
		return "before"
	case HistoryAfter:
		return "after"
	case HistoryAround:
		return "around"
	}
	return "unknown"
}
