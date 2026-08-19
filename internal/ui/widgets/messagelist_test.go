package widgets

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
)

// newTestList builds a Timeline in a headless window of a fixed size.
func newTestList(t *testing.T, opts Options, lines []model.Line) (*MessageList, fyne.Window) {
	t.Helper()
	testApp(t)
	if opts.Render == nil {
		opts.Render = func() render.Options {
			return render.Options{ShowTimestamps: true, TimeLayout: "15:04", ColorfulNicks: true}
		}
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return at }
	}
	m := NewMessageList(opts)
	w := test.NewWindow(m)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(700, 400))
	m.SetBuffer(lines, time.Time{})
	return m, w
}

// TestTimelineDraws is the smoke test: a buffer goes in, rows come out, and
// the widget lays them out without a canvas complaining.
func TestTimelineDraws(t *testing.T) {
	lines := []model.Line{
		msg("alice", "hello there", 0),
		msg("alice", "and another thing", 30*time.Second),
		msg("bob", "hi alice", time.Minute),
		{Time: at.Add(90 * time.Second), Status: true, Text: "carol joined"},
	}
	m, _ := newTestList(t, Options{}, lines)

	if got, want := len(m.Rows()), len(lines)+1; got != want {
		t.Fatalf("%d rows for %d lines, want %d (one day divider)", got, len(lines), want)
	}
	if m.Rows()[0].Kind != RowDay {
		t.Errorf("first row is %v, want a day divider", m.Rows()[0].Kind)
	}
	for i, h := range m.heights {
		if h <= 0 {
			t.Errorf("row %d has height %v", i, h)
		}
	}
	if m.total <= 0 {
		t.Errorf("content height %v", m.total)
	}
}

// TestHeightsMatchTheDrawnRow is the invariant the whole bet rests on: the
// height the List was told is the height the row actually needs. Short means
// the last line of a message is clipped off the bottom of its own row.
func TestHeightsMatchTheDrawnRow(t *testing.T) {
	long := "this is a deliberately long message that has to wrap several times at seven hundred pixels wide, so that the arithmetic and the widget have something to disagree about"
	lines := []model.Line{
		msg("alice", "short", 0),
		msg("verylongnickindeed", long, time.Minute),
		msg("bo", long+" "+long, 2*time.Minute),
	}
	m, _ := newTestList(t, Options{}, lines)
	row := newTimelineRow(m)
	for i, r := range m.Rows() {
		if r.Kind != RowMessage {
			continue
		}
		row.set(r)
		row.Resize(fyne.NewSize(700, m.heights[i]))
		want := row.body.MinSize().Height - 2*m.Theme().Size("innerPadding")
		gap := float32(0)
		if r.Gap {
			gap = authorGap
		}
		if got := m.heights[i] - gap; got < want-0.01 {
			t.Errorf("row %d: list told %v, the drawn body needs %v", i, got, want)
		}
	}
}

// TestArrivalDoesNotYankTheReader is v1's stickBottom discipline, verbatim:
// a message arriving while the reader is up in the scrollback must not move
// the view, and must show up in the jump pill instead.
func TestArrivalDoesNotYankTheReader(t *testing.T) {
	var lines []model.Line
	for i := 0; i < 60; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{}, lines)
	if !m.NearBottom() {
		t.Fatal("a freshly opened buffer should be at the bottom")
	}

	m.list.ScrollToOffset(0) // the reader goes up
	if m.NearBottom() {
		t.Fatal("scrolled to the top but still counted as near the bottom")
	}
	before := m.list.GetScrollOffset()
	m.Append(msg("bob", "something new", 5*time.Minute))
	if got := m.list.GetScrollOffset(); got != before {
		t.Errorf("the view moved from %v to %v while the reader was reading", before, got)
	}
	if m.Unseen() != 1 {
		t.Errorf("unseen = %d, want 1", m.Unseen())
	}
	if !m.pill.Visible() {
		t.Error("the jump pill should be up")
	}

	// and back at the bottom, arrival follows again
	m.ScrollToBottom()
	if m.Unseen() != 0 || m.pill.Visible() {
		t.Error("jumping to the bottom should clear the pill")
	}
	m.Append(msg("bob", "and another", 6*time.Minute))
	if !m.NearBottom() {
		t.Error("the view should have followed the new message")
	}
	if m.Unseen() != 0 {
		t.Errorf("unseen = %d after following, want 0", m.Unseen())
	}
}

// TestReachingTheTopAsksForOlder wires the M9 pager to the scroll position.
func TestReachingTheTopAsksForOlder(t *testing.T) {
	asked := 0
	var lines []model.Line
	for i := 0; i < 80; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{Older: func() { asked++ }}, lines)
	asked = 0
	m.list.ScrollToOffset(0)
	m.list.Refresh()
	if asked == 0 {
		t.Error("reaching the top did not ask the pager for older messages")
	}
}

// TestOpeningABufferDoesNotAskForOlder: laying a fresh List out starts at
// offset zero and therefore visits the top row, before the jump to the bottom
// has happened. If that counted as "the reader reached the top", every buffer
// switch would quietly fetch a page of history nobody asked for.
func TestOpeningABufferDoesNotAskForOlder(t *testing.T) {
	asked := 0
	var lines []model.Line
	for i := 0; i < 80; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{Older: func() { asked++ }}, lines)
	if asked != 0 {
		t.Errorf("opening a buffer asked the pager %d times", asked)
	}
	m.SetBuffer(lines, time.Time{})
	if asked != 0 {
		t.Errorf("switching to a buffer asked the pager %d times", asked)
	}
}

// TestPrependKeepsTheReaderInPlace: a page of older messages arrives above
// the reader, and the line they were looking at stays where it was.
func TestPrependKeepsTheReaderInPlace(t *testing.T) {
	var lines []model.Line
	for i := 0; i < 60; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{}, lines)
	m.list.ScrollToOffset(200)
	before := m.list.GetScrollOffset()
	beforeTotal := m.total

	var older []model.Line
	for i := 0; i < 20; i++ {
		older = append(older, msg("carol", "older", -time.Duration(60-i)*time.Second))
	}
	m.Prepend(older)

	grew := m.total - beforeTotal
	if grew <= 0 {
		t.Fatalf("prepending %d lines grew the content by %v", len(older), grew)
	}
	if got, want := m.list.GetScrollOffset(), before+grew; got != want {
		t.Errorf("offset %v after prepend, want %v (moved by exactly what arrived above)", got, want)
	}
	if len(m.Lines()) != 80 {
		t.Errorf("%d lines after prepend, want 80", len(m.Lines()))
	}
}

// TestLoadingRowAppearsOnTop shows the backfill state where the backfill is.
func TestLoadingRowAppearsOnTop(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{msg("alice", "hi", 0)})
	if m.Rows()[0].Kind == RowLoading {
		t.Fatal("the loading row is up before anything is loading")
	}
	m.SetLoadingOlder(true)
	if m.Rows()[0].Kind != RowLoading {
		t.Fatalf("first row is %v, want the loading row", m.Rows()[0].Kind)
	}
	m.SetLoadingOlder(false)
	if m.Rows()[0].Kind == RowLoading {
		t.Error("the loading row outlived the backfill")
	}
}

// TestRowActionsObeyTheParityRule: whatever the hover strip offers, the
// right-click menu offers, and neither offers anything the other does not
// (blueprint §4.2's Parity Rule). Both read one list, and this is the test
// that says so.
func TestRowActionsObeyTheParityRule(t *testing.T) {
	var copied []string
	var quoted, deleted []model.Line
	lines := []model.Line{
		msg("alice", "someone else's message", 0),
		{Time: at.Add(time.Minute), Nick: "me", Text: "mine, sent", Self: true, MsgID: "abc"},
		{Time: at.Add(2 * time.Minute), Nick: "me", Text: "mine, not acked", Self: true},
		{Time: at.Add(3 * time.Minute), Status: true, Text: "bob joined"},
	}
	m, _ := newTestList(t, Options{
		Copy:   func(s string) { copied = append(copied, s) },
		Quote:  func(l model.Line) { quoted = append(quoted, l) },
		Delete: func(l model.Line) { deleted = append(deleted, l) },
	}, lines)

	row := newTimelineRow(m)
	want := map[string][]actionKind{
		"someone else's message": {actCopy, actCopyNick, actQuote},
		"mine, sent":             {actCopy, actCopyNick, actQuote, actDelete},
		"mine, not acked":        {actCopy, actCopyNick, actQuote},
		"bob joined":             {actCopy},
	}
	for _, r := range m.Rows() {
		row.set(r)
		got := row.actions()
		if r.Kind != RowMessage {
			if got != nil {
				t.Errorf("a %v row offers %d actions", r.Kind, len(got))
			}
			continue
		}
		exp := want[r.Line.Text]
		if len(got) != len(exp) {
			t.Errorf("%q offers %d actions, want %d", r.Line.Text, len(got), len(exp))
			continue
		}
		for j := range got {
			if got[j].kind != exp[j] {
				t.Errorf("%q action %d is %v, want %v", r.Line.Text, j, got[j].kind, exp[j])
			}
			if got[j].label == "" {
				t.Errorf("%q action %d has no label for the menu", r.Line.Text, j)
			}
		}
		// the hover strip is the same list, wired to the same call
		row.MouseIn(nil)
		shown := 0
		for _, o := range row.strip.Objects {
			if o.Visible() {
				shown++
			}
		}
		if shown != len(exp) {
			t.Errorf("%q shows %d hover icons for %d actions", r.Line.Text, shown, len(exp))
		}
		row.MouseOut()
	}

	// and the calls themselves carry the right payload
	row.set(m.Rows()[2]) // "mine, sent"
	row.act(actCopy)
	row.act(actCopyNick)
	row.act(actQuote)
	row.act(actDelete)
	if len(copied) != 2 || copied[0] != "mine, sent" || copied[1] != "me" {
		t.Errorf("copied %q", copied)
	}
	if len(quoted) != 1 || quoted[0].Text != "mine, sent" {
		t.Errorf("quoted %v", quoted)
	}
	if len(deleted) != 1 || deleted[0].MsgID != "abc" {
		t.Errorf("deleted %v", deleted)
	}
}

// TestCopyStripsFormatting: what lands on the clipboard is what a person
// reads, not the wire bytes.
func TestCopyStripsFormatting(t *testing.T) {
	var copied string
	m, _ := newTestList(t, Options{Copy: func(s string) { copied = s }},
		[]model.Line{msg("alice", "look at \x0304this\x03", 0)})
	row := newTimelineRow(m)
	row.set(m.Rows()[1])
	row.act(actCopy)
	if copied != "look at this" {
		t.Errorf("copied %q, want the text without the codes", copied)
	}
}

// TestArtGetsItsOwnRow: mIRC background art is a character grid, not prose,
// and the Timeline gives it a row type of its own rather than trying to wrap
// it (blueprint §4.2).
func TestArtGetsItsOwnRow(t *testing.T) {
	art := "\x0300,01" + strings.Repeat("█", 60)
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("alice", "before", 0),
		msg("alice", art, time.Second),
	})
	var kinds []RowKind
	for _, r := range m.Rows() {
		kinds = append(kinds, r.Kind)
	}
	if len(kinds) != 3 || kinds[2] != RowArt {
		t.Fatalf("rows %v, want the art line as its own row", kinds)
	}
	if m.artFor(1) == nil {
		t.Error("the art row has nothing to draw")
	}
	if m.heights[2] <= 0 {
		t.Errorf("the art row is %v tall", m.heights[2])
	}
}

// TestURLsReachTheTimeline: the v1 gap, closed - a link in a message body is
// tappable, and tapping it is what opens the browser.
func TestURLsReachTheTimeline(t *testing.T) {
	opened := ""
	m, _ := newTestList(t, Options{
		Render: func() render.Options {
			return render.Options{
				ShowTimestamps: true, TimeLayout: "15:04",
				URL: func(raw string) func() { return func() { opened = raw } },
			}
		},
	}, []model.Line{msg("alice", "read https://eco.chat/docs today", 0)})

	var links []*widget.HyperlinkSegment
	for _, s := range m.bodyFor(0) {
		if h, ok := s.(*widget.HyperlinkSegment); ok {
			links = append(links, h)
		}
	}
	if len(links) != 1 {
		t.Fatalf("%d links in the row body, want one", len(links))
	}
	links[0].OnTapped()
	if opened != "https://eco.chat/docs" {
		t.Errorf("tapping the link opened %q", opened)
	}
}

// TestDisplayOptionsRedraw: turning timestamps off moves the gutter and
// every body with it, without the caller doing anything but asking for a
// redraw.
func TestDisplayOptionsRedraw(t *testing.T) {
	stamps := true
	m, _ := newTestList(t, Options{
		Render: func() render.Options {
			return render.Options{ShowTimestamps: stamps, TimeLayout: "15:04"}
		},
	}, []model.Line{msg("alice", "hello", 0)})
	with := m.geom.gutter
	if with <= 0 {
		t.Fatal("timestamps are on but the gutter is empty")
	}
	stamps = false
	m.Redraw()
	if m.geom.gutter != 0 {
		t.Errorf("gutter %v with timestamps off, want 0", m.geom.gutter)
	}
}

// TestVisibleLines is what Reader mode (F6) is aimed with: which lines the
// reader can actually see. A plain-text view of a whole buffer is quadratic
// in Fyne, so the app shows a window around this range - and a wrong answer
// here means the window does not contain what the user was looking at.
func TestVisibleLines(t *testing.T) {
	var lines []model.Line
	for i := 0; i < 400; i++ {
		lines = append(lines, msg("alice", "message", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{}, lines)

	// opened at the bottom: the newest line is on screen and the oldest is not
	lo, hi := m.VisibleLines()
	if hi != len(lines) {
		t.Errorf("at the bottom the visible range is [%d,%d), want it to end at %d", lo, hi, len(lines))
	}
	if lo == 0 {
		t.Error("the whole 400-line buffer cannot fit on one 400px screen")
	}
	if hi-lo <= 0 {
		t.Fatalf("visible range [%d,%d) is empty", lo, hi)
	}

	// scrolled to the top: the first line is on screen and the last is not
	m.ScrollToTop()
	lo, hi = m.VisibleLines()
	if lo != 0 {
		t.Errorf("at the top the visible range starts at %d, want 0", lo)
	}
	if hi >= len(lines) {
		t.Errorf("at the top the visible range reaches %d of %d lines", hi, len(lines))
	}

	// an empty buffer has nothing on screen, and says so rather than lying
	// about line zero
	m.SetBuffer(nil, time.Time{})
	if lo, hi = m.VisibleLines(); lo != 0 || hi != 0 {
		t.Errorf("an empty buffer reports [%d,%d), want [0,0)", lo, hi)
	}
}

// TestScrollPageMovesOneScreenWithOverlap is §4.3's PageUp/PageDown, once the
// composer has delegated them here. A page keeps one row in common between the
// two screens - a page that moved the full height would leave the reader with
// no line to pick the sentence back up from - and paging to the end re-follows,
// so arriving at the newest message by keyboard is not a second, invisible kind
// of "at the bottom".
func TestScrollPageMovesOneScreenWithOverlap(t *testing.T) {
	var lines []model.Line
	for i := 0; i < 200; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	m, _ := newTestList(t, Options{}, lines)
	view := m.list.Size().Height
	if view <= 0 {
		t.Fatal("the list has no height to page")
	}

	m.ScrollToTop()
	top := m.list.GetScrollOffset()
	m.ScrollPage(1)
	after := m.list.GetScrollOffset()
	moved := after - top
	if moved <= 0 {
		t.Fatalf("PageDown moved %v", moved)
	}
	if moved >= view {
		t.Errorf("PageDown moved %v of a %v screen; the pages share no line", moved, view)
	}
	if moved < view/2 {
		t.Errorf("PageDown moved only %v of a %v screen", moved, view)
	}

	m.ScrollPage(-1)
	if got := m.list.GetScrollOffset(); got != top {
		t.Errorf("PageUp came back to %v, want the top at %v", got, top)
	}
	// past the ends: clamped, not lost
	m.ScrollPage(-1)
	if got := m.list.GetScrollOffset(); got != top {
		t.Errorf("PageUp past the top left the view at %v", got)
	}
	for range 40 {
		m.ScrollPage(1)
	}
	if !m.NearBottom() {
		t.Error("paging past the end did not reach the newest message")
	}
	if m.Unseen() != 0 {
		t.Errorf("unseen = %d after paging to the bottom, want the view to be following", m.Unseen())
	}
}
