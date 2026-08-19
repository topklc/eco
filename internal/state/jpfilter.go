package state

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The smart join/part filter — blueprint §4.3's "this is what makes busy
// channels readable", and M12's third slice.
//
// The problem it solves is not that join/part lines are uninteresting. It is
// that they are interesting about PEOPLE YOU KNOW and noise about everyone
// else: in a 900-member channel the connection churn of strangers buries the
// conversation, and v1's only answer was the blunt one — hide them all, and
// with them the fact that the person you were just talking to left.
//
// So the rule is WeeChat's, and it is a rule about the nick rather than about
// the channel: a join, a part or a quit is shown when that nick has said
// something in this buffer recently. Everything else about the existing
// controls survives — the global "hide join/part/quit" preference and the
// per-channel override still mean exactly what they meant, and they are
// evaluated FIRST, because "hide them here" is an instruction and not a
// preference about noise.
//
// Three properties are worth stating because each is one line from being lost:
//
//   - The decision is made once, when the line would be added. A nick who joins
//     and then speaks does not get their join line back retroactively; the line
//     was never written. (WeeChat rewrites the buffer for this. Rewriting
//     history to insert a line above messages the reader has already read is a
//     worse surprise than the missing line.)
//   - Speech is recorded per BUFFER, not per network. Someone chatting in #eco
//     is not thereby interesting when they part #linux.
//   - The table is bounded (speakerCap) and prunes by age, so a channel that
//     has seen fifty thousand speakers costs a bounded map rather than one
//     entry per person who ever typed.

// DefaultSpeakerWindow is how long a nick stays "recently spoke". Ten minutes
// is long enough to cover a conversation with a pause in it and short enough
// that a busy channel does not accumulate a hundred interesting strangers.
const DefaultSpeakerWindow = 10 * time.Minute

// speakerCap bounds one buffer's speaker table. It is generous — a channel with
// more than this many distinct people talking inside one window is one where
// the filter has nothing left to protect — and the prune below is what keeps it
// there.
const speakerCap = 512

// noteSpeaker records that a nick said something in this buffer at a time.
// Called for every real message, from addMessage, so there is one place where
// "spoke" is defined and it is the same place messages are counted.
func (b *buffer) noteSpeaker(folded string, at time.Time, window time.Duration) {
	if folded == "" || at.IsZero() {
		return
	}
	if b.spoke == nil {
		b.spoke = map[string]time.Time{}
	}
	// Keep the newest word: a message arriving out of order (a playback page
	// folded in behind live traffic) must not make a current speaker look stale.
	if prev, ok := b.spoke[folded]; ok && prev.After(at) {
		return
	}
	b.spoke[folded] = at
	if len(b.spoke) > speakerCap {
		b.pruneSpeakers(at.Add(-window))
	}
}

// pruneSpeakers drops everyone whose last word is older than the cutoff. If
// that leaves the table still over the cap — every entry is inside the window,
// which means the channel really does have that many live speakers — it is
// emptied rather than grown: the filter's answer degrades to "show the line",
// which is the safe direction, and the next message refills it.
func (b *buffer) pruneSpeakers(cutoff time.Time) {
	for k, at := range b.spoke {
		if at.Before(cutoff) {
			delete(b.spoke, k)
		}
	}
	if len(b.spoke) > speakerCap {
		b.spoke = map[string]time.Time{}
	}
}

// spokeSince reports whether a nick has spoken in this buffer at or after the
// cutoff.
func (b *buffer) spokeSince(folded string, cutoff time.Time) bool {
	if folded == "" || b.spoke == nil {
		return false
	}
	at, ok := b.spoke[folded]
	return ok && !at.Before(cutoff)
}

// speakerWindow is the network's recency window, defaulted.
func (n *network) speakerWindow() time.Duration {
	if n.speakerWin > 0 {
		return n.speakerWin
	}
	return DefaultSpeakerWindow
}

// showJoinPart is the whole of the filter: whether one nick's join, part or
// quit becomes a status line in this buffer.
//
// The order is deliberate and is the same order §4.4 gives the notification
// pipeline: the explicit instruction first, the heuristic second. "Hide
// joins/parts here" is a thing the user said about this channel and it wins;
// the smart filter is a default about noise and only ever gets to speak when
// nothing louder has.
func (s *Store) showJoinPart(n *network, b *buffer, nick string, meta model.EventMeta) bool {
	if b == nil || n.hidden(b) {
		return false
	}
	if !n.smartJP {
		return true
	}
	// The server buffer and private conversations have no churn worth filtering:
	// a QUIT in a query is the person you are talking to, and it is the whole
	// reason the line exists.
	if b.kind != model.BufferChannel {
		return true
	}
	return b.spokeSince(n.props.Fold(nick), s.eventTime(meta).Add(-n.speakerWindow()))
}
