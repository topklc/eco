package state

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The archive's half of the intent API: everything that puts state BACK into
// the store from disk, rather than from the wire or from the user.
//
// The rule that makes this safe is the one in model.IDSeq's doc: a store
// backed by an archive is created with `IDs: model.NewIDSeq(maxRowID)`, so
// every id the store mints is above every id the archive can hand back, and a
// restored message's id is its archive rowid. Order and identity therefore
// mean the same thing on both sides of the disk, which is what lets the unread
// derivation (`m.ID > markerID`) work on a window that was half read from a
// file.

// RestoreHeadroom is how far above its scrollback cap a buffer's window may be
// pushed by scroll-up.
//
// The cap governs the LIVE path: how much history a buffer keeps as messages
// arrive on their own. A person reading backwards has asked for more than that,
// and answering "no" because they already have their two thousand lines makes
// scroll-up stop working at exactly the point it becomes useful. The window
// shrinks back to the cap on its own as new messages arrive and trim it; this
// number is only what keeps an afternoon of paging from being unbounded.
const RestoreHeadroom = 5000

// RestoreMessages puts a block of archived messages back into a buffer's
// window. The block is older than everything the buffer already holds - it is
// what the pager returned for "the page before what I have" - so it is
// prepended, and messages the window already holds (by local id or by msgid)
// are dropped rather than doubled.
//
// It returns the same HistoryLoaded a CHATHISTORY BEFORE reply produces, so
// the UI has one way to learn that older rows arrived and not two. complete
// reports whether the archive has more before this block.
//
// The window stays bounded: with a scrollback cap set, only as many messages
// as there is room for are restored (the newest of the block), because the RAM
// window is a cache over the archive and not a second copy of it.
func (s *Store) RestoreMessages(id model.BufferID, msgs []model.Message, complete bool) []model.Change {
	b := s.buffer(id)
	if b == nil || len(msgs) == 0 {
		return nil
	}
	kept := make([]model.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.ID == 0 {
			continue // an unidentified row cannot be deduped or ordered
		}
		if _, held := b.pos[m.ID]; held {
			continue
		}
		if m.MsgID != "" && b.seen(m.MsgID) {
			continue
		}
		// Restored rows are NOT playback. Playback means "the server is saying
		// this again", and it is what stops a replay from badging a buffer or
		// raising a toast; these messages are the buffer's own contents coming
		// back, and the unread count derived against the restored marker is the
		// whole reason for restoring them. Nothing notifies for them because
		// they arrive as HistoryLoaded, which never does.
		m.Buffer = b.id
		kept = append(kept, m)
	}
	if s.scrollback > 0 {
		room := s.scrollback + RestoreHeadroom - len(b.msgs)
		if room <= 0 {
			return nil
		}
		if len(kept) > room {
			kept = kept[len(kept)-room:]
		}
	}
	if len(kept) == 0 {
		return nil
	}

	before := b.unread
	beforeHL := b.highlights
	b.prepend(kept)
	// The marker may have arrived while the buffer was still empty - a cold
	// start restores it before there is anything for it to point at - so it is
	// re-resolved against the window it now has.
	b.markerID = b.resolveMarker(b.markerMsgID, b.markerAt)
	b.recount()

	out := []model.Change{model.HistoryLoaded{
		ChangeMeta: b.meta(),
		Messages:   kept,
		Direction:  model.HistoryBefore,
		Complete:   complete,
	}}
	if b.unread != before || b.highlights != beforeHL {
		out = append(out, b.unreadChange())
	}
	return out
}

// RestoreMarker seeds a buffer's read marker from the archive. It is the same
// forward-only move every other marker path makes (ADR-3: max(server, local)),
// so a stored marker can never un-read something a live message already
// counted.
func (s *Store) RestoreMarker(id model.BufferID, msgid string, at time.Time) []model.Change {
	b := s.buffer(id)
	if b == nil {
		return nil
	}
	return s.markRead(b, msgid, at, false)
}

// Oldest returns a buffer's oldest retained message: the anchor scroll-up pages
// backwards from.
func (s *Store) Oldest(id model.BufferID) (model.Message, bool) {
	b := s.buffer(id)
	if b == nil || len(b.msgs) == 0 {
		return model.Message{}, false
	}
	return b.msgs[0], true
}

// OldestNamed returns the oldest retained message the SERVER can name: the
// oldest one carrying a msgid. It is the anchor a `CHATHISTORY BEFORE` is
// issued with (blueprint §5.4).
//
// It is deliberately not Oldest. A status line eco wrote for itself - "You
// joined #eco" - has no msgid and carries eco's LOCAL clock rather than
// server-time, and it is the oldest row in every buffer that was just joined.
// Anchoring the wire on it degrades the request to a timestamp selector in a
// timebase the two ends do not share: a client whose clock runs fast asks for
// the history before a moment in the future and gets back what it already has,
// and one whose clock runs slow silently skips whatever falls in the gap.
func (s *Store) OldestNamed(id model.BufferID) (model.Message, bool) {
	b := s.buffer(id)
	if b == nil {
		return model.Message{}, false
	}
	for i := range b.msgs {
		if b.msgs[i].MsgID != "" {
			return b.msgs[i], true
		}
	}
	return model.Message{}, false
}

// OldestAnchored returns the oldest retained message that can name a position
// in the archive: one carrying a msgid, or one whose id is at or below base.
//
// base is the last id this store did NOT mint - everything at or below it came
// out of the archive with the archive's own numbering, so its id is a row
// identity and can be used as one. Above it are ids this store invented, which
// mean nothing on disk. A caller that anchored a page on one of those would ask
// for "the messages before row 4,182" and get somebody else's conversation.
func (s *Store) OldestAnchored(id model.BufferID, base model.MessageID) (model.Message, bool) {
	b := s.buffer(id)
	if b == nil {
		return model.Message{}, false
	}
	for i := range b.msgs {
		if b.msgs[i].MsgID != "" || b.msgs[i].ID <= base {
			return b.msgs[i], true
		}
	}
	return model.Message{}, false
}

// MessageCount is how many messages a buffer is holding in RAM. It answers
// "is this buffer cold?" without copying the window to find out.
func (s *Store) MessageCount(id model.BufferID) int {
	b := s.buffer(id)
	if b == nil {
		return 0
	}
	return len(b.msgs)
}

// ForgetList drops a network's in-memory LIST result. A client with an archive
// streams the scan into list_cache as it arrives (blueprint §5.4: LIST results
// are never resident), so once the scan has ended the store's copy is the
// second one and this is what stops it existing.
func (s *Store) ForgetList(id model.NetworkID) {
	if n, ok := s.nets[id]; ok {
		n.list = listCache{}
	}
}

// moveTailToFront rotates the last n messages to the front of the window,
// which is what a CHATHISTORY BEFORE reply needs after the ordinary reducers
// have appended it. It does nothing when the window no longer holds n messages
// - the scrollback cap trimmed part of the block as it landed, and a partial
// rotation would interleave history with live traffic rather than order it.
func (b *buffer) moveTailToFront(n int) {
	if n <= 0 || n > len(b.msgs) || n == len(b.msgs) {
		return
	}
	rotated := make([]model.Message, 0, len(b.msgs))
	rotated = append(rotated, b.msgs[len(b.msgs)-n:]...)
	rotated = append(rotated, b.msgs[:len(b.msgs)-n]...)
	b.msgs = rotated
	b.pos = make(map[model.MessageID]int, len(b.msgs))
	for i, m := range b.msgs {
		b.pos[m.ID] = b.base + i
	}
}

// prepend inserts older messages in front of the retained window. The index is
// rebuilt rather than shifted: pos maps an id to base+offset and only ever
// travels back through at(), so what matters is that the whole map agrees, not
// what the numbers are.
func (b *buffer) prepend(ms []model.Message) {
	if len(ms) == 0 {
		return
	}
	merged := make([]model.Message, 0, len(ms)+len(b.msgs))
	merged = append(merged, ms...)
	merged = append(merged, b.msgs...)
	b.msgs = merged
	b.pos = make(map[model.MessageID]int, len(b.msgs))
	b.byMsgID = make(map[string]model.MessageID, len(b.msgs))
	for i, m := range b.msgs {
		b.pos[m.ID] = b.base + i
		if m.MsgID != "" {
			b.byMsgID[m.MsgID] = m.ID
		}
	}
}
