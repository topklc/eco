package model

import "time"

// Change is what the store produces for the UI: one typed fact about how the
// store's state just moved. state.Store.Apply(Event) returns []Change on the
// dispatcher goroutine; ui.Dispatcher coalesces them and applies them in one
// fyne.Do; the archive Recorder consumes the same slice.
//
// Changes are self-contained. A consumer never has to call back into the store
// to interpret one, because the store is single-writer and a callback would
// read a state that has already moved on.
//
// See the package doc for why this union is an interface of concrete structs.
type Change interface {
	// Meta returns the fields every change carries.
	Meta() ChangeMeta
	// ChangeName is a stable lowercase label for logs, test failures and the
	// transcript suite's records. It also seals the union.
	ChangeName() string
}

// ChangeMeta is the envelope every Change carries.
//
// Invariant: Network is always set. Buffer is set for buffer-scoped changes and
// zero for network-scoped ones (connection state, auth, LIST). Use BufMeta and
// NetMeta to build it rather than a struct literal, so the two can never
// disagree.
type ChangeMeta struct {
	Network NetworkID
	Buffer  BufferID
}

// BufMeta builds the envelope for a buffer-scoped change.
func BufMeta(b BufferID) ChangeMeta { return ChangeMeta{Network: b.Network, Buffer: b} }

// NetMeta builds the envelope for a network-scoped change.
func NetMeta(n NetworkID) ChangeMeta { return ChangeMeta{Network: n} }

// Meta implements Change for every variant that embeds ChangeMeta.
func (m ChangeMeta) Meta() ChangeMeta { return m }

// Scoped reports whether the change is about one buffer rather than the whole
// network.
func (m ChangeMeta) Scoped() bool { return !m.Buffer.IsZero() }

// --- messages ---------------------------------------------------------------

// MessageAdded is a new message in a buffer, live or locally echoed. A message
// with Pending == PendingEcho renders immediately and is amended later by
// MessageResolved; the UI must key the row on Message.ID, never on its text.
type MessageAdded struct {
	ChangeMeta
	Message Message
	// Notify is true when this message should raise the user's attention
	// (highlight, or any message in a query). The store evaluates the rules;
	// the UI just obeys.
	Notify bool
}

// ChangeName implements Change.
func (MessageAdded) ChangeName() string { return "messageadded" }

// MessageResolved is a pending message confirmed by its labeled echo: it has
// gained a real msgid and the server's timestamp, and may have been re-ordered.
// ID identifies the row that already exists; Message is its new value.
type MessageResolved struct {
	ChangeMeta
	ID      MessageID
	Message Message
}

// ChangeName implements Change.
func (MessageResolved) ChangeName() string { return "messageresolved" }

// MessageFailed is a pending message the server rejected (a standard-replies
// FAIL carrying our label). The row stays, marked failed, so the text is not
// lost and can be retried.
type MessageFailed struct {
	ChangeMeta
	ID     MessageID
	Code   string
	Reason string
}

// ChangeName implements Change.
func (MessageFailed) ChangeName() string { return "messagefailed" }

// MessageRedacted is a message withdrawn by REDACT. The store leaves a
// tombstone in place rather than a hole, so surrounding context still reads.
type MessageRedacted struct {
	ChangeMeta
	ID     MessageID
	MsgID  string
	ByNick string
	Reason string
}

// ChangeName implements Change.
func (MessageRedacted) ChangeName() string { return "messageredacted" }

// HistoryLoaded is a block of history that landed in a buffer — the Change
// counterpart of the HistoryChunk event (see the package doc's naming rule).
// Messages are in ascending time order regardless of Direction.
type HistoryLoaded struct {
	ChangeMeta
	Messages  []Message
	Direction HistoryDirection
	// Complete is false when more history exists beyond this block, which is
	// what keeps the scroll-up affordance alive.
	Complete bool
}

// ChangeName implements Change.
func (HistoryLoaded) ChangeName() string { return "historyloaded" }

// --- buffers ----------------------------------------------------------------

// BufferCreated is a buffer that now exists. It is emitted before any
// MessageAdded for that buffer, so the UI never has to create rows lazily.
type BufferCreated struct {
	ChangeMeta
	Info BufferInfo
	// Restored is true when the buffer came back from the archive or an
	// autojoin plan rather than from live traffic, so the UI can avoid
	// stealing focus for it.
	Restored bool
}

// ChangeName implements Change.
func (BufferCreated) ChangeName() string { return "buffercreated" }

// BufferClosed is a buffer that no longer exists.
type BufferClosed struct {
	ChangeMeta
}

// ChangeName implements Change.
func (BufferClosed) ChangeName() string { return "bufferclosed" }

// BufferRenamed reports that a buffer's identity moved: draft/channel-rename,
// or a server re-casing a name. From is the old id, Meta.Buffer is the new one,
// and every per-buffer thing the UI holds (draft text, unread, scroll anchor)
// must migrate with it.
type BufferRenamed struct {
	ChangeMeta
	From BufferID
	Name string // new display name
}

// ChangeName implements Change.
func (BufferRenamed) ChangeName() string { return "bufferrenamed" }

// MembersChanged is a member-list delta — never "go re-sort everything".
// Removed carries the full Member so the UI can render the nick that left
// without a lookup into state it no longer holds.
//
// Reset is the one non-delta case: a non-nil Reset means the list was rebuilt
// wholesale (RPL_ENDOFNAMES), and Added/Removed/Updated are then empty.
type MembersChanged struct {
	ChangeMeta
	Added   []Member
	Removed []Member
	Updated []Member
	Reset   []Member
	// Bulk is true when the delta came from a netsplit/netjoin batch, so the
	// UI can collapse a hundred quits into one line.
	Bulk bool
}

// ChangeName implements Change.
func (MembersChanged) ChangeName() string { return "memberschanged" }

// TopicChanged is a buffer's new topic. Cleared distinguishes "the topic was
// removed" from "the topic is empty because we have not learned it".
type TopicChanged struct {
	ChangeMeta
	Topic   string
	SetBy   string
	SetAt   time.Time
	Cleared bool
}

// ChangeName implements Change.
func (TopicChanged) ChangeName() string { return "topicchanged" }

// ModeChanged is a channel's new mode state after applying a delta. Modes is
// the paramless flag set in a stable order; Key and Limit are broken out
// because the client acts on them (rejoining +k channels, showing +l).
type ModeChanged struct {
	ChangeMeta
	Modes  string
	Key    string
	Limit  int
	ByNick string
}

// ChangeName implements Change.
func (ModeChanged) ChangeName() string { return "modechanged" }

// --- attention --------------------------------------------------------------

// UnreadChanged is a buffer's new unread counts, derived from the read marker
// rather than kept as a local tally, so it agrees with your other devices.
type UnreadChanged struct {
	ChangeMeta
	Unread     int
	Highlights int
}

// ChangeName implements Change.
func (UnreadChanged) ChangeName() string { return "unreadchanged" }

// ReadMarkerMoved is a buffer's read marker in its new position, from either a
// local focus event or a MARKREAD from another device (conflicts resolve to the
// later of the two — ADR-3).
type ReadMarkerMoved struct {
	ChangeMeta
	MsgID string
	At    time.Time
	// Remote is true when another device moved it.
	Remote bool
}

// ChangeName implements Change.
func (ReadMarkerMoved) ChangeName() string { return "readmarkermoved" }

// TypingChanged is one nick's typing state in one buffer. The store expires
// stale entries; the UI just renders what it is told.
type TypingChanged struct {
	ChangeMeta
	Nick  string
	State TypingState
}

// ChangeName implements Change.
func (TypingChanged) ChangeName() string { return "typingchanged" }

// --- network ----------------------------------------------------------------

// ConnStateChanged is the network's new connection state.
type ConnStateChanged struct {
	ChangeMeta
	Status  ConnStatus
	Nick    string
	Err     string
	Attempt int
	RetryIn time.Duration
}

// ChangeName implements Change.
func (ConnStateChanged) ChangeName() string { return "connstatechanged" }

// SelfChanged is our own identity on a network: nick, account, away state.
// The UI shows it and the store uses it for highlight matching.
type SelfChanged struct {
	ChangeMeta
	Nick    string
	Account string
	Away    bool
}

// ChangeName implements Change.
func (SelfChanged) ChangeName() string { return "selfchanged" }

// AuthStateChanged is the outcome of SASL for this network — the Change
// counterpart of the AuthResult event. It is a first-class change because a
// silent auth failure is exactly the v1 bug where SASLOptional degraded to an
// unauthenticated session with nothing but a log line.
type AuthStateChanged struct {
	ChangeMeta
	Success   bool
	Mechanism string
	Account   string
	Message   string
}

// ChangeName implements Change.
func (AuthStateChanged) ChangeName() string { return "authstatechanged" }

// ListUpdated is a batch of LIST results. Done marks RPL_LISTEND. Items are
// handed over in batches and not retained by the store: the archive's
// list_cache owns them.
type ListUpdated struct {
	ChangeMeta
	Items []ChannelInfo
	Done  bool
}

// ChangeName implements Change.
func (ListUpdated) ChangeName() string { return "listupdated" }

// JoinFailed is a refused JOIN surfaced to the UI. NeedKey drives the
// "this channel needs a key" prompt instead of a dead end.
type JoinFailed struct {
	ChangeMeta
	Channel string
	Code    string
	Reason  string
	NeedKey bool
}

// ChangeName implements Change.
func (JoinFailed) ChangeName() string { return "joinfailed" }
