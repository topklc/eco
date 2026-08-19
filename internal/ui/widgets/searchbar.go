package widgets

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// SearchBar is Ctrl+F - blueprint §4.3's in-buffer search. A slim bar above the
// Timeline: a field, how many matches there are and which one you are on, two
// steppers and a close. It covers nothing and it dismisses on Escape.
//
// It is not a Palette. A palette answers "which of these do you want"; this
// answers "where in what I am already reading", so the answer is not a list -
// it is the buffer itself, with the matches coloured where they stand
// (find.go). Giving it a results list would put the conversation twice on the
// screen and make the reader choose which copy to read.
type SearchBar struct {
	widget.BaseWidget
	opts SearchBarOptions

	entry   *searchEntry
	count   *widget.Label
	content fyne.CanvasObject
}

// SearchBarOptions is what the bar cannot do for itself.
type SearchBarOptions struct {
	// Changed fires on every keystroke: the host re-runs the search.
	Changed func(query string)
	// Step is Enter (+1) and Shift+Enter (-1).
	Step func(dir int)
	// Cancel is Escape and the close button. The host hides the bar and gives
	// the keyboard back to the composer through the shared helper.
	Cancel func()
	// Shortcut is offered every modifier combo that reaches the field; see
	// PaletteOptions.Shortcut, which exists for the same reason.
	Shortcut func(fyne.Shortcut) bool
}

// NewSearchBar builds the bar.
func NewSearchBar(o SearchBarOptions) *SearchBar {
	b := &SearchBar{opts: o}
	b.entry = newSearchEntry(b)
	b.entry.SetPlaceHolder("Find in this conversation")

	b.count = widget.NewLabel("")
	b.count.TextStyle = fyne.TextStyle{Italic: true}

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { b.step(-1) })
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { b.step(1) })
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), b.Cancel)
	for _, x := range []*widget.Button{prev, next, closeBtn} {
		x.Importance = widget.LowImportance
	}

	b.content = container.NewBorder(nil, widget.NewSeparator(), nil,
		container.NewHBox(b.count, prev, next, closeBtn),
		container.NewPadded(b.entry))
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

func (b *SearchBar) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

// Entry is the field, which is what the host focuses when the bar opens.
func (b *SearchBar) Entry() fyne.Focusable { return b.entry }

// Query is what has been typed.
func (b *SearchBar) Query() string { return b.entry.Text }

// SetQuery seeds the field: reopening the bar offers the last thing looked for,
// selected-free and ready to be typed over.
func (b *SearchBar) SetQuery(s string) {
	b.entry.SetText(s)
	b.entry.CursorColumn = len([]rune(s))
}

// SetCount says where the reader is: "3 of 12", "no matches", or nothing at all
// before anything has been typed.
func (b *SearchBar) SetCount(at, total int) {
	switch {
	case b.Query() == "":
		b.count.SetText("")
	case total == 0:
		b.count.SetText("no matches")
	case at < 0:
		b.count.SetText(plural(total, "match", "matches"))
	default:
		b.count.SetText(itoa(at+1) + " of " + itoa(total))
	}
}

// Count is what the bar is currently saying. Tests read it, because "3 of 12"
// is the whole of what stepping is visible as.
func (b *SearchBar) Count() string { return b.count.Text }

func (b *SearchBar) step(dir int) {
	if b.opts.Step != nil {
		b.opts.Step(dir)
	}
}

// Cancel closes the bar.
func (b *SearchBar) Cancel() {
	if b.opts.Cancel != nil {
		b.opts.Cancel()
	}
}

// searchEntry is the field. Enter steps forward, Shift+Enter steps back, and
// Escape closes; the rest is an ordinary text box.
type searchEntry struct {
	widget.Entry
	b     *SearchBar
	shift bool
}

func newSearchEntry(b *SearchBar) *searchEntry {
	e := &searchEntry{b: b}
	e.ExtendBaseWidget(e)
	e.OnChanged = func(s string) {
		if e.b.opts.Changed != nil {
			e.b.opts.Changed(s)
		}
	}
	return e
}

func (e *searchEntry) TypedShortcut(s fyne.Shortcut) {
	if e.b.opts.Shortcut != nil && e.b.opts.Shortcut(s) {
		return
	}
	e.Entry.TypedShortcut(s)
}

func (e *searchEntry) KeyDown(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shift = true
	}
	e.Entry.KeyDown(k)
}

func (e *searchEntry) KeyUp(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shift = false
	}
	e.Entry.KeyUp(k)
}

func (e *searchEntry) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if e.shift {
			e.b.step(-1)
		} else {
			e.b.step(1)
		}
	case fyne.KeyEscape:
		e.b.Cancel()
	case fyne.KeyUp:
		e.b.step(-1)
	case fyne.KeyDown:
		e.b.step(1)
	default:
		e.Entry.TypedKey(k)
	}
}

// plural keeps the bar saying "1 match" rather than "1 matches".
func plural(n int, one, many string) string {
	if n == 1 {
		return itoa(n) + " " + one
	}
	return itoa(n) + " " + many
}

func itoa(n int) string { return strconv.Itoa(n) }
