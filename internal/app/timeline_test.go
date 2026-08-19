package app

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/model"
)

// M11(c) at the seam where it meets the app: the Timeline is THE chat pane -
// no flag, no second path - and Reader mode is the copy stance's other half.
// The Timeline's own behaviour (grouping, dividers, stick-to-bottom, paging,
// heights) is tested in internal/ui/widgets, headless and without a window.

// newTestUI builds a headless window with the real widget tree.
func newTestUI(t *testing.T) *UI {
	t.Helper()
	pinTestSettings(t)
	a := test.NewApp()
	t.Cleanup(a.Quit)
	applySettings(a)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	return NewUI(a, w)
}

// TestTheChatPaneIsTheTimeline: the flip, asserted. There is one pane and it
// is the Timeline; the old RichText is not hidden somewhere, it is gone.
func TestTheChatPaneIsTheTimeline(t *testing.T) {
	u := newTestUI(t)
	if u.timeline == nil {
		t.Fatal("no Timeline")
	}
	if !u.timeline.Visible() {
		t.Error("the Timeline is not the visible pane")
	}
	if u.readerPane.Visible() {
		t.Error("Reader mode is open on a fresh window")
	}
}

// TestTheTimelineTakesALineAtTheBatchBoundary: the funnel's contract, kept.
// A line does not reach the pane when it arrives, it reaches it when the
// batch closes - one measure-and-repaint for a hundred lines of playback.
func TestTheTimelineTakesALineAtTheBatchBoundary(t *testing.T) {
	u := newTestUI(t)
	u.appendLine(model.Line{Time: time.Now(), Nick: "alice", Text: "hello there"})
	if got := len(u.timeline.Lines()); got != 0 {
		t.Errorf("the Timeline drew %d lines before the batch closed, want 0", got)
	}
	u.BatchDone()
	if got := len(u.timeline.Lines()); got != 1 {
		t.Errorf("the Timeline holds %d lines, want 1", got)
	}
}

// TestNearBottomIsThePane's: the stick-to-bottom measurement is the widget's
// own, and the app asks it rather than keeping a second answer.
func TestNearBottomIsThePanes(t *testing.T) {
	u := newTestUI(t)
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

// TestMonoBodyIsTheGateDecision. The M11(c) gate: conversations are
// proportional, the network log - server output in columns - is not.
func TestMonoBodyIsTheGateDecision(t *testing.T) {
	for _, buffer := range []string{"#chat", "#Ergo", "alice", ""} {
		if monoBody(buffer) {
			t.Errorf("%q renders its bodies monospace; the gate said proportional", buffer)
		}
	}
	for _, buffer := range []string{serverBuffer, strings.ToUpper(serverBuffer)} {
		if !monoBody(buffer) {
			t.Errorf("the network log %q lost its monospace body", buffer)
		}
	}
}

// TestQuoteFillsTheComposer: Quote is not a clipboard operation, it is a
// reply that has been started for you.
func TestQuoteFillsTheComposer(t *testing.T) {
	u := newTestUI(t)
	u.quoteIntoComposer(model.Line{Nick: "alice", Text: "the \x02quoted\x02 line"})
	if got, want := u.input.Text, "> <alice> the quoted line\n"; got != want {
		t.Errorf("composer holds %q, want %q", got, want)
	}
}

// TestURLResolverRefusesWhatItShouldNotOpen. render already filters the
// scheme; this is the second gate, because what comes through it is handed to
// the desktop.
func TestURLResolverRefusesWhatItShouldNotOpen(t *testing.T) {
	u := newTestUI(t)
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

// --- Reader mode -------------------------------------------------------------

// TestReaderModeTogglesFromTheKeyboard: F6 opens it over the chat pane and
// closes it again, from the composer, which is where focus lives.
func TestReaderModeTogglesFromTheKeyboard(t *testing.T) {
	u := newTestUI(t)
	u.appendLine(model.Line{Time: time.Now(), Nick: "alice", Text: "hello there"})
	u.BatchDone()

	u.input.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF6})
	if !u.readerOn || !u.readerPane.Visible() {
		t.Fatal("F6 in the composer did not open Reader mode")
	}
	if u.timeline.Visible() {
		t.Error("the chat pane is still up under Reader mode")
	}
	if !strings.Contains(u.reader.Text, "<alice> hello there") {
		t.Errorf("Reader mode shows %q", u.reader.Text)
	}

	u.reader.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF6})
	if u.readerOn || u.readerPane.Visible() {
		t.Fatal("F6 in Reader mode did not close it")
	}
	if !u.timeline.Visible() {
		t.Error("the chat pane did not come back")
	}
}

// TestReaderModeClosesOnEscape - the Escape stack's bottom step for this
// pane, and the reason the composer gets focus back.
func TestReaderModeClosesOnEscape(t *testing.T) {
	u := newTestUI(t)
	u.toggleReader()
	u.reader.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if u.readerOn {
		t.Error("Escape left Reader mode open")
	}
}

// TestReaderModeIsAStillPicture. Messages keep arriving while it is open and
// they do NOT re-lay the text box out: one arriving message over 500 lines
// costs more than the whole Timeline does, and it would eat the selection
// being made. They are in the pane when it closes.
func TestReaderModeIsAStillPicture(t *testing.T) {
	u := newTestUI(t)
	u.appendLine(model.Line{Time: time.Now(), Nick: "alice", Text: "first"})
	u.BatchDone()
	u.toggleReader()
	before := u.reader.Text
	u.appendLine(model.Line{Time: time.Now(), Nick: "bob", Text: "second"})
	u.BatchDone()
	if u.reader.Text != before {
		t.Errorf("an arriving message rebuilt the open reader:\n%s", u.reader.Text)
	}
	if got := len(u.timeline.Lines()); got != 2 {
		t.Errorf("the pane behind it holds %d lines, want 2", got)
	}
}

// TestReaderModeIsRebuiltWhenTheBufferIs. A withdrawn message must not
// survive in a view someone can copy, and a buffer switch must not leave the
// last one's text sitting there.
func TestReaderModeIsRebuiltWhenTheBufferIs(t *testing.T) {
	u := newTestUI(t)
	u.appendLine(model.Line{Time: time.Now(), Nick: "alice", Text: "secret"})
	u.BatchDone()
	u.toggleReader()
	if !strings.Contains(u.reader.Text, "secret") {
		t.Fatalf("reader shows %q", u.reader.Text)
	}
	u.timeline.SetBuffer(nil, time.Time{}) // the redaction repaint, in miniature
	u.refreshReader()
	if strings.Contains(u.reader.Text, "secret") {
		t.Errorf("the withdrawn text survived in the reader: %q", u.reader.Text)
	}
}

// TestReaderWindow is the arithmetic behind "the part you were reading":
// Fyne lays a text widget out in time quadratic in its rows (500 lines 0.1s,
// 10,000 lines 44s, measured), so this is a bound, and it has to hold what
// was on screen.
func TestReaderWindow(t *testing.T) {
	cases := []struct {
		name              string
		n, first, last    int
		wantLo, wantHi    int
		mustHoldFirstLast bool
	}{
		{name: "empty buffer", n: 0, wantLo: 0, wantHi: 0},
		{name: "short buffer is whole", n: 40, first: 0, last: 40, wantLo: 0, wantHi: 40},
		{name: "nothing on screen shows the newest", n: 5000, first: 0, last: 0,
			wantLo: 5000 - 1 - readerContext, wantHi: 5000},
		{name: "at the bottom", n: 5000, first: 4970, last: 5000,
			wantLo: 4970 - readerContext, wantHi: 5000, mustHoldFirstLast: true},
		{name: "scrolled up", n: 5000, first: 2000, last: 2030,
			wantLo: 2000 - readerContext, wantHi: 2030 + readerContext, mustHoldFirstLast: true},
		{name: "at the top", n: 5000, first: 0, last: 30,
			wantLo: 0, wantHi: 30 + readerContext, mustHoldFirstLast: true},
		{name: "a screenful bigger than the cap", n: 5000, first: 1000, last: 3000,
			wantLo: 3000 + readerContext - readerMax, wantHi: 3000 + readerContext},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lo, hi := readerWindow(c.n, c.first, c.last)
			if lo != c.wantLo || hi != c.wantHi {
				t.Errorf("window = [%d,%d), want [%d,%d)", lo, hi, c.wantLo, c.wantHi)
			}
			if lo < 0 || hi > c.n || lo > hi {
				t.Fatalf("window [%d,%d) is not a slice of %d lines", lo, hi, c.n)
			}
			if hi-lo > readerMax {
				t.Errorf("window holds %d lines, cap is %d", hi-lo, readerMax)
			}
			if c.mustHoldFirstLast && (lo > c.first || hi < c.last) {
				t.Errorf("window [%d,%d) does not hold what was on screen [%d,%d)",
					lo, hi, c.first, c.last)
			}
		})
	}
}

// TestReaderTextIsPlain: no formatting codes reach the clipboard, and the
// three line shapes read the way a log should.
func TestReaderTextIsPlain(t *testing.T) {
	at := time.Date(2025, 6, 2, 14, 2, 0, 0, time.UTC)
	got := readerText([]Line{
		{Time: at, Nick: "alice", Text: "look at \x0304this\x03 and \x02that\x02"},
		{Time: at, Nick: "bob", Text: "waves", Action: true},
		{Time: at, Text: "carol joined", Status: true},
	})
	for _, want := range []string{
		"<alice> look at this and that",
		"* bob waves",
		"* carol joined",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reader text is missing %q:\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "\x02\x03") {
		t.Errorf("formatting codes survived into the reader text: %q", got)
	}
}
