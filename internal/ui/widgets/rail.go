package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The Rail — the buffer sidebar, blueprint §4.1.
//
// A styled flat widget.List of custom rows: an 8px status/activity dot, the
// name, and a right-aligned pill. It holds no state. Which rows exist, which
// one is current, what the pills count and whether a network is folded are all
// the viewmodel's (internal/ui/viewmodel/rail.go); this file draws that and
// fires intents back at it, and the two are tested apart because only one of
// them needs a canvas.
//
// Two rules are worth stating where the pixels are, because both are easy to
// undo by accident:
//
//   - Clicking an offline network NEVER connects. Dialling a server is
//     [Connect]'s job and no other gesture's, so that a stray click in a rail
//     of pinned networks cannot open five connections.
//   - The ⋯ overflow menu and the right-click menu are ONE list, built once by
//     the app (RailOptions.Menu). That is the Parity Rule made structural: they
//     cannot drift into offering different things, because there is nothing to
//     drift.

// RailOptions is everything the Rail cannot do for itself.
type RailOptions struct {
	// Model is the state the rail draws. A nil one is an empty rail, which is
	// what a test that only wants a widget gets.
	Model *viewmodel.Model

	// Show is the intent a click on a buffer row fires.
	Show func(viewmodel.Ref)

	// Connect dials a network profile. It is wired to [Connect] and to the
	// menu item of the same name, and to nothing else.
	Connect func(profile string)

	// Menu builds the actions for one row — the ⋯ button and the right-click
	// menu both ask for it.
	Menu func(viewmodel.Row) []*fyne.MenuItem

	// Gear opens the app menu, anchored under the identity chip's gear.
	Gear func(anchor fyne.CanvasObject)

	// Away is the identity chip's away toggle: true asks to go away, false to
	// come back.
	Away func(away bool)

	// Folded is called after the user folds or unfolds a network, so the app
	// can remember it across restarts.
	Folded func()
}

// Identity is what the chip at the bottom of the rail says about you.
type Identity struct {
	// Nick is the nick the server has us under.
	Nick string
	// Account is the account we are logged in AS, when we are. Non-empty is
	// the badge.
	Account string
	// Away is our own away state.
	Away bool
}

// Rail is the buffer sidebar.
type Rail struct {
	widget.BaseWidget
	opts RailOptions

	list    *widget.List
	caption *canvas.Text
	capBox  *fyne.Container
	chip    *identityChip
	content *fyne.Container

	m        *measurer
	textSize float32
}

// NewRail builds the sidebar.
func NewRail(o RailOptions) *Rail {
	r := &Rail{opts: o}
	r.caption = canvas.NewText("", color.Transparent)
	r.capBox = container.NewPadded(r.caption)
	r.capBox.Hide()
	r.chip = newIdentityChip(r)

	r.list = widget.NewList(
		func() int { return r.model().RowCount() },
		func() fyne.CanvasObject { return newRailRow(r) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row, ok := r.model().Row(i)
			if !ok {
				return
			}
			o.(*railRow).set(i, row)
		},
	)
	r.list.HideSeparators = true
	r.list.OnSelected = func(i widget.ListItemID) {
		// The model owns which row is current. The List's own selection would
		// be a second answer to that, and the two come apart the moment a fold
		// changes the indices — so the selection is dropped as soon as it has
		// been read, and the current row is painted from the model.
		r.list.UnselectAll()
		if row, ok := r.model().Row(i); ok {
			r.tap(row)
		}
	}

	r.content = container.NewBorder(r.capBox, r.chip, nil, nil, r.list)
	r.ExtendBaseWidget(r)
	return r
}

func (r *Rail) CreateRenderer() fyne.WidgetRenderer {
	r.sync()
	return widget.NewSimpleRenderer(r.content)
}

// model is the state this rail draws; an absent one is an empty model rather
// than a nil check at every call site.
func (r *Rail) model() *viewmodel.Model {
	if r.opts.Model == nil {
		r.opts.Model = viewmodel.New(viewmodel.Hooks{})
	}
	return r.opts.Model
}

// Refresh repaints the rail from the model. It is what the app calls when
// anything the rail shows has moved.
func (r *Rail) Refresh() {
	r.sync()
	r.list.Refresh()
	r.chip.Refresh()
	r.BaseWidget.Refresh()
}

// Select shows the row at an index — the keyboard's path in (Alt+Up/Down,
// Alt+A, Alt+1..9). It goes through the List so the row is scrolled into view.
func (r *Rail) Select(i int) { r.list.Select(i) }

// SetIdentity fills the chip at the bottom: the nick, the logged-in badge and
// the away toggle.
func (r *Rail) SetIdentity(id Identity) {
	if r.chip.id == id {
		return
	}
	r.chip.id = id
	r.chip.Refresh()
}

// Identity is what the chip is currently saying.
func (r *Rail) Identity() Identity { return r.chip.id }

// sync re-reads the things that are not per-row: the caption, and the text
// size everything is measured at.
func (r *Rail) sync() {
	th := r.Theme()
	text := r.model().Caption()
	r.caption.Text = text
	r.caption.TextSize = th.Size(theme.SizeNameCaptionText)
	r.caption.Color = th.Color(ecotheme.TextFaint, ecotheme.Variant(th))
	r.caption.Refresh()
	if text == "" {
		r.capBox.Hide()
	} else {
		r.capBox.Show()
	}
}

// meas is the shared text measurer, rebuilt when the user changes their font
// size. Rail rows are cheap to measure and there are few of them, but they are
// measured on every repaint of a list that repaints on every arriving message,
// and the cache is what keeps that free.
func (r *Rail) meas() *measurer {
	size := r.Theme().Size(theme.SizeNameText)
	if r.m == nil || size != r.textSize {
		r.textSize, r.m = size, newMeasurer(size)
	}
	return r.m
}

// rowHeight is one rail row: a line of text with air above and below it.
func (r *Rail) rowHeight() float32 {
	return r.meas().lineHeight(fyne.TextStyle{}) + 2*r.Theme().Size(theme.SizeNamePadding)
}

// tap is what a click on a row means.
func (r *Rail) tap(row viewmodel.Row) {
	switch {
	case row.Selectable():
		if r.opts.Show != nil {
			r.opts.Show(row.Ref)
		}
	case row.IsNetwork() && row.Live:
		// the whole header is the chevron's target; a bigger one than the
		// chevron itself, for the same result
		r.fold(row.Profile)
	}
	// An OFFLINE network does nothing at all here. See the file comment.
}

// fold flips a network's chevron and tells the app, which remembers it.
func (r *Rail) fold(profile string) {
	r.model().ToggleCollapsed(profile)
	r.Refresh()
	if r.opts.Folded != nil {
		r.opts.Folded()
	}
}

// menu pops up one row's actions at a position on screen.
func (r *Rail) menu(row viewmodel.Row, at fyne.Position) {
	if r.opts.Menu == nil {
		return
	}
	items := r.opts.Menu(row)
	if len(items) == 0 {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(r)
	if c == nil {
		return
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), c, at)
}

// --- one row --------------------------------------------------------------------

// dotSize is §4.1's 8px status/activity dot.
const dotSize = 8

// indentStep is how far a buffer row sits under its network header. It is only
// ever applied when there IS a header (Row.Depth), so a single-network rail
// starts at the margin.
const indentStep = 10

// railRow draws every kind of rail row: a network header, live or offline, and
// a buffer under one. One widget rather than three because widget.List builds
// its rows from a single template and pours every id through it.
type railRow struct {
	widget.BaseWidget
	rail *Rail
	row  viewmodel.Row
	id   int

	fill    *canvas.Rectangle // the current/hover wash
	dot     *canvas.Circle
	name    *canvas.Text
	chevron *tapIcon
	pill    *countPill
	more    *tapIcon
	connect *textPill

	hovered bool
}

func newRailRow(rail *Rail) *railRow {
	r := &railRow{rail: rail}
	r.fill = canvas.NewRectangle(color.Transparent)
	r.dot = canvas.NewCircle(color.Transparent)
	r.name = canvas.NewText("", color.Transparent)
	r.pill = newCountPill()
	r.chevron = newTapIcon(theme.MenuDropDownIcon(), func(fyne.Position) { r.rail.fold(r.row.Profile) })
	r.more = newTapIcon(
		theme.NewColoredResource(theme.MoreHorizontalIcon(), ecotheme.TextMuted),
		func(at fyne.Position) { r.rail.menu(r.row, at) })
	r.connect = newTextPill("Connect", func() {
		if r.rail.opts.Connect != nil {
			r.rail.opts.Connect(r.row.Profile)
		}
	})
	r.chevron.Hide()
	r.more.Hide()
	r.connect.Hide()
	r.ExtendBaseWidget(r)
	return r
}

func (r *railRow) CreateRenderer() fyne.WidgetRenderer {
	return &railRowRenderer{r: r, objs: []fyne.CanvasObject{
		r.fill, r.dot, r.name, r.chevron, r.pill, r.connect, r.more,
	}}
}

func (r *railRow) MinSize() fyne.Size {
	return fyne.NewSize(dotSize+4*indentStep, r.rail.rowHeight())
}

// set pours one row of the model through this widget.
func (r *railRow) set(id int, row viewmodel.Row) {
	r.id, r.row, r.hovered = id, row, false
	th := r.Theme()
	v := ecotheme.Variant(th)
	col := func(n fyne.ThemeColorName) color.Color { return th.Color(n, v) }

	r.name.TextSize = th.Size(theme.SizeNameText)
	r.name.TextStyle = fyne.TextStyle{}
	r.chevron.Hide()
	r.more.Hide()
	r.connect.Hide()
	r.dot.FillColor = color.Transparent

	switch {
	case row.IsNetwork():
		r.name.TextStyle = fyne.TextStyle{Bold: true}
		r.more.Show()
		if row.Live {
			r.name.Color = col(ecotheme.TextMuted)
			r.dot.FillColor = col(ecotheme.Success)
			r.chevron.icon = theme.MenuDropDownIcon()
			if row.Collapsed {
				r.chevron.icon = theme.MenuExpandIcon()
			}
			r.chevron.Show()
			break
		}
		// offline: dimmed, and the only way in is the button
		r.name.Color = col(ecotheme.TextFaint)
		r.dot.FillColor = col(ecotheme.Danger)
		r.connect.Show()

	default:
		// A row worth reading is worth reading: the buffer on screen and the
		// ones with something in them are the only ones drawn at full
		// strength, so the rail is quiet until it has something to say.
		if row.Current || row.Counts.Any() {
			r.name.Color = col(ecotheme.TextPrimary)
		} else {
			r.name.Color = col(ecotheme.TextMuted)
		}
		switch {
		case row.Counts.Highlights > 0:
			r.dot.FillColor = col(ecotheme.Accent)
		case row.Counts.Unread > 0:
			r.dot.FillColor = col(ecotheme.TextMuted)
		}
	}

	r.pill.set(row.Counts)
	r.paintFill()
	r.Refresh()
}

// paintFill decides the row's background: the viewer's row is washed with the
// selection colour, a hovered one with the hover overlay, and everything else
// is the rail's own surface showing through.
func (r *railRow) paintFill() {
	th := r.Theme()
	v := ecotheme.Variant(th)
	switch {
	case r.row.Current:
		r.fill.FillColor = th.Color(ecotheme.SelectionOverlay, v)
	case r.hovered:
		r.fill.FillColor = th.Color(ecotheme.HoverOverlay, v)
	default:
		r.fill.FillColor = color.Transparent
	}
}

func (r *railRow) Tapped(*fyne.PointEvent) {
	// Selection is the List's to make (it scrolls, and it is the same path the
	// keyboard uses); implementing Tapped at all is what keeps the row from
	// swallowing the click on its way there, because a widget that answers
	// TappedSecondary answers Tapped too.
	r.rail.Select(r.id)
}

// TappedSecondary is the other half of the Parity Rule: right-click offers
// exactly what the ⋯ button offers, because it asks the same function for it.
func (r *railRow) TappedSecondary(e *fyne.PointEvent) { r.rail.menu(r.row, e.AbsolutePosition) }

func (r *railRow) MouseIn(*desktop.MouseEvent) {
	r.hovered = true
	r.paintFill()
	r.fill.Refresh()
}

func (r *railRow) MouseMoved(*desktop.MouseEvent) {}

func (r *railRow) MouseOut() {
	if !r.hovered {
		return
	}
	r.hovered = false
	r.paintFill()
	r.fill.Refresh()
}

type railRowRenderer struct {
	r    *railRow
	objs []fyne.CanvasObject
}

func (rr *railRowRenderer) Destroy()                     {}
func (rr *railRowRenderer) Objects() []fyne.CanvasObject { return rr.objs }
func (rr *railRowRenderer) MinSize() fyne.Size           { return rr.r.MinSize() }

func (rr *railRowRenderer) Refresh() {
	for _, o := range rr.objs {
		o.Refresh()
	}
	rr.Layout(rr.r.Size())
	canvas.Refresh(rr.r)
}

// Layout places the row: indent, chevron, dot, name, then everything that is
// right-aligned, laid out from the right edge inwards. The name takes what is
// left and is elided into it, because a canvas.Text that does not fit draws
// over its neighbours rather than stopping.
func (rr *railRowRenderer) Layout(size fyne.Size) {
	r := rr.r
	m := r.rail.meas()
	th := r.Theme()
	pad := th.Size(theme.SizeNamePadding)
	icon := m.lineHeight(fyne.TextStyle{})

	r.fill.Resize(size)
	r.fill.Move(fyne.NewPos(0, 0))

	mid := func(h float32) float32 { return (size.Height - h) / 2 }
	place := func(o fyne.CanvasObject, x float32, s fyne.Size) {
		o.Resize(s)
		o.Move(fyne.NewPos(x, mid(s.Height)))
	}

	x := pad + float32(r.row.Depth)*indentStep
	if r.chevron.Visible() {
		place(r.chevron, x, fyne.NewSize(icon, icon))
		x += icon
	}
	place(r.dot, x, fyne.NewSize(dotSize, dotSize))
	x += dotSize + pad

	// everything on the right, laid out from the edge inwards
	right := size.Width - pad
	if r.more.Visible() {
		right -= icon
		place(r.more, right, fyne.NewSize(icon, icon))
		right -= pad
	}
	for _, o := range []fyne.CanvasObject{r.connect, r.pill} {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		right -= s.Width
		place(o, right, s)
		right -= pad
	}

	r.name.Text = m.elide(r.nameText(), r.name.TextStyle, right-x)
	ns := r.name.MinSize()
	place(r.name, x, ns)
}

// nameText is what this row is called, before eliding: the network's profile
// name on a header, the buffer's display name on everything else.
func (r *railRow) nameText() string {
	if r.row.IsNetwork() {
		return r.row.Profile
	}
	return r.row.Ref.Name
}

// --- the identity chip -----------------------------------------------------------

// identityChip is the bottom of the rail (§4.1): who you are on this network,
// whether the server knows it, the away toggle, and the gear.
type identityChip struct {
	widget.BaseWidget
	rail *Rail
	id   Identity

	nick  *canvas.Text
	badge *canvas.Image
	away  *textPill
	gear  *tapIcon
}

func newIdentityChip(rail *Rail) *identityChip {
	c := &identityChip{rail: rail}
	c.nick = canvas.NewText("", color.Transparent)
	c.nick.TextStyle = fyne.TextStyle{Bold: true}
	c.badge = canvas.NewImageFromResource(
		theme.NewColoredResource(theme.ConfirmIcon(), ecotheme.Accent))
	c.badge.FillMode = canvas.ImageFillContain
	c.badge.Hide()
	c.away = newTextPill("Away", func() {
		if c.rail.opts.Away != nil {
			c.rail.opts.Away(!c.id.Away)
		}
	})
	c.gear = newTapIcon(
		theme.NewColoredResource(theme.SettingsIcon(), ecotheme.TextMuted),
		func(fyne.Position) {
			if c.rail.opts.Gear != nil {
				c.rail.opts.Gear(c.gear)
			}
		})
	c.ExtendBaseWidget(c)
	return c
}

func (c *identityChip) CreateRenderer() fyne.WidgetRenderer {
	return &chipRenderer{c: c, objs: []fyne.CanvasObject{c.nick, c.badge, c.away, c.gear}}
}

// MinSize is deliberately narrow: the chip must not be what stops the user
// dragging the rail thin. The nick elides instead.
func (c *identityChip) MinSize() fyne.Size {
	return fyne.NewSize(6*indentStep, c.rail.rowHeight())
}

func (c *identityChip) Refresh() {
	th := c.Theme()
	v := ecotheme.Variant(th)
	c.nick.Text = c.id.Nick
	c.nick.TextSize = th.Size(theme.SizeNameText)
	c.nick.Color = th.Color(ecotheme.TextPrimary, v)
	if c.id.Away {
		c.nick.Color = th.Color(ecotheme.TextFaint, v)
	}
	if c.id.Account != "" {
		c.badge.Show()
	} else {
		c.badge.Hide()
	}
	c.away.on = c.id.Away
	c.BaseWidget.Refresh()
}

type chipRenderer struct {
	c    *identityChip
	objs []fyne.CanvasObject
}

func (cr *chipRenderer) Destroy()                     {}
func (cr *chipRenderer) Objects() []fyne.CanvasObject { return cr.objs }
func (cr *chipRenderer) MinSize() fyne.Size           { return cr.c.MinSize() }

func (cr *chipRenderer) Refresh() {
	for _, o := range cr.objs {
		o.Refresh()
	}
	cr.Layout(cr.c.Size())
	canvas.Refresh(cr.c)
}

func (cr *chipRenderer) Layout(size fyne.Size) {
	c := cr.c
	m := c.rail.meas()
	th := c.Theme()
	pad := th.Size(theme.SizeNamePadding)
	icon := m.lineHeight(fyne.TextStyle{})
	mid := func(h float32) float32 { return (size.Height - h) / 2 }

	right := size.Width - pad
	gs := fyne.NewSize(icon, icon)
	right -= gs.Width
	c.gear.Resize(gs)
	c.gear.Move(fyne.NewPos(right, mid(gs.Height)))
	right -= pad

	as := c.away.MinSize()
	right -= as.Width
	c.away.Resize(as)
	c.away.Move(fyne.NewPos(right, mid(as.Height)))
	right -= pad

	if c.badge.Visible() {
		bs := fyne.NewSize(icon*0.7, icon*0.7)
		right -= bs.Width
		c.badge.Resize(bs)
		c.badge.Move(fyne.NewPos(right, mid(bs.Height)))
		right -= pad
	}

	c.nick.Text = m.elide(c.id.Nick, c.nick.TextStyle, right-pad)
	ns := c.nick.MinSize()
	c.nick.Resize(ns)
	c.nick.Move(fyne.NewPos(pad, mid(ns.Height)))
}

var (
	_ fyne.Tappable          = (*railRow)(nil)
	_ fyne.SecondaryTappable = (*railRow)(nil)
	_ desktop.Hoverable      = (*railRow)(nil)
)
