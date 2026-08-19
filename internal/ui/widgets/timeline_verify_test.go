package widgets

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// The Timeline's behaviour that the M11 mutation sweep found nothing holding.
// Every test below is one mutation that survived the suite as it shipped:
// scrolling and paging, the pill's arithmetic, the Parity Rule at the menu
// side, the heights, and what the row actually draws.

// filler is a buffer tall enough to scroll in a 400px window.
func filler(n int) []model.Line {
	lines := make([]model.Line, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, msg("alice", "line", time.Duration(i)*time.Second))
	}
	return lines
}

// --- scrolling and paging ----------------------------------------------------

// TestAnUnlaidOutPaneFollows. Before the first layout there is no viewport to
// be near the bottom OF, and the answer has to be "follow": a buffer opened
// off screen that answered "the reader has scrolled up" would put every
// arriving message into the jump pill and never scroll at all.
func TestAnUnlaidOutPaneFollows(t *testing.T) {
	testApp(t)
	m := NewMessageList(Options{})
	if !m.NearBottom() {
		t.Error("a pane that has never been laid out says the reader scrolled up")
	}
	m.Append(msg("alice", "hello", 0))
	if m.Unseen() != 0 {
		t.Errorf("%d unseen on a pane with no viewport, want 0", m.Unseen())
	}
}

// TestPrependKeepsTheBufferInOrder. A page of older messages goes ABOVE what
// is on screen. Appending it instead would put yesterday under today with
// nothing to show for it but a day rule facing the wrong way.
func TestPrependKeepsTheBufferInOrder(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("alice", "newest-1", 10*time.Second),
		msg("alice", "newest", 11*time.Second),
	})
	m.Prepend([]model.Line{msg("bob", "oldest", 0), msg("bob", "oldest+1", time.Second)})

	var texts []string
	for _, l := range m.Lines() {
		texts = append(texts, l.Text)
	}
	want := []string{"oldest", "oldest+1", "newest-1", "newest"}
	if strings.Join(texts, ",") != strings.Join(want, ",") {
		t.Errorf("buffer reads %v, want %v", texts, want)
	}
	// and the row model agrees: indices ascend with time
	last := -1
	for _, r := range m.Rows() {
		if r.Index < 0 {
			continue
		}
		if r.Index != last+1 {
			t.Errorf("row index %d follows %d: the page did not renumber", r.Index, last)
		}
		last = r.Index
	}
}

// TestPrependKeepsTheNewDivider. The NEW rule is seeded once when the buffer
// is opened; a page arriving from the archive must not disturb it, or reading
// backwards would erase the landmark you were reading towards.
func TestPrependKeepsTheNewDivider(t *testing.T) {
	m, _ := newTestList(t, Options{}, nil)
	m.SetBuffer([]model.Line{
		msg("alice", "read", 0),
		msg("bob", "unread", time.Minute),
	}, at.Add(30*time.Second))
	before := countKind(m.Rows(), RowNew)
	if before != 1 {
		t.Fatalf("%d NEW rules before the page, want 1", before)
	}
	m.Prepend([]model.Line{msg("carol", "older", -time.Hour)})
	if got := countKind(m.Rows(), RowNew); got != 1 {
		t.Errorf("%d NEW rules after a page of older messages, want 1", got)
	}
}

// TestTheLoadingRowDoesNotMoveTheReader. It appears at the top while a page is
// in flight, which pushes every row down by its own height; the viewport has to
// move with it or the pane jumps under a reader who did nothing but scroll.
func TestTheLoadingRowDoesNotMoveTheReader(t *testing.T) {
	m, _ := newTestList(t, Options{}, filler(80))
	m.list.ScrollToOffset(m.total / 2)
	before := m.list.GetScrollOffset()
	height := m.total

	m.SetLoadingOlder(true)
	grew := m.total - height
	if grew <= 0 {
		t.Fatalf("the loading row added %v of height", grew)
	}
	if got := m.list.GetScrollOffset(); got < before+grew-0.5 || got > before+grew+0.5 {
		t.Errorf("offset %v after the loading row appeared, want %v (it grew by %v above the reader)",
			got, before+grew, grew)
	}
	m.SetLoadingOlder(false)
	if got := m.list.GetScrollOffset(); got < before-0.5 || got > before+0.5 {
		t.Errorf("offset %v after the loading row went away, want %v back", got, before)
	}
}

// TestARedrawKeepsAScrolledUpReaderInPlace. Redraw is the display-options path
// and SetLines is the redaction path; neither is a buffer switch, and neither
// may throw someone reading history back to the newest message.
func TestARedrawKeepsAScrolledUpReaderInPlace(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*MessageList)
	}{
		{"Redraw", func(m *MessageList) { m.Redraw() }},
		{"SetLines", func(m *MessageList) { m.SetLines(m.Lines()) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newTestList(t, Options{}, filler(80))
			m.list.ScrollToOffset(m.total / 3)
			before := m.list.GetScrollOffset()
			if m.NearBottom() {
				t.Fatal("the test did not manage to scroll up")
			}
			c.call(m)
			if got := m.list.GetScrollOffset(); got < before-0.5 || got > before+0.5 {
				t.Errorf("offset %v after %s, want %v: the reader was yanked", got, c.name, before)
			}
		})
	}
}

// TestSetLinesReplacesTheContent. The redaction path: the pane must show what
// the session now holds, not the copy it was handed when the buffer opened.
func TestSetLinesReplacesTheContent(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{msg("alice", "secret", 0)})
	m.SetLines([]model.Line{{Time: at, Status: true, Text: "message withdrawn"}})
	if got := m.Lines(); len(got) != 1 || got[0].Text != "message withdrawn" {
		t.Errorf("the pane holds %v after the withdrawal", got)
	}
	for _, r := range m.Rows() {
		if r.Index >= 0 && strings.Contains(r.Line.Text, "secret") {
			t.Error("the withdrawn text is still in a row")
		}
	}
}

// TestResizeToTheSameWidthCostsNothing. Fyne hands a widget its own size back
// on any layout pass. Re-measuring 10,000 rows and rebuilding the List for
// that is the quadratic trap the whole design avoids.
func TestResizeToTheSameWidthCostsNothing(t *testing.T) {
	m, w := newTestList(t, Options{}, filler(40))
	before := m.list
	w.Resize(fyne.NewSize(700, 300)) // same width, different height
	if m.list != before {
		t.Error("a height-only resize rebuilt the List")
	}
	w.Resize(fyne.NewSize(500, 300)) // width really changed
	if m.list == before {
		t.Error("a width change did not rebuild the List")
	}
}

// TestResizeRemeasuresEveryRow: the other half of the same rule. A narrower
// pane wraps more, so the content must get taller.
func TestResizeRemeasuresEveryRow(t *testing.T) {
	long := strings.Repeat("wrap me at any sensible width ", 6)
	m, w := newTestList(t, Options{}, []model.Line{msg("alice", long, 0), msg("bob", long, time.Minute)})
	wide := m.total
	w.Resize(fyne.NewSize(320, 400))
	if m.total <= wide {
		t.Errorf("content height %v at 320px, %v at 700px: the rows were not re-measured", m.total, wide)
	}
}

// --- the jump pill -----------------------------------------------------------

// TestThePillCountsMessagesOnly. "N new messages" is a count of messages. Day
// rules and the NEW rule are rows too, and counting them would tell the reader
// there is more below than there is.
func TestThePillCountsMessagesOnly(t *testing.T) {
	m, _ := newTestList(t, Options{}, filler(80))
	m.list.ScrollToOffset(0)
	if m.NearBottom() {
		t.Fatal("the test did not manage to scroll up")
	}
	// two messages a day apart: two messages, and a day rule between them -
	// plus one of our own, which is not news to us. The NEW rule already
	// refuses to open on a self message; the pill answers the same question
	// and has to give the same answer.
	m.Append(
		msg("bob", "one", 24*time.Hour),
		model.Line{Time: at.Add(24*time.Hour + time.Second), Nick: "me",
			Text: "sent from here while reading back", Self: true},
		msg("bob", "two", 24*time.Hour+2*time.Second))
	if got := m.Unseen(); got != 2 {
		t.Errorf("pill count %d after two messages, one of our own and a day rule, want 2", got)
	}
	if got := m.pill.text(); !strings.HasPrefix(got, "2 new messages") {
		t.Errorf("pill reads %q", got)
	}
}

// TestThePillWording: one message is not "1 new messages".
func TestThePillWording(t *testing.T) {
	testApp(t)
	p := newJumpPill(nil)
	for _, c := range []struct {
		n    int
		want string
	}{{1, "1 new message  ↓"}, {2, "2 new messages  ↓"}, {17, "17 new messages  ↓"}} {
		p.count = c.n
		if got := p.text(); got != c.want {
			t.Errorf("count %d reads %q, want %q", c.n, got, c.want)
		}
	}
}

// TestThePillClearsAndHides. Three ways the count goes away: jumping to the
// bottom, tapping the pill itself, and switching buffer. A count that survives
// any of them is a permanent lie in the corner of the window.
func TestThePillClearsAndHides(t *testing.T) {
	scrollUpAndFill := func(m *MessageList) {
		m.list.ScrollToOffset(0)
		m.Append(msg("bob", "new one", time.Hour))
		if m.Unseen() == 0 || !m.pill.Visible() {
			t.Fatalf("setup: unseen=%d visible=%v", m.Unseen(), m.pill.Visible())
		}
	}
	for _, c := range []struct {
		name string
		call func(*MessageList)
	}{
		{"ScrollToBottom", func(m *MessageList) { m.ScrollToBottom() }},
		{"tapping the pill", func(m *MessageList) { m.pill.Tapped(nil) }},
		{"a buffer switch", func(m *MessageList) { m.SetBuffer(filler(3), time.Time{}) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newTestList(t, Options{}, filler(80))
			scrollUpAndFill(m)
			c.call(m)
			if m.Unseen() != 0 {
				t.Errorf("%d unseen after %s", m.Unseen(), c.name)
			}
			if m.pill.Visible() {
				t.Errorf("the pill is still on screen after %s", c.name)
			}
			// and the view actually went there: clearing the count without
			// moving is the pill lying about what it did
			if !m.NearBottom() {
				t.Errorf("%s cleared the count but left the reader in the scrollback", c.name)
			}
		})
	}
}

// TestThePillStaysHiddenAtTheBottom. It is the answer to "you are not looking
// at the newest message"; at the bottom there is no question to answer.
func TestThePillStaysHiddenAtTheBottom(t *testing.T) {
	m, _ := newTestList(t, Options{}, filler(80))
	m.unseen = 7 // as if a count had been left behind
	m.updatePill()
	if m.pill.Visible() {
		t.Error("the pill shows while the reader is at the bottom")
	}
}

// --- the Parity Rule ---------------------------------------------------------

// TestTheMenuIsTheStrip. §4.2: right-click offers exactly what hover offers.
// The sweep truncated the menu's loop and nothing failed, because no test read
// the menu - only the list both are supposed to be built from.
func TestTheMenuIsTheStrip(t *testing.T) {
	lines := []model.Line{
		msg("alice", "someone else's, with an id", 0),
		{Time: at.Add(time.Minute), Nick: "me", Text: "mine", Self: true, MsgID: "abc"},
		{Time: at.Add(2 * time.Minute), Status: true, Text: "bob joined"},
	}
	lines[0].MsgID = "xyz" // a message with an id that is not ours
	m, _ := newTestList(t, Options{Copy: func(string) {}, Quote: func(model.Line) {},
		Delete: func(model.Line) {}}, lines)

	row := newTimelineRow(m)
	for _, r := range m.Rows() {
		row.set(r)
		acts, items := row.actions(), row.menuItems()
		if len(acts) != len(items) {
			t.Errorf("%q: %d hover actions, %d menu items", r.Line.Text, len(acts), len(items))
			continue
		}
		for i := range acts {
			if acts[i].label != items[i].Label {
				t.Errorf("%q: action %d is %q on hover and %q in the menu",
					r.Line.Text, i, acts[i].label, items[i].Label)
			}
		}
		// and the strip has a widget for every one of them
		if r.Kind == RowMessage && len(row.strip.Objects) < len(acts) {
			t.Errorf("%q offers %d actions but the strip holds only %d icons",
				r.Line.Text, len(acts), len(row.strip.Objects))
		}
	}
}

// TestDeleteIsOfferedOnlyOnOurOwnMessages. Delete means REDACT, and REDACT on
// someone else's message is a request the server will refuse - offering it is
// offering a button that cannot work.
func TestDeleteIsOfferedOnlyOnOurOwnMessages(t *testing.T) {
	lines := []model.Line{
		{Time: at, Nick: "alice", Text: "theirs, with an id", MsgID: "xyz"},
		{Time: at.Add(time.Minute), Nick: "me", Text: "ours, with an id", Self: true, MsgID: "abc"},
		{Time: at.Add(2 * time.Minute), Nick: "me", Text: "ours, no id yet", Self: true},
	}
	m, _ := newTestList(t, Options{Delete: func(model.Line) {}}, lines)
	row := newTimelineRow(m)
	want := map[string]bool{"theirs, with an id": false, "ours, with an id": true, "ours, no id yet": false}
	for _, r := range m.Rows() {
		if r.Kind != RowMessage {
			continue
		}
		got := false
		for _, a := range row.setAnd(r).actions() {
			if a.kind == actDelete {
				got = true
			}
		}
		if got != want[r.Line.Text] {
			t.Errorf("%q offers Delete = %v, want %v", r.Line.Text, got, want[r.Line.Text])
		}
	}
}

// --- the heights -------------------------------------------------------------

// TestAppendWritesTheHeightsItMeasured. A row taller than one line has to be
// told to the List, or it draws at the template height and the message is cut
// off at its own bottom edge.
func TestAppendWritesTheHeightsItMeasured(t *testing.T) {
	long := strings.Repeat("this wraps several times at seven hundred pixels wide ", 4)
	// taller than the viewport on purpose: content that fits has nothing to
	// scroll, and the question below would answer itself
	m, _ := newTestList(t, Options{}, filler(80))
	m.Append(msg("alice", long, time.Hour))
	i := len(m.Rows()) - 1
	if m.heights[i] <= m.geom.lineH*1.5 {
		t.Fatalf("the appended row measured %v, one line is %v: it did not wrap", m.heights[i], m.geom.lineH)
	}
	// and the LIST was told, which is the half that matters: a height the
	// arithmetic worked out but never wrote is a row drawn at the template's
	// one line with the rest of the message below the cut. The List keeps its
	// item heights unexported, so ask it the one question that depends on
	// them - how far down it can scroll.
	m.list.ScrollToBottom()
	reach := m.list.GetScrollOffset() + m.list.Size().Height
	if reach < m.total-1 {
		t.Errorf("the List scrolls to %v of %v of content: it was never told the appended row's height",
			reach, m.total)
	}
	row := newTimelineRow(m)
	row.set(m.Rows()[i])
	row.Resize(fyne.NewSize(700, m.heights[i]))
	need := row.body.MinSize().Height - 2*m.Theme().Size(theme.SizeNameInnerPadding)
	if m.heights[i] < need-0.01 {
		t.Errorf("appended row: told %v, needs %v", m.heights[i], need)
	}
}

// TestTheNickColumnKeepsItsMinimum. §4.2's soft alignment: at least eight
// cells, so short nicks leave the body's left edge straight instead of
// ragged.
func TestTheNickColumnKeepsItsMinimum(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("bo", "short nick", 0),
		msg("erin_with_a_very_long_nick", "long nick", time.Hour),
	})
	g := m.geom
	if g.nickMin <= 0 {
		t.Fatalf("nick minimum is %v", g.nickMin)
	}
	if got, want := g.bodyX(1), g.bodyX(g.nickMin); got != want {
		t.Errorf("a one-pixel nick puts the body at %v and an eight-cell one at %v: the minimum is gone", got, want)
	}
	if g.bodyX(g.nickMin*3) <= g.bodyX(g.nickMin) {
		t.Error("a long nick does not push its own body right: it is being truncated instead")
	}
}

// TestAGroupedRowGetsTheWholeWidth. A continuation row draws no nick, so its
// body starts where the nick column would have ended - measuring it as if it
// had one costs it a wrap it does not need, and the pane grows for nothing.
func TestAGroupedRowGetsTheWholeWidth(t *testing.T) {
	long := strings.Repeat("some words that will have to wrap somewhere ", 5)
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("erin_with_a_very_long_nick", long, 0),
		msg("erin_with_a_very_long_nick", long, time.Second),
	})
	rows := m.Rows()
	var head, cont Row
	for _, r := range rows {
		if r.Kind != RowMessage {
			continue
		}
		if r.Head {
			head = r
		} else {
			cont = r
		}
	}
	if cont.Kind != RowMessage {
		t.Fatalf("no continuation row in %v", kinds(rows))
	}
	if m.bodyWidth(cont) <= m.bodyWidth(head) {
		t.Errorf("continuation body width %v, header row %v: the grouped row is being measured with a nick it does not draw",
			m.bodyWidth(cont), m.bodyWidth(head))
	}
}

// TestTheAuthorGapIsMeasuredAndDrawn. Four pixels above an author change -
// measured into the row's height AND left empty when it draws. Measured but
// not drawn is a gap that silently becomes trailing space; drawn but not
// measured clips the last line.
func TestTheAuthorGapIsMeasuredAndDrawn(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("alice", "hers", 0),
		msg("bob", "his", time.Second),
	})
	var gapped, plain Row
	var gi int
	for i, r := range m.Rows() {
		if r.Kind != RowMessage {
			continue
		}
		if r.Gap {
			gapped, gi = r, i
		} else {
			plain = r
		}
	}
	if gapped.Kind != RowMessage || plain.Kind != RowMessage {
		t.Fatalf("wanted one gapped and one plain row, got %v", kinds(m.Rows()))
	}
	if got := m.heights[gi]; got < m.geom.lineH+authorGap-0.01 {
		t.Errorf("the gapped row measured %v, one line plus the gap is %v", got, m.geom.lineH+authorGap)
	}
	row := newTimelineRow(m)
	row.set(gapped)
	row.Resize(fyne.NewSize(700, m.heights[gi]))
	if got := row.body.Position().Y + m.Theme().Size(theme.SizeNameInnerPadding); got < authorGap-0.01 {
		t.Errorf("the body of a gapped row is drawn at y=%v, want the gap above it", got)
	}
	row.set(plain)
	row.Resize(fyne.NewSize(700, m.geom.lineH))
	if got := row.body.Position().Y + m.Theme().Size(theme.SizeNameInnerPadding); got > 0.01 {
		t.Errorf("an ungapped row is drawn at y=%v, want 0: every row got the air", got)
	}
}

// TestTheBodyStartsAfterItsColumns. The wrap goes under the message, not back
// to the left edge (§4.2), and the row gives RichText's un-disableable inner
// padding back so the glyphs land on the column the height pass measured to.
func TestTheBodyStartsAfterItsColumns(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{msg("alice", "hello there", 0)})
	var r Row
	for _, c := range m.Rows() {
		if c.Kind == RowMessage {
			r = c
		}
	}
	row := newTimelineRow(m)
	row.set(r)
	row.Resize(fyne.NewSize(m.width, m.heights[1]))

	pad := m.Theme().Size(theme.SizeNameInnerPadding)
	wantX := m.geom.bodyX(m.headWidth(r.Line))
	if got := row.body.Position().X + pad; got < wantX-0.01 || got > wantX+0.01 {
		t.Errorf("the body's glyphs start at x=%v, want %v (gutter + nick column)", got, wantX)
	}
	if wantX <= m.geom.gutter {
		t.Fatalf("bodyX %v is inside the gutter %v", wantX, m.geom.gutter)
	}
	// and the width it was given matches the width it was measured at
	if got, want := row.body.Size().Width-2*pad, m.bodyWidth(r); got < want-0.01 || got > want+0.01 {
		t.Errorf("drawn at %v wide, measured at %v", got, want)
	}
}

// TestLineHeightFollowsTheBodyFace. A proportional body sets taller lines than
// a monospace one; measuring in the wrong face makes every row in the pane the
// wrong height at once.
func TestLineHeightFollowsTheBodyFace(t *testing.T) {
	testApp(t)
	mm := newMeasurer(14)
	th := fyne.CurrentApp().Settings().Theme()
	mono := newGeom(mm, th, render.Options{Mono: true})
	prop := newGeom(mm, th, render.Options{Mono: false})
	if mono.lineH == prop.lineH {
		t.Fatal("the two faces measure the same line height; the test proves nothing here")
	}
	if got := mm.lineHeight(fyne.TextStyle{Monospace: true}); mono.lineH != got {
		t.Errorf("a monospace body gets line height %v, the monospace face is %v", mono.lineH, got)
	}
	if got := mm.lineHeight(fyne.TextStyle{}); prop.lineH != got {
		t.Errorf("a proportional body gets line height %v, the proportional face is %v", prop.lineH, got)
	}
}

// TestVisibleLinesSkipsTheRowsTheTimelineInvented. Reader mode is aimed with
// this: a divider has no line behind it, and counting one would slide the
// window off the messages the reader was actually looking at.
func TestVisibleLinesSkipsTheRowsTheTimelineInvented(t *testing.T) {
	lines := []model.Line{
		msg("alice", "day one", 0),
		msg("alice", "day two", 24*time.Hour),
		msg("alice", "day three", 48*time.Hour),
	}
	m, w := newTestList(t, Options{}, lines)
	if n := countKind(m.Rows(), RowDay); n != 3 {
		t.Fatalf("%d day rules, want 3", n)
	}
	if lo, hi := m.VisibleLines(); lo != 0 || hi != len(lines) {
		t.Errorf("whole buffer on screen: VisibleLines() = %d,%d over %d lines and %d rows",
			lo, hi, len(lines), len(m.Rows()))
	}

	// Now the case that actually needs the guard: a window whose BOTTOM row is
	// a day rule. Rows are [day, m0, day, m1, day, m2]; shrink the viewport and
	// scroll so it holds m0 and the rule under it. The answer is still "line 0
	// is on screen" - the rule is not a line and has no index to report.
	pad := m.Theme().Size(theme.SizeNamePadding)
	y := func(i int) float32 {
		var out float32
		for j := 0; j < i; j++ {
			out += m.heights[j] + pad
		}
		return out
	}
	w.Resize(fyne.NewSize(700, m.heights[1]+pad+m.heights[2]/2))
	m.list.ScrollToOffset(y(1))
	lo, hi := m.VisibleLines()
	if lo != 0 || hi != 1 {
		t.Errorf("a window ending on a day rule: VisibleLines() = %d,%d, want 0,1 (message 0 and nothing else)",
			lo, hi)
	}
}

// --- what the row draws ------------------------------------------------------

// TestTheRowPaintsFromTokens. §4.2 names the roles: the gutter is faint, a
// status line's bullet is faint too, the day rule is the border colour and the
// NEW rule is the accent. A rule painted in the same grey as a day rule stops
// being the landmark it exists to be.
func TestTheRowPaintsFromTokens(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("alice", "hello", 0),
		{Time: at.Add(time.Minute), Status: true, Text: "bob joined"},
		msg("carol", "unread", 2*time.Minute),
	})
	m.SetBuffer(m.Lines(), at.Add(90*time.Second))
	row := newTimelineRow(m)
	th := row.Theme()
	v := ecotheme.Variant(th)
	tok := func(n fyne.ThemeColorName) color.Color { return th.Color(n, v) }

	seen := map[RowKind]bool{}
	for _, r := range m.Rows() {
		row.set(r)
		seen[r.Kind] = true
		switch r.Kind {
		case RowMessage:
			if !sameColor(row.ts.Color, tok(ecotheme.TextFaint)) {
				t.Errorf("the timestamp gutter is %v, want textFaint", row.ts.Color)
			}
			if r.Line.Status && !sameColor(row.nick.Color, tok(ecotheme.TextFaint)) {
				t.Errorf("a status line's bullet is %v, want textFaint", row.nick.Color)
			}
		case RowDay:
			if !sameColor(row.rule.FillColor, tok(ecotheme.Border)) {
				t.Errorf("the day rule is %v, want border", row.rule.FillColor)
			}
		case RowNew:
			if !sameColor(row.rule.FillColor, tok(ecotheme.Accent)) {
				t.Errorf("the NEW rule is %v, want the accent", row.rule.FillColor)
			}
			if !sameColor(row.label.Color, tok(ecotheme.AccentText)) {
				t.Errorf("the NEW label is %v, want accentText", row.label.Color)
			}
			if !strings.HasPrefix(row.label.Text, "NEW - since ") {
				t.Errorf("the NEW rule reads %q", row.label.Text)
			}
		}
	}
	for _, k := range []RowKind{RowMessage, RowDay, RowNew} {
		if !seen[k] {
			t.Errorf("no %v row in the fixture", k)
		}
	}
}

// TestAGroupedRowKeepsItsTimestamp. §4.2 deviation (3): grouping takes the
// nick and nothing else. Losing the timestamp too leaves the gutter ragged and
// the pane unable to answer "when was that said".
func TestAGroupedRowKeepsItsTimestamp(t *testing.T) {
	m, _ := newTestList(t, Options{}, []model.Line{
		msg("alice", "first", 0),
		msg("alice", "second, grouped", time.Second),
	})
	row := newTimelineRow(m)
	found := false
	for _, r := range m.Rows() {
		if r.Kind != RowMessage || r.Head {
			continue
		}
		found = true
		row.set(r)
		if !row.ts.Visible() || row.ts.Text == "" {
			t.Error("a grouped row lost its timestamp as well as its nick")
		}
		if row.nick.Visible() {
			t.Error("a grouped row still draws a nick")
		}
	}
	if !found {
		t.Fatalf("nothing grouped in %v", kinds(m.Rows()))
	}
}

// TestTheNickColumnSaysWhatKindOfLineItIs: a notice wears its dashes, an
// action its star, a status line its bullet - the conventions the nick column
// carries now that the body no longer does.
func TestTheNickColumnSaysWhatKindOfLineItIs(t *testing.T) {
	for _, c := range []struct {
		line model.Line
		want string
	}{
		{model.Line{Nick: "alice"}, "alice"},
		{model.Line{Nick: "alice", Notice: true}, "-alice-"},
		{model.Line{Nick: "alice", Action: true}, "* alice"},
		{model.Line{Status: true}, "·"},
	} {
		if got := headText(c.line); got != c.want {
			t.Errorf("headText(%+v) = %q, want %q", c.line, got, c.want)
		}
	}
}

// --- the wrap arithmetic -----------------------------------------------------

// TestAWordThatExactlyFillsTheRowDoesNotWrap. RichText breaks BEFORE the first
// word that does not fit; a word ending flush with the edge fits. Off by one
// here adds a blank row to every message whose text happens to land evenly.
func TestAWordThatExactlyFillsTheRowDoesNotWrap(t *testing.T) {
	testApp(t)
	m := newMeasurer(14)
	style := fyne.TextStyle{Monospace: true}
	seg := func(text string) []widget.RichTextSegment {
		return []widget.RichTextSegment{&widget.TextSegment{
			Text: text, Style: widget.RichTextStyle{TextStyle: style}}}
	}

	// one word, measured from a pen at zero
	word := "abcdefgh"
	w := m.width(word, style)
	if rows, _ := m.rows(seg(word), w); rows != 1 {
		t.Errorf("a word exactly %vpx wide takes %d rows at %vpx, want 1", w, rows, w)
	}
	if rows, _ := m.rows(seg(word), w-0.5); rows != 2 {
		t.Errorf("the same word takes %d rows half a pixel short, want 2", rows)
	}

	// and the case the pen is NOT at zero for, which is the one the guard in
	// flow() is actually about: a second word landing flush with the edge
	two := "abcd efgh"
	exact := m.width(two, style)
	if rows, _ := m.rows(seg(two), exact); rows != 1 {
		t.Errorf("two words filling the row exactly (%vpx) take %d rows, want 1", exact, rows)
	}
	if rows, _ := m.rows(seg(two), exact-0.5); rows != 2 {
		t.Errorf("the same pair takes %d rows half a pixel short, want 2", rows)
	}
}

// TestANonInlineSegmentEndsItsRow. render marks the last segment of a line
// non-inline to terminate it; a body made of two such runs is two rows, and
// counting it as one clips the second.
func TestANonInlineSegmentEndsItsRow(t *testing.T) {
	testApp(t)
	m := newMeasurer(14)
	seg := func(text string, inline bool) widget.RichTextSegment {
		return &widget.TextSegment{Text: text, Style: widget.RichTextStyle{Inline: inline}}
	}
	one, _ := m.rows([]widget.RichTextSegment{seg("aa", true), seg("bb", false)}, 1000)
	two, _ := m.rows([]widget.RichTextSegment{seg("aa", false), seg("bb", true), seg("", false)}, 1000)
	if one != 1 {
		t.Errorf("one inline run then a terminator is %d rows, want 1", one)
	}
	if two != 2 {
		t.Errorf("a run that ends its own row then another is %d rows, want 2", two)
	}
}

// --- helpers -----------------------------------------------------------------

func countKind(rows []Row, k RowKind) int {
	n := 0
	for _, r := range rows {
		if r.Kind == k {
			n++
		}
	}
	return n
}

// setAnd pours a row through the widget and hands it back, so a test can chain.
func (r *timelineRow) setAnd(row Row) *timelineRow { r.set(row); return r }

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
