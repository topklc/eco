package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// The small pieces the rail and the People pane are built from. They are here
// rather than in either file because both panes wear the same anatomy - a dot,
// a name, and one thing on the right - and two copies of it would drift.

// tapIcon is a bare icon that answers a tap: one text line tall, so a dense
// row keeps its density, which is more than widget.Button will agree to be.
//
// The tap is handed the icon's ABSOLUTE position, because nearly everything
// one of these opens is a popup menu, and a menu has to appear where it was
// asked for.
type tapIcon struct {
	widget.BaseWidget
	icon fyne.Resource
	tap  func(fyne.Position)

	hovered bool
}

func newTapIcon(res fyne.Resource, tap func(fyne.Position)) *tapIcon {
	t := &tapIcon{icon: res, tap: tap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapIcon) Tapped(e *fyne.PointEvent) {
	if t.tap != nil {
		t.tap(e.AbsolutePosition)
	}
}

func (t *tapIcon) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *tapIcon) MouseMoved(*desktop.MouseEvent) {}
func (t *tapIcon) MouseOut()                      { t.hovered = false; t.Refresh() }
func (t *tapIcon) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (t *tapIcon) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(t.icon)
	img.FillMode = canvas.ImageFillContain
	fill := canvas.NewRectangle(color.Transparent)
	return &tapIconRenderer{t: t, img: img, fill: fill, objs: []fyne.CanvasObject{fill, img}}
}

type tapIconRenderer struct {
	t    *tapIcon
	img  *canvas.Image
	fill *canvas.Rectangle
	objs []fyne.CanvasObject
}

func (r *tapIconRenderer) Destroy()                     {}
func (r *tapIconRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *tapIconRenderer) MinSize() fyne.Size           { return fyne.NewSquareSize(16) }

func (r *tapIconRenderer) Layout(size fyne.Size) {
	r.fill.Resize(size)
	inset := size.Height * 0.15
	r.img.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.img.Move(fyne.NewPos(inset, inset))
}

func (r *tapIconRenderer) Refresh() {
	th := r.t.Theme()
	v := ecotheme.Variant(th)
	if r.t.hovered {
		r.fill.FillColor = th.Color(ecotheme.SelectionOverlay, v)
	} else {
		r.fill.FillColor = color.Transparent
	}
	r.img.Resource = r.t.icon
	r.fill.Refresh()
	r.img.Refresh()
}

var (
	_ fyne.Tappable      = (*tapIcon)(nil)
	_ desktop.Hoverable  = (*tapIcon)(nil)
	_ desktop.Cursorable = (*tapIcon)(nil)
)

// textPill is a small labelled button in the same shape as the count pills:
// one text line tall, so it fits inside a row without making the row taller -
// which is the whole reason it is not a widget.Button. It has two looks, the
// outlined one and the accent-filled one, and they are the same two the rest
// of the app's pills have.
//
// It is [Connect] on an offline network and the away toggle in the identity
// chip; there is no third look for a third caller to invent.
type textPill struct {
	widget.BaseWidget
	text string
	on   bool // accent-filled rather than outlined
	tap  func()

	hovered bool
	fill    *canvas.Rectangle
	label   *canvas.Text
}

func newTextPill(text string, tap func()) *textPill {
	p := &textPill{text: text, tap: tap}
	p.fill = canvas.NewRectangle(color.Transparent)
	p.label = canvas.NewText(text, color.Transparent)
	p.ExtendBaseWidget(p)
	return p
}

// setText renames the pill; the renderer re-measures on the next Refresh.
func (p *textPill) setText(s string) {
	if p.text == s {
		return
	}
	p.text = s
	p.label.Text = s
}

func (p *textPill) Tapped(*fyne.PointEvent) {
	if p.tap != nil {
		p.tap()
	}
}

func (p *textPill) MouseIn(*desktop.MouseEvent)    { p.hovered = true; p.Refresh() }
func (p *textPill) MouseMoved(*desktop.MouseEvent) {}
func (p *textPill) MouseOut()                      { p.hovered = false; p.Refresh() }
func (p *textPill) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (p *textPill) CreateRenderer() fyne.WidgetRenderer {
	return &textPillRenderer{p: p, objs: []fyne.CanvasObject{p.fill, p.label}}
}

type textPillRenderer struct {
	p    *textPill
	objs []fyne.CanvasObject
}

func (r *textPillRenderer) Destroy()                     {}
func (r *textPillRenderer) Objects() []fyne.CanvasObject { return r.objs }

func (r *textPillRenderer) MinSize() fyne.Size {
	pad := r.p.Theme().Size(theme.SizeNamePadding)
	t := r.p.label.MinSize()
	return fyne.NewSize(t.Width+3*pad, t.Height+pad)
}

func (r *textPillRenderer) Layout(size fyne.Size) {
	r.p.fill.Resize(size)
	r.p.fill.CornerRadius = size.Height / 2
	t := r.p.label.MinSize()
	r.p.label.Resize(t)
	r.p.label.Move(fyne.NewPos((size.Width-t.Width)/2, (size.Height-t.Height)/2))
}

func (r *textPillRenderer) Refresh() {
	th := r.p.Theme()
	v := ecotheme.Variant(th)
	r.p.label.Text = r.p.text
	r.p.label.TextSize = th.Size(theme.SizeNameCaptionText)
	switch {
	case r.p.on:
		r.p.fill.FillColor = th.Color(ecotheme.Accent, v)
		r.p.fill.StrokeWidth = 0
		r.p.label.Color = th.Color(ecotheme.OnAccent, v)
	default:
		r.p.fill.FillColor = color.Transparent
		if r.p.hovered {
			r.p.fill.FillColor = th.Color(ecotheme.HoverOverlay, v)
		}
		r.p.fill.StrokeColor = th.Color(ecotheme.Border, v)
		r.p.fill.StrokeWidth = 1
		r.p.label.Color = th.Color(ecotheme.TextMuted, v)
	}
	r.Layout(r.p.Size())
	r.p.fill.Refresh()
	r.p.label.Refresh()
}

var (
	_ fyne.Tappable      = (*textPill)(nil)
	_ desktop.Hoverable  = (*textPill)(nil)
	_ desktop.Cursorable = (*textPill)(nil)
)

// elide is the one answer to "this name is wider than the column it is in".
// A canvas.Text does not truncate - it simply draws past its bounds and over
// whatever is to its right - so a row that cannot fit its name has to cut it
// here, at the width the layout actually has.
//
// The common case costs one cached measurement: names fit. The cut itself is a
// binary search over the runes, which is about five measurements, and it is
// runes rather than bytes because half a rune is not a character.
func (m *measurer) elide(text string, style fyne.TextStyle, max float32) string {
	if text == "" || max <= 0 {
		return ""
	}
	if m.width(text, style) <= max {
		return text
	}
	const ellipsis = "…"
	max -= m.width(ellipsis, style)
	if max <= 0 {
		return ellipsis
	}
	rs := []rune(text)
	lo, hi := 0, len(rs)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if m.width(string(rs[:mid]), style) <= max {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(rs[:lo]) + ellipsis
}
