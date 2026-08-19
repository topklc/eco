package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"

	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The composer's half of §4.3: the Tab cycle in both directions with its
// visible candidates, the modifiers being QUERIED rather than tracked, and the
// keys the composer passes through to the Timeline.

// completionUI is a window-less UI with a real session, a real composer and a
// real candidate strip: enough for the keyboard, without a canvas.
func completionUI(t *testing.T) (*UI, *chatEntry) {
	t.Helper()
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco", "@alice", "alfred", "alison")
	u := testCommandUI(s, "#eco")
	u.app = fynetest.NewTempApp(t)
	applySettings(u.app) // the eco tokens the candidate strip paints with
	u.candidates = widgets.NewCandidates()
	e := newChatEntry(u)
	u.input = e
	return u, e
}

// Tab takes the next candidate and Shift+Tab the previous one, wrapping at both
// ends. The direction is decided by asking the driver which modifiers are held
// - the shiftHeld fix - so the test holds Shift down through the same seam.
func TestShiftTabCyclesBackwards(t *testing.T) {
	u, e := completionUI(t)
	shift := false
	u.modifiers = func() fyne.KeyModifier {
		if shift {
			return fyne.KeyModifierShift
		}
		return 0
	}
	tab := func() string {
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
		return strings.TrimSpace(e.Text)
	}

	e.SetText("al")
	e.CursorRow, e.CursorColumn = 0, len("al")

	// forwards through the three nicks, then round
	var forward []string
	for range 4 {
		forward = append(forward, tab())
	}
	// the order is the roster's - SortedMembers ranks the op first - and the
	// cycle simply walks it
	want := []string{"alice:", "alfred:", "alison:", "alice:"}
	if !eqStrings(forward, want) {
		t.Fatalf("Tab cycled %q, want %q", forward, want)
	}

	// backwards from where it stands, and round the other way
	shift = true
	var back []string
	for range 3 {
		back = append(back, tab())
	}
	if want := []string{"alison:", "alfred:", "alice:"}; !eqStrings(back, want) {
		t.Errorf("Shift+Tab cycled %q, want %q", back, want)
	}
}

// Shift+Tab as the FIRST press starts at the other end of the list, which is
// the only reading of it that is not simply Tab.
func TestShiftTabStartsAtTheEnd(t *testing.T) {
	u, e := completionUI(t)
	u.modifiers = func() fyne.KeyModifier { return fyne.KeyModifierShift }

	e.SetText("al")
	e.CursorRow, e.CursorColumn = 0, len("al")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if got := strings.TrimSpace(e.Text); got != "alison:" {
		t.Errorf("the first Shift+Tab gave %q, want the last candidate", got)
	}
}

// The cycle is visible while it runs (§4.3's candidate popup), it says which
// one is current, and it goes away when the cycle does - a strip left on screen
// after the cycle is over is a lie about what Tab will do next.
func TestTheCandidateStripFollowsTheCycle(t *testing.T) {
	u, e := completionUI(t)
	u.modifiers = func() fyne.KeyModifier { return 0 }

	if u.candidates.Visible() {
		t.Fatal("the candidate strip is on screen before anything was completed")
	}
	e.SetText("al")
	e.CursorRow, e.CursorColumn = 0, len("al")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})

	if !u.candidates.Visible() {
		t.Fatal("the candidate strip did not appear for a running cycle")
	}
	if got, want := u.candidates.Items(), []string{"alice", "alfred", "alison"}; !eqStrings(got, want) {
		t.Errorf("strip = %q, want %q", got, want)
	}
	if got := u.candidates.Selected(); got != 0 {
		t.Errorf("selected = %d, want the first", got)
	}
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if got := u.candidates.Selected(); got != 1 {
		t.Errorf("selected after a second Tab = %d, want 1", got)
	}

	// typing ends the cycle, and takes the strip with it
	e.TypedRune('x')
	if u.candidates.Visible() {
		t.Error("the strip outlived the cycle it was describing")
	}
}

// One candidate is not a cycle: the word is completed and a strip saying so is
// chrome for its own sake.
func TestOneCandidateDrawsNoStrip(t *testing.T) {
	u, e := completionUI(t)
	u.modifiers = func() fyne.KeyModifier { return 0 }

	e.SetText("alf")
	e.CursorRow, e.CursorColumn = 0, len("alf")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if got := strings.TrimSpace(e.Text); got != "alfred:" {
		t.Fatalf("Tab gave %q", got)
	}
	if u.candidates.Visible() {
		t.Error("a single candidate drew a strip")
	}
}

// Escape cancels the cycle through the ONE method that also clears the strip.
func TestEscapeEndsTheCycleAndTheStrip(t *testing.T) {
	u, e := completionUI(t)
	u.modifiers = func() fyne.KeyModifier { return 0 }
	e.SetText("al")
	e.CursorRow, e.CursorColumn = 0, len("al")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})

	if got := escapeTarget(u.escapeState()); got != escCompletion {
		t.Fatalf("Escape would have done %d, want the completion", got)
	}
	u.handleEscape()
	if len(e.completions) != 0 {
		t.Error("Escape left the cycle running")
	}
	if u.candidates.Visible() {
		t.Error("Escape left the strip on screen")
	}
}

// Enter sends and Shift+Enter adds a line - decided by asking, not by a bool
// that desyncs when a KeyUp is delivered somewhere else. That desync is the
// bug: v1 would send a message where a newline was meant, for as long as the
// user did not press Shift again.
func TestShiftEnterIsQueriedNotRemembered(t *testing.T) {
	u, e := completionUI(t)
	shift := true
	u.modifiers = func() fyne.KeyModifier {
		if shift {
			return fyne.KeyModifierShift
		}
		return 0
	}

	e.SetText("first")
	e.CursorRow, e.CursorColumn = 0, len("first")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if !strings.Contains(e.Text, "\n") {
		t.Fatalf("Shift+Enter did not add a line: %q", e.Text)
	}

	// The user lets go of Shift somewhere eco never hears about - alt-tab, a
	// dialog, another window. The next Enter must SEND.
	shift = false
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if e.Text != "" {
		t.Errorf("Enter after releasing Shift left %q in the box; it did not send", e.Text)
	}
}

// --- the keys the composer passes through -------------------------------------------

// PageUp and PageDown are delegated from the composer to the Timeline (§4.3):
// focus lives in the message box, so a key that never leaves it can never
// scroll anything.
func TestPageKeysReachTheTimeline(t *testing.T) {
	u := newTestUI(t)
	for _, k := range []fyne.KeyName{fyne.KeyPageUp, fyne.KeyPageDown} {
		if !u.handleKey(&fyne.KeyEvent{Name: k}) {
			t.Errorf("%s was not claimed by the app", k)
		}
	}
	// Reader mode is a text box and pages itself; the delegation stops there,
	// or the pane behind it would scroll instead.
	u.toggleReader()
	if u.handleKey(&fyne.KeyEvent{Name: fyne.KeyPageUp}) {
		t.Error("PageUp was taken from Reader mode")
	}
	u.closeReader()
}

// End is the jump back to the present, but only when the message box is empty:
// taking the end of someone's line to save a keystroke is a bad trade, and
// Escape does the same jump unconditionally.
func TestEndOnlyJumpsWhenTheBoxIsEmpty(t *testing.T) {
	u := newTestUI(t)
	u.timeline.Resize(fyne.NewSize(400, 200))
	u.timeline.SetBuffer(manyLines(200), time.Time{})
	u.timeline.ScrollToTop()

	u.input.SetText("half a sentence")
	if u.handleKey(&fyne.KeyEvent{Name: fyne.KeyEnd}) {
		t.Error("End was taken from a message being typed")
	}
	u.input.SetText("")
	if !u.handleKey(&fyne.KeyEvent{Name: fyne.KeyEnd}) {
		t.Error("End with an empty box did not jump to the present")
	}
	// already at the bottom: End belongs to the box again
	if u.handleKey(&fyne.KeyEvent{Name: fyne.KeyEnd}) {
		t.Error("End was claimed while already reading the newest message")
	}
}

// Ctrl+Home and Ctrl+End are the two ends of the scrollback, and they are
// rebindable rows of the table like everything else.
func TestScrollbackEndsAreBound(t *testing.T) {
	u := newTestUI(t)
	u.timeline.Resize(fyne.NewSize(400, 200))
	u.timeline.SetBuffer(manyLines(200), time.Time{})

	if !u.handleShortcut(bound(t, u, "top")) {
		t.Fatal("Ctrl+Home was not handled")
	}
	if u.timeline.NearBottom() {
		t.Error("Ctrl+Home did not move the reader off the bottom")
	}
	if !u.handleShortcut(bound(t, u, "bottom")) {
		t.Fatal("Ctrl+End was not handled")
	}
	if !u.timeline.NearBottom() {
		t.Error("Ctrl+End did not come back to the newest message")
	}
}

// manyLines is a buffer tall enough to scroll.
func manyLines(n int) []Line {
	out := make([]Line, 0, n)
	for i := range n {
		out = append(out, Line{Nick: "alice", Text: "line " + strconv.Itoa(i)})
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
