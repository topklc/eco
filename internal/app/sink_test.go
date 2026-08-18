package app

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The REAL Sink, headlessly.
//
// Every other test in this package hands the funnel a recorder (testUI) and
// asserts on what it was told, which proves the stream but stops one layer
// short: sink.go — what the window actually DOES with a delivery — was executed
// by nothing. The M8 verification mutated it three ways (never file the store's
// count, never migrate a rename, answer "is this on screen?" by name alone) and
// the whole suite stayed green each time.
//
// The methods below are the ones that touch no widget, which since M8 is where
// the decisions are: the counts, the rename migration and the on-screen test
// are all questions for the viewmodel, and the widgets only draw the answer.
// The rest of sink.go — dialogs, the topic label, the member pane — still needs
// a canvas and is still not covered here.

// testWindowUI builds a UI as a Sink with no widgets in it: a real funnel, a
// real Session and a real viewmodel, with the viewer on the session's log.
func testWindowUI(t *testing.T) (*UI, *Session, *testUI) {
	t.Helper()
	rec, d := newTestSink(t)
	s := NewSession(Config{Server: "irc.test", Port: "6667", Nick: testNick}, d)
	s.Name = "libera"
	u := &UI{dispatch: d, sessions: []*Session{s}, vm: viewmodel.New(viewmodel.Hooks{})}
	u.vm.AddSession(s.ID(), s.Name)
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: serverBuffer})
	return u, s, rec
}

// The last mile of the count. Everything upstream of this is proven with a
// recorder standing in for the window (TestRegression_UnreadCountsComeFromThe
// Store), so this is the only place the window's own arithmetic is checked: the
// number the store derived has to reach the viewmodel, and the sidebar has to
// be told to repaint.
func TestWindow_UnreadChangedFilesTheStoresCount(t *testing.T) {
	u, s, _ := testWindowUI(t)

	u.UnreadChanged(s.ID(), dispatch.Buffer{Name: "#eco"}, 3, 1)

	want := viewmodel.Counts{Unread: 3, Highlights: 1}
	if got := u.vm.Badge(viewmodel.Ref{Session: s.ID(), Name: "#eco"}); got != want {
		t.Errorf("the sidebar badge for #eco = %+v, want %+v", got, want)
	}
	if !u.unreadDirty || !u.flushPending {
		t.Errorf("a new count did not ask for a repaint (unreadDirty=%v flushPending=%v)",
			u.unreadDirty, u.flushPending)
	}
	// A count for a session this window does not hold is not filed anywhere.
	u.UnreadChanged("gone", dispatch.Buffer{Name: "#eco"}, 9, 9)
	if got := u.vm.Badge(viewmodel.Ref{Session: "gone", Name: "#eco"}); got != (viewmodel.Counts{}) {
		t.Errorf("a count for an unknown session was filed: %+v", got)
	}
}

// A rename migrates what the window holds per buffer even when the renamed
// buffer is in the background — the case that needs no canvas, and the one
// nobody notices until their half-typed reply is gone.
func TestWindow_RenameMigratesWhatTheWindowHolds(t *testing.T) {
	u, s, _ := testWindowUI(t)
	from := dispatch.Buffer{Name: "#old"}
	to := dispatch.Buffer{Name: "#new"}

	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: from.Name})
	u.vm.SetDraft("half a sentence")
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: serverBuffer}) // park it
	u.UnreadChanged(s.ID(), from, 4, 2)

	u.BufferRenamed(s.ID(), from, to)

	if got := u.vm.Parked(viewmodel.Ref{Session: s.ID(), Name: to.Name}); got != "half a sentence" {
		t.Errorf("the draft did not follow the rename: %q", got)
	}
	want := viewmodel.Counts{Unread: 4, Highlights: 2}
	if got := u.vm.Badge(viewmodel.Ref{Session: s.ID(), Name: to.Name}); got != want {
		t.Errorf("counts after the rename = %+v, want %+v", got, want)
	}
	if got := u.vm.Badge(viewmodel.Ref{Session: s.ID(), Name: from.Name}); got != (viewmodel.Counts{}) {
		t.Errorf("the old name kept a badge: %+v", got)
	}
}

// "Is this on screen?" is a question about a buffer OF A SESSION, not about a
// name. Two networks with the same channel open is the ordinary case, and
// answering by name alone paints one network's messages into the other's pane.
func TestWindow_ShowingIsPerSession(t *testing.T) {
	u, s, _ := testWindowUI(t)
	other := NewSession(Config{Server: "irc.other", Port: "6667", Nick: testNick}, u.dispatch)
	other.Name = "ergo"
	u.sessions = append(u.sessions, other)
	u.vm.AddSession(other.ID(), other.Name)
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: "#eco"})

	if !u.showing(s.ID(), dispatch.Buffer{Name: "#eco"}) {
		t.Error("the buffer on screen does not report itself as showing")
	}
	if u.showing(other.ID(), dispatch.Buffer{Name: "#eco"}) {
		t.Error("another network's #eco reported as the buffer on screen")
	}
	if u.showing(s.ID(), dispatch.Buffer{Name: "#other"}) {
		t.Error("a background buffer of the same session reported as on screen")
	}
}

// §4.4's notification rule, where it is enforced. The suite used to assert this
// against a copy of the rule written in the test file, so changing the shipping
// one — to notify for replayed history, say — broke nothing.
func TestWindow_NotificationRule(t *testing.T) {
	cases := []struct {
		name      string
		line      model.Line
		notifyAll bool
		want      bool
	}{
		{"a mention always notifies", model.Line{Highlight: true}, false, true},
		{"plain chat does not, by default", model.Line{}, false, false},
		{"plain chat does when the user asked for everything", model.Line{}, true, true},
		{"replayed history never does", model.Line{Playback: true}, true, false},
		{"status noise never does", model.Line{Status: true}, true, false},
		{"our own message never does", model.Line{Self: true}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := notifies(c.line, c.notifyAll); got != c.want {
				t.Errorf("notifies(%+v, notifyAll=%v) = %v, want %v", c.line, c.notifyAll, got, c.want)
			}
		})
	}
}
