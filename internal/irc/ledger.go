package irc

import (
	"strconv"
	"sync"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// CmdKind says what an outgoing labelled command was, so that the reply can
// be interpreted without guessing. It is the answer to questions v1 had to
// answer with global state: which buffer asked for this WHOIS, was this 403 a
// refused JOIN or a refused PART, which direction was this history request.
type CmdKind uint8

const (
	CmdOther CmdKind = iota
	CmdMessage
	CmdJoin
	CmdPart
	CmdHistory
	CmdList
	CmdWhois
)

// String returns the debug name of the kind.
func (k CmdKind) String() string {
	switch k {
	case CmdMessage:
		return "message"
	case CmdJoin:
		return "join"
	case CmdPart:
		return "part"
	case CmdHistory:
		return "history"
	case CmdList:
		return "list"
	case CmdWhois:
		return "whois"
	}
	return "other"
}

// Sent is one entry of the labelled-response ledger: what we wrote, so the
// reply that carries the same label can be understood.
type Sent struct {
	Label  string
	Kind   CmdKind
	Target string
	// Direction and Limit are set for CmdHistory: the CHATHISTORY reply
	// arrives as a bare batch that says nothing about which way it walked or
	// whether the server truncated it, so the request has to remember.
	Direction model.HistoryDirection
	Limit     int
	// Lines is how many wire lines the send became (multiline).
	Lines int
	At    time.Time

	// seq is the allocation order, which is how eviction picks what to drop.
	seq uint64
}

// ledgerTTL bounds how long an unanswered label is remembered, and ledgerMax
// bounds the map even if a server answers nothing at all. Both exist because
// this map is fed by the user and drained by the server: without them a
// server that ignores labels leaks one entry per message sent.
const (
	ledgerTTL = 10 * time.Minute
	ledgerMax = 512
	// ledgerSweepEvery keeps the expiry scan off the per-message path: the
	// bound is enforced on every allocation, the clock only every so often.
	ledgerSweepEvery = 64
)

// ledger allocates labels and remembers what each one was for. Writes come
// from whatever goroutine calls a send method; reads come from the decode
// goroutine, so it carries its own mutex - the only lock in this package that
// two goroutines contend for.
type ledger struct {
	clock model.Clock

	mu      sync.Mutex
	seq     uint64
	entries map[string]Sent
}

func newLedger(clock model.Clock) *ledger {
	return &ledger{clock: clock, entries: map[string]Sent{}}
}

// next allocates a label and records what it was attached to.
//
// The label is deliberately not a bare number: the library underneath parses
// numeric labels as its own, and eco wants its correlation to be visibly its
// own. A per-connection sequence would collide across a reconnect, so the
// sequence is per-client and monotonic for the client's whole life.
func (l *ledger) next(s Sent) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	s.seq = l.seq
	s.Label = "e" + strconv.FormatUint(l.seq, 10)
	s.At = l.clock.Now()
	l.sweepLocked(s.At)
	l.entries[s.Label] = s
	return s.Label
}

// lookup returns the entry a label was allocated for.
func (l *ledger) lookup(label string) (Sent, bool) {
	if label == "" {
		return Sent{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.entries[label]
	return s, ok
}

// forget drops an entry once its reply has been fully consumed.
func (l *ledger) forget(label string) {
	if label == "" {
		return
	}
	l.mu.Lock()
	delete(l.entries, label)
	l.mu.Unlock()
}

// len reports how many labels are outstanding (tests assert the ledger does
// not grow without bound).
func (l *ledger) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// sweepLocked keeps the map bounded. It runs on allocation, which is the only
// moment the map can grow.
//
// At the cap it evicts the oldest half by allocation order - it does not clear
// the map. v1 emptied its msgid dedupe map wholesale at a threshold and then
// started duplicating old history; a bound has to evict, not forget everything.
func (l *ledger) sweepLocked(now time.Time) {
	if len(l.entries) >= ledgerMax {
		// seq only ever grows and every entry holds a distinct one, so
		// len(entries) <= l.seq and the subtraction cannot wrap.
		cutoff := l.seq - ledgerMax/2
		for k, v := range l.entries {
			if v.seq <= cutoff {
				delete(l.entries, k)
			}
		}
	}
	if l.seq%ledgerSweepEvery != 0 {
		return
	}
	for k, v := range l.entries {
		if now.Sub(v.At) > ledgerTTL {
			delete(l.entries, k)
		}
	}
}
