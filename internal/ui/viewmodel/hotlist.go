package viewmodel

import "strings"

// The hotlist and the pins — blueprint §4.3's two keyboard answers to "where do
// I go next?", and the only two that do not require reading the rail.
//
// # The hotlist (Alt+A)
//
// WeeChat's semantics, deliberately: mentions first, then unread, each in rail
// order. The rail is already ordered the way the user arranged their networks
// and channels, so within a priority band the answer is "the topmost one you
// have not read", which is stable and requires no history to explain.
//
// What makes it a CYCLE rather than one jump is the read marker: arriving at a
// buffer marks it read (Model.Select), which takes it out of the list, so the
// next Alt+A is the next one down. Nothing here keeps a cursor — a cursor would
// be a second source of truth about what is unread, and it would be wrong the
// moment a message arrived behind it.
//
// # The pins (Alt+1..9)
//
// §4.3 is explicit that the digits are USER-PINNED buffers and not absolute row
// numbers, because a row number is not a name: with two networks connected,
// Alt+2 was a different conversation every time one of them dropped. A pin is a
// (profile, buffer name) pair — the same profile-scoped identity drafts and
// counts use, so a pin survives a reconnect, which a session id would not.

// MaxPins is how many buffers can be pinned: Alt+1 through Alt+9.
const MaxPins = 9

// Hotlist returns the buffers waiting for the reader, most important first:
// everything with a mention, then everything merely unread, each in rail order.
// The buffer on screen is never in it — Badge has already suppressed it.
func (m *Model) Hotlist() []Ref {
	var mentions, unread []Ref
	for _, row := range m.Rows() {
		if row.Kind != RowBuffer || !row.Live {
			continue
		}
		switch {
		case row.Counts.Highlights > 0:
			mentions = append(mentions, row.Ref)
		case row.Counts.Unread > 0:
			unread = append(unread, row.Ref)
		}
	}
	// Folded networks are hidden, not silent: their buffers belong in the list
	// for the same reason NextHot will unfold one to get there.
	for _, s := range m.sessions {
		if !m.collapsed[s.profile] {
			continue
		}
		for _, name := range s.buffers {
			r := Ref{Session: s.id, Name: name}
			switch c := m.Badge(r); {
			case c.Highlights > 0:
				mentions = append(mentions, r)
			case c.Unread > 0:
				unread = append(unread, r)
			}
		}
	}
	return append(mentions, unread...)
}

// NextHot returns the rail row Alt+A lands on: the first entry of the hotlist,
// with the network unfolded when that is where it lives.
//
// Collapsing a network hides its rows, not its attention. A jump-to-unread that
// silently refuses to reach half the buffers is a worse answer than one that
// opens the fold it has to (M12(a)'s rule, kept — and now it applies to
// mentions first).
func (m *Model) NextHot() (int, bool) {
	hot := m.Hotlist()
	if len(hot) == 0 {
		return 0, false
	}
	r := hot[0]
	if i := m.IndexOf(r); i >= 0 {
		return i, true
	}
	// it is inside a fold: open the fold it is in, then ask again
	if s := m.session(r.Session); s != nil {
		m.SetCollapsed(s.profile, false)
	}
	if i := m.IndexOf(r); i >= 0 {
		return i, true
	}
	return 0, false
}

// TotalMentions is the OS badge's number (§4.4: mention count only), summed
// over every buffer of every network. The buffer on screen contributes nothing,
// because it is being read.
func (m *Model) TotalMentions() int {
	total := 0
	for _, s := range m.sessions {
		for _, name := range s.buffers {
			total += m.Badge(Ref{Session: s.id, Name: name}).Highlights
		}
	}
	return total
}

// --- the pins ------------------------------------------------------------------

// pin is one pinned buffer: the network PROFILE and the buffer's display name.
// Profile-scoped for the same reason keys are — a reconnect is a new session id
// for the same network, and a pin must not evaporate with it.
type pin struct {
	Profile string
	Name    string
}

func (p pin) same(o pin) bool {
	return p.Profile == o.Profile && strings.EqualFold(p.Name, o.Name)
}

// pinOf turns a ref into a pin. A ref whose session this model does not know
// has no profile, and cannot be pinned.
func (m *Model) pinOf(r Ref) (pin, bool) {
	profile := m.Profile(r.Session)
	if profile == "" || r.Name == "" {
		return pin{}, false
	}
	return pin{Profile: profile, Name: r.Name}, true
}

// PinnedAt returns the digit a buffer is pinned to (1..9), or 0.
func (m *Model) PinnedAt(r Ref) int {
	want, ok := m.pinOf(r)
	if !ok {
		return 0
	}
	for i, p := range m.pins {
		if p.same(want) {
			return i + 1
		}
	}
	return 0
}

// Pin pins a buffer to the next free digit and reports which one, or 0 when it
// is already pinned or there is no room. Nine is not an arbitrary cap: it is how
// many digits Alt has.
func (m *Model) Pin(r Ref) int {
	p, ok := m.pinOf(r)
	if !ok {
		return 0
	}
	if at := m.PinnedAt(r); at > 0 {
		return at
	}
	if len(m.pins) >= MaxPins {
		return 0
	}
	m.pins = append(m.pins, p)
	m.dirty = true
	return len(m.pins)
}

// Unpin removes a buffer's pin and closes the gap, so the digits stay
// contiguous: an Alt+3 that jumps to nothing because the third pin was removed
// is a keyboard shortcut that teaches the user not to trust it.
func (m *Model) Unpin(r Ref) bool {
	at := m.PinnedAt(r)
	if at == 0 {
		return false
	}
	m.pins = append(m.pins[:at-1], m.pins[at:]...)
	m.dirty = true
	return true
}

// TogglePin pins or unpins, and reports the digit afterwards (0 = not pinned).
func (m *Model) TogglePin(r Ref) int {
	if m.Unpin(r) {
		return 0
	}
	return m.Pin(r)
}

// PinRef returns the buffer pinned to a digit (1-based), if its network is
// connected. A pin whose network is offline is kept — the profile is still
// pinned, it is just not reachable right now — and reports false rather than
// jumping somewhere else.
func (m *Model) PinRef(n int) (Ref, bool) {
	if n < 1 || n > len(m.pins) {
		return Ref{}, false
	}
	p := m.pins[n-1]
	for _, s := range m.sessions {
		if s.profile != p.Profile {
			continue
		}
		for _, name := range s.buffers {
			if strings.EqualFold(name, p.Name) {
				return Ref{Session: s.id, Name: name}, true
			}
		}
	}
	return Ref{}, false
}

// PinKeys serializes the pins for preferences, one "profile\x00name" per entry,
// in digit order.
func (m *Model) PinKeys() []string {
	out := make([]string, 0, len(m.pins))
	for _, p := range m.pins {
		out = append(out, p.Profile+"\x00"+p.Name)
	}
	return out
}

// SetPinKeys restores the pins from preferences. Anything malformed or past the
// ninth is dropped rather than shifting the digits of everything after it.
func (m *Model) SetPinKeys(keys []string) {
	m.pins = m.pins[:0]
	for _, k := range keys {
		profile, name, ok := strings.Cut(k, "\x00")
		if !ok || profile == "" || name == "" || len(m.pins) >= MaxPins {
			continue
		}
		p := pin{Profile: profile, Name: name}
		if m.pinnedAlready(p) {
			continue
		}
		m.pins = append(m.pins, p)
	}
	m.dirty = true
}

func (m *Model) pinnedAlready(p pin) bool {
	for _, have := range m.pins {
		if have.same(p) {
			return true
		}
	}
	return false
}
