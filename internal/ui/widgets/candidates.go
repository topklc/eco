package widgets

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// Candidates is the visible completion popup §4.3 asks for: the row of things
// Tab is cycling through, with the current one lit, in a single line above the
// composer.
//
// It is a strip and not a floating list on purpose. A popup over the
// conversation would cover the messages someone is completing a nick FROM, and
// a dropdown tall enough to be worth floating is a second list to navigate with
// its own keys. One line says everything the cycle can say - what is selected,
// what is next, and how many there are - and it costs the layout nothing when
// there is no cycle running, because it is hidden.
//
// It draws and decides nothing else: the composer owns the cycle (which
// candidates, which index), and hands them here.
type Candidates struct {
	widget.BaseWidget

	items []string
	sel   int

	texts []*canvas.Text
	more  *canvas.Text
	objs  []fyne.CanvasObject
}

// maxCandidatesShown is how many fit before the strip stops naming them and
// starts counting them.
const maxCandidatesShown = 8

// NewCandidates returns an empty, hidden strip.
func NewCandidates() *Candidates {
	c := &Candidates{}
	c.more = canvas.NewText("", nil)
	c.ExtendBaseWidget(c)
	c.Hide()
	return c
}

// Set shows a cycle: the candidates and which one is current. An empty list
// hides the strip, so the composer clearing its cycle is all it takes.
func (c *Candidates) Set(items []string, sel int) {
	if len(items) == 0 {
		c.Clear()
		return
	}
	c.items = items
	c.sel = sel
	c.rebuild()
	c.Show()
	c.Refresh()
}

// Clear ends the cycle on screen.
func (c *Candidates) Clear() {
	if len(c.items) == 0 && !c.Visible() {
		return
	}
	c.items = nil
	c.sel = 0
	c.rebuild()
	c.Hide()
}

// Items returns what is on the strip; Selected returns which one is lit. Both
// exist for the tests and for the composer's own assertions - nothing else
// reads them.
func (c *Candidates) Items() []string { return c.items }

// Selected is the index of the lit candidate.
func (c *Candidates) Selected() int { return c.sel }

// window returns the slice of candidates on screen, and how many are hidden.
// It keeps the selected one visible: cycling past the eighth candidate must
// move the window, not run the highlight off the end of it.
func (c *Candidates) window() (items []string, from, hidden int) {
	if len(c.items) <= maxCandidatesShown {
		return c.items, 0, 0
	}
	from = c.sel - maxCandidatesShown + 1
	if from < 0 {
		from = 0
	}
	if from > len(c.items)-maxCandidatesShown {
		from = len(c.items) - maxCandidatesShown
	}
	return c.items[from : from+maxCandidatesShown], from, len(c.items) - maxCandidatesShown
}

func (c *Candidates) rebuild() {
	shown, _, hidden := c.window()
	c.texts = c.texts[:0]
	c.objs = c.objs[:0]
	for _, it := range shown {
		t := canvas.NewText(it, nil)
		c.texts = append(c.texts, t)
		c.objs = append(c.objs, t)
	}
	c.more.Text = ""
	if hidden > 0 {
		c.more.Text = "+" + strconv.Itoa(hidden)
	}
	c.objs = append(c.objs, c.more)
}

func (c *Candidates) CreateRenderer() fyne.WidgetRenderer {
	c.rebuild()
	return &candidatesRenderer{c: c}
}

type candidatesRenderer struct{ c *Candidates }

func (r *candidatesRenderer) Destroy() {}

func (r *candidatesRenderer) Objects() []fyne.CanvasObject { return r.c.objs }

func (r *candidatesRenderer) MinSize() fyne.Size {
	if len(r.c.items) == 0 {
		return fyne.Size{}
	}
	pad := r.c.Theme().Size(theme.SizeNamePadding)
	w, h := float32(0), float32(0)
	for _, t := range r.c.texts {
		s := t.MinSize()
		w += s.Width + pad*2
		h = max32(h, s.Height)
	}
	if r.c.more.Text != "" {
		s := r.c.more.MinSize()
		w += s.Width + pad*2
		h = max32(h, s.Height)
	}
	return fyne.NewSize(w, h)
}

func (r *candidatesRenderer) Layout(fyne.Size) {
	pad := r.c.Theme().Size(theme.SizeNamePadding)
	x := pad
	for _, t := range r.c.texts {
		s := t.MinSize()
		t.Resize(s)
		t.Move(fyne.NewPos(x, 0))
		x += s.Width + pad*2
	}
	if r.c.more.Text != "" {
		s := r.c.more.MinSize()
		r.c.more.Resize(s)
		r.c.more.Move(fyne.NewPos(x, 0))
	}
}

func (r *candidatesRenderer) Refresh() {
	th := r.c.Theme()
	v := ecotheme.Variant(th)
	size := th.Size(theme.SizeNameText)
	_, from, _ := r.c.window()
	for i, t := range r.c.texts {
		t.TextSize = size
		// One accent, and it is the selection. Everything else is the faint
		// text token, which is what a hint is drawn in everywhere else.
		if from+i == r.c.sel {
			t.Color = th.Color(ecotheme.AccentText, v)
			t.TextStyle = fyne.TextStyle{Bold: true}
		} else {
			t.Color = th.Color(ecotheme.TextFaint, v)
			t.TextStyle = fyne.TextStyle{}
		}
		t.Refresh()
	}
	r.c.more.TextSize = size
	r.c.more.Color = th.Color(ecotheme.TextFaint, v)
	r.c.more.Refresh()
	r.Layout(r.c.Size())
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
