package widgets

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// jumpPill is the "3 new messages ↓" button that appears only when the reader
// is up in the scrollback and something arrived below them. It is the whole
// of the Timeline's chrome.
type jumpPill struct {
	widget.BaseWidget
	count int
	onTap func()

	fill  *canvas.Rectangle
	label *canvas.Text
}

func newJumpPill(tap func()) *jumpPill {
	p := &jumpPill{onTap: tap}
	p.fill = canvas.NewRectangle(nil)
	p.label = canvas.NewText("", nil)
	p.ExtendBaseWidget(p)
	return p
}

func (p *jumpPill) setCount(n int) {
	if n == p.count {
		return
	}
	p.count = n
	p.Refresh()
}

func (p *jumpPill) text() string {
	if p.count == 1 {
		return "1 new message  ↓"
	}
	return fmt.Sprintf("%d new messages  ↓", p.count)
}

func (p *jumpPill) Tapped(*fyne.PointEvent) {
	if p.onTap != nil {
		p.onTap()
	}
}

func (p *jumpPill) CreateRenderer() fyne.WidgetRenderer {
	return &pillRenderer{p: p, objs: []fyne.CanvasObject{p.fill, p.label}}
}

type pillRenderer struct {
	p    *jumpPill
	objs []fyne.CanvasObject
}

func (pr *pillRenderer) Destroy()                     {}
func (pr *pillRenderer) Objects() []fyne.CanvasObject { return pr.objs }

func (pr *pillRenderer) MinSize() fyne.Size {
	pad := pr.p.Theme().Size(theme.SizeNameInnerPadding)
	t := pr.p.label.MinSize()
	return fyne.NewSize(t.Width+2*pad, t.Height+pad)
}

func (pr *pillRenderer) Layout(size fyne.Size) {
	pr.p.fill.Resize(size)
	pr.p.fill.CornerRadius = size.Height / 2
	t := pr.p.label.MinSize()
	pr.p.label.Resize(t)
	pr.p.label.Move(fyne.NewPos((size.Width-t.Width)/2, (size.Height-t.Height)/2))
}

func (pr *pillRenderer) Refresh() {
	th := pr.p.Theme()
	v := ecotheme.Variant(th)
	pr.p.label.Text = pr.p.text()
	pr.p.label.TextSize = th.Size(theme.SizeNameText)
	pr.p.label.Color = th.Color(ecotheme.OnAccent, v)
	pr.p.fill.FillColor = th.Color(ecotheme.Accent, v)
	pr.Layout(pr.p.Size())
	pr.p.fill.Refresh()
	pr.p.label.Refresh()
}

var _ fyne.Tappable = (*jumpPill)(nil)
