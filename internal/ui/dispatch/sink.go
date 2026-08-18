package dispatch

import "github.com/toprakkilic/eco/internal/model"

// SessionID is one session's identity for its whole life: from the moment the
// app opens a connection to the moment it tears that connection down. It is
// stable by construction and is NOT the network's display name — that name is
// the user's to edit and the server's to re-case, and keying liveness on it was
// how a reconnect could inherit a dead session's traffic.
type SessionID string

// Buffer is a buffer as the Sink sees it: the stable id the store keys on, plus
// the display name it carried at the moment the change happened. Both are here
// because they answer different questions — the id says *which* buffer, the
// name says what to put on screen — and a rename moves them apart.
type Buffer struct {
	ID   model.BufferID
	Name string
}

// Item is one Change on its way to the screen.
//
// The Change is the fact and selects the Sink method. The other fields carry
// what the store cannot: display names (the store keys buffers by folded id and
// treats the pretty name as data) and the rendered rows, which the session has
// already built for its own projection and which are not worth building twice.
type Item struct {
	// Change is the fact. Its concrete type decides which Sink method runs.
	Change model.Change

	// Buffer is Change's buffer with its display name at emit time. The zero
	// value means the change is network-scoped.
	Buffer Buffer

	// From is the identity a BufferRenamed left behind; zero otherwise.
	From Buffer

	// Lines are the rows this change put on screen, in the order they belong
	// there: one for a message, N for a history chunk, none for anything else.
	Lines []model.Line
}

// Of builds an Item for a buffer-scoped change. The id comes from the Change,
// so the two can never name different buffers.
func Of(c model.Change, name string, lines ...model.Line) Item {
	return Item{
		Change: c,
		Buffer: Buffer{ID: c.Meta().Buffer, Name: name},
		Lines:  lines,
	}
}

// Sink is the UI side of the funnel: what the screen can be asked to do.
//
// Every method runs on the Fyne thread, inside the single fyne.Do of one batch,
// in arrival order. None of them may block: the whole batch shares one turn of
// the UI thread. A method that wants an expensive repaint marks it and lets
// BatchDone pay for it once.
//
// The method set is derived from the Change union rather than from the twelve
// callbacks it replaces, so it is named for what these things mean now.
type Sink interface {
	// LinesAdded appends rows to the end of a buffer: one message, or a whole
	// history chunk in the order the chunk carried it.
	LinesAdded(s SessionID, b Buffer, lines []model.Line)

	// LineAmended reports that a row already on screen changed in place — a
	// pending message resolved by its echo into something that reads
	// differently. The row keeps its position; only its content moved.
	LineAmended(s SessionID, b Buffer)

	// LineRedacted reports that a message was withdrawn. The store has already
	// replaced it with a tombstone, so this is a repaint, not a deletion.
	LineRedacted(s SessionID, b Buffer, msgID string)

	// BufferListChanged reports that the set of buffers moved: one was created,
	// closed or renamed.
	BufferListChanged(s SessionID)

	// BufferRenamed reports that a buffer's identity moved. Everything the UI
	// holds per buffer — draft text, unread count, mention flag, the viewer
	// itself — migrates from `from` to `to`, or it is silently orphaned.
	BufferRenamed(s SessionID, from, to Buffer)

	// MembersChanged reports that a buffer's roster moved.
	MembersChanged(s SessionID, b Buffer)

	// UnreadChanged reports a buffer's unread and highlight counts as the store
	// derives them from its read marker. It is the ONLY place a count comes
	// from: the screen files this number, it does not compute one of its own
	// from the lines it happens to have been handed.
	UnreadChanged(s SessionID, b Buffer, unread, highlights int)

	// TopicChanged reports a buffer's new topic, already stripped of formatting
	// codes (the topic bar is a plain label).
	TopicChanged(s SessionID, b Buffer, topic string)

	// ConnStateChanged reports a network's connection state.
	ConnStateChanged(s SessionID, c ConnState)

	// JoinRefused reports a JOIN the server turned down. needKey means the
	// channel is +k, which is a prompt rather than a dead end.
	JoinRefused(s SessionID, channel, reason string, needKey bool)

	// STSOffered reports an STS policy the server advertised. Whether it may be
	// persisted depends on the connection it arrived on, which is the UI's to
	// know; seconds == 0 revokes.
	STSOffered(s SessionID, port string, seconds int64)

	// ChannelListDone reports a finished channel scan; the result is on the
	// session. requested is true when a person asked for this scan, which is
	// the difference between opening the browser and quietly refreshing it.
	ChannelListDone(s SessionID, requested bool)

	// BatchDone closes one delivery batch. Everything the Sink deferred while
	// the batch was landing — relayouts, scrolling, list repaints — belongs
	// here: it runs exactly once per fyne.Do, which is the point of the funnel.
	// It is not called for a batch that delivered nothing.
	BatchDone()
}

// --- app-local changes -------------------------------------------------------
//
// Three facts the UI needs are not the store's to know, because they depend on
// what the *app* did: whether a disconnect is one the user asked for, what a
// capability advertised before any of it reached the store, and whether a
// channel scan was somebody's idea. They ride the same queue as the store's
// Changes, in the same order, rather than jumping the funnel through a side
// channel — one stream is the whole point.

// ConnState is a network's connection state as the UI is asked to show it. It
// is a Change of this package's own because Retrying is the app's fact, not the
// store's: the wire cannot tell a dropped connection from a deliberate /quit.
type ConnState struct {
	model.ChangeMeta
	Status model.ConnStatus
	Nick   string // the nick the server assigned, when Status is ConnReady
	Err    string // why, when Status is ConnFailed
	// Retrying is true when the client will dial again by itself AND the user
	// did not ask to leave.
	Retrying bool
}

// ChangeName implements model.Change.
func (ConnState) ChangeName() string { return "dispatch.connstate" }

// STSOffered is an STS policy seen in CAP LS/NEW. Duration is in seconds and
// zero revokes a stored policy.
type STSOffered struct {
	model.ChangeMeta
	Port     string
	Duration int64
}

// ChangeName implements model.Change.
func (STSOffered) ChangeName() string { return "dispatch.stsoffered" }

// ChannelListDone is a finished LIST. Requested distinguishes a scan somebody
// asked for from a background refresh after a reconnect.
type ChannelListDone struct {
	model.ChangeMeta
	Requested bool
}

// ChangeName implements model.Change.
func (ChannelListDone) ChangeName() string { return "dispatch.channellistdone" }

// deliver maps one Item onto the Sink and reports whether the Sink was told
// anything. It is the whole Change-to-UI vocabulary in one switch,
// deliberately: a fact that no method covers is dropped here, visibly, instead
// of being quietly absent at a dozen call sites.
func deliver(sink Sink, s SessionID, it Item) bool {
	switch c := it.Change.(type) {
	case model.MessageAdded, model.HistoryLoaded:
		sink.LinesAdded(s, it.Buffer, it.Lines)
	case model.MessageResolved:
		sink.LineAmended(s, it.Buffer)
	case model.MessageRedacted:
		sink.LineRedacted(s, it.Buffer, c.MsgID)
	case model.BufferCreated, model.BufferClosed:
		sink.BufferListChanged(s)
	case model.BufferRenamed:
		sink.BufferRenamed(s, it.From, it.Buffer)
		sink.BufferListChanged(s)
	case model.MembersChanged:
		sink.MembersChanged(s, it.Buffer)
	case model.UnreadChanged:
		sink.UnreadChanged(s, it.Buffer, c.Unread, c.Highlights)
	case model.TopicChanged:
		sink.TopicChanged(s, it.Buffer, c.Topic)
	case model.JoinFailed:
		sink.JoinRefused(s, c.Channel, c.Reason, c.NeedKey)
	case ConnState:
		sink.ConnStateChanged(s, c)
	case STSOffered:
		sink.STSOffered(s, c.Port, c.Duration)
	case ChannelListDone:
		sink.ChannelListDone(s, c.Requested)
	default:
		return false
		// Changes the screen has no use for yet — ModeChanged,
		// ReadMarkerMoved, TypingChanged, SelfChanged, AuthStateChanged,
		// ListUpdated, MessageFailed — reach the funnel only if a session
		// chooses to forward them, and stop here until a Sink method means
		// something for them (M9 markers, M12 typing).
	}
	return true
}
