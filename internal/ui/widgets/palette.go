package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// Palette is the shape both finding overlays wear - blueprint §4.1's Switcher
// (Ctrl+K) and Finder (Ctrl+Shift+F): a rounded panel on the surface token, a
// text field, and a list. That is the whole of it. There is no animation, no
// icon column, no second pane and no preview, because none of those help you
// find a room faster than reading the list does.
//
// It is ONE widget rather than two nearly identical ones for the same reason
// the rail's ⋯ and its right-click menu are one list: two copies of a surface
// drift, and the drift is always the half nobody is looking at.
//
// It holds no rows of its own. The host answers Rows() when asked and is told
// what was chosen; what the rows MEAN - a buffer, a channel to join, a message
// in the archive - is the host's business entirely.
type Palette struct {
	widget.BaseWidget
	opts PaletteOptions

	entry   *paletteEntry
	list    *widget.List
	note    *widget.Label
	content fyne.CanvasObject

	rows []PaletteRow
	sel  int
	// moving guards the programmatic list.Select calls. widget.List reports a
	// keyboard move and a mouse click through the same callback, and only one
	// of the two means "take this one" - so the arrows raise this while they
	// move, and an OnSelected outside it is a click.
	moving bool
}

// PaletteRow is one line: a name, something about it, and where it is from.
type PaletteRow struct {
	// Title is the name, drawn at full strength.
	Title string
	// Detail is the sentence beside it - a topic, a message snippet - muted
	// and elided at whatever width is left.
	Detail string
	// Meta is the right-hand note: the network, a timestamp, a member count.
	Meta string
	// Accent draws the title in the accent. It is the "Create #name" row and
	// nothing else: one row in the list is an action rather than a place.
	Accent bool
}

// PaletteOptions is everything the panel cannot answer for itself.
type PaletteOptions struct {
	// Placeholder is the empty field's prompt - the one sentence that says
	// what typing here does.
	Placeholder string
	// Rows is asked for the list whenever it is repainted. It is a function
	// rather than a slice so the host can rank against the live query without
	// this widget owning either.
	Rows func() []PaletteRow
	// Changed fires on every keystroke.
	Changed func(query string)
	// Chosen fires on Enter, or a click, with the row's index.
	Chosen func(i int)
	// Cancel fires on Escape. The host closes the overlay and restores focus
	// to the composer through the one helper that does that (§4.1).
	Cancel func()
	// Shortcut is offered every modifier combo that reaches the field, and
	// reports whether it claimed it. Focus lives here while the panel is open,
	// so without it the app's own shortcuts - quit, the pane toggles, the other
	// finding surface - would be swallowed by a text box (§4.3: app shortcuts
	// pass through wherever focus happens to be).
	Shortcut func(fyne.Shortcut) bool
	// Empty is what the panel says when the query matched nothing.
	Empty string
}

// NewPalette builds the panel.
func NewPalette(o PaletteOptions) *Palette {
	p := &Palette{opts: o}
	p.entry = newPaletteEntry(p)
	p.entry.SetPlaceHolder(o.Placeholder)

	p.list = widget.NewList(
		func() int { return len(p.rows) },
		func() fyne.CanvasObject { return newPaletteRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(p.rows) {
				return
			}
			o.(*paletteRow).set(p.rows[i])
		},
	)
	p.list.HideSeparators = true
	p.list.OnSelected = func(i widget.ListItemID) {
		p.sel = i
		if !p.moving {
			p.Choose() // a click, which means the same as Enter on that row
		}
	}

	p.note = widget.NewLabel("")
	p.note.TextStyle = fyne.TextStyle{Italic: true}
	p.note.Hide()

	p.content = newPanel(container.NewBorder(
		container.NewPadded(p.entry), p.note, nil, nil, p.list))
	p.ExtendBaseWidget(p)
	return p
}

func (p *Palette) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}

// Entry is the text field, which is what the host focuses when the overlay
// opens: the keyboard belongs to the query, and every key the panel cares
// about is handled there (paletteEntry.TypedKey).
func (p *Palette) Entry() fyne.Focusable { return p.entry }

// Query is what has been typed.
func (p *Palette) Query() string { return p.entry.Text }

// SetQuery seeds the field - the Finder opens on the word under the cursor,
// and Ctrl+F's bar remembers what was last looked for.
func (p *Palette) SetQuery(s string) {
	p.entry.SetText(s)
	p.entry.CursorColumn = len([]rune(s))
}

// Reset empties the field and reloads. It is what opening the overlay does, so
// a palette never opens showing the answer to a question somebody asked
// yesterday.
func (p *Palette) Reset() {
	p.entry.SetText("")
	p.Reload()
}

// Reload re-asks the host for the rows and repaints, keeping the selection on
// the first row - which is the one Enter takes, so it must always be visible
// and must never be a row that has scrolled out from under the answer.
func (p *Palette) Reload() {
	if p.opts.Rows != nil {
		p.rows = p.opts.Rows()
	}
	p.sel = 0
	p.list.Refresh()
	p.moving = true
	if len(p.rows) > 0 {
		p.list.Select(0)
		p.list.ScrollToTop()
	} else {
		p.list.UnselectAll()
	}
	p.moving = false
	if len(p.rows) == 0 && p.opts.Empty != "" {
		p.note.SetText(p.opts.Empty)
		p.note.Show()
	} else {
		p.note.Hide()
	}
	p.Refresh()
}

// SetNote writes the line under the list - a count, or why there is nothing to
// show. An empty string takes it away.
func (p *Palette) SetNote(s string) {
	if s == "" {
		p.note.Hide()
		return
	}
	p.note.SetText(s)
	p.note.Show()
}

// Rows is what the panel is currently showing. Tests read it; so does the host,
// to find out what Enter just chose.
func (p *Palette) Rows() []PaletteRow { return p.rows }

// Selected is the highlighted row's index, or -1 when the list is empty.
func (p *Palette) Selected() int {
	if len(p.rows) == 0 {
		return -1
	}
	if p.sel < 0 || p.sel >= len(p.rows) {
		return 0
	}
	return p.sel
}

// Step moves the selection, clamping at both ends. It deliberately does not
// wrap: a list ordered best-first has a top, and arriving back at it by holding
// Down is disorienting in a way a rail's buffer cycle is not.
func (p *Palette) Step(d int) {
	if len(p.rows) == 0 {
		return
	}
	next := p.Selected() + d
	if next < 0 {
		next = 0
	}
	if next >= len(p.rows) {
		next = len(p.rows) - 1
	}
	p.sel = next
	p.moving = true
	p.list.Select(next)
	p.moving = false
	p.list.ScrollTo(next)
}

// Choose is Enter: hand the host the selected row.
func (p *Palette) Choose() {
	if i := p.Selected(); i >= 0 && p.opts.Chosen != nil {
		p.opts.Chosen(i)
	}
}

// Cancel is Escape.
func (p *Palette) Cancel() {
	if p.opts.Cancel != nil {
		p.opts.Cancel()
	}
}

// --- the field ---------------------------------------------------------------

// paletteEntry is the query field. Everything the panel does with the keyboard
// happens here, because this is where focus is: the arrows move the selection
// rather than the cursor, Enter takes the highlighted row, and Escape closes.
// Every other key is the Entry's.
type paletteEntry struct {
	widget.Entry
	p *Palette
}

func newPaletteEntry(p *Palette) *paletteEntry {
	e := &paletteEntry{p: p}
	e.ExtendBaseWidget(e)
	e.OnChanged = func(s string) {
		if e.p.opts.Changed != nil {
			e.p.opts.Changed(s)
		}
		e.p.Reload()
	}
	return e
}

func (e *paletteEntry) TypedShortcut(s fyne.Shortcut) {
	if e.p.opts.Shortcut != nil && e.p.opts.Shortcut(s) {
		return
	}
	e.Entry.TypedShortcut(s)
}

func (e *paletteEntry) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyDown:
		e.p.Step(1)
	case fyne.KeyUp:
		e.p.Step(-1)
	case fyne.KeyPageDown:
		e.p.Step(palettePage)
	case fyne.KeyPageUp:
		e.p.Step(-palettePage)
	case fyne.KeyReturn, fyne.KeyEnter:
		e.p.Choose()
	case fyne.KeyEscape:
		e.p.Cancel()
	default:
		e.Entry.TypedKey(k)
	}
}

// palettePage is how far PageUp and PageDown jump: about a screenful of rows.
const palettePage = 8

// --- one row -----------------------------------------------------------------

// paletteRow draws a name, a sentence and a note, in that order across the row.
// It is three labels in a border layout - browser.go's shape, which was the
// right shape - rather than a custom renderer, because a palette shows a few
// dozen rows and not three thousand.
type paletteRow struct {
	widget.BaseWidget
	title  *widget.Label
	detail *widget.Label
	meta   *widget.Label
	box    *fyne.Container
}

func newPaletteRow() *paletteRow {
	r := &paletteRow{}
	r.title = widget.NewLabel("")
	r.title.TextStyle = fyne.TextStyle{Bold: true}
	r.detail = widget.NewLabel("")
	r.detail.Truncation = fyne.TextTruncateEllipsis
	r.meta = widget.NewLabel("")
	r.box = container.NewBorder(nil, nil, r.title, r.meta, r.detail)
	r.ExtendBaseWidget(r)
	return r
}

func (r *paletteRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.box)
}

func (r *paletteRow) set(row PaletteRow) {
	r.title.SetText(row.Title)
	r.title.Importance = widget.MediumImportance
	if row.Accent {
		r.title.Importance = widget.HighImportance
	}
	r.detail.SetText(row.Detail)
	r.meta.SetText(row.Meta)
	r.Refresh()
}

// --- the panel ---------------------------------------------------------------

// panel is the rounded surface both overlays sit on: one step up from the
// window (§4.1's Surface token), a hairline border, and nothing else - no
// shadow, no gradient. It is a widget rather than a container so that a theme
// change repaints it.
type panel struct {
	widget.BaseWidget
	fill    *canvas.Rectangle
	content fyne.CanvasObject
	box     *fyne.Container
}

func newPanel(content fyne.CanvasObject) *panel {
	p := &panel{fill: canvas.NewRectangle(color.Transparent), content: content}
	p.box = container.NewStack(p.fill, container.NewPadded(content))
	p.ExtendBaseWidget(p)
	return p
}

func (p *panel) CreateRenderer() fyne.WidgetRenderer {
	p.paint()
	return widget.NewSimpleRenderer(p.box)
}

func (p *panel) Refresh() {
	p.paint()
	p.BaseWidget.Refresh()
}

func (p *panel) paint() {
	th := p.Theme()
	v := ecotheme.Variant(th)
	p.fill.FillColor = th.Color(ecotheme.Surface, v)
	p.fill.StrokeColor = th.Color(ecotheme.Border, v)
	p.fill.StrokeWidth = 1
	p.fill.CornerRadius = th.Size(theme.SizeNamePadding) * 1.5
	p.fill.Refresh()
}
