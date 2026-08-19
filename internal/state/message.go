package state

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// applyPrivMsg reduces a PRIVMSG. Non-ACTION CTCP never reaches a buffer:
// internal/irc answers VERSION and friends, and showing the raw \x01 payload
// was never useful (v1 dropped it here too).
func (s *Store) applyPrivMsg(n *network, e model.PrivMsg) []model.Change {
	if e.CTCP != "" && !e.Action {
		return nil
	}
	kind := model.MsgPrivmsg
	if e.Action {
		kind = model.MsgAction
	}
	return s.message(n, e.EventMeta, e.Sender, e.Target, e.Text, kind)
}

// applyNotice reduces a NOTICE. CTCP replies are dropped for the same reason.
func (s *Store) applyNotice(n *network, e model.Notice) []model.Change {
	if e.CTCP != "" {
		return nil
	}
	return s.message(n, e.EventMeta, e.Sender, e.Target, e.Text, model.MsgNotice)
}

// message is the routing, dedupe, echo-resolution, highlight and notification
// pipeline every inbound message goes through. It is the port of v1's
// handleMessage with the four blueprint fixes applied.
func (s *Store) message(n *network, meta model.EventMeta, sender model.Sender, target, text string, kind model.MessageKind) []model.Change {
	self := n.isSelf(sender)
	if !self && n.ignored(sender) {
		return nil
	}

	b, out := s.route(n, sender, target, self)
	if b == nil {
		return out
	}

	// A labeled echo resolves the pending message we already rendered instead
	// of adding a second copy. This is the whole of v1's pendingEcho, minus the
	// text matching that made two identical messages indistinguishable.
	if meta.Label != "" && self {
		if id, ok := n.pending[meta.Label]; ok {
			return append(out, s.resolve(n, b, id, meta, text)...)
		}
	}

	// Exact per-buffer msgid dedupe: a repeated /history fetch or an
	// overlapping chathistory replay must not double a message already shown.
	if meta.MsgID != "" && b.seen(meta.MsgID) {
		return out
	}

	notice := kind == model.MsgNotice
	m := s.newMessage(b, meta, kind, sender, text)
	m.Sender.Self = self
	m.Highlight = s.highlight(n, b, m, notice)

	return append(out, s.addMessage(n, b, m)...)
}

// route decides which buffer a message belongs in, and creates it if needed.
//
//   - a message from the server itself lands in the network's status buffer;
//   - a channel target (CHANTYPES, with any STATUSMSG prefix stripped) lands in
//     that channel;
//   - a query lands in the *other party's* buffer, which for our own message
//     echoed from another device is the target, not us.
func (s *Store) route(n *network, sender model.Sender, target string, self bool) (*buffer, []model.Change) {
	if isServerSource(sender) {
		return n.server(), nil
	}
	_, name := n.props.SplitStatusMsg(target)
	if n.props.IsChannel(name) {
		return s.ensure(n, name)
	}
	peer := sender.Nick
	if self {
		peer = name
	}
	if peer == "" {
		return n.server(), nil
	}
	// Our own nick as the peer would open a buffer named after us; that happens
	// when a message we sent is echoed with us as the target, and the only
	// sensible home for it is the sender's buffer.
	if self && n.isSelfNick(peer) && sender.Nick != "" && !n.isSelfNick(sender.Nick) {
		peer = sender.Nick
	}
	return s.ensure(n, peer)
}

// newMessage mints a message for a buffer. Time comes from the server-time tag
// when there is one and from the injected clock otherwise, so a test never sees
// a timestamp it did not choose.
func (s *Store) newMessage(b *buffer, meta model.EventMeta, kind model.MessageKind, sender model.Sender, text string) model.Message {
	return model.Message{
		ID:         s.ids.Next(),
		MsgID:      meta.MsgID,
		Label:      meta.Label,
		Buffer:     b.id,
		Time:       s.eventTime(meta),
		ServerTime: meta.ServerTime,
		Sender:     sender,
		Kind:       kind,
		Text:       text,
		Tags:       meta.Tags,
		Playback:   meta.Playback,
		Batch:      meta.Batch,
	}
}

// addMessage stores a message, derives its notification and unread effect and
// returns the changes. It is the single funnel every message goes through, so
// the counters can never disagree with the log.
func (s *Store) addMessage(n *network, b *buffer, m model.Message) []model.Change {
	notify := s.notifies(n, b, m)
	// "Who has spoken here recently" is derived HERE, in the one funnel every
	// message goes through, for the same reason the counters are: a second
	// place that decided it would be a second answer. Playback counts, because
	// it carries the time the message was actually said - which is the only
	// thing the smart filter's window ever asks about (jpfilter.go).
	if m.Kind != model.MsgStatus && m.Sender.Nick != "" {
		b.noteSpeaker(n.props.Fold(m.Sender.Nick), m.Time, n.speakerWindow())
	}
	b.appendMsg(m, s.scrollback)
	out := []model.Change{model.MessageAdded{ChangeMeta: b.meta(), Message: m, Notify: notify}}
	if b.count(m) {
		out = append(out, b.unreadChange())
	}
	return out
}

// addStatus appends a client-generated status line ("alice joined"). Status
// lines never count as unread and never notify, which is what keeps a reconnect
// from badging every buffer (v1 invariant, regression_buffers_test.go).
//
// The line inherits the event's msgid when it has one, so it dedupes and can be
// redacted like any other message: with draft/event-playback a replayed JOIN
// arrives again with the same msgid, and v1 would have drawn it twice.
func (s *Store) addStatus(n *network, b *buffer, meta model.EventMeta, text string) []model.Change {
	if meta.MsgID != "" && b.seen(meta.MsgID) {
		return nil
	}
	m := s.newMessage(b, meta, model.MsgStatus, model.Sender{}, text)
	return s.addMessage(n, b, m)
}

// status is addStatus for a line the store invented, with no event behind it.
func (s *Store) status(n *network, b *buffer, text string) []model.Change {
	return s.addStatus(n, b, model.EventMeta{Network: n.id, Time: s.now()}, text)
}

// highlight decides whether a message mentions the user. Playback, our own
// messages and status lines never highlight - a reconnect replaying a mention
// must not raise it again (audit severe #2).
func (s *Store) highlight(n *network, b *buffer, m model.Message, notice bool) bool {
	if m.Sender.Self || m.Playback || m.Kind == model.MsgStatus {
		return false
	}
	// A query addressed to us is a highlight by definition, but a NOTICE is
	// not: services send routine notices and every one of them would pop a
	// desktop notification.
	if b.kind == model.BufferQuery && !notice {
		return true
	}
	return n.matchesHighlight(m.Text)
}

// notifies applies the per-buffer notification level to a message. Evaluation
// order is the blueprint's (§4.4): level, then highlight match, then
// suppression when the buffer is focused.
func (s *Store) notifies(n *network, b *buffer, m model.Message) bool {
	if !countsUnread(m) {
		return false
	}
	notify := m.Highlight
	switch n.level(b) {
	case NotifyMute:
		notify = false
	case NotifyAll:
		notify = true
	}
	if notify && n.focus == b.id {
		return false
	}
	return notify
}

// --- outgoing ----------------------------------------------------------------

// Outgoing describes a message the user just sent. The label is the
// labeled-response correlation tag internal/irc put on the wire; it is what the
// echo resolves against.
type Outgoing struct {
	Label string
	Text  string
	Kind  model.MessageKind // MsgPrivmsg (default), MsgAction or MsgNotice
}

// Echo registers a locally composed message so it renders immediately, before
// the server has acknowledged it. The message is model.PendingEcho until its
// labeled echo arrives (Resolve) or the server rejects it (Fail).
//
// This is MUST 3: v1 rendered the text and then matched the server's echo by
// text, so its own messages never carried a msgid and could not be redacted or
// read-marked, and a second identical message ate the first one's echo.
func (s *Store) Echo(id model.BufferID, o Outgoing) (model.MessageID, []model.Change) {
	n, ok := s.nets[id.Network]
	if !ok {
		return 0, nil
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return 0, nil
	}
	kind := o.Kind
	if kind == model.MsgStatus || kind == model.MsgTombstone {
		kind = model.MsgPrivmsg
	}
	m := model.Message{
		ID:      s.ids.Next(),
		Label:   o.Label,
		Buffer:  b.id,
		Time:    s.now(),
		Sender:  model.Sender{Nick: n.nick, Account: n.account, Self: true},
		Kind:    kind,
		Text:    o.Text,
		Pending: model.PendingEcho,
	}
	if o.Label != "" {
		n.pending[o.Label] = m.ID
	}
	return m.ID, s.addMessage(n, b, m)
}

// Resolve confirms a pending message from outside the event stream - a
// labeled-response batch that closed with an ACK rather than an echo. Callers
// that see the echo itself do not need it: the PRIVMSG reducer resolves.
func (s *Store) Resolve(id model.NetworkID, label, msgid string, at time.Time) []model.Change {
	n, ok := s.nets[id]
	if !ok || label == "" {
		return nil
	}
	mid, ok := n.pending[label]
	if !ok {
		return nil
	}
	b := s.pendingBuffer(n, mid)
	if b == nil {
		delete(n.pending, label)
		return nil
	}
	meta := model.EventMeta{Network: id, MsgID: msgid, Label: label, Time: at, ServerTime: !at.IsZero()}
	return s.resolve(n, b, mid, meta, "")
}

// resolve upgrades a pending message in place: it gains the server's msgid and
// server-time, and stops being pending. The row keeps its MessageID, so the UI
// amends the line it already drew instead of drawing a second one.
func (s *Store) resolve(n *network, b *buffer, id model.MessageID, meta model.EventMeta, text string) []model.Change {
	if meta.Label != "" {
		delete(n.pending, meta.Label)
	}
	m := b.at(id)
	if m == nil {
		return nil
	}
	if meta.MsgID != "" {
		m.MsgID = meta.MsgID
		b.byMsgID[meta.MsgID] = id
	}
	if !meta.Time.IsZero() {
		m.Time = meta.Time
		m.ServerTime = meta.ServerTime
	}
	if text != "" {
		m.Text = text
	}
	if meta.Tags != nil {
		m.Tags = meta.Tags
	}
	m.Pending = model.Delivered
	return []model.Change{model.MessageResolved{ChangeMeta: b.meta(), ID: id, Message: *m}}
}

// Fail marks a pending message rejected (a standard-replies FAIL carrying our
// label). The row stays so the text is not lost and can be retried.
func (s *Store) Fail(id model.NetworkID, label, code, reason string) []model.Change {
	n, ok := s.nets[id]
	if !ok || label == "" {
		return nil
	}
	mid, ok := n.pending[label]
	if !ok {
		return nil
	}
	delete(n.pending, label)
	b := s.pendingBuffer(n, mid)
	if b == nil {
		return nil
	}
	m := b.at(mid)
	if m == nil {
		return nil
	}
	m.Pending = model.PendingFailed
	return []model.Change{model.MessageFailed{ChangeMeta: b.meta(), ID: mid, Code: code, Reason: reason}}
}

// pendingBuffer finds the buffer holding a pending message.
func (s *Store) pendingBuffer(n *network, id model.MessageID) *buffer {
	for _, k := range n.order {
		if b := n.bufs[k]; b != nil && b.at(id) != nil {
			return b
		}
	}
	return nil
}

// --- redaction, markers, typing ----------------------------------------------

// applyRedact withdraws a message (draft/message-redaction). The store leaves a
// tombstone rather than a hole, so the surrounding conversation still reads.
func (s *Store) applyRedact(n *network, e model.Redact) []model.Change {
	var b *buffer
	var out []model.Change
	if e.Target != "" {
		_, name := n.props.SplitStatusMsg(e.Target)
		b = n.lookup(name)
		if b == nil && !n.props.IsChannel(name) && !n.isSelfNick(name) {
			b = n.lookup(e.Sender.Nick)
		}
	}
	if b == nil {
		// A REDACT for a buffer we cannot name still has to land: search.
		for _, k := range n.order {
			if cand := n.bufs[k]; cand != nil && cand.byMsgIDLookup(e.TargetMsgID) != nil {
				b = cand
				break
			}
		}
	}
	if b == nil {
		return out
	}
	m := b.byMsgIDLookup(e.TargetMsgID)
	if m == nil || m.Redacted {
		return out
	}
	wasUnread := countsUnread(*m) && b.afterMarker(*m)
	wasHighlight := m.Highlight
	m.Redacted = true
	m.Kind = model.MsgTombstone
	m.Text = ""
	m.Highlight = false
	out = append(out, model.MessageRedacted{
		ChangeMeta: b.meta(), ID: m.ID, MsgID: e.TargetMsgID,
		ByNick: e.Sender.Nick, Reason: e.Reason,
	})
	if wasUnread {
		// A tombstone is a status line now, so it stops counting.
		b.unread--
		if wasHighlight {
			b.highlights--
		}
		out = append(out, b.unreadChange())
	}
	return out
}

// applyMarkRead consumes a MARKREAD from another device. Conflicts resolve to
// the later marker, so a device that was behind cannot un-read a buffer.
func (s *Store) applyMarkRead(n *network, e model.MarkRead) []model.Change {
	if e.Target == "" {
		return nil
	}
	_, name := n.props.SplitStatusMsg(e.Target)
	b, out := s.ensure(n, name)
	if e.Unset {
		return out
	}
	if !b.setMarker("", e.Timestamp) {
		return out
	}
	return append(out,
		model.ReadMarkerMoved{ChangeMeta: b.meta(), At: e.Timestamp, Remote: true},
		b.unreadChange())
}

// applyTyping records a draft/typing indicator. Our own typing is not shown
// back to us.
func (s *Store) applyTyping(n *network, e model.Typing) []model.Change {
	if n.isSelf(e.Sender) || e.Sender.Nick == "" {
		return nil
	}
	if !n.isSelf(e.Sender) && n.ignored(e.Sender) {
		return nil
	}
	_, name := n.props.SplitStatusMsg(e.Target)
	target := name
	if !n.props.IsChannel(name) {
		target = e.Sender.Nick
	}
	b, out := s.ensure(n, target)
	if b.typing == nil {
		b.typing = map[string]typingEntry{}
	}
	folded := n.props.Fold(e.Sender.Nick)
	if e.State == model.TypingDone {
		if _, ok := b.typing[folded]; !ok {
			return out
		}
		delete(b.typing, folded)
	} else {
		b.typing[folded] = typingEntry{nick: e.Sender.Nick, state: e.State, at: s.eventTime(e.EventMeta)}
		n.hasTyping = true
	}
	return append(out, model.TypingChanged{ChangeMeta: b.meta(), Nick: e.Sender.Nick, State: e.State})
}

// --- history -----------------------------------------------------------------

// applyHistory folds a closed chathistory batch. The enclosed events run
// through the ordinary reducers - playback JOINs still touch nothing, playback
// messages still dedupe by msgid - but the messages they produce are collected
// into one HistoryLoaded so the UI prepends a block instead of watching a
// hundred lines arrive one at a time.
func (s *Store) applyHistory(n *network, e model.HistoryChunk) []model.Change {
	var target model.BufferID
	var out []model.Change
	if e.Target != "" {
		_, name := n.props.SplitStatusMsg(e.Target)
		b, created := s.ensure(n, name)
		target, out = b.id, created
	}
	var msgs []model.Message
	for _, sub := range e.Events {
		for _, c := range s.apply(n, sub) {
			if added, isAdd := c.(model.MessageAdded); isAdd {
				if target.IsZero() {
					target = added.Meta().Buffer
				}
				if added.Meta().Buffer == target {
					msgs = append(msgs, added.Message)
					continue
				}
			}
			out = append(out, c)
		}
	}
	if target.IsZero() {
		return out
	}
	if b := n.bufs[target.Folded]; b != nil {
		b.historyDone = true
		// A BEFORE chunk is the page above what we are already showing, but the
		// reducers that built it appended, because appending is what every
		// other message does. Move the block it added to the front so the
		// window stays in the order a reader reads it.
		//
		// The counters are deliberately left alone: a chathistory message is
		// playback and playback never counts as unread (the v1 rule the whole
		// attention model rests on), so there is nothing to recount - and the
		// read marker is not re-resolved, because these messages are older than
		// the window but carry ids minted just now, and re-resolving against
		// them would move the marker onto one of them.
		if e.Direction == model.HistoryBefore {
			b.moveTailToFront(len(msgs))
		}
	}
	return append(out, model.HistoryLoaded{
		ChangeMeta: model.BufMeta(target),
		Messages:   msgs,
		Direction:  e.Direction,
		Complete:   e.Complete,
	})
}

// applyHistoryTarget opens the buffer a CHATHISTORY TARGETS row names - but
// only a query's. A channel with offline activity announces itself by being
// rejoined; opening one from a TARGETS row would resurrect every channel the
// user has ever left, every reconnect. A private message has nobody to
// announce it, which is the whole reason the subcommand exists.
//
// The buffer is Restored, so the UI does not steal focus for it; fetching what
// was said in it is the caller's next request, not the store's business.
func (s *Store) applyHistoryTarget(n *network, e model.HistoryTarget) []model.Change {
	if e.Target == "" {
		return nil
	}
	_, name := n.props.SplitStatusMsg(e.Target)
	if name == "" || n.props.IsChannel(name) || n.isSelfNick(name) {
		return nil
	}
	_, out := s.ensureRestored(n, name, true)
	return out
}
