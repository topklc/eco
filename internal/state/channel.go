package state

import (
	"strconv"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// applyTopic reduces a TOPIC command or RPL_TOPIC. A topic assertion with a
// sender also leaves a status line; the numeric form (learned on join) does
// not, because nobody just changed it.
//
// The topic is stored exactly as the wire carried it, formatting codes and all.
// v1 stripped them here; in v2 stripping is the render layer's job, so the
// store never destroys data the UI may want to show.
func (s *Store) applyTopic(n *network, e model.Topic) []model.Change {
	if e.Channel == "" {
		return nil
	}
	b, out := s.ensure(n, e.Channel)
	b.topic, b.topicKnown = e.Topic, true
	if e.Cleared {
		b.topic = ""
	}
	if e.SetBy != "" {
		b.topicSetBy = e.SetBy
	} else if e.Sender.Nick != "" {
		b.topicSetBy = e.Sender.Nick
	}
	if !e.SetAt.IsZero() {
		b.topicSetAt = e.SetAt
	} else if !e.Sender.IsZero() {
		b.topicSetAt = s.eventTime(e.EventMeta)
	}
	out = append(out, model.TopicChanged{
		ChangeMeta: b.meta(), Topic: b.topic, SetBy: b.topicSetBy,
		SetAt: b.topicSetAt, Cleared: e.Cleared,
	})
	if e.Sender.IsZero() {
		return out
	}
	text := e.Sender.Nick + " set the topic: " + e.Topic
	if e.Cleared {
		text = e.Sender.Nick + " cleared the topic"
	}
	return append(out, s.addStatus(n, b, e.EventMeta, text)...)
}

// applyMode reduces a MODE change or RPL_CHANNELMODEIS (Full).
//
// The delta is parsed against the network's CHANMODES and PREFIX, so a member
// mode updates that member's prefixes directly. v1 could not tell the classes
// apart and answered every mode change with a NAMES — a self-inflicted flood on
// a busy channel, and a full roster rebuild for one +v.
func (s *Store) applyMode(n *network, e model.Mode) []model.Change {
	if e.Target == "" {
		return nil
	}
	if !n.props.IsChannel(e.Target) {
		return s.applyUserMode(n, e)
	}
	b, out := s.ensure(n, e.Target)

	params := append([]string{e.ModeString}, e.Args...)
	changes, _ := proto.ParseModeDelta(n.props, params)
	if len(changes) == 0 {
		return out
	}
	if e.Full {
		// 324 is the authoritative full state: it replaces what we hold rather
		// than amending it, or a stale +k survives forever.
		b.modes, b.key, b.limit = "", "", 0
	}

	var updated []model.Member
	channelChanged := false
	for _, c := range changes {
		switch c.Class {
		case proto.ModeClassMember:
			if m, ok := s.applyMemberMode(n, b, c); ok {
				updated = append(updated, m)
			}
		case proto.ModeClassKey:
			channelChanged = true
			if c.Add {
				b.key = c.Param
			} else {
				b.key = ""
			}
		case proto.ModeClassLimit:
			channelChanged = true
			if c.Add {
				b.limit, _ = strconv.Atoi(c.Param)
			} else {
				b.limit = 0
			}
		case proto.ModeClassList:
			// Ban/except/invex lists are not held in state yet; the parameter
			// was consumed, which is what keeps the rest of the delta aligned.
		default:
			channelChanged = true
		}
	}
	b.modes = proto.ApplyFlags(b.modes, changes)
	if channelChanged || e.Full {
		out = append(out, model.ModeChanged{
			ChangeMeta: b.meta(), Modes: b.modes, Key: b.key,
			Limit: b.limit, ByNick: e.Sender.Nick,
		})
	}
	if len(updated) > 0 {
		out = append(out, model.MembersChanged{ChangeMeta: b.meta(), Updated: updated})
	}
	if e.Full || e.Sender.IsZero() {
		return out
	}
	text := "mode " + strings.TrimSpace(e.ModeString+" "+strings.Join(e.Args, " ")) + " by " + e.Sender.Nick
	return append(out, s.addStatus(n, b, e.EventMeta, text)...)
}

// applyMemberMode grants or removes one membership prefix.
func (s *Store) applyMemberMode(n *network, b *buffer, c proto.ModeChange) (model.Member, bool) {
	if c.Param == "" || c.Prefix == 0 {
		return model.Member{}, false
	}
	folded := n.props.Fold(c.Param)
	m, ok := b.members[folded]
	if !ok {
		// A mode for someone we have not seen join: record them, so the roster
		// is right even when we missed the JOIN.
		m = model.Member{Nick: c.Param, Folded: folded}
	}
	before := m.Prefixes
	if c.Add {
		if !strings.ContainsRune(m.Prefixes, rune(c.Prefix)) {
			m.Prefixes = n.props.Prefix.Sort(m.Prefixes + string(c.Prefix))
		}
	} else {
		m.Prefixes = n.props.Prefix.Sort(strings.ReplaceAll(m.Prefixes, string(c.Prefix), ""))
	}
	if ok && m.Prefixes == before {
		return model.Member{}, false
	}
	b.members[folded] = m
	return m, true
}

// applyUserMode reduces a MODE addressed to a nick. There is nothing to track —
// user modes are the server's business — but the line belongs in the log.
func (s *Store) applyUserMode(n *network, e model.Mode) []model.Change {
	if e.ModeString == "" || !n.isSelfNick(e.Target) {
		return nil
	}
	text := "mode " + strings.TrimSpace(e.ModeString+" "+strings.Join(e.Args, " "))
	if e.Sender.Nick != "" && !n.isSelf(e.Sender) {
		text += " by " + e.Sender.Nick
	}
	return s.addStatus(n, n.server(), e.EventMeta, text)
}

// applyRename reduces draft/channel-rename. The buffer keeps its identity —
// same messages, same members, same unread — under a new name and a new folded
// key, and BufferRenamed tells the UI to migrate everything it keyed by the old
// id (draft text, scroll anchor).
func (s *Store) applyRename(n *network, e model.ChannelRenamed) []model.Change {
	if e.From == "" || e.To == "" {
		return nil
	}
	b := n.lookup(e.From)
	if b == nil {
		return nil
	}
	from := b.id
	newFolded := n.props.Fold(e.To)
	if newFolded == from.Folded {
		b.name = e.To
		return nil
	}
	if _, taken := n.bufs[newFolded]; taken {
		// Renaming onto a buffer that already exists would silently merge two
		// conversations; refuse and say so.
		return s.addStatus(n, b, e.EventMeta, "Channel renamed to "+e.To+", which is already open.")
	}
	delete(n.bufs, from.Folded)
	for i, k := range n.order {
		if k == from.Folded {
			n.order[i] = newFolded
			break
		}
	}
	b.id = model.BufferID{Network: n.id, Folded: newFolded}
	b.name = e.To
	n.bufs[newFolded] = b
	migrateKey(n.pendingJoin, from.Folded, newFolded)
	migrateKey(n.pendingPart, from.Folded, newFolded)
	migrateKey(n.keepOnPart, from.Folded, newFolded)
	if acc, ok := n.names[from.Folded]; ok {
		delete(n.names, from.Folded)
		n.names[newFolded] = acc
	}
	if n.focus == from {
		n.focus = b.id
	}
	for i := range b.msgs {
		b.msgs[i].Buffer = b.id
	}
	out := []model.Change{model.BufferRenamed{ChangeMeta: b.meta(), From: from, Name: b.name}}
	text := "Channel renamed from " + e.From + " to " + e.To
	if e.Reason != "" {
		text += " (" + e.Reason + ")"
	}
	return append(out, s.addStatus(n, b, e.EventMeta, text)...)
}

// migrateKey moves a set entry from one folded key to another.
func migrateKey(m map[string]bool, from, to string) {
	if m[from] {
		delete(m, from)
		m[to] = true
	}
}
