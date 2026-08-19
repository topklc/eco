package widgets

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
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

// countPill is the rail's attention badge: the right-hand end of a row
// (blueprint §4.1). What it says, and which of the two it is, is pillFor's —
// a rule with no pixels in it, so it is tested without a canvas.
type countPill struct {
	widget.BaseWidget
	counts viewmodel.Counts

	fill  *canvas.Rectangle
	label *canvas.Text
}

func newCountPill() *countPill {
	p := &countPill{}
	p.fill = canvas.NewRectangle(nil)
	p.label = canvas.NewText("", nil)
	p.label.TextStyle = fyne.TextStyle{Bold: true}
	p.ExtendBaseWidget(p)
	p.Hide()
	return p
}

// set pours one row's counts into the pill and reports whether there is a pill
// to draw at all.
func (p *countPill) set(c viewmodel.Counts) bool {
	p.counts = c
	_, _, _, show := pillFor(c)
	if !show {
		p.Hide()
		return false
	}
	p.Show()
	p.Refresh()
	return true
}

// pillFor is the whole of §4.1's "grey = unread count, accent-filled =
// mentions", as one function.
//
// Mentions win, and they are what the number counts when there are any: a
// channel with forty unread lines and one that named you are not the same
// event, and the accent is the app's ONE accent, spent on the thing that is
// actually addressed to you. Past ninety-nine the exact number stops being
// information — the row is behind either way — so it becomes "99+" and the
// pill keeps a width the rail can plan around.
func pillFor(c viewmodel.Counts) (text string, fill, ink fyne.ThemeColorName, show bool) {
	switch {
	case c.Highlights > 0:
		return pillCount(c.Highlights), ecotheme.MentionPill, ecotheme.OnAccent, true
	case c.Unread > 0:
		return pillCount(c.Unread), ecotheme.UnreadPill, ecotheme.TextMuted, true
	}
	return "", "", "", false
}

func pillCount(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

func (p *countPill) CreateRenderer() fyne.WidgetRenderer {
	return &countPillRenderer{p: p, objs: []fyne.CanvasObject{p.fill, p.label}}
}

type countPillRenderer struct {
	p    *countPill
	objs []fyne.CanvasObject
}

func (r *countPillRenderer) Destroy()                     {}
func (r *countPillRenderer) Objects() []fyne.CanvasObject { return r.objs }

func (r *countPillRenderer) MinSize() fyne.Size {
	pad := r.p.Theme().Size(theme.SizeNamePadding)
	t := r.p.label.MinSize()
	// a round pill at one digit, a lozenge at three
	w := fyne.Max(t.Width+2*pad, t.Height+2*pad)
	return fyne.NewSize(w, t.Height+pad)
}

func (r *countPillRenderer) Layout(size fyne.Size) {
	r.p.fill.Resize(size)
	r.p.fill.CornerRadius = size.Height / 2
	t := r.p.label.MinSize()
	r.p.label.Resize(t)
	r.p.label.Move(fyne.NewPos((size.Width-t.Width)/2, (size.Height-t.Height)/2))
}

func (r *countPillRenderer) Refresh() {
	th := r.p.Theme()
	v := ecotheme.Variant(th)
	text, fill, ink, show := pillFor(r.p.counts)
	if !show {
		return
	}
	r.p.label.Text = text
	r.p.label.TextSize = th.Size(theme.SizeNameCaptionText)
	r.p.label.Color = th.Color(ink, v)
	r.p.fill.FillColor = th.Color(fill, v)
	r.Layout(r.p.Size())
	r.p.fill.Refresh()
	r.p.label.Refresh()
}
