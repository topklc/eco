package app

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The overlay layer - blueprint §4.1's "Overlays (Escape-dismissable, never
// blocking modals where avoidable, every close restores composer focus through
// one shared helper)".
//
// There are two palettes on it, the Switcher and the Finder, and they are the
// same widget configured twice (widgets.Palette). They sit centred in a layer
// stacked over the splits, with the window dimmed behind them: the same scrim
// every menu draws, so what is in front is unmistakably in front.
//
// Which one is open is not a field. It is which box is visible, asked of the
// widgets themselves, so there is no second copy of that fact to go stale - the
// same reason the People pane's visibility is the pane's and not a bool beside
// it.

// overlaySize is how big a palette is at the default font size; dialogSize
// scales it with the user's.
const (
	overlayWidth  = 620
	overlayHeight = 420
)

// newOverlayLayer builds the two palettes and the layer they float in.
func (u *UI) newOverlayLayer() fyne.CanvasObject {
	u.switcher = widgets.NewPalette(widgets.PaletteOptions{
		Placeholder: "Go to a channel or conversation - or type a name to join it",
		Rows:        u.switcherRows,
		Chosen:      u.switcherChose,
		Cancel:      u.closeOverlays,
		Shortcut:    u.overlayShortcut,
		Empty:       "Nothing by that name. Type a channel name to create it.",
	})
	u.finder = widgets.NewPalette(widgets.PaletteOptions{
		Placeholder: "Search everything - words, from:nick, in:#channel, after:2026-01-31",
		Rows:        u.finderQueryRows,
		Chosen:      u.finderChose,
		Cancel:      u.closeOverlays,
		Shortcut:    u.overlayShortcut,
		Empty:       "No messages match.",
	})

	u.switcherBox = container.NewGridWrap(u.dialogSize(overlayWidth, overlayHeight), u.switcher)
	u.finderBox = container.NewGridWrap(u.dialogSize(overlayWidth, overlayHeight), u.finder)
	u.switcherBox.Hide()
	u.finderBox.Hide()

	u.overlay = container.NewStack(widgets.NewScrim(),
		container.NewCenter(container.NewVBox(u.switcherBox, u.finderBox)))
	u.overlay.Hide()
	return u.overlay
}

// overlayShortcut passes an app combo through an overlay's text field. Focus
// lives in the field while one is open, and Fyne sends a shortcut to the
// FOCUSED widget rather than to the canvas - so without this, Ctrl+Q would type
// nothing and do nothing, and the two palettes could not hand over to each
// other. It is the same rule the composer follows (chatEntry.TypedShortcut),
// applied where focus actually is.
func (u *UI) overlayShortcut(s fyne.Shortcut) bool {
	cs, ok := s.(*desktop.CustomShortcut)
	return ok && u.handleShortcut(cs)
}

// overlayOpen reports whether any of the three finding surfaces is on screen.
// It is escape.go's escOverlay rung.
func (u *UI) overlayOpen() bool {
	return (u.switcherBox != nil && u.switcherBox.Visible()) ||
		(u.finderBox != nil && u.finderBox.Visible()) ||
		(u.searchBar != nil && u.searchBar.Visible())
}

// openOverlay shows one palette and nothing else. Two of these on screen at
// once is not a state worth having, so opening one closes the other.
func (u *UI) openOverlay(box *fyne.Container, p *widgets.Palette) {
	u.closeOverlays()
	p.Reset()
	box.Show()
	u.overlay.Show()
	u.overlay.Refresh()
	u.win.Canvas().Focus(p.Entry())
}

// hidePalettes takes both palettes off screen without touching the search bar
// or the keyboard. It is what opening the third surface does - the palettes and
// the bar are one rung of the Escape stack, so only one of them is ever the
// thing Escape means.
func (u *UI) hidePalettes() {
	if u.switcherBox == nil {
		return
	}
	if !u.switcherBox.Visible() && !u.finderBox.Visible() {
		return
	}
	u.switcherBox.Hide()
	u.finderBox.Hide()
	u.overlay.Hide()
	u.overlay.Refresh()
}

// closeOverlays puts all three away and gives the keyboard back. It is called
// from Escape, from a choice being made, and from anything that changes what is
// on screen underneath - a buffer switch leaves a search bar about a
// conversation you are no longer in.
func (u *UI) closeOverlays() {
	if !u.overlayOpen() {
		return
	}
	if u.switcherBox != nil {
		u.switcherBox.Hide()
	}
	if u.finderBox != nil {
		u.finderBox.Hide()
	}
	if u.overlay != nil {
		u.overlay.Hide()
		u.overlay.Refresh()
	}
	u.closeSearch()
	u.focusComposer()
}
