// Package widgets holds eco's own Fyne widgets — the ones the standard
// toolkit has no answer for. The first of them is MessageList, the Timeline.
//
// The bet (blueprint risk #1). A chat log is a list of rows whose heights all
// differ and all change when the window is resized, and fyne's widget.List
// asks for those heights up front. Measuring 10,000 of them by building the
// text costs seconds; writing them into a live List costs seconds more,
// because every SetItemHeight refreshes the whole widget and every refresh is
// O(rows). Both are avoidable, and this file avoids them in one sentence
// each:
//
//   - Heights are arithmetic (wrap.go), not layout. 10,000 of them take
//     about ten milliseconds instead of four hundred.
//   - Heights are loaded into a List that has not been rendered yet, which
//     is the only state in which SetItemHeight is O(1). So whenever row
//     identities move — a buffer switch, a page of older messages, a resize —
//     the Timeline builds a NEW List and swaps it in, rather than mutating
//     the one on screen. Appending is the one case that does not move
//     anything, and it is the one case that mutates.
//
// The numbers are in budget_test.go, which is the gate the blueprint asked
// for, and they are recorded in the blueprint's M11(b) entry.
package widgets

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
)

// Options is everything the Timeline needs from the application: what the
// display settings are, and what to do when the reader asks for something.
// Every field is optional; a nil callback is a feature the host does not
// offer, and the Timeline simply does not show it.
type Options struct {
	// Render supplies the display options for a pass: timestamps, the time
	// layout, monospace, colorful nicks, and the mention and URL resolvers.
	// It is called per rebuild, so changing a preference and repainting is
	// all it takes for the change to land.
	Render func() render.Options
	// Older is called when the reader reaches the top of the buffer. It is
	// the M9 pager's doorbell; the session decides whether there is anything
	// to fetch, and answers asynchronously.
	Older func()
	// Copy puts text on the clipboard.
	Copy func(string)
	// Quote hands a message back to the composer.
	Quote func(model.Line)
	// Delete withdraws one of our own messages.
	Delete func(model.Line)
	// Now is the clock day dividers are named against; nil means time.Now.
	Now func() time.Time
}

// nearBottomMargin is how close to the newest message counts as "following".
// It is v1's number, kept verbatim along with the discipline it serves:
// arrival never yanks a reader who has scrolled up.
const nearBottomMargin = 80

// topPagingRows is how many rows from the top count as "the reader wants the
// page above this one".
const topPagingRows = 1

// MessageList is the Timeline: one buffer's lines as a virtualized list of
// heterogeneous rows.
type MessageList struct {
	widget.BaseWidget
	opts Options

	lines  []model.Line
	bodies []bodyCache // parallel to lines
	build  *rowBuilder
	rows   []Row

	heights []float32
	total   float32 // content height, which is how "near the bottom" is known

	since   time.Time // the NEW divider's seed, captured on buffer open
	loading bool

	list *widget.List
	pill *jumpPill
	objs []fyne.CanvasObject

	metrics  *measurer
	geom     geom
	ropts    render.Options
	textSize float32
	width    float32 // the width the current heights were computed at

	unseen int  // messages that arrived while the reader was up in the scrollback
	armed  bool // paging is live: see swap
}

// bodyCache is one line's rendered form, kept because a rebuild must not pay
// for parsing the whole buffer again (blueprint §5.5: the per-line segment
// cache is what makes a virtualized Timeline cheap).
type bodyCache struct {
	segs []widget.RichTextSegment
	art  fyne.CanvasObject // set instead of segs for mIRC background art
}

func NewMessageList(opts Options) *MessageList {
	m := &MessageList{opts: opts}
	if m.opts.Render == nil {
		m.opts.Render = func() render.Options { return render.Options{} }
	}
	m.pill = newJumpPill(func() {
		m.unseen = 0
		m.updatePill()
		m.ScrollToBottom()
	})
	m.pill.Hide()
	m.ExtendBaseWidget(m)
	m.reset(nil, time.Time{})
	// a List from the first moment: every path below writes to one, and an
	// empty buffer is a state the app can sit in for a whole session
	m.swap()
	return m
}

// --- the content ------------------------------------------------------------

// SetBuffer replaces everything on screen: a buffer switch. since seeds the
// one persistent NEW divider, and is the read marker as it stood the moment
// the buffer was opened — never the live one, which would crawl down the
// screen as the reader caught up.
func (m *MessageList) SetBuffer(lines []model.Line, since time.Time) {
	m.reset(lines, since)
	m.swap()
	m.unseen = 0
	m.updatePill()
	m.ScrollToBottom()
}

// Append adds newly arrived lines to the bottom. This is the one path that
// does not rebuild the List: appending moves no existing row, so the heights
// already on screen stay true and only the new ones are written.
//
// It is also where v1's stickBottom discipline lives: whether the view
// follows is decided from where the reader was BEFORE the lines landed.
func (m *MessageList) Append(lines ...model.Line) {
	if len(lines) == 0 {
		return
	}
	follow := m.NearBottom()
	first := len(m.rows)
	for _, l := range lines {
		body := m.renderLine(l)
		m.lines = append(m.lines, l)
		m.bodies = append(m.bodies, body)
		m.build.addKind(l, body.art != nil)
	}
	m.rows = m.build.rows
	m.measure(first)
	// The one legal in-place write: appending moved no existing row, so only
	// the new ones need telling — and a row that came out exactly one line
	// tall needs not even that, because that is already what the List uses
	// for a row it has never been told about (the template's MinSize). Every
	// write costs a full refresh of the widget, so the ones skipped here are
	// the difference between a chatty channel being free and being O(rows)
	// per message.
	wrote := false
	for i := first; i < len(m.rows); i++ {
		if m.heights[i] != m.geom.lineH {
			m.list.SetItemHeight(i, m.heights[i])
			wrote = true
		}
	}
	if !wrote {
		m.list.Refresh() // the row count changed even if no height did
	}
	if follow {
		m.unseen = 0
		m.ScrollToBottom()
	} else {
		for _, r := range m.rows[first:] {
			// dividers are not messages, and neither is something you wrote
			// yourself: "3 new messages" that includes your own is the same
			// wrongness the NEW rule already refuses (see rowBuilder.addKind)
			if (r.Kind == RowMessage || r.Kind == RowArt) && !r.Line.Self {
				m.unseen++
			}
		}
	}
	m.updatePill()
}

// Prepend puts a page of older messages above what is on screen and leaves
// the reader looking at the same line they were looking at. Every row index
// below has just moved, so this is a rebuild.
func (m *MessageList) Prepend(lines []model.Line) {
	if len(lines) == 0 {
		return
	}
	before := m.total
	offset := m.list.GetScrollOffset()
	m.reset(append(append([]model.Line(nil), lines...), m.lines...), m.since)
	m.swap()
	// the content grew above the reader by exactly this much; moving the
	// viewport down by the same amount is standing still
	m.list.ScrollToOffset(offset + (m.total - before))
}

// SetLoadingOlder shows or hides the "Loading older…" row at the top.
func (m *MessageList) SetLoadingOlder(on bool) {
	if on == m.loading {
		return
	}
	m.loading = on
	offset := m.list.GetScrollOffset()
	before := m.total
	m.rebuildRows()
	m.swap()
	m.list.ScrollToOffset(offset + (m.total - before))
}

// Redraw rebuilds everything from the lines it already holds: the display
// options changed under the pane. Nothing about the buffer moved, so neither
// does the reader.
func (m *MessageList) Redraw() { m.repaint(m.lines) }

// SetLines replaces the buffer's content without moving the reader. It is the
// same buffer, changed underneath — a withdrawn message, an amended row — and
// none of those is a reason to throw someone reading history back to the
// bottom. A buffer SWITCH is SetBuffer, and it is the only path that jumps.
func (m *MessageList) SetLines(lines []model.Line) { m.repaint(lines) }

// repaint is Redraw and SetLines: rebuild everything, then put the reader back
// where they were — following the tail if that is where they were, and exactly
// still if it is not.
func (m *MessageList) repaint(lines []model.Line) {
	follow := m.NearBottom()
	offset := m.list.GetScrollOffset()
	m.reset(lines, m.since)
	m.swap()
	if follow {
		m.ScrollToBottom()
	} else {
		m.list.ScrollToOffset(offset)
	}
}

// Lines returns the buffer as the Timeline holds it.
func (m *MessageList) Lines() []model.Line { return m.lines }

// Rows returns the row model on screen. It is what the headless tests read.
func (m *MessageList) Rows() []Row { return m.rows }

// Unseen is the jump pill's count: messages that arrived below the fold
// while the reader was up in the scrollback.
func (m *MessageList) Unseen() int { return m.unseen }

// ContentHeight is how tall the rows add up to. It is the one number that
// says the pane really re-measured — the app's test for "a settings change
// redraws the pane" has nothing else to look at, since every other observable
// (the lines, the rows, the scroll position) is deliberately unchanged by it.
func (m *MessageList) ContentHeight() float32 { return m.total }

// Since is what the NEW divider was seeded with when this buffer was opened.
// It exists so the app's own test can check the seam it hands over — the
// marker as it stood at the switch, never the live one — for a buffer with no
// marker as well as one with, which is the case a "did a divider appear?"
// assertion cannot tell apart.
func (m *MessageList) Since() time.Time { return m.since }

// VisibleLines reports which of Lines() the reader can actually see, as a
// half-open range. It is what Reader mode (F6) is built around: a plain-text
// view of a whole buffer is quadratic in Fyne — 500 lines cost 0.1s to lay
// out, 10,000 cost 44s, measured — so the plain-text view shows the part of
// the buffer you were looking at, and this is the only thing that knows which
// part that is.
//
// An empty range (lo == hi) means nothing is on screen yet.
func (m *MessageList) VisibleLines() (lo, hi int) {
	if m.list == nil || len(m.rows) == 0 {
		return 0, 0
	}
	top := m.list.GetScrollOffset()
	bottom := top + m.list.Size().Height
	if bottom <= top {
		bottom = top + m.total // never laid out: treat everything as visible
	}
	pad := m.Theme().Size(theme.SizeNamePadding)
	lo, hi = -1, -1
	y := float32(0)
	for i, r := range m.rows {
		h := m.heights[i]
		if y+h >= top && y <= bottom && r.Index >= 0 {
			if lo < 0 {
				lo = r.Index
			}
			hi = r.Index
		}
		y += h + pad
		if y > bottom {
			break
		}
	}
	if lo < 0 {
		return 0, 0
	}
	return lo, hi + 1
}

// --- scrolling --------------------------------------------------------------

// NearBottom reports whether the reader is close enough to the newest message
// for the view to follow new ones. The measurement is this layer's because it
// owns the pixels; nothing else in the app is allowed an opinion about it.
func (m *MessageList) NearBottom() bool {
	if m.list == nil {
		return true
	}
	view := m.list.Size().Height
	if view <= 0 {
		return true
	}
	return m.total-view-m.list.GetScrollOffset() < nearBottomMargin
}

// scrollable reports whether the List can be asked to scroll yet.
//
// widget.List builds its scroller in its renderer, and in Fyne 2.8
// ScrollToBottom and ScrollToTop are the only two scroll methods that do NOT
// nil-check it (list.go:349 and :357 — ScrollTo, ScrollToOffset,
// GetScrollOffset and SetItemHeight all do). So a Timeline handed a buffer
// before it has ever been laid out — build the pane, fill it, then show it —
// crashed the app on a nil dereference. swap() resizes the fresh List whenever
// this widget has a height, and BaseWidget.Resize is what builds the renderer,
// so a height is exactly the condition. A pane with no height has nothing to
// scroll anyway.
func (m *MessageList) scrollable() bool { return m.list != nil && m.Size().Height > 0 }

// ScrollToBottom jumps to the newest message and follows again.
func (m *MessageList) ScrollToBottom() {
	if !m.scrollable() {
		m.unseen = 0
		return
	}
	m.list.ScrollToBottom()
	m.unseen = 0
	m.updatePill()
}

// ScrollToTop is Ctrl+Home.
func (m *MessageList) ScrollToTop() {
	if m.scrollable() {
		m.list.ScrollToTop()
	}
}

// updatePill is called after anything that could change the count or the
// reader's position. It repaints rather than refreshing: a Refresh here would
// rebuild the List, which is O(rows), for a change of one small label.
func (m *MessageList) updatePill() {
	want := m.unseen > 0 && !m.NearBottom()
	if want {
		m.pill.setCount(m.unseen)
	}
	if want == m.pill.Visible() {
		return
	}
	if want {
		m.pill.Show()
	} else {
		m.pill.Hide()
	}
	canvas.Refresh(m)
}

// --- the model, the heights, and the swap ------------------------------------

// reset rebuilds every derived thing from a set of lines.
func (m *MessageList) reset(lines []model.Line, since time.Time) {
	m.lines = lines
	m.since = since
	m.refreshPass()
	m.bodies = make([]bodyCache, 0, len(lines))
	for _, l := range lines {
		m.bodies = append(m.bodies, m.renderLine(l))
	}
	m.rebuildRows()
}

// refreshPass re-reads everything the app can change under us: the display
// options, the font size, and the column geometry that follows from them.
func (m *MessageList) refreshPass() {
	m.ropts = m.opts.Render()
	th := m.Theme()
	if size := th.Size(theme.SizeNameText); m.metrics == nil || size != m.textSize {
		m.textSize = size
		m.metrics = newMeasurer(size)
	}
	m.geom = newGeom(m.metrics, th, m.ropts)
}

// rebuildRows re-runs the row model over the lines already rendered.
func (m *MessageList) rebuildRows() {
	m.build = newRowBuilder(RowOptions{Since: m.since, Loading: m.loading, Now: m.now()})
	// the kind is known already: rendering the line is what decided it
	for i, l := range m.lines {
		m.build.addKind(l, m.bodies[i].art != nil)
	}
	m.rows = m.build.rows
	m.measure(0)
}

// measure fills in the heights of rows[from:] and the content height they
// add up to. It touches no widget: writing a height into a List that is
// already on screen refreshes the whole thing, which is O(rows) — a thousand
// of those in a row is the quadratic trap this design exists to avoid. Only
// swap (on an unrendered List) and Append (a handful of rows) ever write.
func (m *MessageList) measure(from int) {
	pad := m.Theme().Size(theme.SizeNamePadding)
	if from == 0 {
		m.heights = make([]float32, 0, len(m.rows))
		m.total = 0
	} else if m.total > 0 {
		m.total += pad // re-open the gap the last row closed
	}
	for i := from; i < len(m.rows); i++ {
		h := m.heightOf(m.rows[i])
		m.heights = append(m.heights, h)
		m.total += h + pad
	}
	if m.total > 0 {
		m.total -= pad // n rows have n-1 gaps between them
	}
}

// heightOf is the whole variable-height bet in one function: no widget is
// built, nothing is laid out, and the answer is exact (wrap_test.go).
func (m *MessageList) heightOf(row Row) float32 {
	gap := float32(0)
	if row.Gap {
		gap = authorGap
	}
	switch row.Kind {
	case RowDay, RowNew:
		return m.geom.lineH + dividerAir
	case RowLoading:
		return m.geom.lineH
	case RowArt:
		h := m.geom.lineH
		if a := m.artFor(row.Index); a != nil {
			h = a.MinSize().Height
		}
		return h + gap
	}
	segs := m.bodyFor(row.Index)
	width := m.bodyWidth(row)
	rows, lineH := m.metrics.rows(segs, width)
	return float32(rows)*lineH + gap
}

// bodyWidth is how much room the message text has on this row, which depends
// on how wide its own nick is: the column is soft, and a long nick spends the
// body's width rather than being cut.
func (m *MessageList) bodyWidth(row Row) float32 {
	nick := float32(0)
	if row.Head {
		nick = m.headWidth(row.Line)
	}
	w := m.width - m.geom.bodyX(nick)
	if w < m.geom.cell {
		w = m.geom.cell // a column too narrow to hold a character still wraps
	}
	return w
}

// headWidth is the width of the nick column's text. The row widget asks the
// same question of the same function, because a disagreement between the
// measured width and the drawn one is a clipped message.
func (m *MessageList) headWidth(l model.Line) float32 {
	return m.metrics.width(headText(l), nickStyle(l))
}

func nickStyle(l model.Line) fyne.TextStyle {
	return fyne.TextStyle{Monospace: true, Bold: true, Italic: l.Action}
}

// renderLine turns one line into the form the rows draw: segments for prose,
// a character grid for mIRC background art. Both come from internal/render —
// the Timeline never re-implements a pixel of the engine.
func (m *MessageList) renderLine(l model.Line) bodyCache {
	if art, ok := render.Art(l, m.ropts); ok {
		return bodyCache{art: art}
	}
	return bodyCache{segs: render.Body(l, m.ropts)}
}

func (m *MessageList) bodyFor(i int) []widget.RichTextSegment {
	if i < 0 || i >= len(m.bodies) {
		return nil
	}
	return m.bodies[i].segs
}

func (m *MessageList) artFor(i int) fyne.CanvasObject {
	if i < 0 || i >= len(m.bodies) {
		return nil
	}
	return m.bodies[i].art
}

func (m *MessageList) now() time.Time {
	if m.opts.Now != nil {
		return m.opts.Now()
	}
	return time.Now()
}

// swap builds a fresh List, loads every height into it while it is still
// unrendered — the only state in which that is cheap — and puts it on screen
// in place of the old one.
func (m *MessageList) swap() {
	m.armed = false
	defer func() { m.armed = true }()
	l := widget.NewList(
		func() int { return len(m.rows) },
		func() fyne.CanvasObject { return newTimelineRow(m) },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < 0 || id >= len(m.rows) {
				return
			}
			o.(*timelineRow).set(m.rows[id])
			// The top of the buffer came into view: ask for the page above
			// it. The session swallows a request already in flight and
			// answers through the funnel, so this never re-enters us.
			//
			// armed is what keeps that from meaning "every buffer switch
			// fetches a page": laying a fresh List out starts at offset zero
			// and therefore visits row zero, before the jump to the bottom
			// has happened. Only a reader who scrolled there counts.
			if m.armed && id <= topPagingRows && m.opts.Older != nil && len(m.lines) > 0 {
				m.opts.Older()
			}
		})
	l.HideSeparators = true
	for i, h := range m.heights {
		l.SetItemHeight(i, h)
	}
	m.list = l
	m.objs = []fyne.CanvasObject{m.list, m.pill}
	// the swap replaces a laid-out child with a fresh one, and a refresh
	// alone does not re-run the parent's layout: give it the size the old
	// one had, or it renders into nothing and every scroll question below
	// answers from a viewport of zero height
	if s := m.Size(); s.Height > 0 {
		l.Resize(s)
	}
	canvas.Refresh(m)
}

// --- the widget -------------------------------------------------------------

// Resize catches the one thing that invalidates every height at once. Rows
// are only re-measured when the width really changed: a layout pass that
// hands back the same size must not cost a rebuild.
func (m *MessageList) Resize(size fyne.Size) {
	changed := size.Width != m.width
	m.BaseWidget.Resize(size)
	if !changed {
		return
	}
	follow := m.NearBottom()
	offset := float32(0)
	if m.list != nil {
		offset = m.list.GetScrollOffset()
	}
	m.width = size.Width
	m.refreshPass()
	m.measure(0)
	m.swap()
	m.list.Resize(size)
	if follow {
		m.list.ScrollToBottom()
	} else {
		m.list.ScrollToOffset(offset)
	}
}

func (m *MessageList) CreateRenderer() fyne.WidgetRenderer {
	return &messageListRenderer{m: m}
}

type messageListRenderer struct {
	m *MessageList
}

func (r *messageListRenderer) Destroy() {}

func (r *messageListRenderer) Objects() []fyne.CanvasObject { return r.m.objs }

func (r *messageListRenderer) MinSize() fyne.Size { return fyne.NewSize(0, r.m.geom.lineH) }

func (r *messageListRenderer) Layout(size fyne.Size) {
	if r.m.list != nil {
		r.m.list.Resize(size)
		r.m.list.Move(fyne.NewPos(0, 0))
	}
	ps := r.m.pill.MinSize()
	r.m.pill.Resize(ps)
	r.m.pill.Move(fyne.NewPos((size.Width-ps.Width)/2, size.Height-ps.Height-dividerAir))
}

func (r *messageListRenderer) Refresh() {
	if r.m.list != nil {
		r.m.list.Refresh()
	}
	r.m.pill.Refresh()
	canvas.Refresh(r.m)
}
