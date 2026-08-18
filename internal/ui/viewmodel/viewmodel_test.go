package viewmodel

import (
	"fmt"
	"go/build"
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// The bugs this file exists for were all found by hand, in a running window,
// because the state they live in was a field of a Fyne struct. Every one of
// them is a table below, and none of them needs a canvas.
//
// The M5 audit's names for them, kept so a failure points at the report:
//   - draft ownership across networks (a draft filed under the wrong network)
//   - rename migration (draft, counts and the viewer orphaned by a RENAME)
//   - the buffer-vanished fallback (closing a buffer dropped a multi-network
//     window into another network's log)
//   - unread bookkeeping (badges for messages the user watched arrive)
//   - pinned-but-offline rows (keyboard browsing dialling a server)

const (
	sA dispatch.SessionID = "s-1"
	sB dispatch.SessionID = "s-2"
)

// harness is a Model plus the effects it asked for.
type harness struct {
	*Model
	marked []string // "session/buffer" per MarkRead hook call, in order
}

func newHarness() *harness {
	h := &harness{}
	h.Model = New(Hooks{
		MarkRead: func(id dispatch.SessionID, buffer string) {
			h.marked = append(h.marked, string(id)+"/"+buffer)
		},
	})
	return h
}

// twoNetworks is the fixture most tables start from: two connected profiles,
// each with a channel, and nothing shown yet.
func twoNetworks(t *testing.T) *harness {
	t.Helper()
	h := newHarness()
	h.AddSession(sA, "libera")
	h.AddSession(sB, "ergo")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})
	h.SetBuffers(sB, []string{ServerBuffer, "#eco"})
	return h
}

func ref(id dispatch.SessionID, name string) Ref { return Ref{Session: id, Name: name} }

// --- draft ownership ---------------------------------------------------------

// v1 filed the draft under the buffer being switched TO, because by the time
// the switch was applied the session pointer had already moved: typing in
// libera's #go and clicking ergo's #eco parked libera's text under ergo. The
// model owns both ends of the switch now, so the outgoing key cannot drift.
func TestDraftOwnershipAcrossNetworks(t *testing.T) {
	type step struct {
		show  Ref    // buffer to show ("" name means: do not switch)
		type_ string // text the user then types
	}
	cases := []struct {
		name  string
		steps []step
		want  map[string]string // Ref (as session/buffer) -> parked draft
	}{
		{
			name: "text follows its own network",
			steps: []step{
				{show: ref(sA, "#go"), type_: "half a thought about go"},
				{show: ref(sB, "#eco"), type_: "something else entirely"},
			},
			want: map[string]string{
				"s-1/#go":  "half a thought about go",
				"s-2/#eco": "something else entirely",
			},
		},
		{
			name: "switching back restores the right one",
			steps: []step{
				{show: ref(sA, "#go"), type_: "one"},
				{show: ref(sB, "#eco"), type_: "two"},
				{show: ref(sA, "#go")},
			},
			want: map[string]string{"s-1/#go": "one", "s-2/#eco": "two"},
		},
		{
			name: "same buffer name on two networks does not collide",
			steps: []step{
				{show: ref(sA, "#eco"), type_: "libera side"},
				{show: ref(sB, "#eco"), type_: "ergo side"},
			},
			want: map[string]string{"s-1/#eco": "libera side", "s-2/#eco": "ergo side"},
		},
		{
			name: "the server log of each network has its own slot",
			steps: []step{
				{show: ref(sA, ServerBuffer), type_: "for libera"},
				{show: ref(sB, ServerBuffer), type_: "for ergo"},
			},
			want: map[string]string{"s-1/server": "for libera", "s-2/server": "for ergo"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := twoNetworks(t)
			for _, s := range c.steps {
				if s.show.Name != "" {
					h.Select(s.show)
				}
				if s.type_ != "" {
					h.SetDraft(s.type_)
				}
			}
			for key, want := range c.want {
				id, name, _ := strings.Cut(key, "/")
				if got := h.Parked(ref(dispatch.SessionID(id), name)); got != want {
					t.Errorf("draft for %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// A draft survives a reconnect, which is why the key is scoped to the network
// PROFILE and not to the session id: the id is new every time the client
// dials, the profile is what the user calls that network.
func TestDraftSurvivesAReconnect(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})
	h.Select(ref(sA, "#go"))
	h.SetDraft("this took a while to write")
	h.Select(ref(sA, ServerBuffer)) // park it

	h.RemoveSession(sA)

	const reconnected dispatch.SessionID = "s-99"
	h.AddSession(reconnected, "libera")
	h.SetBuffers(reconnected, []string{ServerBuffer, "#go"})
	if reload := h.Select(ref(reconnected, "#go")); !reload {
		t.Error("Select did not reload the composer for a buffer with a parked draft")
	}
	if got := h.Draft(); got != "this took a while to write" {
		t.Errorf("draft after reconnect = %q, want it back", got)
	}
}

// The first show adopts the draft slot without touching the composer, so text
// typed before there was anywhere to send it survives the connection landing.
func TestFirstShowKeepsWhatWasTyped(t *testing.T) {
	h := newHarness()
	h.SetDraft("typed before connecting")
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer})
	if reload := h.Select(ref(sA, ServerBuffer)); reload {
		t.Error("the first show asked for the composer to be reloaded; it would erase what was typed")
	}
	if got := h.Draft(); got != "typed before connecting" {
		t.Errorf("draft = %q, want it untouched", got)
	}
}

// --- rename migration --------------------------------------------------------

// A RENAME moves a buffer's identity. Everything filed under the old name has
// to move with it or it is orphaned under a name nothing will ever ask for
// again — the half-typed reply included.
func TestRenameMigratesEverything(t *testing.T) {
	cases := []struct {
		name     string
		watching bool // is the renamed buffer the one on screen?
	}{
		{"renamed while you are looking at it", true},
		{"renamed while you are looking elsewhere", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := twoNetworks(t)
			// #go has a draft, three unread and a mention waiting in it
			h.Select(ref(sA, "#go"))
			h.SetDraft("mid-sentence")
			h.Select(ref(sA, ServerBuffer)) // park the draft, leave the buffer
			h.SetUnread(sA, "#go", Counts{Unread: 3, Highlights: 1})
			if c.watching {
				h.Select(ref(sA, "#go"))
				h.SetUnread(sA, "#go", Counts{Unread: 3, Highlights: 1})
			}

			followed := h.BufferRenamed(sA, "#go", "#golang")
			if followed != c.watching {
				t.Errorf("BufferRenamed reported the viewer moved = %v, want %v", followed, c.watching)
			}

			if got := h.Parked(ref(sA, "#golang")); got != "mid-sentence" {
				t.Errorf("draft under the new name = %q, want it migrated", got)
			}
			if got := h.Parked(ref(sA, "#go")); got != "" {
				t.Errorf("draft still filed under the old name: %q", got)
			}
			if c.watching {
				if got := h.Current(); !got.same(ref(sA, "#golang")) {
					t.Errorf("viewer is on %+v, want it to follow the rename", got)
				}
				// on screen, so the badge stays suppressed either way
				if got := h.Badge(ref(sA, "#golang")); got != (Counts{}) {
					t.Errorf("badge for the buffer on screen = %+v, want zero", got)
				}
			} else {
				if got := h.Current(); !got.same(ref(sA, ServerBuffer)) {
					t.Errorf("viewer moved to %+v; a rename elsewhere must not move it", got)
				}
				if got := h.Badge(ref(sA, "#golang")); got != (Counts{Unread: 3, Highlights: 1}) {
					t.Errorf("counts under the new name = %+v, want them migrated", got)
				}
			}
			if got := h.Badge(ref(sA, "#go")); got != (Counts{}) {
				t.Errorf("counts still filed under the old name: %+v", got)
			}
			// and the sidebar answers to the new name, without waiting for the
			// next buffer-list refresh
			if h.IndexOf(ref(sA, "#golang")) < 0 {
				t.Errorf("no sidebar row for #golang after the rename: %+v", h.Rows())
			}
			if h.IndexOf(ref(sA, "#go")) >= 0 {
				t.Error("the sidebar still has a row for the old name")
			}
		})
	}
}

// A rename on one network must not touch the identically named buffer on
// another one.
func TestRenameStaysOnItsNetwork(t *testing.T) {
	h := twoNetworks(t)
	h.SetBuffers(sB, []string{ServerBuffer, "#go"})
	h.Select(ref(sA, "#go"))
	h.SetDraft("libera text")
	h.Select(ref(sB, "#go"))
	h.SetDraft("ergo text")

	h.BufferRenamed(sA, "#go", "#golang")

	if got := h.Parked(ref(sB, "#go")); got != "ergo text" {
		t.Errorf("the other network's draft = %q, want it untouched", got)
	}
	if got := h.Current(); !got.same(ref(sB, "#go")) {
		t.Errorf("viewer is on %+v; the rename was on the other network", got)
	}
}

// --- the buffer-vanished fallback --------------------------------------------

// Closing the buffer you are viewing falls back to THAT network's log. A
// fallback that picked the first row, or the first buffer it could find, drops
// a multi-network window into a different conversation.
func TestBufferVanishedFallback(t *testing.T) {
	cases := []struct {
		name    string
		show    Ref
		session dispatch.SessionID // whose buffer list changes
		left    []string           // what that session still holds
		want    Ref                // where the viewer must go
		move    bool
	}{
		{
			name: "the buffer you are viewing closes",
			show: ref(sB, "#eco"), session: sB, left: []string{ServerBuffer},
			want: ref(sB, ServerBuffer), move: true,
		},
		{
			name: "never to another network's log",
			show: ref(sB, "#eco"), session: sB, left: []string{ServerBuffer},
			want: ref(sB, ServerBuffer), move: true,
		},
		{
			name: "a buffer closing elsewhere leaves the viewer alone",
			show: ref(sB, "#eco"), session: sA, left: []string{ServerBuffer},
			move: false,
		},
		{
			name: "a buffer closing on your network but not yours",
			show: ref(sB, "#eco"), session: sB, left: []string{ServerBuffer, "#eco"},
			move: false,
		},
		{
			name: "the server buffer never falls back to itself",
			show: ref(sB, ServerBuffer), session: sB, left: []string{ServerBuffer},
			move: false,
		},
		{
			name: "case does not make a buffer vanish",
			show: ref(sB, "#eco"), session: sB, left: []string{ServerBuffer, "#ECO"},
			move: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := twoNetworks(t)
			h.Select(c.show)
			got, move := h.SetBuffers(c.session, c.left)
			if move != c.move {
				t.Fatalf("fallback fired = %v, want %v (got %+v)", move, c.move, got)
			}
			if !move {
				return
			}
			if !got.same(c.want) {
				t.Errorf("fallback = %+v, want %+v", got, c.want)
			}
			if got.Session != c.show.Session {
				t.Errorf("fallback crossed networks: %+v came from showing %+v", got, c.show)
			}
		})
	}
}

// Closing the network you are on moves the viewer to what is left, and takes
// that network's counts with it — a reconnect must not inherit badges for
// messages no buffer holds any more.
func TestRemovingASessionMovesTheViewer(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sB, "#eco"))
	h.SetDraft("unsent")
	h.Select(ref(sA, "#go")) // park it and move away
	h.SetUnread(sB, "#eco", Counts{Unread: 4, Highlights: 2})

	next, move := h.RemoveSession(sA)
	if !move || !next.same(ref(sB, ServerBuffer)) {
		t.Fatalf("RemoveSession = (%+v, %v), want the remaining network's log", next, move)
	}
	h.Select(next)
	if got := h.Badge(ref(sB, "#eco")); got != (Counts{Unread: 4, Highlights: 2}) {
		t.Errorf("the surviving network's counts = %+v, want them kept", got)
	}
	if got := h.Parked(ref(sB, "#eco")); got != "unsent" {
		t.Errorf("draft after closing the OTHER network = %q, want it kept", got)
	}

	// and closing the last one leaves nothing to show
	if next, move := h.RemoveSession(sB); !move || !next.IsZero() {
		t.Errorf("RemoveSession of the last session = (%+v, %v), want (zero, true)", next, move)
	}
	if n := len(h.Rows()); n != 0 {
		t.Errorf("%d rows left after every session closed", n)
	}
}

// The viewer lands on the NEWEST remaining network, not on whichever one is
// first in the list. With two sessions the two answers are the same, which is
// how the M8 verification's mutation (send it to m.sessions[0]) passed this
// package: telling them apart needs three.
func TestRemovingASessionLandsOnTheNewestRemaining(t *testing.T) {
	const sC dispatch.SessionID = "s-3"
	h := twoNetworks(t)
	h.AddSession(sC, "oftc")
	h.SetBuffers(sC, []string{ServerBuffer, "#tor"})
	h.Select(ref(sB, "#eco"))

	next, move := h.RemoveSession(sB)
	if !move {
		t.Fatal("closing the session on screen left the viewer on it")
	}
	if want := ref(sC, ServerBuffer); !next.same(want) {
		t.Errorf("the viewer landed on %+v, want the newest remaining network %+v", next, want)
	}
}

// A session announced twice is one session. The app registers each session
// once, so this is the guard rather than the ordinary path — and an unguarded
// second registration doubles a whole network in the sidebar, with two rows
// that both claim to be current.
func TestASessionRegisteredTwiceIsOneRow(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})
	before := h.RowCount()

	h.AddSession(sA, "libera")

	if got := h.RowCount(); got != before {
		t.Errorf("the sidebar has %d rows after re-registering one session, want %d", got, before)
	}
	// A renamed profile is a re-registration too: it moves, it does not double.
	h.AddSession(sA, "libera-eu")
	if got := h.RowCount(); got != before {
		t.Errorf("re-registering under a new profile name made %d rows, want %d", got, before)
	}
	if got := h.Profile(sA); got != "libera-eu" {
		t.Errorf("profile after re-registration = %q, want %q", got, "libera-eu")
	}
}

// Closing a network the viewer is not on does not move it.
func TestRemovingABackgroundSessionLeavesTheViewer(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sB, "#eco"))
	h.SetUnread(sA, "#go", Counts{Unread: 2})

	if next, move := h.RemoveSession(sA); move {
		t.Errorf("RemoveSession moved the viewer to %+v; it was on another network", next)
	}
	if !h.Current().same(ref(sB, "#eco")) {
		t.Errorf("viewer is on %+v, want it untouched", h.Current())
	}

	// The counts went with it, and the proof has to be taken after a
	// reconnect: they are keyed by PROFILE, so counts left behind are counts a
	// new session under the same name inherits — badges for messages that are
	// no longer in any buffer.
	const again dispatch.SessionID = "s-1b"
	h.AddSession(again, "libera")
	h.SetBuffers(again, []string{ServerBuffer, "#go"})
	if got := h.Badge(ref(again, "#go")); got != (Counts{}) {
		t.Errorf("a reconnected network inherited %+v from the session before it", got)
	}
}

// --- unread and highlight bookkeeping ----------------------------------------

// The counts are the store's (state.countsUnread, evaluated against the read
// marker). What this layer owes is: file them, suppress them for the buffer on
// screen, and ask for the marker to move when the user reads or dismisses one.
func TestUnreadBookkeeping(t *testing.T) {
	cases := []struct {
		name  string
		run   func(h *harness)
		badge map[string]Counts // "session/buffer" -> what the sidebar shows
		marks []string          // MarkRead hook calls, in order
	}{
		{
			name: "a background buffer badges what the store says",
			run: func(h *harness) {
				h.Select(ref(sA, ServerBuffer))
				h.SetUnread(sA, "#go", Counts{Unread: 2})
			},
			badge: map[string]Counts{"s-1/#go": {Unread: 2}},
			marks: []string{"s-1/server"},
		},
		{
			name: "a mention badges as a highlight too",
			run: func(h *harness) {
				h.Select(ref(sA, ServerBuffer))
				h.SetUnread(sA, "#go", Counts{Unread: 5, Highlights: 1})
			},
			badge: map[string]Counts{"s-1/#go": {Unread: 5, Highlights: 1}},
			marks: []string{"s-1/server"},
		},
		{
			name: "the buffer on screen never accumulates one",
			run: func(h *harness) {
				h.Select(ref(sA, "#go"))
				// the store counts them: it cannot see the screen
				h.SetUnread(sA, "#go", Counts{Unread: 3, Highlights: 1})
			},
			badge: map[string]Counts{"s-1/#go": {}},
			marks: []string{"s-1/#go"},
		},
		{
			name: "leaving a buffer marks it read",
			run: func(h *harness) {
				h.Select(ref(sA, "#go"))
				h.SetUnread(sA, "#go", Counts{Unread: 3})
				h.Select(ref(sA, ServerBuffer))
			},
			badge: map[string]Counts{"s-1/#go": {}},
			marks: []string{"s-1/#go", "s-1/#go", "s-1/server"},
		},
		{
			name: "showing a buffer clears what was waiting in it",
			run: func(h *harness) {
				h.Select(ref(sA, ServerBuffer))
				h.SetUnread(sA, "#go", Counts{Unread: 9, Highlights: 4})
				h.Select(ref(sA, "#go"))
			},
			badge: map[string]Counts{"s-1/#go": {}},
			marks: []string{"s-1/server", "s-1/server", "s-1/#go"},
		},
		{
			name: "mark as read clears both counters without showing it",
			run: func(h *harness) {
				h.Select(ref(sA, ServerBuffer))
				h.SetUnread(sA, "#go", Counts{Unread: 7, Highlights: 2})
				h.MarkRead(ref(sA, "#go"))
			},
			badge: map[string]Counts{"s-1/#go": {}},
			marks: []string{"s-1/server", "s-1/#go"},
		},
		{
			name: "counts are per network, not per name",
			run: func(h *harness) {
				h.SetBuffers(sB, []string{ServerBuffer, "#go"})
				h.Select(ref(sA, "#go"))
				h.SetUnread(sB, "#go", Counts{Unread: 6})
			},
			badge: map[string]Counts{"s-1/#go": {}, "s-2/#go": {Unread: 6}},
			marks: []string{"s-1/#go"},
		},
		{
			name: "the store zeroing a count clears the badge",
			run: func(h *harness) {
				h.Select(ref(sA, ServerBuffer))
				h.SetUnread(sA, "#go", Counts{Unread: 2})
				h.SetUnread(sA, "#go", Counts{})
			},
			badge: map[string]Counts{"s-1/#go": {}},
			marks: []string{"s-1/server"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := twoNetworks(t)
			c.run(h)
			for key, want := range c.badge {
				id, name, _ := strings.Cut(key, "/")
				if got := h.Badge(ref(dispatch.SessionID(id), name)); got != want {
					t.Errorf("badge for %s = %+v, want %+v", key, got, want)
				}
			}
			if fmt.Sprint(h.marked) != fmt.Sprint(c.marks) {
				t.Errorf("MarkRead calls = %v, want %v", h.marked, c.marks)
			}
		})
	}
}

// Nothing in this package invents a count. The only way one appears is
// SetUnread, which is the store's word arriving through the funnel — the rule
// about which lines count lives in state.countsUnread and nowhere else.
func TestTheModelNeverCountsForItself(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, ServerBuffer))
	// everything a busy channel does short of the store reporting a count
	h.SetBuffers(sA, []string{ServerBuffer, "#go", "#extra"})
	h.SetMembers([]string{"alice", "bob"})
	h.BufferRenamed(sA, "#extra", "#extras")
	h.SetFollow(false)
	for _, r := range []Ref{ref(sA, "#go"), ref(sA, "#extras"), ref(sB, "#eco")} {
		if got := h.Badge(r); got != (Counts{}) {
			t.Errorf("badge for %+v = %+v with nothing reported, want zero", r, got)
		}
	}
}

// Alt+A goes to the first buffer with something in it, and never to the one
// already on screen.
func TestNextUnreadSkipsTheBufferOnScreen(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetUnread(sA, "#go", Counts{Unread: 3})
	if i, ok := h.NextUnread(); ok {
		t.Errorf("NextUnread = row %d (%+v), want none: the only counts are on screen", i, h.Rows()[i])
	}
	h.SetUnread(sB, "#eco", Counts{Unread: 1})
	i, ok := h.NextUnread()
	if !ok {
		t.Fatal("NextUnread found nothing with a background buffer waiting")
	}
	if got := h.Rows()[i]; !got.Ref.same(ref(sB, "#eco")) {
		t.Errorf("NextUnread = %+v, want the ergo channel", got)
	}
}

// --- rows, pinned profiles and keyboard stepping ------------------------------

// A pinned profile with no session keeps a clickable root row; one that is
// connected is already a root and must not be doubled.
func TestPinnedOfflineRows(t *testing.T) {
	type want struct {
		profile string
		root    bool
		live    bool
		name    string
	}
	cases := []struct {
		name    string
		connect []string // profiles with a live session
		pinned  []string
		rows    []want
	}{
		{
			name:    "an offline pin is a root of its own",
			connect: []string{"libera"},
			pinned:  []string{"oftc"},
			rows: []want{
				{profile: "libera", root: true, live: true, name: ServerBuffer},
				{profile: "libera", live: true, name: "#go"},
				{profile: "oftc", root: true, name: ServerBuffer},
			},
		},
		{
			name:    "a connected profile is not doubled by its pin",
			connect: []string{"libera"},
			pinned:  []string{"libera", "oftc"},
			rows: []want{
				{profile: "libera", root: true, live: true, name: ServerBuffer},
				{profile: "libera", live: true, name: "#go"},
				{profile: "oftc", root: true, name: ServerBuffer},
			},
		},
		{
			name:    "with nothing connected the sidebar is all pins",
			connect: nil,
			pinned:  []string{"oftc", "efnet"},
			rows: []want{
				{profile: "oftc", root: true, name: ServerBuffer},
				{profile: "efnet", root: true, name: ServerBuffer},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			for i, p := range c.connect {
				id := dispatch.SessionID(fmt.Sprintf("s-%d", i+1))
				h.AddSession(id, p)
				h.SetBuffers(id, []string{ServerBuffer, "#go"})
			}
			h.SetPinned(c.pinned)

			rows := h.Rows()
			if len(rows) != len(c.rows) {
				t.Fatalf("%d rows, want %d: %+v", len(rows), len(c.rows), rows)
			}
			for i, w := range c.rows {
				got := rows[i]
				if got.Profile != w.profile || got.Root != w.root || got.Live != w.live ||
					!strings.EqualFold(got.Ref.Name, w.name) {
					t.Errorf("row %d = %+v, want %+v", i, got, w)
				}
			}
		})
	}
}

// Two offline pins carry the same (no session, "server") ref, so a row's
// identity has to include its profile — without it, clicking the second pinned
// network selected, and connected, the first.
func TestOfflinePinsAreToldApart(t *testing.T) {
	h := newHarness()
	h.SetPinned([]string{"oftc", "efnet"})
	rows := h.Rows()
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	for i, row := range rows {
		if got := h.Index(row); got != i {
			t.Errorf("Index(%q) = %d, want %d", row.Profile, got, i)
		}
	}
}

// Stepping with the keyboard walks live rows only. Selecting an offline root
// dials a server, and browsing must never do that by itself.
func TestStepNeverLandsOnAnOfflineRow(t *testing.T) {
	// rows: 0 libera root, 1 #go, 2 ergo root, 3 #eco, 4 oftc (offline pin)
	cases := []struct {
		name string
		from Ref
		dir  int
		want Ref
	}{
		{"forward within a network", ref(sA, ServerBuffer), 1, ref(sA, "#go")},
		{"forward across networks", ref(sA, "#go"), 1, ref(sB, ServerBuffer)},
		{"forward past the offline pin wraps", ref(sB, "#eco"), 1, ref(sA, ServerBuffer)},
		{"backward past the offline pin wraps", ref(sA, ServerBuffer), -1, ref(sB, "#eco")},
		{"backward within a network", ref(sB, "#eco"), -1, ref(sB, ServerBuffer)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := twoNetworks(t)
			h.SetPinned([]string{"oftc"})
			h.Select(c.from)
			i, ok := h.Step(c.dir)
			if !ok {
				t.Fatal("Step found nowhere to go")
			}
			row := h.Rows()[i]
			if !row.Ref.same(c.want) {
				t.Errorf("Step(%d) from %+v = %+v, want %+v", c.dir, c.from, row.Ref, c.want)
			}
			if !row.Live {
				t.Errorf("Step(%d) landed on %q, a profile with no session: selecting it would dial", c.dir, row.Profile)
			}
		})
	}
}

// With nothing but offline pins there is nowhere to step, and saying so is the
// point: the alternative is stepping onto one and connecting.
func TestStepWithNoLiveRows(t *testing.T) {
	h := newHarness()
	h.SetPinned([]string{"oftc", "efnet"})
	if i, ok := h.Step(1); ok {
		t.Errorf("Step = row %d, want nowhere to go among offline pins", i)
	}
}

// Showing a buffer re-attaches the message pane to the tail; the renderer's
// own sampling is what detaches it again.
func TestSelectFollowsTheTail(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetFollow(false) // the reader scrolled up
	h.Select(ref(sB, "#eco"))
	if !h.Follow() {
		t.Error("a freshly shown buffer is not following the tail; it would open mid-scrollback")
	}
}

// --- structural ---------------------------------------------------------------

// The viewmodel exists so this state can be tested without a toolkit. If an
// import of one ever appears, that is no longer true — and the next person to
// need a widget here will reach for one unless this fails first.
func TestNoToolkitImports(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("reading this package's imports: %v", err)
	}
	for _, imp := range append(append([]string{}, pkg.Imports...), pkg.TestImports...) {
		if strings.Contains(imp, "fyne") {
			t.Errorf("internal/ui/viewmodel imports %q; it must hold no widgets", imp)
		}
	}
}
