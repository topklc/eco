package viewmodel

import (
	"strings"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// ServerBuffer is the display name of a network's status buffer: what a root
// row stands for, and where the viewer lands when the buffer it was showing
// goes away. It is state.ServerBufferName; the value is written out rather than
// imported so this package depends on nothing below the funnel.
const ServerBuffer = "server"

// Ref names one buffer of one session. The name is the DISPLAY name and is
// compared case-insensitively, because that is what the user and the sidebar
// deal in; the store's folded key never reaches this layer.
type Ref struct {
	Session dispatch.SessionID
	Name    string
}

// IsZero reports whether the ref names nothing — the idle window's "no session,
// no buffer".
func (r Ref) IsZero() bool { return r.Session == "" && r.Name == "" }

// same reports whether two refs name the same buffer.
func (r Ref) same(o Ref) bool {
	return r.Session == o.Session && strings.EqualFold(r.Name, o.Name)
}

// Counts is a buffer's attention state as state.Store derives it from the read
// marker. Nothing in this package produces one; SetUnread files what the store
// said and Badge reports it.
type Counts struct {
	Unread     int
	Highlights int
}

// zero reports whether a buffer has nothing outstanding.
func (c Counts) zero() bool { return c == Counts{} }

// Any reports whether a buffer is waiting for the reader at all. It is the
// question a renderer asks — "is this row worth drawing at full strength?" —
// and it is here rather than in the widget so there is one answer to it.
func (c Counts) Any() bool { return !c.zero() }

// Hooks are the effects the Model asks for but cannot perform. Everything here
// is optional: a test that does not care leaves it nil.
type Hooks struct {
	// MarkRead asks the app to move a buffer's read marker to its newest
	// message. The store owns the count, so this is the only way one reaches
	// zero for real; the Model applies the same zero locally at once, because
	// the answer coming back is a coalescing tick away.
	//
	// It runs on the Fyne thread, inside whatever intent provoked it.
	MarkRead func(id dispatch.SessionID, buffer string)
}

// Model is the window's state. See the package doc for what it owns and for the
// one-thread rule.
type Model struct {
	hooks Hooks

	sessions []*session // live sessions, in the order the app opened them
	pinned   []string   // pinned profile names, in preferences order

	// unread is the store's last word per buffer, keyed by profile+folded name
	// (see key). It is NOT derived here.
	unread map[string]Counts

	// drafts is text the user typed and has not sent, per buffer, under the
	// same key. draft is the one currently in the composer.
	drafts map[string]string
	draft  string

	cur Ref
	// curKey is the key of the buffer that OWNS the draft slot. It is stored
	// rather than recomputed from cur, which is the whole fix for v1's
	// cross-network draft bug: by the time a switch is applied the session has
	// already moved, so recomputing files the outgoing text under the network
	// being switched TO.
	curKey string

	// collapsed are the networks whose rail rows are folded away, by profile.
	// See rail.go.
	collapsed map[string]bool

	// recent is the last few buffer keys shown, most recent first: the
	// Switcher's half of "fuzzy-ranked (recency + match quality)". See
	// switcher.go.
	recent []string

	// The People pane's state; see people.go.
	members     []model.Member
	prefixModes string
	prefixChars string
	filter      string
	peopleRows  []PeopleRow
	peopleDirty bool
	peopleShown map[string]bool

	rows  []Row
	dirty bool
}

// session is one live connection as the sidebar sees it.
type session struct {
	id      dispatch.SessionID
	profile string
	buffers []string // display names in display order, the server buffer included
}

// New returns an empty Model showing nothing.
func New(h Hooks) *Model {
	return &Model{
		hooks:       h,
		unread:      map[string]Counts{},
		drafts:      map[string]string{},
		collapsed:   map[string]bool{},
		peopleShown: map[string]bool{},
		cur:         Ref{Name: ServerBuffer},
		dirty:       true,
		peopleDirty: true,
	}
}

// --- wiring: what the app tells the model about sessions ----------------------

// AddSession registers a live session under its profile name. The profile — not
// the session id — is what scopes drafts and counts, deliberately: a reconnect
// is a new session id for the same network, and the text the user was typing
// when the connection dropped has to still be there afterwards.
func (m *Model) AddSession(id dispatch.SessionID, profile string) {
	if id == "" {
		return
	}
	for _, s := range m.sessions {
		if s.id == id {
			s.profile = profile
			m.dirty = true
			return
		}
	}
	m.sessions = append(m.sessions, &session{id: id, profile: profile})
	m.dirty = true
}

// RemoveSession retires a session the app tore down and reports where the
// viewer must go: the newest remaining session's server buffer, or the zero Ref
// when there is nothing left to show (the idle window). The bool is false when
// the viewer was not on the closed session and so does not move at all.
//
// Its counts go with it — they are name-scoped, so a later reconnect to the
// same network would otherwise inherit badges for messages no buffer holds any
// more. Drafts stay on purpose: typed text is worth keeping across a reconnect,
// and that is exactly what the profile-scoped key buys.
func (m *Model) RemoveSession(id dispatch.SessionID) (Ref, bool) {
	idx := -1
	for i, s := range m.sessions {
		if s.id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Ref{}, false
	}
	profile := m.sessions[idx].profile
	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)
	m.dirty = true

	prefix := profile + "\x00"
	for k := range m.unread {
		if strings.HasPrefix(k, prefix) {
			delete(m.unread, k)
		}
	}
	if m.cur.Session != id {
		return Ref{}, false
	}
	if len(m.sessions) == 0 {
		// Nothing to show. cur and curKey are deliberately left where they are:
		// the draft slot still belongs to the buffer the user was typing in,
		// and a reconnect to the same profile has to find it there.
		return Ref{}, true
	}
	last := m.sessions[len(m.sessions)-1]
	return Ref{Session: last.id, Name: ServerBuffer}, true
}

// SetBuffers replaces a session's buffer list with what its projection now
// holds, in display order, and reports the buffer-vanished fallback: when the
// buffer on screen is no longer in ITS OWN session's list, the second return is
// true and the first is that session's server buffer.
//
// "That session's" is the load-bearing half. A fallback that picked the first
// row, or the first buffer it could find, drops a multi-network window into
// another network's log when a channel closes — which is a different
// conversation with a different roster and a different draft.
//
// There is deliberately no CloseBuffer intent to go with this. Closing a buffer
// is a verb on the model (Session.CloseBuffer, Session.Part, or a KICK nobody
// asked for), and this layer only ever learns about it the same way it learns
// about a buffer appearing: the list it is handed changed. An intent here would
// be a second path to one fact, and the two would disagree the first time a
// server closed a buffer on its own.
func (m *Model) SetBuffers(id dispatch.SessionID, names []string) (Ref, bool) {
	s := m.session(id)
	if s == nil {
		return Ref{}, false
	}
	s.buffers = append(s.buffers[:0], names...)
	m.dirty = true

	if m.cur.Session != id || strings.EqualFold(m.cur.Name, ServerBuffer) {
		return Ref{}, false
	}
	for _, n := range names {
		if strings.EqualFold(n, m.cur.Name) {
			return Ref{}, false
		}
	}
	return Ref{Session: id, Name: ServerBuffer}, true
}

// SetPinned records the pinned profiles, newest preferences first. A pinned
// profile with no live session keeps a clickable root row so it can be
// reconnected; one that IS connected is already a root and is not doubled.
func (m *Model) SetPinned(profiles []string) {
	m.pinned = append(m.pinned[:0], profiles...)
	m.dirty = true
}

// Profile returns a session's network profile name, or "" for a session this
// model does not know.
func (m *Model) Profile(id dispatch.SessionID) string {
	if s := m.session(id); s != nil {
		return s.profile
	}
	return ""
}

func (m *Model) session(id dispatch.SessionID) *session {
	for _, s := range m.sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

// --- what the funnel delivers -------------------------------------------------

// SetUnread files the counts state.Store reported for a buffer. This is the
// only way a count enters the model.
func (m *Model) SetUnread(id dispatch.SessionID, buffer string, c Counts) {
	if id == "" || buffer == "" {
		return
	}
	k := m.key(Ref{Session: id, Name: buffer})
	if c.zero() {
		delete(m.unread, k)
	} else {
		m.unread[k] = c
	}
	m.dirty = true
}

// BufferRenamed moves everything filed under a buffer to its new name: the
// draft, the counts, the sidebar row and — when it is the buffer on screen —
// the viewer itself. It reports whether the viewer followed, which is the
// renderer's cue to retitle the pane.
//
// Anything not moved here is orphaned under a name nothing will ever ask for
// again: the user's half-typed reply disappears the moment the network renames
// their channel.
func (m *Model) BufferRenamed(id dispatch.SessionID, from, to string) bool {
	if id == "" || from == "" || to == "" {
		return false
	}
	oldKey, newKey := m.key(Ref{Session: id, Name: from}), m.key(Ref{Session: id, Name: to})
	if oldKey != newKey {
		if v, ok := m.drafts[oldKey]; ok {
			m.drafts[newKey] = v
			delete(m.drafts, oldKey)
		}
		if v, ok := m.unread[oldKey]; ok {
			m.unread[newKey] = v
			delete(m.unread, oldKey)
		}
	}
	if s := m.session(id); s != nil {
		for i, n := range s.buffers {
			if strings.EqualFold(n, from) {
				s.buffers[i] = to
			}
		}
	}
	m.dirty = true

	if !m.cur.same(Ref{Session: id, Name: from}) {
		return false
	}
	m.cur.Name = to
	m.curKey = newKey
	return true
}

// --- intents: what the widgets ask for ----------------------------------------

// Select shows a buffer. It parks the composer's text under the buffer that
// owned the draft slot, loads the one belonging to the buffer being shown,
// marks both read, and re-attaches the message pane to the tail.
//
// It reports whether the composer must be reloaded from Draft. A first show
// (nothing owned the slot yet) says no on purpose: text typed before there was
// anywhere to send it survives the connection completing.
func (m *Model) Select(r Ref) bool {
	if r.Name == "" {
		r.Name = ServerBuffer
	}
	newKey := m.key(r)
	reload := false
	if m.curKey != "" && newKey != m.curKey {
		// Park under the key the outgoing buffer was ACTUALLY shown under, not
		// under one recomputed from a session that has already moved.
		m.drafts[m.curKey] = m.draft
		m.draft = m.drafts[newKey]
		// drafts holds text for buffers that are NOT on screen; the one being
		// shown lives in draft until it is parked again, so a second copy here
		// could only go stale.
		delete(m.drafts, newKey)
		reload = true
	}
	if !m.cur.same(r) {
		m.leave()
		// The filter box is a lens on THIS roster; the next buffer's is a
		// different set of people, and a filter left over from the last one
		// hides them without saying why.
		m.SetPeopleFilter("")
	}
	m.cur, m.curKey = r, newKey
	m.noteVisit(newKey)
	m.markRead(r, newKey)
	m.dirty = true
	return reload
}

// MarkRead clears a buffer's badge, from the row menu or from showing it.
func (m *Model) MarkRead(r Ref) {
	m.markRead(r, m.key(r))
	m.dirty = true
}

// markRead applies the zero the store is about to send and asks for the marker
// move that makes it true. Doing both is not two sources of truth: the store
// derives the count and this is its answer applied early, so the badge does not
// survive the click that dismissed it by a coalescing tick.
func (m *Model) markRead(r Ref, key string) {
	delete(m.unread, key)
	if m.hooks.MarkRead != nil && r.Session != "" {
		m.hooks.MarkRead(r.Session, r.Name)
	}
}

// leave marks the buffer the viewer is on read as it goes. This is what keeps
// the visible buffer from growing a badge for messages the user watched arrive:
// the store counts them (it cannot see the screen), and leaving is the moment
// that stops being interesting.
func (m *Model) leave() {
	if m.curKey == "" {
		return
	}
	m.markRead(m.cur, m.curKey)
}

// SetDraft records what is in the composer right now, for the buffer on screen.
func (m *Model) SetDraft(text string) { m.draft = text }

// Draft returns the composer's text for the buffer on screen.
func (m *Model) Draft() string { return m.draft }

// Parked returns the text filed for a buffer that is not on screen. It exists
// for tests and for assertions about ownership; the renderer only ever needs
// Draft.
func (m *Model) Parked(r Ref) string {
	if m.cur.same(r) {
		return m.draft
	}
	return m.drafts[m.key(r)]
}

// --- reads --------------------------------------------------------------------

// Current returns the buffer on screen.
func (m *Model) Current() Ref { return m.cur }

// CurrentProfile returns the network profile name of the session on screen.
func (m *Model) CurrentProfile() string { return m.Profile(m.cur.Session) }

// Showing reports whether a buffer is the one on screen. It is a rendering
// question, not a liveness one: a background session keeps recording, it just
// does not drive the visible pane.
func (m *Model) Showing(r Ref) bool { return m.cur.same(r) }

// Badge returns what the sidebar shows for a buffer: the store's counts, except
// for the buffer on screen, which is read by definition (§4.4's suppression
// step — the marker move that makes the store agree is one tick behind).
func (m *Model) Badge(r Ref) Counts {
	if m.Showing(r) {
		return Counts{}
	}
	return m.unread[m.key(r)]
}

// key scopes a buffer name to its network PROFILE, folded ASCII-wise. It is
// v1's skey, kept exactly: profile-scoped so a draft survives a reconnect (a
// session id would not), and folded so "#Chan" and "#chan" are one buffer to
// the sidebar as they are to the server.
func (m *Model) key(r Ref) string {
	return m.Profile(r.Session) + "\x00" + strings.ToLower(r.Name)
}
