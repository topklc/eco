package viewmodel

import "strings"

// The Rail's row model — blueprint §4.1, and R-6's rule about when network
// chrome is allowed to exist at all.
//
// # Disclosure by invitation
//
// One network is not a hierarchy, so it does not get the furniture of one. Its
// buffers ARE the rail: no header row, no chevron, no overflow menu, and
// Caption names the network once, quietly, or not at all. The moment a second
// network exists the headers appear, because only then does a row have to say
// which network it belongs to.
//
// An offline network is the one exception, and it is not chrome for its own
// sake: a pinned profile with no session has no buffers, so its header row is
// the only thing it has — and it is what carries [Connect].
//
// # Where the pixels are not
//
// Everything here is a pure function of the sessions, the pinned profiles, the
// counts and the collapse set, so all of it is answerable in a table test with
// no toolkit in the process (rail_test.go). internal/ui/widgets draws these
// rows and calls the intents below; it decides nothing.
type RowKind uint8

const (
	// RowBuffer is a channel, a private conversation, or a network's own log.
	// It is the only kind the viewer can be shown.
	RowBuffer RowKind = iota
	// RowNetwork is a network header: the name, its connection state, the
	// collapse chevron, the overflow menu, and — when it is offline — the
	// [Connect] button. It exists only when disclosure has been invited.
	RowNetwork
)

// Row is one rail row: a network header or a buffer under one.
type Row struct {
	Kind RowKind
	// Ref names the buffer a RowBuffer shows. A RowNetwork carries its
	// session (so the row can be acted on) and no buffer name; an offline
	// one carries nothing but its Profile.
	Ref Ref
	// Profile is the network profile name — the row's identity for a network
	// with no session, which is the only identity it has.
	Profile string
	// Live reports that there is a session behind this row.
	Live bool

	// Counts is what the pill shows: the store's counts with §4.4's
	// suppression already applied, so a renderer cannot forget it. On a
	// collapsed network header it is the rollup of everything underneath.
	Counts Counts

	// Current marks the row the viewer is on. A collapsed header wears it for
	// the buffer it is hiding, which is the only place left to say so.
	Current bool

	// Collapsed reports that a network header's buffers are filtered out.
	Collapsed bool

	// Depth is the row's indent step: 1 for a buffer under a header, 0 when
	// there is no header to sit under.
	Depth int
}

// IsNetwork reports whether this row stands for a network rather than a buffer.
func (r Row) IsNetwork() bool { return r.Kind == RowNetwork }

// IsServerLog reports whether this row is a network's own log — the buffer the
// server talks in. It is a buffer like any other; it is only ever singled out
// because the menus that act on a network also act from it.
func (r Row) IsServerLog() bool {
	return r.Kind == RowBuffer && strings.EqualFold(r.Ref.Name, ServerBuffer)
}

// Selectable reports whether showing this row means showing a buffer. A network
// header is not selectable: clicking one collapses it, and clicking an OFFLINE
// one does nothing at all — dialling a server is [Connect]'s job and no other
// gesture's (blueprint §4.1).
func (r Row) Selectable() bool { return r.Kind == RowBuffer && r.Live }

// --- collapse: state, not decoration -------------------------------------------

// Collapsed reports whether a network's rows are folded away.
func (m *Model) Collapsed(profile string) bool { return m.collapsed[profile] }

// SetCollapsed folds a network's rows away, or brings them back.
func (m *Model) SetCollapsed(profile string, v bool) {
	if profile == "" || m.collapsed[profile] == v {
		return
	}
	if v {
		m.collapsed[profile] = true
	} else {
		delete(m.collapsed, profile)
	}
	m.dirty = true
}

// ToggleCollapsed flips one network's chevron and reports the new state.
func (m *Model) ToggleCollapsed(profile string) bool {
	v := !m.collapsed[profile]
	m.SetCollapsed(profile, v)
	return v
}

// CollapsedProfiles lists the folded networks, so the app can put them back
// after a restart. Order is the rail's, not the map's.
func (m *Model) CollapsedProfiles() []string {
	var out []string
	for _, s := range m.sessions {
		if m.collapsed[s.profile] {
			out = append(out, s.profile)
		}
	}
	for _, p := range m.pinned {
		if m.collapsed[p] && !m.hasSession(p) {
			out = append(out, p)
		}
	}
	return out
}

// SetCollapsedProfiles restores the folded set from preferences.
func (m *Model) SetCollapsedProfiles(profiles []string) {
	m.collapsed = make(map[string]bool, len(profiles))
	for _, p := range profiles {
		if p != "" {
			m.collapsed[p] = true
		}
	}
	m.dirty = true
}

// Caption is the quiet line above a rail with no network chrome: the name of
// the one network on screen. It is empty whenever headers are showing, because
// then every row already says which network it is under, and empty when there
// is no session, because then there is nothing to name.
func (m *Model) Caption() string {
	if m.headers() || len(m.sessions) != 1 {
		return ""
	}
	return m.sessions[0].profile
}

// headers reports whether network header rows are invited: more than one
// network exists, counting the pinned profiles that are not connected.
func (m *Model) headers() bool { return len(m.sessions)+len(m.offlinePins()) > 1 }

// offlinePins are the pinned profiles with no session, in preferences order.
func (m *Model) offlinePins() []string {
	var out []string
	for _, p := range m.pinned {
		if !m.hasSession(p) {
			out = append(out, p)
		}
	}
	return out
}

func (m *Model) hasSession(profile string) bool {
	for _, s := range m.sessions {
		if s.profile == profile {
			return true
		}
	}
	return false
}

// --- reads ---------------------------------------------------------------------

// Rows returns the rail, rebuilt if anything it depends on moved. The slice
// belongs to the model and is valid until the next intent; read it, do not keep
// it.
func (m *Model) Rows() []Row {
	if m.dirty {
		m.rebuild()
	}
	return m.rows
}

// RowCount is len(Rows) without the copy at the call site.
func (m *Model) RowCount() int { return len(m.Rows()) }

// Row returns one row by index.
func (m *Model) Row(i int) (Row, bool) {
	rows := m.Rows()
	if i < 0 || i >= len(rows) {
		return Row{}, false
	}
	return rows[i], true
}

// IndexOf returns the row showing a buffer, or -1.
func (m *Model) IndexOf(r Ref) int {
	for i, row := range m.Rows() {
		if row.Kind == RowBuffer && row.Ref.same(r) {
			return i
		}
	}
	return -1
}

// Index returns a row's current position, or -1. It matches on the profile too,
// because every offline network row carries the same empty ref: without the
// profile, acting on the second pinned network acted on the first.
func (m *Model) Index(want Row) int {
	for i, row := range m.Rows() {
		if row.Kind == want.Kind && row.Ref.same(want.Ref) && row.Profile == want.Profile {
			return i
		}
	}
	return -1
}

// Nth returns the index of the n-th selectable row (0-based) — what Alt+1..9
// counts. Headers are not rows a keyboard jump can land on, so they are not
// counted either; without that the same digit means a different buffer
// depending on how many networks happen to be connected.
func (m *Model) Nth(n int) (int, bool) {
	if n < 0 {
		return 0, false
	}
	for i, row := range m.Rows() {
		if !row.Selectable() {
			continue
		}
		if n == 0 {
			return i, true
		}
		n--
	}
	return 0, false
}

// Step returns the row index the keyboard's next/previous buffer lands on,
// wrapping, or false when there is nowhere to go.
//
// Only selectable rows are candidates: a network header switches no buffer, and
// an offline one is a row whose only action DIALS A SERVER. Stepping through
// buffers with the keyboard must never do either. A collapsed network is
// skipped with the rest of its rows — that is what collapsing it asked for; the
// hotlist (NextUnread) is the way back in.
func (m *Model) Step(dir int) (int, bool) {
	rows := m.Rows()
	if len(rows) == 0 || dir == 0 {
		return 0, false
	}
	cur := 0
	for i, row := range rows {
		if row.Current {
			cur = i
		}
	}
	next := cur
	for range rows {
		next = (next + dir + len(rows)) % len(rows)
		if rows[next].Selectable() {
			return next, true
		}
	}
	return 0, false
}

// NextUnread returns the first row with something outstanding, or false. The
// buffer on screen is never it: Badge has already suppressed it.
//
// Collapsing a network hides its rows, not its attention. If nothing visible is
// waiting, this looks inside the folded ones and UNFOLDS the one it finds
// something in — a jump-to-unread that silently refuses to reach half the
// buffers is a worse answer than one that opens the fold it has to.
func (m *Model) NextUnread() (int, bool) {
	for i, row := range m.Rows() {
		if row.Kind == RowBuffer && row.Live && row.Counts.Unread > 0 {
			return i, true
		}
	}
	for _, s := range m.sessions {
		if !m.collapsed[s.profile] {
			continue
		}
		for _, name := range s.buffers {
			r := Ref{Session: s.id, Name: name}
			if m.Badge(r).Unread == 0 {
				continue
			}
			m.SetCollapsed(s.profile, false)
			if i := m.IndexOf(r); i >= 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// --- the rebuild ---------------------------------------------------------------

// rebuild recomputes the rail: every network, its buffers under it, and the
// pinned profiles that have no session at the end.
func (m *Model) rebuild() {
	m.rows = m.rows[:0]
	headers := m.headers()
	depth := 0
	if headers {
		depth = 1
	}
	for _, s := range m.sessions {
		collapsed := headers && m.collapsed[s.profile]
		if headers {
			m.rows = append(m.rows, m.networkRow(s, collapsed))
		}
		if collapsed {
			continue
		}
		for _, name := range s.buffers {
			m.rows = append(m.rows, m.bufferRow(Ref{Session: s.id, Name: name}, s.profile, depth))
		}
	}
	// An offline network is always a header row, headers or not: it has no
	// buffers, so this is the only row it gets and the only place [Connect]
	// can live.
	for _, p := range m.offlinePins() {
		m.rows = append(m.rows, Row{Kind: RowNetwork, Profile: p, Collapsed: m.collapsed[p]})
	}
	m.dirty = false
}

// networkRow builds one header. A collapsed one carries the rollup of
// everything it is hiding — both the counts and, so the viewer can still see
// where they are, the current-buffer mark.
func (m *Model) networkRow(s *session, collapsed bool) Row {
	row := Row{
		Kind:      RowNetwork,
		Ref:       Ref{Session: s.id},
		Profile:   s.profile,
		Live:      true,
		Collapsed: collapsed,
	}
	if !collapsed {
		return row
	}
	for _, name := range s.buffers {
		r := Ref{Session: s.id, Name: name}
		c := m.Badge(r)
		row.Counts.Unread += c.Unread
		row.Counts.Highlights += c.Highlights
		if m.Showing(r) {
			row.Current = true
		}
	}
	return row
}

func (m *Model) bufferRow(r Ref, profile string, depth int) Row {
	return Row{
		Kind:    RowBuffer,
		Ref:     r,
		Profile: profile,
		Live:    true,
		Depth:   depth,
		Counts:  m.Badge(r),
		Current: m.Showing(r),
	}
}
