package state

import (
	"sort"

	"github.com/toprakkilic/eco/internal/model"
)

// Apply folds one event into the store and returns the facts that changed. It
// is the only way wire traffic enters the store, it never blocks, and it is
// total: an event the store does not model produces no changes.
//
// The returned slice is ordered so a consumer can apply it blindly: a buffer's
// BufferCreated always precedes anything that refers to that buffer.
func (s *Store) Apply(ev model.Event) []model.Change {
	if ev == nil {
		return nil
	}
	meta := ev.Meta()
	if meta.Network == "" {
		return nil
	}
	n := s.network(meta.Network)
	out := s.expireTyping(n)
	return append(out, s.apply(n, ev)...)
}

// ApplyAll folds a slice of events in order, concatenating their changes. It is
// the shape the dispatcher drains a batch with.
func (s *Store) ApplyAll(evs []model.Event) []model.Change {
	var out []model.Change
	for _, ev := range evs {
		out = append(out, s.Apply(ev)...)
	}
	return out
}

// apply is the reducer table. Every case is a pure function of (network state,
// event) -> (new state, changes).
func (s *Store) apply(n *network, ev model.Event) []model.Change {
	switch e := ev.(type) {
	// messages
	case model.PrivMsg:
		return s.applyPrivMsg(n, e)
	case model.Notice:
		return s.applyNotice(n, e)
	case model.Typing:
		return s.applyTyping(n, e)
	case model.Redact:
		return s.applyRedact(n, e)
	case model.MarkRead:
		return s.applyMarkRead(n, e)

	// membership
	case model.Join:
		return s.applyJoin(n, e)
	case model.Part:
		return s.applyPart(n, e)
	case model.Quit:
		return s.applyQuit(n, e)
	case model.Kick:
		return s.applyKick(n, e)
	case model.Nick:
		return s.applyNick(n, e)
	case model.Invite:
		return s.applyInvite(n, e)
	case model.Names:
		return s.applyNames(n, e)
	case model.AwayChange:
		return s.applyAway(n, e)
	case model.AccountChange:
		return s.applyAccount(n, e)
	case model.ChgHost:
		return s.applyChgHost(n, e)
	case model.SetName:
		return s.applySetName(n, e)

	// channel state
	case model.Topic:
		return s.applyTopic(n, e)
	case model.Mode:
		return s.applyMode(n, e)
	case model.ChannelRenamed:
		return s.applyRename(n, e)

	// protocol plumbing
	case model.BatchOpen:
		n.batches.Open(e.Ref, e.Type, e.Params, e.Parent) //nolint:errcheck // a duplicate ref is the server's problem; the tracker keeps the first
		return nil
	case model.BatchClose:
		n.batches.Close(e.Ref) //nolint:errcheck // closing an unknown ref is a no-op
		return nil
	case model.CapChange:
		return s.applyCapChange(n, e)
	case model.ISupport:
		return s.applyISupport(n, e)
	case model.ConnState:
		return s.applyConnState(n, e)
	case model.AuthResult:
		return s.applyAuthResult(n, e)
	case model.JoinRefused:
		return s.applyJoinRefused(n, e)
	case model.ListItem:
		return s.applyListItem(n, e)
	case model.ListEnd:
		return s.applyListEnd(n, e)
	case model.HistoryChunk:
		return s.applyHistory(n, e)
	case model.HistoryTarget:
		return s.applyHistoryTarget(n, e)
	case model.Numeric:
		return s.applyNumeric(n, e)
	case model.ServerError:
		return s.applyServerError(n, e)
	case model.Raw:
		return nil // the raw log is the UI's tap, not state
	}
	return nil
}

// expireTyping drops typing indicators that timed out. It runs before every
// event so expiry is driven by the injected clock rather than a timer
// goroutine — the store owns no goroutines.
func (s *Store) expireTyping(n *network) []model.Change {
	if !n.hasTyping {
		return nil // the common case: nobody is typing anywhere on this network
	}
	var out []model.Change
	now := s.now()
	n.hasTyping = false
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil || len(b.typing) == 0 {
			continue
		}
		var stale []string
		for folded, t := range b.typing {
			if now.Sub(t.at) >= s.typingTimeout {
				stale = append(stale, folded)
			}
		}
		if len(stale) < len(b.typing) {
			n.hasTyping = true // something survives; keep scanning next time
		}
		sort.Strings(stale) // map order must never reach the change stream
		for _, folded := range stale {
			t := b.typing[folded]
			delete(b.typing, folded)
			out = append(out, model.TypingChanged{
				ChangeMeta: b.meta(), Nick: t.nick, State: model.TypingDone,
			})
		}
	}
	return out
}
