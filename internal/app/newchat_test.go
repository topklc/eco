package app

import (
	"net/url"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/model"
)

// M11(b) at the seam where it meets the app: the flag really does choose, and
// each path really is the one that runs. The Timeline's own behaviour —
// grouping, dividers, stick-to-bottom, paging, heights — is tested in
// internal/ui/widgets, headless and without a window.

func TestNewChatFlagParsing(t *testing.T) {
	cases := map[string]bool{
		"":       false,
		"0":      false,
		"false":  false,
		"banana": false,
		"1":      true,
		"true":   true,
		"TRUE":   true,
	}
	for v, want := range cases {
		t.Setenv("ECO_NEWCHAT", v)
		if got := newChatEnabled(); got != want {
			t.Errorf("ECO_NEWCHAT=%q enables the Timeline = %v, want %v", v, got, want)
		}
	}
}

// newFlagUI builds a headless window with the flag set the given way.
func newFlagUI(t *testing.T, on string) *UI {
	t.Helper()
	pinTestSettings(t)
	t.Setenv("ECO_NEWCHAT", on)
	a := test.NewApp()
	t.Cleanup(a.Quit)
	applySettings(a)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	return NewUI(a, w)
}

// TestFlagChoosesTheChatPane: one chat pane, chosen at build time, and the
// other one is not merely hidden — it is not in the window.
func TestFlagChoosesTheChatPane(t *testing.T) {
	off := newFlagUI(t, "0")
	if off.timelineShowing() {
		t.Error("the Timeline is up with the flag off")
	}
	if off.chatPane != off.msgScroll {
		t.Errorf("the chat pane is %T, want v1's scroller", off.chatPane)
	}

	on := newFlagUI(t, "1")
	if !on.timelineShowing() {
		t.Fatal("the flag is on but there is no Timeline")
	}
	if on.chatPane != on.timeline {
		t.Errorf("the chat pane is %T, want the Timeline", on.chatPane)
	}
}

// TestBothPathsTakeALine is the "both must work" half of the flag: the same
// call lands in whichever pane is up, and never in both.
func TestBothPathsTakeALine(t *testing.T) {
	l := model.Line{Time: time.Now(), Nick: "alice", Text: "hello there"}

	old := newFlagUI(t, "0")
	old.appendLine(l)
	if len(old.messages.Segments) == 0 {
		t.Error("the old path drew nothing")
	}

	timeline := newFlagUI(t, "1")
	timeline.appendLine(l)
	if got := len(timeline.timeline.Lines()); got != 0 {
		t.Errorf("the Timeline drew %d lines before the batch closed, want 0", got)
	}
	timeline.BatchDone() // the funnel's one repaint per batch
	if got := len(timeline.timeline.Lines()); got != 1 {
		t.Errorf("the Timeline holds %d lines, want 1", got)
	}
	if len(timeline.messages.Segments) != 0 {
		t.Error("the old RichText was drawn into as well")
	}
}

// TestNearBottomFollowsThePane: the stick-to-bottom measurement is the pane's
// own, and the app asks whichever pane is up rather than keeping a second
// answer.
func TestNearBottomFollowsThePane(t *testing.T) {
	u := newFlagUI(t, "1")
	if !u.nearBottom() {
		t.Error("an empty Timeline should count as at the bottom")
	}
	for i := 0; i < 200; i++ {
		u.appendLine(model.Line{Time: time.Now(), Nick: "alice", Text: "line"})
	}
	u.BatchDone()
	u.timeline.ScrollToTop()
	if u.nearBottom() {
		t.Error("scrolled to the top of a full Timeline, still reported near the bottom")
	}
}

// TestQuoteFillsTheComposer: Quote is not a clipboard operation, it is a
// reply that has been started for you.
func TestQuoteFillsTheComposer(t *testing.T) {
	u := newFlagUI(t, "1")
	u.quoteIntoComposer(model.Line{Nick: "alice", Text: "the \x02quoted\x02 line"})
	if got, want := u.input.Text, "> <alice> the quoted line\n"; got != want {
		t.Errorf("composer holds %q, want %q", got, want)
	}
}

// TestURLResolverRefusesWhatItShouldNotOpen. render already filters the
// scheme; this is the second gate, because what comes through it is handed to
// the desktop.
func TestURLResolverRefusesWhatItShouldNotOpen(t *testing.T) {
	u := newFlagUI(t, "1")
	resolve := u.urlResolver()
	for _, raw := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.com/x",
		"https://",
		"http://",
		":://not a url",
	} {
		if resolve(raw) != nil {
			t.Errorf("%q was offered as a tappable link", raw)
		}
	}
	for _, raw := range []string{"https://eco.chat/docs", "http://example.com"} {
		if resolve(raw) == nil {
			t.Errorf("%q was refused", raw)
		}
		if _, err := url.Parse(raw); err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
	}
}
