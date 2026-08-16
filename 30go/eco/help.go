package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// The user documentation ships inside the binary itself, so Help works
// offline and the exe alone is a complete distribution.

//go:embed docs/getting-started.md
var docGettingStarted string

//go:embed docs/commands-and-shortcuts.md
var docCommands string

//go:embed docs/faq.md
var docFAQ string

//go:embed LICENSE
var licenseText string

//go:embed docs/irc-basics.md
var docIRCBasics string

//go:embed docs/power-users.md
var docPowerUsers string

//go:embed docs/ergo-guide.md
var docErgoGuide string

func (u *UI) showHelp() {
	page := func(md string) fyne.CanvasObject {
		rt := widget.NewRichTextFromMarkdown(md)
		rt.Wrapping = fyne.TextWrapWord
		// RichText's built-in scroller handles the mouse wheel correctly;
		// an external scroll container around it does not
		rt.Scroll = fyne.ScrollVerticalOnly
		// breathing room around the text so it does not touch the edges
		return container.NewPadded(container.NewPadded(rt))
	}

	// tabs render lazily: parsing all six documents up front made opening
	// the dialog visibly lag
	names := []string{"Getting Started", "IRC Basics", "Commands", "Power Users", "Ergo Guide", "FAQ"}
	bodies := []string{docGettingStarted, docIRCBasics, docCommands, docPowerUsers, docErgoGuide, docFAQ}
	items := make([]*container.TabItem, len(names))
	for i, n := range names {
		items[i] = container.NewTabItem(n, container.NewStack())
	}
	tabs := container.NewAppTabs(items...)
	rendered := make([]bool, len(names))
	render := func(i int) {
		if i >= 0 && i < len(rendered) && !rendered[i] {
			rendered[i] = true
			items[i].Content = page(bodies[i])
			tabs.Refresh()
		}
	}
	tabs.OnSelected = func(t *container.TabItem) {
		for i, it := range items {
			if it == t {
				render(i)
			}
		}
	}
	render(0)
	// our own compact close button, bottom right: the stock dialog button
	// stretches to the full width at this dialog size
	var d dialog.Dialog
	closeBtn := widget.NewButton("Close", func() { d.Hide() })
	body := container.NewBorder(nil,
		container.NewBorder(nil, nil, nil, closeBtn), nil, nil, tabs)
	d = dialog.NewCustomWithoutButtons("Help",
		container.NewGridWrap(u.dialogSize(780, 560), body), u.win)
	u.trackDialog(d)
	d.Show()
}

// showAbout presents the app info with the full MIT license.
func (u *UI) showAbout() {
	info := widget.NewLabelWithStyle(
		"eco\n\nA simple, modern IRC client.\nBuilt with ergochat/irc-go and Fyne.\nFull IRCv3 + Ergo draft feature support.\n\nDeveloped with AI assistance.",
		fyne.TextAlignCenter, fyne.TextStyle{})
	lic := widget.NewLabel(licenseText)
	lic.Wrapping = fyne.TextWrapWord
	lic.TextStyle = fyne.TextStyle{Monospace: true}
	body := container.NewVScroll(container.NewVBox(info, widget.NewSeparator(), lic))

	var d dialog.Dialog
	closeBtn := widget.NewButton("Close", func() { d.Hide() })
	d = dialog.NewCustomWithoutButtons("About eco",
		container.NewGridWrap(u.dialogSize(560, 460),
			container.NewBorder(nil, container.NewBorder(nil, nil, nil, closeBtn), nil, nil,
				container.NewPadded(body))), u.win)
	u.trackDialog(d)
	d.Show()
}
