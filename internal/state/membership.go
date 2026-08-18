package state

import (
	"github.com/toprakkilic/eco/internal/model"
)

// applyJoin reduces a JOIN.
//
// Playback JOINs mutate nothing: v1 learned this the hard way when a
// chathistory replay of a busy channel rebuilt the member list and fired a
// history request per line (the JOIN storm). The one thing a playback JOIN may
// do is leave a status line, because it happened.
func (s *Store) applyJoin(n *network, e model.Join) []model.Change {
	if e.Channel == "" || e.Sender.Nick == "" {
		return nil
	}
	self := n.isSelf(e.Sender)
	b, out := s.ensure(n, e.Channel)

	if e.Playback {
		if !self && !n.hidden(b) {
			out = append(out, s.addStatus(n, b, e.EventMeta, e.Sender.Nick+" joined")...)
		}
		return out
	}

	m := model.Member{
		Nick:     e.Sender.Nick,
		Folded:   n.props.Fold(e.Sender.Nick),
		Account:  firstNonEmpty(e.Account, e.Sender.Account),
		RealName: e.RealName,
	}
	stored, added := b.addMember(m)
	if added {
		out = append(out, model.MembersChanged{
			ChangeMeta: b.meta(), Added: []model.Member{stored}, Bulk: n.bulk(e.EventMeta),
		})
	}
	if self {
		b.joined, b.wantJoin = true, true
		delete(n.pendingJoin, b.id.Folded)
		delete(n.keepOnPart, b.id.Folded)
		out = append(out, s.addStatus(n, b, e.EventMeta, "You joined "+b.name)...)
		return out
	}
	if !n.hidden(b) && !n.bulk(e.EventMeta) {
		out = append(out, s.addStatus(n, b, e.EventMeta, e.Sender.Nick+" joined")...)
	}
	return out
}

// applyPart reduces a PART. Our own PART closes the buffer — unless a /cycle is
// in flight, which keeps the buffer and its scrollback and only empties the
// member list.
func (s *Store) applyPart(n *network, e model.Part) []model.Change {
	if e.Channel == "" || e.Sender.Nick == "" {
		return nil
	}
	b := n.lookup(e.Channel)
	self := n.isSelf(e.Sender)

	if e.Playback {
		if b == nil || self || n.hidden(b) {
			return nil
		}
		return s.addStatus(n, b, e.EventMeta, e.Sender.Nick+" left")
	}
	if b == nil {
		return nil
	}
	if self {
		delete(n.pendingPart, b.id.Folded)
		b.joined = false
		if n.keepOnPart[b.id.Folded] {
			delete(n.keepOnPart, b.id.Folded)
			b.members = map[string]model.Member{}
			out := []model.Change{model.MembersChanged{ChangeMeta: b.meta(), Reset: []model.Member{}}}
			return append(out, s.addStatus(n, b, e.EventMeta, "Rejoining "+b.name)...)
		}
		return s.closeBuffer(n, b)
	}
	folded := n.props.Fold(e.Sender.Nick)
	gone, ok := b.removeMember(folded)
	if !ok {
		return nil
	}
	out := []model.Change{model.MembersChanged{
		ChangeMeta: b.meta(), Removed: []model.Member{gone}, Bulk: n.bulk(e.EventMeta),
	}}
	if !n.hidden(b) && !n.bulk(e.EventMeta) {
		out = append(out, s.addStatus(n, b, e.EventMeta, e.Sender.Nick+" left")...)
	}
	return out
}

// applyQuit removes a nick from every buffer they were in. A quit inside a
// netsplit batch reports one bulk delta per buffer and no status lines, so a
// 200-user split does not write 200 lines into every channel.
func (s *Store) applyQuit(n *network, e model.Quit) []model.Change {
	if e.Playback || e.Sender.Nick == "" {
		return nil
	}
	folded := n.props.Fold(e.Sender.Nick)
	bulk := n.bulk(e.EventMeta)
	var out []model.Change
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil {
			continue
		}
		gone, ok := b.removeMember(folded)
		if !ok {
			continue
		}
		out = append(out, model.MembersChanged{
			ChangeMeta: b.meta(), Removed: []model.Member{gone}, Bulk: bulk,
		})
		if !n.hidden(b) && !bulk {
			out = append(out, s.addStatus(n, b, e.EventMeta, e.Sender.Nick+" quit")...)
		}
	}
	return out
}

// applyKick reduces a KICK. Being kicked ourselves leaves the channel: keeping
// a buffer for a channel we are no longer in is how v1 accumulated dead rooms.
func (s *Store) applyKick(n *network, e model.Kick) []model.Change {
	if e.Channel == "" || e.Target == "" {
		return nil
	}
	b := n.lookup(e.Channel)
	if b == nil {
		return nil
	}
	reason := ""
	if e.Reason != "" {
		reason = " (" + e.Reason + ")"
	}
	if n.isSelfNick(e.Target) && !e.Playback {
		delete(n.pendingPart, b.id.Folded)
		out := s.addStatus(n, n.server(), e.EventMeta,
			"You were kicked from "+b.name+" by "+e.Sender.Nick+reason)
		return append(out, s.closeBuffer(n, b)...)
	}
	folded := n.props.Fold(e.Target)
	gone, ok := b.removeMember(folded)
	var out []model.Change
	if ok {
		out = append(out, model.MembersChanged{ChangeMeta: b.meta(), Removed: []model.Member{gone}})
	}
	return append(out, s.addStatus(n, b, e.EventMeta,
		e.Target+" was kicked by "+e.Sender.Nick+reason)...)
}

// applyNick renames a member everywhere they appear. The member map is keyed by
// the folded nick, so a server that re-cases a nick moves the entry instead of
// leaving the ghost v1 left behind.
func (s *Store) applyNick(n *network, e model.Nick) []model.Change {
	if e.Sender.Nick == "" || e.NewNick == "" {
		return nil
	}
	oldFolded := n.props.Fold(e.Sender.Nick)
	newFolded := n.props.Fold(e.NewNick)
	self := n.isSelf(e.Sender) || n.isSelfNick(e.Sender.Nick)

	var out []model.Change
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil {
			continue
		}
		m, ok := b.members[oldFolded]
		if !ok {
			continue
		}
		delete(b.members, oldFolded)
		m.Nick, m.Folded = e.NewNick, newFolded
		b.members[newFolded] = m
		out = append(out, model.MembersChanged{ChangeMeta: b.meta(), Updated: []model.Member{m}})
		out = append(out, s.addStatus(n, b, e.EventMeta,
			e.Sender.Nick+" is now known as "+e.NewNick)...)
	}
	if self {
		n.nick, n.folded = e.NewNick, newFolded
		out = append(out, model.SelfChanged{
			ChangeMeta: model.NetMeta(n.id), Nick: n.nick, Account: n.account, Away: n.away,
		})
	}
	return out
}

// applyInvite surfaces an INVITE in the network log with the command that
// accepts it — an invite the user cannot act on is just noise.
func (s *Store) applyInvite(n *network, e model.Invite) []model.Change {
	if e.Channel == "" {
		return nil
	}
	if !n.isSelfNick(e.Target) && e.Target != "" {
		return nil
	}
	return s.addStatus(n, n.server(), e.EventMeta,
		e.Sender.Nick+" invited you to "+e.Channel+". Use /join "+e.Channel+" to accept.")
}

// applyNames accumulates RPL_NAMREPLY entries and rebuilds the member list at
// RPL_ENDOFNAMES. The rebuild is one MembersChanged with Reset set, never a
// stream of per-nick deltas.
func (s *Store) applyNames(n *network, e model.Names) []model.Change {
	if e.Channel == "" {
		return nil
	}
	b, out := s.ensure(n, e.Channel)
	folded := b.id.Folded
	if !e.End {
		acc, seen := n.names[folded]
		if !seen {
			acc = []model.Member{}
		}
		for _, entry := range e.Names {
			m := n.props.ParseNamesEntry(entry)
			if m.Folded == "" {
				continue
			}
			acc = append(acc, m)
		}
		n.names[folded] = acc
		return out
	}
	acc, seen := n.names[folded]
	if !seen {
		// ENDOFNAMES with no reply body tells us nothing; wiping the roster on
		// it would empty a channel because someone ran /names elsewhere.
		return out
	}
	delete(n.names, folded)
	members := make(map[string]model.Member, len(acc))
	for _, m := range acc {
		if old, ok := b.members[m.Folded]; ok {
			m.Account = firstNonEmpty(m.Account, old.Account)
			m.RealName = firstNonEmpty(m.RealName, old.RealName)
			m.Away = old.Away
		}
		members[m.Folded] = m
	}
	b.members = members
	reset := make([]model.Member, 0, len(members))
	for _, m := range members {
		reset = append(reset, m)
	}
	sortMembers(n.props.Prefix, reset)
	return append(out, model.MembersChanged{ChangeMeta: b.meta(), Reset: reset})
}

// applyAway reduces away-notify.
func (s *Store) applyAway(n *network, e model.AwayChange) []model.Change {
	if n.isSelf(e.Sender) {
		n.away = e.Away
		return []model.Change{model.SelfChanged{
			ChangeMeta: model.NetMeta(n.id), Nick: n.nick, Account: n.account, Away: n.away,
		}}
	}
	return s.updateMember(n, e.Sender.Nick, func(m *model.Member) bool {
		if m.Away == e.Away {
			return false
		}
		m.Away = e.Away
		return true
	})
}

// applyAccount reduces account-notify. An empty Account means logged out.
func (s *Store) applyAccount(n *network, e model.AccountChange) []model.Change {
	out := s.updateMember(n, e.Sender.Nick, func(m *model.Member) bool {
		if m.Account == e.Account {
			return false
		}
		m.Account = e.Account
		return true
	})
	if n.isSelf(e.Sender) {
		n.account = e.Account
		out = append(out, model.SelfChanged{
			ChangeMeta: model.NetMeta(n.id), Nick: n.nick, Account: n.account, Away: n.away,
		})
	}
	return out
}

// applyChgHost reduces CHGHOST. The store keeps no host per member today, so
// this exists to keep ignore matching honest once it does; it currently only
// refreshes the account we may have learned alongside.
func (s *Store) applyChgHost(n *network, e model.ChgHost) []model.Change {
	return s.updateMember(n, e.Sender.Nick, func(m *model.Member) bool {
		if e.Sender.Account == "" || m.Account == e.Sender.Account {
			return false
		}
		m.Account = e.Sender.Account
		return true
	})
}

// applySetName reduces SETNAME.
func (s *Store) applySetName(n *network, e model.SetName) []model.Change {
	return s.updateMember(n, e.Sender.Nick, func(m *model.Member) bool {
		if m.RealName == e.RealName {
			return false
		}
		m.RealName = e.RealName
		return true
	})
}

// updateMember applies a mutation to one nick in every buffer they are in,
// emitting one MembersChanged per buffer that actually changed.
func (s *Store) updateMember(n *network, nick string, fn func(*model.Member) bool) []model.Change {
	if nick == "" {
		return nil
	}
	folded := n.props.Fold(nick)
	var out []model.Change
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil {
			continue
		}
		m, ok := b.members[folded]
		if !ok {
			continue
		}
		if !fn(&m) {
			continue
		}
		b.members[folded] = m
		out = append(out, model.MembersChanged{ChangeMeta: b.meta(), Updated: []model.Member{m}})
	}
	return out
}

// closeBuffer removes a buffer and reports it gone. Everything hanging off the
// buffer goes with it — including its msgid index, so reopening the buffer
// refetches history instead of silently swallowing the replay (the v1
// characterization test at regression_buffers_test.go inverts here).
func (s *Store) closeBuffer(n *network, b *buffer) []model.Change {
	if b == nil || b.id.IsServer() {
		return nil
	}
	b.wantJoin, b.joined = false, false
	if !n.remove(b) {
		return nil
	}
	return []model.Change{model.BufferClosed{ChangeMeta: b.meta()}}
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
