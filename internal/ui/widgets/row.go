package widgets

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// Row anatomy, §4.2. Three columns on one baseline:
//
//	14:02   alice  the message text, which wraps
//	               under itself and not back to the gutter
//	14:03          a second message from alice, inside the
//	               grouping window, so it has no nick of its own
//
// The gutter and the nick column are monospace whatever the body does —
// that is the "mono gutter + proportional body" typography §4.2 names as
// the fallback, and it is what makes the columns line up. The nick is
// right-aligned inside a column at least eight cells wide: short nicks
// leave the body edge straight, long ones push their own body right rather
// than being cut (soft alignment, never truncation).
type geom struct {
	gutter  float32 // timestamp column, zero when timestamps are off
	nickMin float32 // the eight cells
	cell    float32 // one monospace cell
	pad     float32 // the gap between columns
	lineH   float32 // one wrapped row of body text
	layout  string  // timestamp format
}

// authorGap is the one airy concession §4.2 allows: a few pixels above a row
// whose author differs from the one above it. There is no spacing scale in
// the app yet, so this is a number rather than a token, and it is the only
// one in the Timeline.
const authorGap = 4

// dividerAir is the space a day or NEW rule adds around itself.
const dividerAir = 6

func newGeom(m *measurer, th fyne.Theme, opts render.Options) geom {
	g := geom{pad: th.Size(theme.SizeNamePadding), layout: opts.TimeLayout}
	g.cell = m.width("M", fyne.TextStyle{Monospace: true})
	g.nickMin = 8 * g.cell
	_, g.lineH = m.rows([]widget.RichTextSegment{
		&widget.TextSegment{Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Monospace: opts.Mono}}},
	}, 1000)
	if opts.ShowTimestamps {
		// the widest the clock ever gets in this layout: a 12-hour layout
		// is a character wider in the afternoon than in the morning
		day := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
		for _, h := range []int{0, 13, 23} {
			if w := m.width(day.Add(time.Duration(h)*time.Hour).Format(g.layout),
				fyne.TextStyle{Monospace: true}); w > g.gutter {
				g.gutter = w
			}
		}
		g.gutter += g.pad
	}
	return g
}

// bodyX is where the message text starts for a row whose nick column came
// out this wide.
func (g geom) bodyX(nickW float32) float32 {
	if nickW < g.nickMin {
		nickW = g.nickMin
	}
	return g.gutter + nickW + g.pad
}

// timelineRow draws every kind of row: message, art, day rule, NEW rule and
// the loading line. One widget rather than five because widget.List builds
// its rows from a single template and pours every id through it.
type timelineRow struct {
	widget.BaseWidget
	m   *MessageList
	row Row

	ts    *canvas.Text
	nick  *canvas.Text
	body  *widget.RichText
	art   *fyne.Container
	rule  *canvas.Rectangle
	label *canvas.Text
	fill  *canvas.Rectangle // the hover wash
	strip *fyne.Container   // the hover actions

	hovered bool
}

func newTimelineRow(m *MessageList) *timelineRow {
	r := &timelineRow{m: m}
	r.ts = canvas.NewText("", color.Transparent)
	r.ts.TextStyle = fyne.TextStyle{Monospace: true}
	r.nick = canvas.NewText("", color.Transparent)
	r.nick.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	r.body = widget.NewRichText()
	r.body.Wrapping = fyne.TextWrapWord
	// the zero value of Scroll nests a scroller inside the list's own and
	// collapses the text to nothing (the same trap v1's msgView fell into)
	r.body.Scroll = fyne.ScrollNone
	r.art = container.NewWithoutLayout()
	r.rule = canvas.NewRectangle(color.Transparent)
	r.label = canvas.NewText("", color.Transparent)
	r.fill = canvas.NewRectangle(color.Transparent)
	r.strip = container.NewWithoutLayout(
		newRowAction(r), newRowAction(r), newRowAction(r), newRowAction(r),
	)
	r.strip.Hide()
	r.ExtendBaseWidget(r)
	return r
}

func (r *timelineRow) CreateRenderer() fyne.WidgetRenderer {
	return &rowRenderer{r: r, objs: []fyne.CanvasObject{
		r.fill, r.rule, r.label, r.ts, r.nick, r.body, r.art, r.strip,
	}}
}

// MinSize is one line: the List uses the template's minimum as the height of
// any row that has not been given one, and as its own. Real heights come
// from the height pass in messagelist.go.
func (r *timelineRow) MinSize() fyne.Size {
	return fyne.NewSize(0, r.m.geom.lineH)
}

// set pours one row of the model through this widget.
func (r *timelineRow) set(row Row) {
	r.row = row
	th := r.Theme()
	v := ecotheme.Variant(th)
	col := func(n fyne.ThemeColorName) color.Color { return th.Color(n, v) }

	r.ts.Text, r.nick.Text, r.label.Text = "", "", ""
	r.body.Hide()
	r.art.Hide()
	r.rule.Hide()
	r.label.Hide()
	r.ts.Hide()
	r.nick.Hide()

	switch row.Kind {
	case RowMessage, RowArt:
		if r.m.ropts.ShowTimestamps {
			r.ts.Text = row.Line.Time.Local().Format(r.m.geom.layout)
			r.ts.Color = col(ecotheme.TextFaint)
			r.ts.TextSize = r.m.textSize
			r.ts.Show()
		}
		if row.Kind == RowArt {
			r.art.Objects = r.art.Objects[:0]
			if obj := r.m.artFor(row.Index); obj != nil {
				r.art.Objects = append(r.art.Objects, obj)
			}
			r.art.Show()
			break
		}
		if row.Head {
			r.nick.Text = headText(row.Line)
			r.nick.Color = col(render.NickColor(row.Line, r.m.ropts))
			if row.Line.Status {
				r.nick.Color = col(ecotheme.TextFaint)
			}
			r.nick.TextSize = r.m.textSize
			r.nick.TextStyle.Italic = row.Line.Action
			r.nick.Show()
		}
		r.body.Segments = r.m.bodyFor(row.Index)
		r.body.Refresh()
		r.body.Show()
	case RowDay, RowNew, RowLoading:
		r.label.TextSize = r.m.textSize
		r.rule.Show()
		r.label.Show()
		switch row.Kind {
		case RowDay:
			r.label.Text = DayLabel(row.At, r.m.now())
			r.label.Color = col(ecotheme.TextMuted)
			r.rule.FillColor = col(ecotheme.Border)
		case RowNew:
			r.label.Text = "NEW — since " + row.At.Local().Format(r.m.geom.layout)
			r.label.Color = col(ecotheme.AccentText)
			r.rule.FillColor = col(ecotheme.Accent)
		case RowLoading:
			r.label.Text = "Loading older…"
			r.label.Color = col(ecotheme.TextMuted)
			r.rule.Hide()
		}
		r.label.TextStyle = fyne.TextStyle{Italic: row.Kind == RowLoading}
	}
	r.hovered = false
	r.strip.Hide()
	r.fill.FillColor = color.Transparent
	r.Refresh()
}

// headText is what the nick column says: the nick, a notice in its dashes,
// an action's "* nick", or the status bullet.
func headText(l model.Line) string {
	switch {
	case l.Status:
		return "·"
	case l.Action:
		return "* " + l.Nick
	case l.Notice:
		return "-" + l.Nick + "-"
	}
	return l.Nick
}

// --- hover and the row menu --------------------------------------------------

func (r *timelineRow) MouseIn(*desktop.MouseEvent) {
	specs := r.actions()
	if len(specs) == 0 {
		return
	}
	for i, o := range r.strip.Objects {
		a := o.(*rowAction)
		if i >= len(specs) {
			a.Hide()
			continue
		}
		a.kind, a.icon = specs[i].kind, iconFor(specs[i].kind)
		a.Show()
	}
	r.hovered = true
	th := r.Theme()
	r.fill.FillColor = th.Color(ecotheme.HoverOverlay, ecotheme.Variant(th))
	r.strip.Show()
	r.Refresh()
}

// iconFor is the one place an action gets a face, so the strip and the menu
// can never drift into offering different things.
func iconFor(k actionKind) fyne.Resource {
	switch k {
	case actCopyNick:
		return theme.AccountIcon()
	case actQuote:
		return theme.MailReplyIcon()
	case actDelete:
		return theme.DeleteIcon()
	}
	return theme.ContentCopyIcon()
}

func (r *timelineRow) MouseMoved(*desktop.MouseEvent) {}

func (r *timelineRow) MouseOut() {
	if !r.hovered {
		return
	}
	r.hovered = false
	r.fill.FillColor = color.Transparent
	r.strip.Hide()
	r.Refresh()
}

// TappedSecondary is the Parity Rule (blueprint §4.2): right-click offers
// exactly what the hover strip offers, never more and never less.
func (r *timelineRow) TappedSecondary(e *fyne.PointEvent) {
	if !r.actionable() {
		return
	}
	var items []*fyne.MenuItem
	for _, a := range r.actions() {
		act := a
		items = append(items, fyne.NewMenuItem(act.label, func() { r.act(act.kind) }))
	}
	if len(items) == 0 {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(r)
	if c == nil {
		return
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), c, e.AbsolutePosition)
}

func (r *timelineRow) actionable() bool {
	return r.row.Kind == RowMessage || r.row.Kind == RowArt
}

type actionKind uint8

const (
	actCopy actionKind = iota
	actCopyNick
	actQuote
	actDelete
)

type rowActionSpec struct {
	kind  actionKind
	label string
}

// actions is the one list both the hover strip and the context menu read.
func (r *timelineRow) actions() []rowActionSpec {
	if !r.actionable() {
		return nil
	}
	l := r.row.Line
	out := []rowActionSpec{{actCopy, "Copy message"}}
	if l.Nick != "" && !l.Status {
		out = append(out, rowActionSpec{actCopyNick, "Copy nick: " + l.Nick})
	}
	if !l.Status {
		out = append(out, rowActionSpec{actQuote, "Quote"})
	}
	if l.Self && l.MsgID != "" && r.m.opts.Delete != nil {
		out = append(out, rowActionSpec{actDelete, "Delete this message"})
	}
	return out
}

func (r *timelineRow) act(k actionKind) {
	l := r.row.Line
	switch k {
	case actCopy:
		if r.m.opts.Copy != nil {
			r.m.opts.Copy(render.PlainText(l))
		}
	case actCopyNick:
		if r.m.opts.Copy != nil {
			r.m.opts.Copy(l.Nick)
		}
	case actQuote:
		if r.m.opts.Quote != nil {
			r.m.opts.Quote(l)
		}
	case actDelete:
		if r.m.opts.Delete != nil {
			r.m.opts.Delete(l)
		}
	}
}

// --- the renderer ------------------------------------------------------------

type rowRenderer struct {
	r    *timelineRow
	objs []fyne.CanvasObject
}

func (rr *rowRenderer) Destroy()                     {}
func (rr *rowRenderer) Objects() []fyne.CanvasObject { return rr.objs }
func (rr *rowRenderer) MinSize() fyne.Size           { return rr.r.MinSize() }

func (rr *rowRenderer) Refresh() {
	for _, o := range rr.objs {
		o.Refresh()
	}
	rr.Layout(rr.r.Size())
	canvas.Refresh(rr.r)
}

func (rr *rowRenderer) Layout(size fyne.Size) {
	r := rr.r
	g := r.m.geom
	top := float32(0)
	if r.row.Gap {
		top = authorGap
	}
	r.fill.Resize(size)
	r.fill.Move(fyne.NewPos(0, 0))

	switch r.row.Kind {
	case RowMessage, RowArt:
		if r.ts.Visible() {
			r.ts.Resize(r.ts.MinSize())
			r.ts.Move(fyne.NewPos(0, top))
		}
		nickW := r.headWidth()
		if r.nick.Visible() {
			r.nick.Resize(fyne.NewSize(nickW, g.lineH))
			// right-aligned inside its column, so the body edge stays put
			x := g.bodyX(nickW) - g.pad - nickW
			r.nick.Move(fyne.NewPos(x, top))
		}
		if r.row.Kind == RowArt {
			// art carries its own gutter and nick inside the grid, so it
			// starts at the left edge — and it is deliberately NOT put in a
			// scroller: a horizontal scroller under the pointer swallows the
			// wheel the Timeline needs (§5.6's clip, without the h-scroll).
			for _, o := range r.art.Objects {
				o.Resize(o.MinSize())
				o.Move(fyne.NewPos(0, top))
			}
			r.art.Resize(size)
			r.art.Move(fyne.NewPos(0, 0))
			break
		}
		x := g.bodyX(nickW)
		pad := r.Theme().Size(theme.SizeNameInnerPadding)
		// RichText reserves an inner padding on every side that a chat row
		// cannot afford — it would be four pixels of air per message. There
		// is no way to switch it off, so the widget is offset by exactly
		// that much and given the padding back in its size: the glyphs land
		// where the row wants them and the empty margin falls outside.
		r.body.Move(fyne.NewPos(x-pad, top-pad))
		r.body.Resize(fyne.NewSize(size.Width-x+2*pad, size.Height-top+2*pad))
	case RowDay, RowNew:
		lw := r.label.MinSize().Width
		r.label.Resize(r.label.MinSize())
		r.label.Move(fyne.NewPos(g.gutter, dividerAir/2))
		x := g.gutter + lw + g.pad
		r.rule.Resize(fyne.NewSize(fyne.Max(size.Width-x, 0), 1))
		r.rule.Move(fyne.NewPos(x, dividerAir/2+g.lineH/2))
	case RowLoading:
		r.label.Resize(r.label.MinSize())
		r.label.Move(fyne.NewPos(g.gutter, 0))
	}

	if r.strip.Visible() {
		w := g.lineH
		visible := 0
		for _, o := range r.strip.Objects {
			if o.Visible() {
				visible++
			}
		}
		x := size.Width - float32(visible)*(w+2)
		for _, o := range r.strip.Objects {
			if !o.Visible() {
				continue
			}
			o.Resize(fyne.NewSize(w, w))
			o.Move(fyne.NewPos(x, top))
			x += w + 2
		}
		r.strip.Resize(size)
		r.strip.Move(fyne.NewPos(0, 0))
	}
}

// headWidth is the width the nick column wants for this row, asked of the
// same function the height pass asked: if the two ever disagreed, the body
// would be measured at one width and drawn at another, and the last line of
// every long message would be clipped.
func (r *timelineRow) headWidth() float32 {
	if !r.nick.Visible() {
		return 0
	}
	return r.m.headWidth(r.row.Line)
}

// --- one hover action --------------------------------------------------------

// rowAction is a single icon in the hover strip: the size of one text line,
// so a dense row keeps its density, which is more than a widget.Button will
// agree to be.
type rowAction struct {
	widget.BaseWidget
	row     *timelineRow
	icon    fyne.Resource
	kind    actionKind
	hovered bool
}

func newRowAction(row *timelineRow) *rowAction {
	a := &rowAction{row: row, icon: theme.ContentCopyIcon()}
	a.ExtendBaseWidget(a)
	a.Hide()
	return a
}

func (a *rowAction) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(a.icon)
	img.FillMode = canvas.ImageFillContain
	fill := canvas.NewRectangle(color.Transparent)
	return &actionRenderer{a: a, img: img, fill: fill, objs: []fyne.CanvasObject{fill, img}}
}

func (a *rowAction) Tapped(*fyne.PointEvent) { a.row.act(a.kind) }

func (a *rowAction) MouseIn(*desktop.MouseEvent) { a.hovered = true; a.Refresh() }
func (a *rowAction) MouseMoved(*desktop.MouseEvent) {
}
func (a *rowAction) MouseOut() { a.hovered = false; a.Refresh() }

func (a *rowAction) Cursor() desktop.Cursor { return desktop.PointerCursor }

type actionRenderer struct {
	a    *rowAction
	img  *canvas.Image
	fill *canvas.Rectangle
	objs []fyne.CanvasObject
}

func (ar *actionRenderer) Destroy()                     {}
func (ar *actionRenderer) Objects() []fyne.CanvasObject { return ar.objs }
func (ar *actionRenderer) MinSize() fyne.Size           { return fyne.NewSquareSize(16) }

func (ar *actionRenderer) Layout(size fyne.Size) {
	ar.fill.Resize(size)
	inset := size.Height * 0.15
	ar.img.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	ar.img.Move(fyne.NewPos(inset, inset))
}

func (ar *actionRenderer) Refresh() {
	th := ar.a.Theme()
	v := ecotheme.Variant(th)
	if ar.a.hovered {
		ar.fill.FillColor = th.Color(ecotheme.SelectionOverlay, v)
	} else {
		ar.fill.FillColor = color.Transparent
	}
	ar.img.Resource = ar.a.icon
	ar.fill.Refresh()
	ar.img.Refresh()
}

var (
	_ desktop.Hoverable      = (*timelineRow)(nil)
	_ fyne.SecondaryTappable = (*timelineRow)(nil)
	_ fyne.Tappable          = (*rowAction)(nil)
	_ desktop.Hoverable      = (*rowAction)(nil)
	_ desktop.Cursorable     = (*rowAction)(nil)
)
