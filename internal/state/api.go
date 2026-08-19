package state

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The intent API: everything the user does that moves state. Each call returns
// the same []model.Change an event would, so the dispatcher has exactly one
// kind of thing to apply and a test can drive the store without a server.
//
// None of these write to the wire. internal/irc owns every byte that leaves the
// process; what the store contributes is the bookkeeping that makes the write
// correct later (pendingJoin, pendingPart, the rejoin plan, the label ledger).

// OpenBuffer creates a buffer for a name the user asked for, if it does not
// already exist. The BufferID it returns is the folded identity - the caller
// should key everything on it and never on the name.
func (s *Store) OpenBuffer(id model.NetworkID, name string) (model.BufferID, []model.Change) {
	n := s.network(id)
	b, out := s.ensure(n, name)
	return b.id, out
}

// RestoreBuffer is OpenBuffer for a buffer coming back from the archive or an
// autojoin plan rather than from the user. The BufferCreated is marked
// Restored, so the UI knows not to steal focus for it.
func (s *Store) RestoreBuffer(id model.NetworkID, name string) (model.BufferID, []model.Change) {
	n := s.network(id)
	b, out := s.ensureRestored(n, name, true)
	return b.id, out
}

// CloseBuffer removes a buffer and everything it held. The network's status
// buffer cannot be closed: it survives every teardown path (v1 invariant).
func (s *Store) CloseBuffer(id model.BufferID) []model.Change {
	n, ok := s.nets[id.Network]
	if !ok {
		return nil
	}
	return s.closeBuffer(n, n.bufs[id.Folded])
}

// RequestJoin records that the user asked to join a channel. Only a join
// recorded here may raise a dialog when it is refused; autojoin and reconnect
// joins must stay quiet.
func (s *Store) RequestJoin(id model.NetworkID, channel string) {
	if channel == "" {
		return
	}
	n := s.network(id)
	n.pendingJoin[n.props.Fold(channel)] = true
}

// RequestPart records that the user asked to leave a channel. The buffer
// normally closes on the server's PART echo, but a server that thinks we are
// not on the channel answers 442/403 instead, and that reply has to close the
// buffer too or it stays open forever.
func (s *Store) RequestPart(id model.BufferID, label string) {
	n, ok := s.nets[id.Network]
	if !ok {
		return
	}
	n.pendingPart[id.Folded] = true
	if label != "" {
		n.labelPart[label] = id.Folded
	}
}

// RequestCycle records a /cycle: the PART that follows keeps its buffer, its
// scrollback and its place in the rail.
func (s *Store) RequestCycle(id model.BufferID) {
	n, ok := s.nets[id.Network]
	if !ok {
		return
	}
	n.keepOnPart[id.Folded] = true
	n.pendingPart[id.Folded] = true
}

// PartLocally closes a buffer with no server round trip - the connection is
// down, so there is nobody to ask.
func (s *Store) PartLocally(id model.BufferID) []model.Change { return s.CloseBuffer(id) }

// TrackWhois records which buffer asked about a nick, so the replies land where
// the question was asked. The entry clears itself on RPL_ENDOFWHOIS. This is
// the per-request correlation that replaces v1's single global slot.
func (s *Store) TrackWhois(id model.NetworkID, nick string, from model.BufferID) {
	if nick == "" {
		return
	}
	n := s.network(id)
	n.whois[n.props.Fold(nick)] = from
}

// Focus marks a buffer as the one the user is looking at: notifications for it
// are suppressed and its read marker moves to the newest message. Pass the zero
// BufferID to blur.
func (s *Store) Focus(id model.BufferID) []model.Change {
	if id.IsZero() {
		for _, n := range s.nets {
			n.focus = model.BufferID{}
		}
		return nil
	}
	n, ok := s.nets[id.Network]
	if !ok {
		return nil
	}
	for _, other := range s.nets {
		if other != n {
			other.focus = model.BufferID{}
		}
	}
	n.focus = id
	b := n.bufs[id.Folded]
	if b == nil {
		return nil
	}
	m, ok := b.lastReadable()
	if !ok {
		return nil
	}
	return s.markRead(b, m.MsgID, m.Time, false)
}

// Blur clears the focused buffer for a network.
func (s *Store) Blur(id model.NetworkID) {
	if n, ok := s.nets[id]; ok {
		n.focus = model.BufferID{}
	}
}

// Focused returns the buffer the user is looking at on a network.
func (s *Store) Focused(id model.NetworkID) model.BufferID {
	if n, ok := s.nets[id]; ok {
		return n.focus
	}
	return model.BufferID{}
}

// MarkRead moves a buffer's read marker locally (focus, scroll-to-bottom). The
// marker only ever moves forward, so a stale call cannot un-read a buffer.
func (s *Store) MarkRead(id model.BufferID, msgid string, at time.Time) []model.Change {
	n, ok := s.nets[id.Network]
	if !ok {
		return nil
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return nil
	}
	return s.markRead(b, msgid, at, false)
}

// MarkReadLatest moves a buffer's marker to its newest message and returns the
// marker so the caller can send MARKREAD.
func (s *Store) MarkReadLatest(id model.BufferID) (msgid string, at time.Time, out []model.Change) {
	n, ok := s.nets[id.Network]
	if !ok {
		return "", time.Time{}, nil
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return "", time.Time{}, nil
	}
	m, ok := b.lastReadable()
	if !ok {
		return "", time.Time{}, nil
	}
	return m.MsgID, m.Time, s.markRead(b, m.MsgID, m.Time, false)
}

// markRead is the shared marker mover.
func (s *Store) markRead(b *buffer, msgid string, at time.Time, remote bool) []model.Change {
	if !b.setMarker(msgid, at) {
		return nil
	}
	return []model.Change{
		model.ReadMarkerMoved{ChangeMeta: b.meta(), MsgID: msgid, At: at, Remote: remote},
		b.unreadChange(),
	}
}

// ClearBuffer drops a buffer's retained messages and its msgid index, so a
// later /history refills it. It is the store half of v1's /clear; the caller
// initiated it, so it also clears its own rows - there is no Change for "the
// view was emptied" and inventing one would put display state in the store.
func (s *Store) ClearBuffer(id model.BufferID) []model.Change {
	b := s.buffer(id)
	if b == nil {
		return nil
	}
	b.msgs = nil
	b.base = 0
	b.pos = map[model.MessageID]int{}
	b.byMsgID = map[string]model.MessageID{}
	b.historyDone = false
	b.unread, b.highlights = 0, 0
	return []model.Change{b.unreadChange()}
}

// SetNotifyLevel sets one buffer's notification level. NotifyDefault puts it
// back on the network's default.
func (s *Store) SetNotifyLevel(id model.BufferID, l NotifyLevel) {
	if b := s.buffer(id); b != nil {
		b.notify = l
	}
}

// NotifyLevelFor returns the level in force for a buffer, resolving
// NotifyDefault against the network.
func (s *Store) NotifyLevelFor(id model.BufferID) NotifyLevel {
	n, ok := s.nets[id.Network]
	if !ok {
		return NotifyMentions
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return n.level(&buffer{})
	}
	return n.level(b)
}

// NotifyLevelDefault returns the level a buffer of this network INHERITS when
// it has none of its own.
//
// It is a different question from NotifyLevelFor, and the menu asks this one:
// with the buffer muted, the level in force is mute and the level it would fall
// back to is whatever the network says. Naming the first beside "Use the
// network's setting" tells the user the network is muted when it is not.
func (s *Store) NotifyLevelDefault(id model.NetworkID) NotifyLevel {
	n, ok := s.nets[id]
	if !ok {
		return NotifyMentions
	}
	return n.level(&buffer{})
}

// NotifyLevelSet returns the level SET on a buffer, without resolving
// NotifyDefault against the network. A menu needs this one: "inherit" is a
// choice a person made and has to be shown as the choice it is, not as the
// level it happens to resolve to today.
func (s *Store) NotifyLevelSet(id model.BufferID) NotifyLevel {
	if b := s.buffer(id); b != nil {
		return b.notify
	}
	return NotifyDefault
}

// SetNetworkNotifyLevel sets the default level for a network's buffers.
func (s *Store) SetNetworkNotifyLevel(id model.NetworkID, l NotifyLevel) {
	s.network(id).defaultNotify = l
}

// SetHideJoinParts overrides join/part/quit visibility for one buffer.
func (s *Store) SetHideJoinParts(id model.BufferID, hidden bool) {
	if b := s.buffer(id); b != nil {
		b.hideJP = &hidden
	}
}

// ClearHideJoinParts puts a buffer back on the network's setting.
func (s *Store) ClearHideJoinParts(id model.BufferID) {
	if b := s.buffer(id); b != nil {
		b.hideJP = nil
	}
}

// SetNetworkHideJoinParts sets the default join/part visibility for a network.
func (s *Store) SetNetworkHideJoinParts(id model.NetworkID, hidden bool) {
	s.network(id).hideJoinParts = hidden
}

// HideJoinParts reports whether join/part/quit lines are suppressed for a
// buffer.
func (s *Store) HideJoinParts(id model.BufferID) bool {
	n, ok := s.nets[id.Network]
	if !ok {
		return false
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return n.hideJoinParts
	}
	return n.hidden(b)
}

// SetNetworkSmartJoinParts turns the smart join/part filter on or off for a
// network (blueprint §4.3; jpfilter.go has the rule). It is a network-wide
// setting on purpose: it is a default about noise, and the per-channel control
// people actually reach for is the hide override, which already exists and is
// evaluated first.
func (s *Store) SetNetworkSmartJoinParts(id model.NetworkID, on bool) {
	s.network(id).smartJP = on
}

// SetNetworkSpeakerWindow sets how recently a nick must have spoken to keep
// their join/part/quit lines visible. Zero restores DefaultSpeakerWindow.
func (s *Store) SetNetworkSpeakerWindow(id model.NetworkID, d time.Duration) {
	s.network(id).speakerWin = d
}

// SmartJoinParts reports whether the smart filter is on for a network.
func (s *Store) SmartJoinParts(id model.NetworkID) bool {
	n, ok := s.nets[id]
	return ok && n.smartJP
}

// SpokeRecently reports whether a nick counts as a recent speaker in a buffer,
// as of now. It is the smart filter's evidence, exposed so a caller can explain
// itself ("you are seeing this because they were talking") rather than for the
// filter to be re-implemented anywhere.
func (s *Store) SpokeRecently(id model.BufferID, nick string) bool {
	n, ok := s.nets[id.Network]
	if !ok {
		return false
	}
	b := n.bufs[id.Folded]
	if b == nil {
		return false
	}
	return b.spokeSince(n.props.Fold(nick), s.now().Add(-n.speakerWindow()))
}
