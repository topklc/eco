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

// Title is the conversation's name in the chat header, and the door to
// everything you can do to that conversation.
//
// It replaces a ⋮ button that sat in the corner offering the same menu the
// sidebar row's right-click already offered: two doors to one room, one of
// them costing permanent space to say nothing new. Hanging the menu on the
// name instead puts it on the object it acts on, and a bold title that
// lights up under the cursor invites a click from someone who would never
// think to right-click a list.
type Title struct {
	widget.BaseWidget

	text    string
	tap     func(fyne.CanvasObject)
	enabled bool
	hovered bool

	fill  *canvas.Rectangle
	label *canvas.Text
}

// NewTitle builds the header title. tap is called with the widget itself so
// the caller can anchor a menu under it.
func NewTitle(tap func(anchor fyne.CanvasObject)) *Title {
	t := &Title{tap: tap}
	t.fill = canvas.NewRectangle(color.Transparent)
	t.label = canvas.NewText("", color.Transparent)
	t.label.TextStyle = fyne.TextStyle{Bold: true}
	t.ExtendBaseWidget(t)
	return t
}

// SetText names the conversation. An empty name (the idle window) disables
// the widget: there is nothing to open a menu about.
func (t *Title) SetText(s string) {
	if t.text == s {
		return
	}
	t.text = s
	t.Refresh()
}

// SetEnabled says whether the menu is available yet.
func (t *Title) SetEnabled(on bool) {
	if t.enabled == on {
		return
	}
	t.enabled = on
	t.Refresh()
}

func (t *Title) live() bool { return t.enabled && t.text != "" }

func (t *Title) Tapped(*fyne.PointEvent) {
	if t.live() && t.tap != nil {
		t.tap(t)
	}
}

func (t *Title) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *Title) MouseMoved(*desktop.MouseEvent) {}
func (t *Title) MouseOut()                      { t.hovered = false; t.Refresh() }

func (t *Title) Cursor() desktop.Cursor {
	if t.live() {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (t *Title) CreateRenderer() fyne.WidgetRenderer {
	return &titleRenderer{t: t, objs: []fyne.CanvasObject{t.fill, t.label}}
}

type titleRenderer struct {
	t    *Title
	objs []fyne.CanvasObject
}

func (r *titleRenderer) Destroy()                     {}
func (r *titleRenderer) Objects() []fyne.CanvasObject { return r.objs }

func (r *titleRenderer) MinSize() fyne.Size {
	pad := r.t.Theme().Size(theme.SizeNamePadding)
	s := r.t.label.MinSize()
	return fyne.NewSize(s.Width+pad*2, s.Height+pad)
}

func (r *titleRenderer) Layout(size fyne.Size) {
	pad := r.t.Theme().Size(theme.SizeNamePadding)
	r.t.fill.Resize(size)
	r.t.label.Move(fyne.NewPos(pad, (size.Height-r.t.label.MinSize().Height)/2))
	r.t.label.Resize(r.t.label.MinSize())
}

func (r *titleRenderer) Refresh() {
	th, v := r.t.Theme(), fyne.CurrentApp().Settings().ThemeVariant()
	r.t.label.Text = r.t.text
	r.t.label.TextSize = th.Size(theme.SizeNameText)
	r.t.label.Color = th.Color(ecotheme.TextPrimary, v)
	// the hover wash is the only thing that says "this is a control"; without
	// a name, or before a session exists, it stays a plain label
	if r.t.hovered && r.t.live() {
		r.t.fill.FillColor = th.Color(ecotheme.HoverOverlay, v)
	} else {
		r.t.fill.FillColor = color.Transparent
	}
	r.t.fill.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.t.fill.Refresh()
	r.t.label.Refresh()
	canvas.Refresh(r.t)
}
