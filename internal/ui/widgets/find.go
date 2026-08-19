package widgets

import (
	"strings"

	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// matchColorName is what a search hit is drawn in: the accent, as text. There
// is one accent in this program (§4.1) and this is it doing the one job a
// search has — saying "here".
const matchColorName = ecotheme.AccentText

// In-buffer search — blueprint §4.3's Ctrl+F. The bar is drawn elsewhere
// (searchbar.go); this is the part that decides anything, and it is a pure
// function of the lines and the query.
//
// The one design rule here is that a highlighted body is MEASURED, not painted
// over. The Timeline's whole bet is that row heights are arithmetic (wrap.go)
// rather than layout, so a row that draws different segments than the ones it
// was measured from is a row with the wrong height — the last line of a long
// message clipped, and nothing to say why. So setting a query re-renders the
// matching lines' bodies, re-measures every row from those same segments, and
// swaps the List: one search keystroke costs what one resize costs, which is
// what budget_test.go holds it to.

// MatchLines returns the indices of the lines whose text contains the query,
// case-insensitively. It searches the line as a person reads it — the IRC
// colour and formatting codes are stripped first, so a query never matches a
// control byte and never fails because one is in the way.
func MatchLines(lines []model.Line, query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []int
	for i, l := range lines {
		if strings.Contains(strings.ToLower(render.PlainText(l)), q) {
			out = append(out, i)
		}
	}
	return out
}

// StepMatch is Enter and Shift+Enter: the next match after cur in direction
// dir, wrapping at both ends. cur is an index into the match list, or -1 for
// "nothing chosen yet"; the answer is false only when there is nothing to step
// through.
//
// Wrapping is deliberate and is what makes the bar usable without a count in
// your head: the last match steps to the first, the first steps back to the
// last, and a fresh search starting at -1 steps forward to 0 and backward to
// the newest match — which is the one nearest where the reader already is.
func StepMatch(n, cur, dir int) (int, bool) {
	if n <= 0 {
		return -1, false
	}
	if dir == 0 {
		dir = 1
	}
	if cur < 0 {
		if dir > 0 {
			return 0, true
		}
		return n - 1, true
	}
	return ((cur+dir)%n + n) % n, true
}

// --- the Timeline's half -----------------------------------------------------

// SetSearch highlights every line matching the query and returns how many
// matched. An empty query clears the search.
//
// It does not move the reader. Ctrl+F over a buffer you are already reading
// must not throw you somewhere else before you have finished typing; stepping
// is what moves, and it is SearchStep.
func (m *MessageList) SetSearch(query string) int {
	q := strings.TrimSpace(query)
	if q == m.query {
		return len(m.matches)
	}
	m.query = q
	m.refreshSearch()
	m.rebuildRows()
	m.swap()
	return len(m.matches)
}

// SearchMatches is how many lines the current query matched.
func (m *MessageList) SearchMatches() int { return len(m.matches) }

// SearchAt is which match the reader is on, as an index into the matches, or
// -1 before the first step.
func (m *MessageList) SearchAt() int { return m.matchAt }

// SearchLine is the buffer index of the match the reader is on, or -1.
func (m *MessageList) SearchLine() int {
	if m.matchAt < 0 || m.matchAt >= len(m.matches) {
		return -1
	}
	return m.matches[m.matchAt]
}

// SearchStep moves to the next match in direction dir and scrolls it into
// view, reporting the line it landed on and whether there was one.
func (m *MessageList) SearchStep(dir int) (int, bool) {
	next, ok := StepMatch(len(m.matches), m.matchAt, dir)
	if !ok {
		return -1, false
	}
	m.matchAt = next
	line := m.matches[next]
	m.ScrollToLine(line)
	// the row under the reader is the one wearing the current-match wash, and
	// which row that is has just changed
	if m.list != nil {
		m.list.Refresh()
	}
	return line, true
}

// ScrollToLine puts one line of the buffer on screen. It is what a search step
// and a jump from the Finder both land through.
func (m *MessageList) ScrollToLine(line int) {
	if !m.scrollable() {
		return
	}
	for i, r := range m.rows {
		if r.Index == line {
			m.list.ScrollTo(i)
			return
		}
	}
}

// refreshSearch recomputes the match set and the highlighted bodies for it.
// Only matching lines are re-rendered: a query typed over a ten-thousand-line
// buffer must cost the handful of rows that answer it, not the buffer.
func (m *MessageList) refreshSearch() {
	m.matches = MatchLines(m.lines, m.query)
	m.matchAt = -1
	if len(m.hits) > 0 {
		m.hits = nil
	}
	if len(m.matches) == 0 {
		return
	}
	m.hits = make(map[int][]widget.RichTextSegment, len(m.matches))
	for _, i := range m.matches {
		if segs := highlight(m.bodies[i].segs, m.query); segs != nil {
			m.hits[i] = segs
		}
	}
}

// noteSearch files one newly appended line against the live query, so a match
// that arrives while the bar is open is a match. Called from Append, where the
// line's body has just been rendered.
func (m *MessageList) noteSearch(i int) {
	if m.query == "" {
		return
	}
	if !strings.Contains(strings.ToLower(render.PlainText(m.lines[i])), strings.ToLower(m.query)) {
		return
	}
	m.matches = append(m.matches, i)
	if segs := highlight(m.bodies[i].segs, m.query); segs != nil {
		if m.hits == nil {
			m.hits = map[int][]widget.RichTextSegment{}
		}
		m.hits[i] = segs
	}
}

// isCurrentMatch reports whether a line is the match the reader stepped to. It
// is what puts the wash under one row: every match is coloured, and exactly one
// of them is where you are.
func (m *MessageList) isCurrentMatch(line int) bool {
	return line >= 0 && line == m.SearchLine()
}

// highlight re-colours the runs of a rendered body that match the query, and
// returns nil when nothing in it did.
//
// Only plain text segments are split. A link keeps its own colour (recolouring
// half a URL says it is two things), and a segment with a painted background is
// mIRC art, where the character grid is the meaning. Both are left whole, so a
// match inside one is still a match — the row is in the list and wears the
// wash — it simply is not repainted.
//
// A match that straddles a colour change inside one line is not repainted
// either, for the same reason: the segments are what the server's own
// formatting made them, and this does not join them back up.
func highlight(segs []widget.RichTextSegment, query string) []widget.RichTextSegment {
	q := strings.ToLower(query)
	if q == "" {
		return nil
	}
	out := make([]widget.RichTextSegment, 0, len(segs)+2)
	found := false
	for _, seg := range segs {
		ts, ok := seg.(*widget.TextSegment)
		if !ok || !strings.Contains(strings.ToLower(ts.Text), q) {
			out = append(out, seg)
			continue
		}
		found = true
		out = append(out, splitMatch(ts, q)...)
	}
	if !found {
		return nil
	}
	return out
}

// splitMatch cuts one text segment into alternating plain and matched runs. The
// pieces carry the original's style, so the only thing that changes is the
// colour of the matched runs — and the terminator bit, which stays on the last
// piece, because that is what stops two rows of a buffer merging into one
// (render.finish's workaround, preserved here rather than re-derived).
func splitMatch(ts *widget.TextSegment, q string) []widget.RichTextSegment {
	var out []widget.RichTextSegment
	lower := strings.ToLower(ts.Text)
	piece := func(text string, match bool) {
		if text == "" {
			return
		}
		st := ts.Style
		st.Inline = true
		if match {
			st.ColorName = matchColorName
		}
		out = append(out, &widget.TextSegment{Text: text, Style: st})
	}
	for i := 0; ; {
		j := strings.Index(lower[i:], q)
		if j < 0 {
			piece(ts.Text[i:], false)
			break
		}
		piece(ts.Text[i:i+j], false)
		piece(ts.Text[i+j:i+j+len(q)], true)
		i += j + len(q)
	}
	if len(out) == 0 {
		return []widget.RichTextSegment{ts}
	}
	// the row has to end where it ended before
	out[len(out)-1].(*widget.TextSegment).Style.Inline = ts.Style.Inline
	return out
}
