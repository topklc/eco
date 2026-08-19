package widgets

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// Ctrl+F's half of M12: which lines match, how stepping walks them, and the one
// property the highlight has to keep - that a row draws the segments it was
// measured from, because the Timeline's heights are arithmetic and a row drawn
// from anything else is a clipped message.

// findList is a Timeline over a fixed little conversation.
func findList(t *testing.T, lines []model.Line) *MessageList {
	t.Helper()
	testApp(t)
	m := NewMessageList(timelineOptions())
	w := test.NewWindow(m)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(600, 400))
	m.SetBuffer(lines, time.Time{})
	return m
}

var findCorpus = []model.Line{
	msg("alice", "the DEPLOY went out this morning", 0),
	msg("bob", "nice", time.Minute),
	msg("carol", "did the deploy break the build?", 2*time.Minute),
	msg("bob", "no, the build is fine", 3*time.Minute),
	msg("alice", "second deploy queued", 4*time.Minute),
}

// Matching is case-insensitive and is done on the line as a person reads it -
// the formatting codes are stripped first, so a colour code in the middle of a
// word never hides it from the search.
func TestMatchLinesIsCaseInsensitiveAndIgnoresFormatting(t *testing.T) {
	lines := append([]model.Line(nil), findCorpus...)
	lines = append(lines, msg("dave", "the \x0304dep\x03loy is red", 5*time.Minute))

	got := MatchLines(lines, "DePlOy")
	want := []int{0, 2, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("MatchLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MatchLines = %v, want %v", got, want)
		}
	}
	if got := MatchLines(lines, "   "); got != nil {
		t.Errorf("a blank query matched %v; it must match nothing", got)
	}
}

// Stepping wraps at both ends, and a fresh search steps forward to the first
// match and backward to the last.
func TestStepMatchWrapsBothWays(t *testing.T) {
	for _, c := range []struct {
		n, cur, dir, want int
		ok                bool
	}{
		{3, -1, 1, 0, true},  // fresh search, forward
		{3, -1, -1, 2, true}, // fresh search, backward: the newest match
		{3, 0, 1, 1, true},
		{3, 2, 1, 0, true},  // off the end, round to the front
		{3, 0, -1, 2, true}, // off the front, round to the end
		{1, 0, 1, 0, true},  // one match steps to itself
		{1, 0, -1, 0, true},
		{0, -1, 1, -1, false}, // no matches at all
		{0, 0, -1, -1, false},
	} {
		got, ok := StepMatch(c.n, c.cur, c.dir)
		if got != c.want || ok != c.ok {
			t.Errorf("StepMatch(%d, %d, %d) = (%d, %v), want (%d, %v)",
				c.n, c.cur, c.dir, got, ok, c.want, c.ok)
		}
	}
}

// The Timeline's own stepping: setting a query does not move the reader, and
// each step lands on the next matching line, wrapping.
func TestSearchStepWalksTheBuffer(t *testing.T) {
	m := findList(t, findCorpus)

	if got := m.SetSearch("deploy"); got != 3 {
		t.Fatalf("SetSearch found %d matches, want 3", got)
	}
	if m.SearchAt() != -1 || m.SearchLine() != -1 {
		t.Errorf("setting a query already moved to a match (%d/%d); typing must not jump",
			m.SearchAt(), m.SearchLine())
	}
	for _, want := range []int{0, 2, 4, 0} {
		line, ok := m.SearchStep(1)
		if !ok {
			t.Fatalf("SearchStep stopped early, wanted line %d", want)
		}
		if line != want {
			t.Fatalf("SearchStep landed on line %d, want %d", line, want)
		}
	}
	if line, _ := m.SearchStep(-1); line != 4 {
		t.Errorf("Shift+Enter from the first match landed on %d, want the last (4)", line)
	}
}

// Retyping is a new question. The cursor has to go back to "nothing chosen
// yet", or the bar counts against the old answer: step to the third match of
// twelve, narrow the query to two matches, and "3 of 2" is both wrong and
// unrecoverable - the next step walks from an index that was never in this
// list.
func TestANewQueryStartsFromNothingChosen(t *testing.T) {
	m := findList(t, findCorpus)
	m.SetSearch("deploy")
	m.SearchStep(1)
	m.SearchStep(1)
	if m.SearchAt() != 1 {
		t.Fatalf("two steps left the cursor at %d, want 1", m.SearchAt())
	}
	if got := m.SetSearch("build"); got != 2 {
		t.Fatalf("SetSearch(build) found %d matches, want 2", got)
	}
	if m.SearchAt() != -1 || m.SearchLine() != -1 {
		t.Errorf("a new query kept the old cursor (%d/%d), want nothing chosen yet",
			m.SearchAt(), m.SearchLine())
	}
	if line, _ := m.SearchStep(1); line != 2 {
		t.Errorf("the first step of the new query landed on line %d, want the first match (2)", line)
	}
}

// SearchToLine stands the cursor on one particular match - how the archive
// fallback lands, so the bar counts the message that was opened rather than
// whichever one the wrap reached.
func TestSearchToLineStandsOnThatMatch(t *testing.T) {
	m := findList(t, findCorpus)
	m.SetSearch("deploy") // lines 0, 2, 4
	if !m.SearchToLine(4) {
		t.Fatal("SearchToLine refused a line that matches")
	}
	if m.SearchAt() != 2 || m.SearchLine() != 4 {
		t.Errorf("SearchToLine(4) left the cursor at %d/%d, want 2/4", m.SearchAt(), m.SearchLine())
	}
	if m.SearchToLine(1) {
		t.Error("SearchToLine accepted a line that does not match")
	}
	if m.SearchLine() != 4 {
		t.Errorf("a refused SearchToLine moved the cursor to %d", m.SearchLine())
	}
}

// ScrollToLine takes a BUFFER line and has to find the row showing it. The two
// are not the same number: a day divider, a NEW divider or the "Loading older…"
// row is a row with no line under it, so rows[line] is a different message from
// the one that was asked for - and past the end of a short buffer it is no
// message at all.
func TestScrollToLineFindsTheRowNotTheIndex(t *testing.T) {
	day := time.Date(2026, 3, 4, 23, 30, 0, 0, time.Local)
	var lines []model.Line
	for i := 0; i < 120; i++ {
		lines = append(lines, model.Line{
			Time: day.Add(time.Duration(i) * time.Minute),
			Nick: "alice" + itoa(i), Text: "line " + itoa(i),
		})
	}
	m := findList(t, lines)
	rows := m.Rows()
	if len(rows) <= len(lines) {
		t.Fatalf("%d rows for %d lines; this test needs a divider row in the way", len(rows), len(lines))
	}

	const line = 100
	row := -1
	for i, r := range rows {
		if r.Kind != RowDay && r.Kind != RowNew && r.Index == line {
			row = i
		}
	}
	if row < 0 || row == line {
		t.Fatalf("line %d is row %d; the test needs the two numbers to differ", line, row)
	}

	m.ScrollToLine(line)
	got := m.list.GetScrollOffset()
	m.list.ScrollTo(row)
	if want := m.list.GetScrollOffset(); got != want {
		t.Errorf("ScrollToLine(%d) landed at %v, want the row showing it (%d) at %v",
			line, got, row, want)
	}
	m.list.ScrollTo(line)
	if wrong := m.list.GetScrollOffset(); got == wrong {
		t.Errorf("ScrollToLine(%d) landed where ScrollTo(%d) does; the line index is not the row index",
			line, line)
	}
}

// No matches is not an error and not a jump: nothing moves, and stepping says
// so rather than landing somewhere arbitrary.
func TestSearchWithNoMatchesStepsNowhere(t *testing.T) {
	m := findList(t, findCorpus)
	if got := m.SetSearch("kubernetes"); got != 0 {
		t.Fatalf("SetSearch found %d matches for a word nobody said", got)
	}
	if line, ok := m.SearchStep(1); ok || line != -1 {
		t.Errorf("SearchStep = (%d, %v) with no matches, want (-1, false)", line, ok)
	}
	if m.SearchLine() != -1 {
		t.Errorf("SearchLine = %d with no matches, want -1", m.SearchLine())
	}
}

// Clearing the query takes the highlighting away and leaves the buffer exactly
// as it was - the bodies are the render engine's again, not the search's.
func TestClearingTheSearchRestoresTheBodies(t *testing.T) {
	m := findList(t, findCorpus)
	m.SetSearch("deploy")
	if len(m.hits) == 0 {
		t.Fatal("a matched buffer produced no highlighted bodies")
	}
	if &m.bodyFor(0)[0] == &m.bodies[0].segs[0] {
		t.Fatal("a matched line is still handing out its plain body")
	}
	if got := m.SetSearch(""); got != 0 {
		t.Fatalf("clearing the query left %d matches", got)
	}
	if len(m.hits) != 0 {
		t.Error("clearing the query left highlighted bodies behind")
	}
	for i := range m.lines {
		if &m.bodyFor(i)[0] != &m.bodies[i].segs[0] {
			t.Fatalf("line %d is still drawing a search body", i)
		}
	}
}

// The property that keeps the Timeline honest: the segments a row DRAWS are the
// segments its height was MEASURED from. Both ask bodyFor - one function, so
// they cannot disagree - and this holds that to it from the drawing end: a
// matched row's RichText really does carry the accent, and the row it was
// measured as is taller than one line, so a body drawn from anything else would
// be clipped.
func TestAMatchedRowDrawsTheHighlightItWasMeasuredWith(t *testing.T) {
	long := strings.Repeat("the deploy went out and then some more words about it ", 6)
	m := findList(t, []model.Line{msg("alice", long, 0)})
	m.SetSearch("deploy")
	if len(m.hits) != 1 {
		t.Fatalf("the long line did not highlight: %d bodies", len(m.hits))
	}

	var row Row
	for _, r := range m.Rows() {
		if r.Index == 0 {
			row = r
		}
	}
	if n, _ := m.metrics.rows(m.bodyFor(0), m.bodyWidth(row)); n <= 1 {
		t.Fatalf("the corpus line does not wrap at this width; the test proves nothing")
	}

	w := newTimelineRow(m)
	w.set(row)
	if !hasAccent(w.body.Segments) {
		t.Error("the row drew a body with no highlight in it, but its height was measured with one")
	}
	if got, want := textOf(w.body.Segments), textOf(m.bodies[0].segs); got != want {
		t.Errorf("the drawn text changed: %q, want %q", got, want)
	}
}

// hasAccent reports whether any segment is drawn in the search-match colour.
func hasAccent(segs []widget.RichTextSegment) bool {
	for _, s := range segs {
		if ts, ok := s.(*widget.TextSegment); ok && ts.Style.ColorName == matchColorName {
			return true
		}
	}
	return false
}

func textOf(segs []widget.RichTextSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Textual())
	}
	return b.String()
}

// The colouring itself: the matched run is the accent and nothing else changes
// - same text, same order, same style.
func TestHighlightRecoloursOnlyTheMatch(t *testing.T) {
	segs := []widget.RichTextSegment{
		&widget.TextSegment{Text: "the Deploy is done", Style: widget.RichTextStyle{ColorName: "irc3"}},
	}
	out := highlight(segs, "deploy")
	if out == nil {
		t.Fatal("highlight found nothing in a line that contains the word")
	}
	var text strings.Builder
	accent := 0
	for _, s := range out {
		ts := s.(*widget.TextSegment)
		text.WriteString(ts.Text)
		if ts.Style.ColorName == ecotheme.AccentText {
			accent++
			if ts.Text != "Deploy" {
				t.Errorf("the accented run is %q, want the matched text with its own case", ts.Text)
			}
		} else if ts.Style.ColorName != "irc3" {
			t.Errorf("an unmatched run lost its colour: %q is %q", ts.Text, ts.Style.ColorName)
		}
	}
	if got := text.String(); got != "the Deploy is done" {
		t.Errorf("highlighting changed the text: %q", got)
	}
	if accent != 1 {
		t.Errorf("%d accented runs, want 1", accent)
	}
	if highlight(segs, "kubernetes") != nil {
		t.Error("highlight returned segments for a word that is not there")
	}
}

// A row must still end where it ended: render terminates a line by clearing
// Inline on its last segment, and splitting that segment must not lose the bit
// - two rows of a buffer merging into one is what that costs.
func TestHighlightKeepsTheRowTerminator(t *testing.T) {
	segs := []widget.RichTextSegment{
		&widget.TextSegment{Text: "deploy now", Style: widget.RichTextStyle{Inline: false}},
	}
	out := highlight(segs, "deploy")
	if len(out) < 2 {
		t.Fatalf("expected the segment to split, got %d", len(out))
	}
	for i, s := range out {
		last := i == len(out)-1
		if got := s.(*widget.TextSegment).Style.Inline; got == last {
			t.Errorf("segment %d (last=%v) has Inline=%v", i, last, got)
		}
	}
}

// A line arriving while the bar is open is a match like any other.
func TestAMessageArrivingIntoAnOpenSearchIsAMatch(t *testing.T) {
	m := findList(t, findCorpus)
	m.SetSearch("deploy")
	m.Append(msg("erin", "one more deploy", 10*time.Minute))
	if got := m.SearchMatches(); got != 4 {
		t.Errorf("%d matches after an arriving one, want 4", got)
	}
	if _, ok := m.hits[len(m.lines)-1]; !ok {
		t.Error("the arriving line matched but was not coloured")
	}
}

// Only one row wears the current-match wash, and it is the one stepped to.
func TestOneRowIsTheCurrentMatch(t *testing.T) {
	m := findList(t, findCorpus)
	m.SetSearch("deploy")
	if m.isCurrentMatch(0) {
		t.Error("a row is current before anything was stepped to")
	}
	m.SearchStep(1)
	current := 0
	for i := range m.lines {
		if m.isCurrentMatch(i) {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d rows claim to be the current match, want exactly 1", current)
	}
	if !m.isCurrentMatch(m.SearchLine()) {
		t.Error("the line stepped to is not the current match")
	}
}
