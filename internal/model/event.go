package model

import "time"

// Event is what the wire produces: one typed fact per message the irc client
// decoded. internal/irc emits these on its own goroutine; internal/state is the
// only consumer that interprets them.
//
// Events deliberately stay close to the wire. They carry raw target names and
// raw mode strings, never folded keys or parsed mode deltas, because folding
// and mode classification both need the network's ServerProps and the store is
// where those live. That keeps model free of any dependency on internal/proto
// (proto imports model, never the other way round) and keeps events directly
// comparable in the transcript suite's records.
//
// See the package doc for why this union is an interface of concrete structs.
type Event interface {
	// Meta returns the fields every event carries.
	Meta() EventMeta
	// EventName is a stable lowercase label for logs, test failures and record
	// diffing. It also seals the union: implementing Event means implementing
	// this, and nothing outside this package does.
	EventName() string
}

// EventMeta is the envelope every Event carries. Embed it in each variant;
// it supplies Meta.
type EventMeta struct {
	// Network is always set. It is what makes every event routable in a
	// multi-network client.
	Network NetworkID

	// Time is the server-time tag when the server sent one, otherwise the
	// client clock reading taken when the line was decoded.
	Time time.Time
	// ServerTime distinguishes those two cases. History merging and ordering
	// must know which it got.
	ServerTime bool

	// MsgID is the IRCv3 msgid tag, "" when absent. It is the dedupe key, the
	// REDACT target, the CHATHISTORY anchor and the read-marker position.
	MsgID string
	// Label is the labeled-response tag, "" when absent. A non-empty Label on
	// an inbound event means it answers something we sent.
	Label string

	// Tags is the full message-tag map, "" values for valueless tags. Consumers
	// must treat it as read-only; the irc layer does not copy it per consumer.
	Tags map[string]string

	// Batch is the ref of the innermost enclosing BATCH, "" outside a batch.
	Batch string
	// Playback is true when the enclosing batch chain contains a chathistory
	// batch. It is resolved once, by the irc layer, from the real batch
	// registry - never guessed from "does this line have a batch tag", which is
	// the v1 bug (session.go isPlayback) this field exists to kill.
	Playback bool
}

// Meta implements Event for every variant that embeds EventMeta.
func (m EventMeta) Meta() EventMeta { return m }

// Tag returns the value of a message tag and whether it was present.
func (m EventMeta) Tag(name string) (string, bool) {
	v, ok := m.Tags[name]
	return v, ok
}

// --- messages ---------------------------------------------------------------

// PrivMsg is a PRIVMSG. Target is the raw target: a channel name, our nick (a
// query), or a STATUSMSG-prefixed channel such as "@#chan" - the store splits
// the prefix off with the network's STATUSMSG set.
type PrivMsg struct {
	EventMeta
	Target string
	Sender Sender
	Text   string
	// Action is true when the body was a CTCP ACTION; Text has the \x01ACTION
	// wrapper removed.
	Action bool
	// CTCP is the CTCP command for non-ACTION CTCP requests ("VERSION",
	// "PING"), "" for ordinary messages.
	CTCP string
}

// EventName implements Event.
func (PrivMsg) EventName() string { return "privmsg" }

// Notice is a NOTICE. Same shape as PrivMsg minus the action case; the split
// exists because notices route and render differently everywhere.
type Notice struct {
	EventMeta
	Target string
	Sender Sender
	Text   string
	// CTCP is set when this is a CTCP reply.
	CTCP string
}

// EventName implements Event.
func (Notice) EventName() string { return "notice" }

// Typing is a TAGMSG carrying +typing (draft/typing).
type Typing struct {
	EventMeta
	Target string
	Sender Sender
	State  TypingState
}

// EventName implements Event.
func (Typing) EventName() string { return "typing" }

// Redact is an inbound REDACT (draft/message-redaction): TargetMsgID is the
// msgid of the message being withdrawn.
type Redact struct {
	EventMeta
	Target      string
	Sender      Sender
	TargetMsgID string
	Reason      string
}

// EventName implements Event.
func (Redact) EventName() string { return "redact" }

// MarkRead is an inbound MARKREAD (draft/read-marker): another device moved the
// read marker for Target. Timestamp is zero when the server sent "*" (no
// marker set), which Unset reports.
type MarkRead struct {
	EventMeta
	Target    string
	Timestamp time.Time
	Unset     bool
}

// EventName implements Event.
func (MarkRead) EventName() string { return "markread" }

// --- membership -------------------------------------------------------------

// Join is a JOIN. Account and RealName are populated only with extended-join.
type Join struct {
	EventMeta
	Channel  string
	Sender   Sender
	Account  string
	RealName string
}

// EventName implements Event.
func (Join) EventName() string { return "join" }

// Part is a PART.
type Part struct {
	EventMeta
	Channel string
	Sender  Sender
	Reason  string
}

// EventName implements Event.
func (Part) EventName() string { return "part" }

// Quit is a QUIT. It has no channel: the store removes the sender from every
// buffer they were in.
type Quit struct {
	EventMeta
	Sender Sender
	Reason string
}

// EventName implements Event.
func (Quit) EventName() string { return "quit" }

// Kick is a KICK. Target is the nick removed, Sender is the operator.
type Kick struct {
	EventMeta
	Channel string
	Sender  Sender
	Target  string
	Reason  string
}

// EventName implements Event.
func (Kick) EventName() string { return "kick" }

// Nick is a NICK change. Sender.Nick is the old nick.
type Nick struct {
	EventMeta
	Sender  Sender
	NewNick string
}

// EventName implements Event.
func (Nick) EventName() string { return "nick" }

// Invite is an INVITE addressed to Target (usually us).
type Invite struct {
	EventMeta
	Channel string
	Sender  Sender
	Target  string
}

// EventName implements Event.
func (Invite) EventName() string { return "invite" }

// Names carries RPL_NAMREPLY content. Names holds the raw entries exactly as
// the server wrote them, prefixes included ("@nick", "+nick", and with
// multi-prefix "@+nick"); the store splits them with the network's PREFIX
// table. End marks RPL_ENDOFNAMES, at which point Names may be empty and the
// accumulated list is authoritative.
type Names struct {
	EventMeta
	Channel string
	Names   []string
	End     bool
}

// EventName implements Event.
func (Names) EventName() string { return "names" }

// AwayChange is an away-notify AWAY. Away is false when the user came back.
type AwayChange struct {
	EventMeta
	Sender  Sender
	Away    bool
	Message string
}

// EventName implements Event.
func (AwayChange) EventName() string { return "awaychange" }

// AccountChange is an account-notify ACCOUNT. Account is "" when the user
// logged out (the wire spells that "*").
type AccountChange struct {
	EventMeta
	Sender  Sender
	Account string
}

// EventName implements Event.
func (AccountChange) EventName() string { return "accountchange" }

// ChgHost is a CHGHOST: the user's ident/host changed without a reconnect.
type ChgHost struct {
	EventMeta
	Sender  Sender
	NewUser string
	NewHost string
}

// EventName implements Event.
func (ChgHost) EventName() string { return "chghost" }

// SetName is a SETNAME: the user's realname changed.
type SetName struct {
	EventMeta
	Sender   Sender
	RealName string
}

// EventName implements Event.
func (SetName) EventName() string { return "setname" }

// --- channel state ----------------------------------------------------------

// Topic is a topic assertion, from either a TOPIC command (Sender set) or
// RPL_TOPIC/RPL_TOPICWHOTIME (Sender zero, SetBy/SetAt set). Cleared is true
// when the topic was removed, which is distinct from "we do not know it".
type Topic struct {
	EventMeta
	Channel string
	Sender  Sender
	Topic   string
	SetBy   string
	SetAt   time.Time
	Cleared bool
}

// EventName implements Event.
func (Topic) EventName() string { return "topic" }

// Mode is a MODE change or, with Full set, RPL_CHANNELMODEIS (324) - the
// authoritative full mode state, which replaces rather than amends what the
// store holds.
//
// The delta arrives unparsed on purpose: proto.ParseModeDelta needs the
// network's CHANMODES and PREFIX to classify it, and those live with the store.
// ModeString is the "+ik-m" word; Args are the remaining parameters in order.
type Mode struct {
	EventMeta
	Target     string // channel name, or a nick for a user-mode change
	Sender     Sender
	ModeString string
	Args       []string
	Full       bool
}

// EventName implements Event.
func (Mode) EventName() string { return "mode" }

// ChannelRenamed is a draft/channel-rename RENAME.
type ChannelRenamed struct {
	EventMeta
	From   string
	To     string
	Reason string
}

// EventName implements Event.
func (ChannelRenamed) EventName() string { return "channelrenamed" }

// --- protocol plumbing ------------------------------------------------------

// BatchOpen is "BATCH +ref type params...". Parent is the ref of the enclosing
// batch when this batch was itself opened inside one, "" otherwise; it is what
// makes nested chathistory-wrapping-multiline resolvable.
type BatchOpen struct {
	EventMeta
	Ref    string
	Type   string
	Params []string
	Parent string
}

// EventName implements Event.
func (BatchOpen) EventName() string { return "batchopen" }

// BatchClose is "BATCH -ref". Type is filled in from the registry so consumers
// do not have to remember what opened.
type BatchClose struct {
	EventMeta
	Ref  string
	Type string
}

// EventName implements Event.
func (BatchClose) EventName() string { return "batchclose" }

// CapOp is which CAP subcommand produced a CapChange.
type CapOp uint8

const (
	CapLS  CapOp = 0
	CapNEW CapOp = 1
	CapDEL CapOp = 2
	CapACK CapOp = 3
	CapNAK CapOp = 4
)

// String returns the CAP subcommand name.
func (o CapOp) String() string {
	switch o {
	case CapLS:
		return "LS"
	case CapNEW:
		return "NEW"
	case CapDEL:
		return "DEL"
	case CapACK:
		return "ACK"
	case CapNAK:
		return "NAK"
	}
	return "?"
}

// Cap is one capability token: a name and its value ("" for valueless caps).
type Cap struct {
	Name  string
	Value string
}

// CapChange reports a CAP subcommand. Caps holds the tokens that subcommand
// carried; Enabled is the network's full enabled set *after* applying it, so a
// consumer that only cares about "is multiline on now" never has to replay CAP
// state itself.
type CapChange struct {
	EventMeta
	Op      CapOp
	Caps    []Cap
	Enabled []string
	// More is true for a CAP LS continuation line ("CAP * LS * :..."); the
	// advertised set is not complete until a line arrives with More false.
	More bool
}

// EventName implements Event.
func (CapChange) EventName() string { return "capchange" }

// ISupport carries the tokens of one RPL_ISUPPORT (005) line, with the nick and
// the trailing human text already stripped. Reset is true on the first 005 of a
// fresh registration, telling the store to drop the previous server's
// properties before applying these.
type ISupport struct {
	EventMeta
	Tokens []string
	Reset  bool
}

// EventName implements Event.
func (ISupport) EventName() string { return "isupport" }

// ConnState reports a transition of the connection state machine.
type ConnState struct {
	EventMeta
	Status ConnStatus
	// Nick is our current nick, known from ConnRegistering onward.
	Nick string
	// Err is a human-readable reason for ConnDisconnected/ConnFailed.
	Err string
	// Attempt counts connection attempts in the current run of failures,
	// starting at 1; RetryIn is the backoff before the next one (0 = no retry).
	Attempt int
	RetryIn time.Duration
}

// EventName implements Event.
func (ConnState) EventName() string { return "connstate" }

// AuthResult reports how SASL ended. It fires exactly once per connection when
// SASL was attempted, and gates autojoin: v1's SCRAM/autojoin race was joining
// before this existed.
type AuthResult struct {
	EventMeta
	Success   bool
	Mechanism string
	Account   string
	// Aborted is true when we gave up (no mechanism in common, user cancel)
	// rather than the server rejecting the credentials.
	Aborted bool
	Message string
}

// EventName implements Event.
func (AuthResult) EventName() string { return "authresult" }

// JoinRefused reports a JOIN that the server rejected. Code is the numeric
// ("475", "473", "471", "477", "403", "474"); NeedKey singles out ERR_BADCHANNELKEY
// so the UI can prompt for one instead of showing a dead end.
type JoinRefused struct {
	EventMeta
	Channel string
	Code    string
	Reason  string
	NeedKey bool
}

// EventName implements Event.
func (JoinRefused) EventName() string { return "joinrefused" }

// ListItem is one RPL_LIST row. LIST results stream: they are never held in
// RAM as a slice (blueprint §5.4), so they arrive one event at a time.
type ListItem struct {
	EventMeta
	Channel string
	Users   int
	Topic   string
}

// EventName implements Event.
func (ListItem) EventName() string { return "listitem" }

// ListEnd is RPL_LISTEND.
type ListEnd struct {
	EventMeta
}

// EventName implements Event.
func (ListEnd) EventName() string { return "listend" }

// HistoryChunk is a closed chathistory batch, delivered whole so the store can
// prepend it as one unit instead of interleaving it with live traffic. Events
// holds the enclosed events in wire order - the same PrivMsg/Join/… values the
// live path uses, each with Playback already true - so the store runs its
// ordinary reducers over them.
type HistoryChunk struct {
	EventMeta
	Target    string
	Direction HistoryDirection
	Events    []Event
	// Complete is false when the server truncated the reply at its own limit,
	// meaning more history exists before/after this chunk.
	Complete bool
}

// EventName implements Event.
func (HistoryChunk) EventName() string { return "historychunk" }

// HistoryTarget is one row of a CHATHISTORY TARGETS reply: a conversation the
// server has history for, and when it last saw traffic. It is how a client
// discovers the private messages that arrived while it was offline - a channel
// announces itself by being rejoined, a query has nobody to announce it.
type HistoryTarget struct {
	EventMeta
	Target string
	At     time.Time
}

// EventName implements Event.
func (HistoryTarget) EventName() string { return "historytarget" }

// Numeric is any numeric reply that has meaning beyond the ones modelled above:
// MOTD lines, WHOIS replies, errors. Params excludes the leading nick; Text is
// the trailing parameter.
type Numeric struct {
	EventMeta
	Code   string
	Params []string
	Text   string
}

// EventName implements Event.
func (Numeric) EventName() string { return "numeric" }

// ServerError is the ERROR command: the server is closing the link.
type ServerError struct {
	EventMeta
	Message string
}

// EventName implements Event.
func (ServerError) EventName() string { return "servererror" }

// Raw is an unparsed protocol line, for the raw log and /raw. Outgoing marks
// lines we wrote. Nothing in the store reacts to Raw; it exists so the debug
// view does not need a second tap on the socket.
type Raw struct {
	EventMeta
	Line     string
	Outgoing bool
}

// EventName implements Event.
func (Raw) EventName() string { return "raw" }
