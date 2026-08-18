package state

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// buffer is one conversation's state. Its identity is b.id, whose folded name
// is the key of every map that points at it; b.name is display data that the
// server may re-case at any time.
type buffer struct {
	id   model.BufferID
	name string
	kind model.BufferKind

	// joined is true while we are actually in the channel; wantJoin stays true
	// across a disconnect, because it is what the reconnect plan reads.
	joined   bool
	wantJoin bool

	members map[string]model.Member // folded nick -> member

	topic      string
	topicSetBy string
	topicSetAt time.Time
	topicKnown bool

	modes string // paramless flags, sorted (proto.ApplyFlags)
	key   string // +k, tracked so a reconnect can rejoin
	limit int    // +l, 0 when unset

	// msgs is the retained window of the buffer's messages, oldest first. base
	// is how many messages have already been trimmed off the front, so pos maps
	// a MessageID to an absolute index that survives trimming.
	msgs    []model.Message
	base    int
	pos     map[model.MessageID]int
	byMsgID map[string]model.MessageID

	unread      int
	highlights  int
	markerMsgID string
	markerAt    time.Time
	// markerID is the marker resolved to a local position. Server-time has
	// millisecond granularity and a burst of messages can share a timestamp, so
	// "after the marker" is answered by arrival order whenever the marker
	// resolves to a message we still hold.
	markerID model.MessageID

	typing map[string]typingEntry // folded nick -> indicator

	notify NotifyLevel
	hideJP *bool // per-buffer override of the network's join/part setting

	historyDone bool // a backfill has been requested since this buffer opened
}

// typingEntry is one nick's live typing indicator.
type typingEntry struct {
	nick  string
	state model.TypingState
	at    time.Time
}

// info returns the buffer's identity for the UI.
func (b *buffer) info() model.BufferInfo {
	return model.BufferInfo{ID: b.id, Name: b.name, Kind: b.kind}
}

// meta is the change envelope for this buffer.
func (b *buffer) meta() model.ChangeMeta { return model.BufMeta(b.id) }

// at returns a pointer to a retained message, or nil when it was trimmed or
// never existed. The pointer is valid only until the next append.
func (b *buffer) at(id model.MessageID) *model.Message {
	abs, ok := b.pos[id]
	if !ok {
		return nil
	}
	i := abs - b.base
	if i < 0 || i >= len(b.msgs) {
		return nil
	}
	return &b.msgs[i]
}

// byMsgIDLookup returns the retained message with this IRCv3 msgid.
func (b *buffer) byMsgIDLookup(msgid string) *model.Message {
	if msgid == "" {
		return nil
	}
	id, ok := b.byMsgID[msgid]
	if !ok {
		return nil
	}
	return b.at(id)
}

// seen reports whether this msgid was already displayed in this buffer. It is
// exact and bounded by the buffer's own scrollback, replacing v1's shared
// 8192-entry map that emptied itself wholesale and started duplicating history.
func (b *buffer) seen(msgid string) bool {
	if msgid == "" {
		return false
	}
	_, ok := b.byMsgID[msgid]
	return ok
}

// appendMsg stores a message and trims the window to cap (Unlimited keeps
// everything). Trimming forgets the trimmed msgids, which is what keeps the
// dedupe index bounded without ever wiping live entries.
func (b *buffer) appendMsg(m model.Message, cap int) {
	b.msgs = append(b.msgs, m)
	b.pos[m.ID] = b.base + len(b.msgs) - 1
	if m.MsgID != "" {
		b.byMsgID[m.MsgID] = m.ID
	}
	if cap <= 0 || len(b.msgs) <= cap {
		return
	}
	drop := len(b.msgs) - cap
	for _, old := range b.msgs[:drop] {
		delete(b.pos, old.ID)
		if old.MsgID != "" && b.byMsgID[old.MsgID] == old.ID {
			delete(b.byMsgID, old.MsgID)
		}
	}
	b.msgs = append(b.msgs[:0], b.msgs[drop:]...)
	b.base += drop
}

// countsUnread is the rule the whole attention model rests on: status lines,
// history playback and our own messages never mark a buffer unread. It is v1's
// rule (regression_buffers_test.go's bufCountsUnread) with the flags moved onto
// the Message.
func countsUnread(m model.Message) bool {
	return m.Kind != model.MsgStatus && !m.Playback && !m.Sender.Self && m.Pending != model.PendingEcho
}

// unreadChange reports the buffer's current counts.
func (b *buffer) unreadChange() model.Change {
	return model.UnreadChanged{ChangeMeta: b.meta(), Unread: b.unread, Highlights: b.highlights}
}

// count adds one message to the derived counters.
func (b *buffer) count(m model.Message) bool {
	if !countsUnread(m) || !b.afterMarker(m) {
		return false
	}
	b.unread++
	if m.Highlight {
		b.highlights++
	}
	return true
}

// afterMarker reports whether a message sits after the read marker. The marker
// means "everything up to and including this point is read".
func (b *buffer) afterMarker(m model.Message) bool {
	if b.markerID != 0 {
		return m.ID > b.markerID
	}
	if b.markerMsgID != "" && m.MsgID != "" && m.MsgID == b.markerMsgID {
		return false
	}
	if b.markerAt.IsZero() {
		return true
	}
	return m.Time.After(b.markerAt)
}

// recount rederives the unread counters from the retained window. It runs when
// the marker moves, which is the only time the answer can change wholesale.
func (b *buffer) recount() {
	b.unread, b.highlights = 0, 0
	for i := range b.msgs {
		m := b.msgs[i]
		if !countsUnread(m) || !b.afterMarker(m) {
			continue
		}
		b.unread++
		if m.Highlight {
			b.highlights++
		}
	}
}

// setMarker moves the read marker forward. Conflicts resolve to the later of
// the two positions (ADR-3: max(server, local)), so a stale MARKREAD from a
// device that was behind cannot un-read a buffer. It reports whether the marker
// actually moved.
func (b *buffer) setMarker(msgid string, at time.Time) bool {
	if at.IsZero() && msgid == "" {
		return false
	}
	if !at.IsZero() && !b.markerAt.IsZero() && at.Before(b.markerAt) {
		return false
	}
	id := b.resolveMarker(msgid, at)
	if id == b.markerID && at.Equal(b.markerAt) && (msgid == "" || msgid == b.markerMsgID) {
		return false
	}
	if msgid != "" {
		b.markerMsgID = msgid
	}
	if !at.IsZero() {
		b.markerAt = at
	}
	b.markerID = id
	b.recount()
	return true
}

// resolveMarker maps a marker position onto the newest retained message at or
// before it, never moving backwards from where the marker already is. The
// msgid is the exact answer; the timestamp is the portable one and resolves to
// the last message not after it. It is separate from setMarker because a
// marker also has to be re-resolved when the WINDOW moves under it: messages
// restored from the archive can be older than a marker that arrived while the
// buffer was still empty.
func (b *buffer) resolveMarker(msgid string, at time.Time) model.MessageID {
	id := b.markerID
	if m := b.byMsgIDLookup(msgid); m != nil {
		// The msgid names one message; the timestamp names a moment, and a
		// moment holds as many messages as arrived in that millisecond. Taking
		// the later of the two would let the coarse answer overrule the exact
		// one and mark a burst read that the user has not seen.
		if m.ID > id {
			id = m.ID
		}
		return id
	}
	if !at.IsZero() {
		for i := len(b.msgs) - 1; i >= 0; i-- {
			if b.msgs[i].Time.After(at) {
				continue
			}
			if b.msgs[i].ID > id {
				id = b.msgs[i].ID
			}
			break
		}
	}
	return id
}

// lastReadable returns the newest message that a read marker may point at.
func (b *buffer) lastReadable() (model.Message, bool) {
	for i := len(b.msgs) - 1; i >= 0; i-- {
		if b.msgs[i].Pending == model.PendingEcho {
			continue
		}
		return b.msgs[i], true
	}
	return model.Message{}, false
}

// addMember inserts or replaces a member, keeping the prefix set the existing
// entry had when the new one carries none (a JOIN never grants prefixes).
func (b *buffer) addMember(m model.Member) (model.Member, bool) {
	if m.Folded == "" {
		return model.Member{}, false
	}
	if old, ok := b.members[m.Folded]; ok {
		if m.Prefixes == "" {
			m.Prefixes = old.Prefixes
		}
		if m.Account == "" {
			m.Account = old.Account
		}
		if m.RealName == "" {
			m.RealName = old.RealName
		}
		m.Away = old.Away
		b.members[m.Folded] = m
		return m, false
	}
	b.members[m.Folded] = m
	return m, true
}

// removeMember drops a member and returns what was removed.
func (b *buffer) removeMember(folded string) (model.Member, bool) {
	m, ok := b.members[folded]
	if !ok {
		return model.Member{}, false
	}
	delete(b.members, folded)
	return m, true
}
