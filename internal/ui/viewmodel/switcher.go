package viewmodel

import (
	"sort"
	"strings"

	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// The Switcher's model - blueprint §4.1's Ctrl+K overlay and §4.6's "type,
// Enter joins the top match".
//
// It is ONE surface for two questions that were two surfaces in v1: "take me to
// #eco" and "put me in #eco". A buffer you are in and a channel you are not are
// the same row here, ranked against each other, and the only difference is what
// Enter does - which is why browser.go is gone rather than moved.
//
// Everything below is a pure function of the buffers, the LIST-cache
// candidates the app hands in, the recency order and the query, so the whole of
// it is a table test with no toolkit in the process (switcher_test.go).
// internal/ui/widgets draws the result and decides nothing.

// SwitchKind is what one row of the Switcher stands for, and therefore what
// Enter on it does.
type SwitchKind uint8

const (
	// SwitchBuffer is a buffer that already exists: showing it is a switch.
	SwitchBuffer SwitchKind = iota
	// SwitchChannel is a channel from the network's LIST cache that we are not
	// in: choosing it joins.
	SwitchChannel
	// SwitchCreate is the "Create #name" row, offered when the typed name is
	// not a channel anybody has heard of. It is browser.go's create
	// affordance, promoted (§4.6).
	SwitchCreate
)

// Candidate is one row of the Switcher.
type Candidate struct {
	Kind SwitchKind

	// Session is the network the row belongs to. A SwitchBuffer carries it in
	// Ref as well; a SwitchChannel and a SwitchCreate have only this.
	Session dispatch.SessionID
	// Ref names the buffer a SwitchBuffer shows.
	Ref Ref
	// Name is what the row says: the buffer or channel name.
	Name string
	// Network is the profile name, drawn beside the name when there is more
	// than one network to tell apart.
	Network string
	// Users and Topic come from the LIST cache; they are zero for a buffer.
	Users int
	Topic string
	// Counts is the buffer's badge, suppressed for the one on screen exactly
	// as the rail's is.
	Counts Counts

	// Score is the ranking this candidate was sorted on. It is exported so a
	// test can say WHY an order is what it is rather than only that it is.
	Score int
}

// SwitchLimit is how many rows the Switcher offers. It is a screenful and a
// bit: past that the answer is a better query, not more scrolling.
const SwitchLimit = 60

// Switch ranks the buffers and the offered channels against a query and returns
// the rows to draw, best first.
//
// extra is what the app read out of the LIST cache - the channels this client
// is NOT in. They are ranked in the same list as the buffers rather than in a
// second pane, which is the whole point: "join #something" and "switch to
// #something" are one gesture and one keystroke (§4.6).
//
// A channel already open on the same network is dropped from extra, because a
// buffer and the LIST row that named it are one thing to the person typing.
func (m *Model) Switch(query string, extra []Candidate) []Candidate {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Candidate, 0, len(extra)+16)

	open := map[string]bool{}
	for _, s := range m.sessions {
		for _, name := range s.buffers {
			r := Ref{Session: s.id, Name: name}
			open[string(s.id)+"\x00"+strings.ToLower(name)] = true
			c := Candidate{
				Kind:    SwitchBuffer,
				Session: s.id,
				Ref:     r,
				Name:    name,
				Network: s.profile,
				Counts:  m.Badge(r),
			}
			if score, ok := m.score(c, q); ok {
				c.Score = score
				out = append(out, c)
			}
		}
	}
	for _, c := range extra {
		if c.Name == "" || open[string(c.Session)+"\x00"+strings.ToLower(c.Name)] {
			continue
		}
		c.Kind = SwitchChannel
		c.Ref = Ref{}
		if c.Network == "" {
			c.Network = m.Profile(c.Session)
		}
		if score, ok := m.score(c, q); ok {
			c.Score = score
			out = append(out, c)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return m.less(out[i], out[j]) })
	if len(out) > SwitchLimit {
		out = out[:SwitchLimit]
	}
	if c, ok := m.createRow(query, out); ok {
		out = append(out, c)
	}
	return out
}

// createRow is the "Create #name" offer. It appears only when the typed text
// names a channel nothing else in the list matches EXACTLY - with "dev" typed
// and "#developers" listed, the offer is still "Create #dev", because joining
// the top match is what Enter already does and the button must never mean the
// same thing twice (browser.go's rule, kept verbatim).
func (m *Model) createRow(query string, ranked []Candidate) (Candidate, bool) {
	name := ChannelName(query)
	if name == "" || len(m.sessions) == 0 {
		return Candidate{}, false
	}
	for _, c := range ranked {
		if c.Kind != SwitchCreate && strings.EqualFold(c.Name, name) {
			return Candidate{}, false
		}
	}
	cur := m.cur.Session
	if cur == "" {
		cur = m.sessions[len(m.sessions)-1].id
	}
	return Candidate{
		Kind:    SwitchCreate,
		Session: cur,
		Name:    name,
		Network: m.Profile(cur),
	}, true
}

// ChannelName is the channel the user meant by what they typed: "eco" and
// "#eco" are both #eco, and anything with a space in it is not a channel name
// at all. It is exported because the app needs the same answer when it joins.
func ChannelName(query string) string {
	q := strings.TrimSpace(query)
	if q == "" || strings.ContainsAny(q, " \t,") {
		return ""
	}
	if strings.HasPrefix(q, "#") || strings.HasPrefix(q, "&") {
		if len(q) == 1 {
			return ""
		}
		return q
	}
	return "#" + q
}

// --- the ranking -----------------------------------------------------------

// The score bands. They are far enough apart that no bonus can lift a worse
// kind of match over a better one: a substring hit never outranks a prefix hit
// however recently it was read, because "what I typed starts this name" is a
// stronger statement about intent than anything else here.
const (
	scoreExact       = 1000
	scorePrefix      = 800
	scoreSigilPrefix = 700 // "eco" against "#eco": the sigil is not typed
	scoreWordStart   = 600
	scoreSubstring   = 500
	scoreSubsequence = 300
)

// kindBonus puts an open buffer above a channel that only the LIST knows
// about, at equal match quality. Being in a room is evidence.
//
// It is 50 and not 60 because of one number: kindBonus + recencyTop is the most
// any row can be lifted, and the bands are 100 apart. At 60 the maximum lift
// was exactly one band - which does not CROSS a band but lands on it, and a tie
// is then broken by kind, in favour of the row that was lifted. Typing "eco"
// with #the-eco-room open and recent put it above #ecosystem, which is what the
// band order exists to forbid. The bonuses now have to fit strictly inside a
// band, and the arithmetic is asserted in switcher_test.go rather than left as
// this sentence.
const kindBonus = 50

// score grades one candidate against the query, and reports whether it matches
// at all. An empty query matches everything with score zero, which leaves the
// order entirely to recency - the useful answer to Ctrl+K on its own.
func (m *Model) score(c Candidate, q string) (int, bool) {
	if q == "" {
		return m.recencyBonus(c), true
	}
	score, ok := matchScore(strings.ToLower(c.Name), q)
	if !ok {
		return 0, false
	}
	if c.Kind == SwitchBuffer {
		score += kindBonus
	}
	return score + m.recencyBonus(c), true
}

// matchScore grades how well a name answers a query. The bands are ordered by
// how much the match says about what the user meant, and the fuzzy band is last
// because a subsequence match is a guess.
func matchScore(name, q string) (int, bool) {
	switch {
	case name == q:
		return scoreExact, true
	case strings.HasPrefix(name, q):
		return scorePrefix, true
	}
	if bare := strings.TrimLeft(name, "#&"); bare != name && strings.HasPrefix(bare, q) {
		return scoreSigilPrefix, true
	}
	if i := strings.Index(name, q); i >= 0 {
		if wordStart(name, i) {
			return scoreWordStart, true
		}
		// an earlier hit is a better hit, but never enough to cross a band
		return scoreSubstring - penalty(i), true
	}
	if gaps, ok := subsequence(name, q); ok {
		return scoreSubsequence - penalty(gaps), true
	}
	return 0, false
}

// wordStart reports whether position i begins a word - the boundary a person
// reads as "it starts with that": after a separator, or after the sigil.
func wordStart(name string, i int) bool {
	if i == 0 {
		return true
	}
	switch name[i-1] {
	case '#', '&', '-', '_', '.', ' ', '/', '|', '[':
		return true
	}
	return false
}

// penalty turns "how far off" into points, capped so it can never sink a
// candidate into the band below.
func penalty(n int) int {
	if n > 90 {
		n = 90
	}
	return n
}

// subsequence reports whether every rune of q appears in name in order, and how
// many times the run was broken to do it. Fewer breaks is a better guess:
// "mesg" against "#messages" (one break) beats "mesg" against "#my-easy-song".
func subsequence(name, q string) (gaps int, ok bool) {
	if q == "" {
		return 0, true
	}
	qr := []rune(q)
	j := 0
	contiguous := false
	for _, r := range name {
		if r != qr[j] {
			contiguous = false
			continue
		}
		if j > 0 && !contiguous {
			gaps++
		}
		contiguous = true
		j++
		if j == len(qr) {
			return gaps, true
		}
	}
	return 0, false
}

// recencyBonus is the second half of §4.1's "fuzzy-ranked as you type (recency
// + match quality)". Only the last few buffers get anything at all: a list
// where every buffer carries a different tiny number is a list ordered by
// history rather than by the query.
func (m *Model) recencyBonus(c Candidate) int {
	if c.Kind != SwitchBuffer {
		return 0
	}
	rank, ok := m.recency(c.Ref)
	if !ok {
		return 0
	}
	if bonus := recencyTop - rank*recencyStep; bonus > 0 {
		return bonus
	}
	return 0
}

const (
	recencyTop  = 40 // the buffer you were in a moment ago
	recencyStep = 8  // ...and how fast that fades, over five buffers
)

// less is the total order the Switcher draws in. Every tiebreak below the score
// exists so the same query always produces the same list: score, then an open
// buffer before a channel, then the more populated channel, then the name, then
// the network. Two rows can only tie on all five by being the same row.
func (m *Model) less(a, b Candidate) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Kind == SwitchChannel && a.Users != b.Users {
		return a.Users > b.Users
	}
	if !strings.EqualFold(a.Name, b.Name) {
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	}
	return a.Network < b.Network
}

// --- recency ----------------------------------------------------------------

// recentDepth is how many buffers back the Switcher remembers. It is a couple
// past where the bonus reaches zero, and it is a hard cap rather than a map
// that grows for the life of the process: "recent" is a handful of buffers, and
// the ranking has to stay cheap enough to run on every keystroke.
const recentDepth = 8

// noteVisit records that a buffer was shown, most recent first. It is called
// from Select, which is the one path a buffer reaches the screen through, so
// the Switcher's idea of "recent" cannot drift from what actually happened.
func (m *Model) noteVisit(key string) {
	if key == "" {
		return
	}
	for i, k := range m.recent {
		if k == key {
			m.recent = append(m.recent[:i], m.recent[i+1:]...)
			break
		}
	}
	m.recent = append([]string{key}, m.recent...)
	if len(m.recent) > recentDepth {
		m.recent = m.recent[:recentDepth]
	}
}

// recency returns how many buffers back this one was last shown - 0 is the one
// on screen - and whether it was shown recently enough to be remembered.
func (m *Model) recency(r Ref) (int, bool) {
	want := m.key(r)
	for i, k := range m.recent {
		if k == want {
			return i, true
		}
	}
	return 0, false
}
