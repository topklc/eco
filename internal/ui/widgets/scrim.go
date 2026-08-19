package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// The scrim: while a menu or an overlay is open, the window behind it dims.
// It is what every other application does and what the owner asked for, and
// it earns its keep twice: the thing in front is unmistakably in front, and
// the eye stops trying to read the conversation it is covering.
//
// Menus go through ShowMenuAt* rather than Fyne's package functions so the
// dim cannot be forgotten at a new call site: there is one door.

// NewScrim returns a full-canvas dimming layer. It draws only; taps pass to
// the overlay above it, which is what dismisses the menu.
func NewScrim() *fyne.Container {
	rect := canvas.NewRectangle(theme.Color(ecotheme.Scrim))
	return container.NewStack(rect)
}

// ShowMenuAtPosition shows a popup menu with the window dimmed behind it.
func ShowMenuAtPosition(menu *fyne.Menu, c fyne.Canvas, pos fyne.Position) {
	p := newDimmedMenu(menu, c)
	if p == nil {
		return
	}
	p.ShowAtPosition(pos)
}

// ShowMenuAtRelativePosition shows a popup menu anchored to a widget, with
// the window dimmed behind it.
func ShowMenuAtRelativePosition(menu *fyne.Menu, c fyne.Canvas, rel fyne.Position, to fyne.CanvasObject) {
	p := newDimmedMenu(menu, c)
	if p == nil {
		return
	}
	p.ShowAtRelativePosition(rel, to)
}

// newDimmedMenu builds the popup and hangs the scrim behind it, removing the
// scrim on whatever dismisses the menu (a choice, a tap outside, Escape).
func newDimmedMenu(menu *fyne.Menu, c fyne.Canvas) *widget.PopUpMenu {
	if c == nil {
		return nil
	}
	scrim := NewScrim()
	c.Overlays().Add(scrim)
	p := widget.NewPopUpMenu(menu, c)
	if p == nil {
		c.Overlays().Remove(scrim)
		return nil
	}
	prev := p.OnDismiss
	p.OnDismiss = func() {
		c.Overlays().Remove(scrim)
		if prev != nil {
			prev()
		}
	}
	return p
}
