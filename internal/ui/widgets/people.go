package widgets

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The People pane - blueprint §4.1: the member list, with a filter box,
// sectioned by prefix rank, away-dimmed, registered-badged, and a hover kebab
// carrying the op actions.
//
// Like the rail, it draws a row model it does not own: the sections, the
// filtering and the per-buffer visibility are the viewmodel's
// (internal/ui/viewmodel/people.go), which is why a three-thousand-nick
// channel can be tested for correctness in a table and for speed in a budget,
// neither of which needs this file.

// PeopleOptions is everything the pane cannot do for itself.
type PeopleOptions struct {
	// Model is the state the pane draws.
	Model *viewmodel.Model

	// Open is what clicking a nick does: open the conversation with them.
	Open func(nick string)

	// Menu builds one member's actions. The hover kebab and the right-click
	// menu both ask for it - the Parity Rule, structurally: one list, so they
	// cannot offer different things.
	Menu func(nick string) []*fyne.MenuItem

	// Ignored reports whether a nick is on the ignore list. Ignored people
	// stay listed - otherwise the only way to un-ignore someone is to
	// remember their name - and are drawn as what they are.
	Ignored func(nick string) bool
}

// People is the member pane.
type People struct {
	widget.BaseWidget
	opts PeopleOptions

	filter  *widget.Entry
	list    *widget.List
	content *fyne.Container

	syncing  bool // the filter box is being written to, not typed in
	m        *measurer
	textSize float32
}

// NewPeople builds the member pane.
func NewPeople(o PeopleOptions) *People {
	p := &People{opts: o}
	p.filter = widget.NewEntry()
	p.filter.SetPlaceHolder("Filter")
	p.filter.OnChanged = func(s string) {
		if p.syncing {
			return
		}
		p.model().SetPeopleFilter(s)
		p.list.Refresh()
	}

	p.list = widget.NewList(
		func() int { return p.model().PeopleRowCount() },
		func() fyne.CanvasObject { return newPeopleRow(p) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row, ok := p.model().PeopleRow(i)
			if !ok {
				return
			}
			o.(*peopleRow).set(row)
		},
	)
	p.list.HideSeparators = true

	p.content = container.NewBorder(container.NewPadded(p.filter), nil, nil, nil, p.list)
	p.ExtendBaseWidget(p)
	return p
}

func (p *People) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}

func (p *People) model() *viewmodel.Model {
	if p.opts.Model == nil {
		p.opts.Model = viewmodel.New(viewmodel.Hooks{})
	}
	return p.opts.Model
}

// Refresh repaints the roster and puts the filter box back in step with the
// model - which is how a buffer switch clears it, since the filter belongs to
// the buffer it was typed in.
func (p *People) Refresh() {
	if want := p.model().PeopleFilter(); want != p.filter.Text {
		p.syncing = true
		p.filter.SetText(want)
		p.syncing = false
	}
	p.list.Refresh()
	p.BaseWidget.Refresh()
}

// meas is the shared text measurer; see Rail.meas. Here it matters more: this
// is the pane that has to stay responsive at three thousand nicks.
func (p *People) meas() *measurer {
	size := p.Theme().Size(theme.SizeNameText)
	if p.m == nil || size != p.textSize {
		p.textSize, p.m = size, newMeasurer(size)
	}
	return p.m
}

func (p *People) rowHeight() float32 {
	return p.meas().lineHeight(fyne.TextStyle{}) + 2*p.Theme().Size(theme.SizeNamePadding)
}

// ignored asks the app whether a nick is on the ignore list; nobody is when
// nothing was wired.
func (p *People) ignored(nick string) bool {
	return p.opts.Ignored != nil && p.opts.Ignored(nick)
}

// menu pops up one member's actions.
func (p *People) menu(nick string, at fyne.Position) {
	if p.opts.Menu == nil || nick == "" {
		return
	}
	items := p.opts.Menu(nick)
	if len(items) == 0 {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(p)
	if c == nil {
		return
	}
	ShowMenuAtPosition(fyne.NewMenu("", items...), c, at)
}

// --- one row ----------------------------------------------------------------------

// peopleRow draws a section heading or a member.
type peopleRow struct {
	widget.BaseWidget
	people *People
	row    viewmodel.PeopleRow

	fill  *canvas.Rectangle
	dot   *canvas.Circle
	name  *canvas.Text
	count *canvas.Text
	kebab *tapIcon

	hovered bool
}

func newPeopleRow(p *People) *peopleRow {
	r := &peopleRow{people: p}
	r.fill = canvas.NewRectangle(color.Transparent)
	r.dot = canvas.NewCircle(color.Transparent)
	r.name = canvas.NewText("", color.Transparent)
	r.count = canvas.NewText("", color.Transparent)
	r.kebab = newTapIcon(
		theme.NewColoredResource(theme.MoreVerticalIcon(), ecotheme.TextMuted),
		func(at fyne.Position) { r.people.menu(r.row.Member.Nick, at) })
	r.kebab.Hide()
	r.ExtendBaseWidget(r)
	return r
}

func (r *peopleRow) CreateRenderer() fyne.WidgetRenderer {
	return &peopleRowRenderer{r: r, objs: []fyne.CanvasObject{
		r.fill, r.dot, r.name, r.count, r.kebab,
	}}
}

func (r *peopleRow) MinSize() fyne.Size {
	return fyne.NewSize(dotSize+4*indentStep, r.people.rowHeight())
}

// set pours one row of the model through this widget.
func (r *peopleRow) set(row viewmodel.PeopleRow) {
	r.row, r.hovered = row, false
	th := r.Theme()
	v := ecotheme.Variant(th)
	col := func(n fyne.ThemeColorName) color.Color { return th.Color(n, v) }

	r.kebab.Hide()
	r.count.Text = ""
	r.dot.FillColor = color.Transparent
	r.fill.FillColor = color.Transparent

	if row.Kind == viewmodel.PeopleSection {
		r.name.Text = row.Title
		r.name.TextSize = th.Size(theme.SizeNameCaptionText)
		r.name.TextStyle = fyne.TextStyle{Bold: true}
		r.name.Color = col(ecotheme.TextFaint)
		r.count.Text = strconv.Itoa(row.Count)
		r.count.TextSize = th.Size(theme.SizeNameCaptionText)
		r.count.Color = col(ecotheme.TextFaint)
		r.Refresh()
		return
	}

	r.name.Text = row.Member.Nick
	r.name.TextSize = th.Size(theme.SizeNameText)
	r.name.TextStyle = fyne.TextStyle{}
	// Away-notify is the whole of this: someone who has said they are not
	// there is drawn as not there, and nothing else changes. Someone IGNORED
	// is dimmed too, and italic, so the two states are not one look.
	switch {
	case r.people.ignored(row.Member.Nick):
		r.name.Color = col(ecotheme.TextFaint)
		r.name.TextStyle = fyne.TextStyle{Italic: true}
	case row.Member.Away:
		r.name.Color = col(ecotheme.TextFaint)
	default:
		r.name.Color = col(ecotheme.TextPrimary)
	}
	// account-tag: the badge says the server knows who they are. It is a dot
	// and not a word because it is true of most people in a modern channel.
	if row.Member.Account != "" {
		r.dot.FillColor = col(ecotheme.TextMuted)
	}
	r.Refresh()
}

func (r *peopleRow) isMember() bool {
	return r.row.Kind == viewmodel.PeopleMember && r.row.Member.Nick != ""
}

func (r *peopleRow) Tapped(*fyne.PointEvent) {
	if !r.isMember() || r.people.opts.Open == nil {
		return
	}
	r.people.opts.Open(r.row.Member.Nick)
}

func (r *peopleRow) TappedSecondary(e *fyne.PointEvent) {
	if !r.isMember() {
		return
	}
	r.people.menu(r.row.Member.Nick, e.AbsolutePosition)
}

func (r *peopleRow) MouseIn(*desktop.MouseEvent) {
	if !r.isMember() {
		return
	}
	th := r.Theme()
	r.hovered = true
	r.fill.FillColor = th.Color(ecotheme.HoverOverlay, ecotheme.Variant(th))
	r.kebab.Show()
	r.Refresh()
}

func (r *peopleRow) MouseMoved(*desktop.MouseEvent) {}

func (r *peopleRow) MouseOut() {
	if !r.hovered {
		return
	}
	r.hovered = false
	r.fill.FillColor = color.Transparent
	r.kebab.Hide()
	r.Refresh()
}

type peopleRowRenderer struct {
	r    *peopleRow
	objs []fyne.CanvasObject
}

func (pr *peopleRowRenderer) Destroy()                     {}
func (pr *peopleRowRenderer) Objects() []fyne.CanvasObject { return pr.objs }
func (pr *peopleRowRenderer) MinSize() fyne.Size           { return pr.r.MinSize() }

func (pr *peopleRowRenderer) Refresh() {
	for _, o := range pr.objs {
		o.Refresh()
	}
	pr.Layout(pr.r.Size())
	canvas.Refresh(pr.r)
}

func (pr *peopleRowRenderer) Layout(size fyne.Size) {
	r := pr.r
	m := r.people.meas()
	th := r.Theme()
	pad := th.Size(theme.SizeNamePadding)
	icon := m.lineHeight(fyne.TextStyle{})
	mid := func(h float32) float32 { return (size.Height - h) / 2 }
	place := func(o fyne.CanvasObject, x float32, s fyne.Size) {
		o.Resize(s)
		o.Move(fyne.NewPos(x, mid(s.Height)))
	}

	r.fill.Resize(size)
	r.fill.Move(fyne.NewPos(0, 0))

	x := pad
	right := size.Width - pad
	if r.row.Kind == viewmodel.PeopleMember {
		place(r.dot, x, fyne.NewSize(dotSize, dotSize))
		x += dotSize + pad
		if r.kebab.Visible() {
			right -= icon
			place(r.kebab, right, fyne.NewSize(icon, icon))
			right -= pad
		}
	} else if r.count.Text != "" {
		cs := r.count.MinSize()
		right -= cs.Width
		place(r.count, right, cs)
		right -= pad
	}

	r.name.Text = m.elide(r.nameText(), r.name.TextStyle, right-x)
	ns := r.name.MinSize()
	place(r.name, x, ns)
}

// nameText is the untruncated text of this row.
func (r *peopleRow) nameText() string {
	if r.row.Kind == viewmodel.PeopleSection {
		return r.row.Title
	}
	return r.row.Member.Nick
}

var (
	_ fyne.Tappable          = (*peopleRow)(nil)
	_ fyne.SecondaryTappable = (*peopleRow)(nil)
	_ desktop.Hoverable      = (*peopleRow)(nil)
)
