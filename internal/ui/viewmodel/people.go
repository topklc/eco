package viewmodel

import (
	"strings"

	"github.com/toprakkilic/eco/internal/model"
)

// The People pane's row model — blueprint §4.1: a member list with a filter
// box, sectioned by prefix rank, away-dimmed, with a registered badge.
//
// # The ranks are the server's, not ours
//
// v1 wrote "~&@%+" into four places and called it the rank order. It is not:
// ISUPPORT PREFIX says which modes a network has and in what order, and a
// server with PREFIX=(Yohv)!@%+ has a rank v1 could not name and an order it
// would have got wrong. The table comes in through SetPrefixes — proto parsed
// it, the store keeps it, the app hands it over — and everything below ranks
// and titles members from that. The one thing written down here is the ENGLISH
// for the five modes RFC-adjacent servers agree on; an unknown mode is titled
// with its own prefix character, which is at least true.
//
// # Sections appear only when they mean something
//
// A channel where nobody is opped would otherwise get one header reading
// "Everyone else" over the entire list — a label that separates nothing from
// nothing. One section means no headers at all (chrome disappears until it is
// needed), which is the same rule the rail applies to network headers.

// PeopleRowKind distinguishes the two kinds of row in the pane.
type PeopleRowKind uint8

const (
	// PeopleSection is a rank heading: "Operators", and how many.
	PeopleSection PeopleRowKind = iota
	// PeopleMember is one nick.
	PeopleMember
)

// PeopleRow is one entry in the People list: a section heading or a member.
// Rows are values, not widgets, for the Timeline's reason — the widget has a
// handful of row widgets and pours these through them.
type PeopleRow struct {
	Kind PeopleRowKind
	// Title and Count name a section: the rank, and how many are in it.
	Title string
	Count int
	// Member is the nick a PeopleMember row draws, exactly as the store
	// ranked it: prefixes, account (the registered badge) and away
	// (away-notify) included.
	Member model.Member
}

// rankTitles is the English for the membership modes that mean the same thing
// nearly everywhere. Anything else is titled with its prefix character.
var rankTitles = map[byte]string{
	'q': "Owners",
	'a': "Admins",
	'o': "Operators",
	'h': "Half-ops",
	'v': "Voiced",
}

// restTitle heads the members with no prefix at all.
const restTitle = "Everyone else"

// SetPrefixes records the network's PREFIX table: the membership mode letters
// and the prefix characters that grant them, highest rank first. Empty means
// "this network has told us nothing", and everyone ranks the same.
func (m *Model) SetPrefixes(modes, prefixes string) {
	if m.prefixModes == modes && m.prefixChars == prefixes {
		return
	}
	m.prefixModes, m.prefixChars = modes, prefixes
	m.peopleDirty = true
}

// SetMembers records the roster the People pane draws. It takes the slice as
// given: the session hands out a fresh one per call.
func (m *Model) SetMembers(ms []model.Member) {
	m.members = ms
	m.peopleDirty = true
}

// Members returns the roster of the buffer on screen, unfiltered and in the
// order the store ranked it.
func (m *Model) Members() []model.Member { return m.members }

// MemberCount is how many people are in the buffer on screen, whatever the
// filter box says.
func (m *Model) MemberCount() int { return len(m.members) }

// SetPeopleFilter narrows the pane to the nicks containing s.
func (m *Model) SetPeopleFilter(s string) {
	if m.filter == s {
		return
	}
	m.filter = s
	m.peopleDirty = true
}

// PeopleFilter returns the filter box's text.
func (m *Model) PeopleFilter() string { return m.filter }

// PeopleRows returns the pane's rows — sections and members, filtered — rebuilt
// only when the roster, the filter or the PREFIX table moved. The slice belongs
// to the model and is valid until the next intent.
//
// The cache is not an optimisation detail: widget.List asks for the length and
// for rows many times per repaint, and a three-thousand-nick channel rebuilt
// per question is the whole of the People pane's budget spent on nothing.
func (m *Model) PeopleRows() []PeopleRow {
	if m.peopleDirty {
		m.buildPeople()
	}
	return m.peopleRows
}

// PeopleRowCount is len(PeopleRows) without the copy at the call site.
func (m *Model) PeopleRowCount() int { return len(m.PeopleRows()) }

// PeopleRow returns one row by index.
func (m *Model) PeopleRow(i int) (PeopleRow, bool) {
	rows := m.PeopleRows()
	if i < 0 || i >= len(rows) {
		return PeopleRow{}, false
	}
	return rows[i], true
}

// buildPeople sections the roster by rank and drops what the filter excludes.
func (m *Model) buildPeople() {
	m.peopleRows = m.peopleRows[:0]
	m.peopleDirty = false

	want := strings.ToLower(strings.TrimSpace(m.filter))
	buckets := make([][]model.Member, len(m.prefixChars)+1)
	filled := 0
	for _, mem := range m.members {
		if want != "" && !strings.Contains(strings.ToLower(mem.Nick), want) {
			continue
		}
		r := m.rank(mem.Prefixes)
		if len(buckets[r]) == 0 {
			filled++
		}
		buckets[r] = append(buckets[r], mem)
	}
	for i, b := range buckets {
		if len(b) == 0 {
			continue
		}
		if filled > 1 {
			m.peopleRows = append(m.peopleRows,
				PeopleRow{Kind: PeopleSection, Title: m.sectionTitle(i), Count: len(b)})
		}
		for _, mem := range b {
			m.peopleRows = append(m.peopleRows, PeopleRow{Kind: PeopleMember, Member: mem})
		}
	}
}

// rank is the index of a member's HIGHEST prefix in the network's table, or
// len(prefixes) for someone with none. The store already sorts a member's
// prefixes by rank, so the first would do — the minimum is taken anyway
// because "already sorted" is an invariant of another package, and getting it
// wrong here would file an op under "voiced".
func (m *Model) rank(prefixes string) int {
	best := len(m.prefixChars)
	for i := 0; i < len(prefixes); i++ {
		if at := strings.IndexByte(m.prefixChars, prefixes[i]); at >= 0 && at < best {
			best = at
		}
	}
	return best
}

// sectionTitle names one rank: the English for its mode where there is one,
// its prefix character where there is not, and the catch-all at the end.
func (m *Model) sectionTitle(rank int) string {
	if rank >= len(m.prefixChars) {
		return restTitle
	}
	if rank < len(m.prefixModes) {
		if t, ok := rankTitles[m.prefixModes[rank]]; ok {
			return t
		}
	}
	return string(m.prefixChars[rank])
}

// --- per-buffer visibility ------------------------------------------------------

// PeopleVisible reports whether the pane is open for the buffer on screen.
//
// The default is the honest one: a buffer with a roster shows it, a private
// conversation has nobody to list and does not. Ctrl+U records a choice for
// that buffer, and the choice is what is remembered — per buffer, per
// blueprint §4.1, so opening the pane in one channel does not open it in every
// private message.
func (m *Model) PeopleVisible() bool {
	if v, ok := m.peopleShown[m.curKey]; ok {
		return v
	}
	return len(m.members) > 0
}

// SetPeopleVisible records a deliberate choice for the buffer on screen.
func (m *Model) SetPeopleVisible(v bool) { m.peopleShown[m.curKey] = v }

// TogglePeople flips the pane for the buffer on screen and reports the new
// state. It is Ctrl+U.
func (m *Model) TogglePeople() bool {
	v := !m.PeopleVisible()
	m.SetPeopleVisible(v)
	return v
}
