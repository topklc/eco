package app

import (
	_ "embed"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/command"
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

// The two generated sections of the documentation page. The prose around them
// is written by hand; the lists are rendered from the tables that implement
// them, so a command added to internal/command and a key added to keyActions
// are both documented by existing - which is what the hand-written lists failed
// at (M10 for the commands, M12(c) for the keys, where the shortcut list had
// already drifted about which key stepped buffers).
const (
	commandsMarker  = "<!--commands-->"
	shortcutsMarker = "<!--shortcuts-->"
)

// commandsDoc is the Commands page with both generated sections in it. The
// shortcut half is rendered against the user's OWN bindings, so a rebound key
// documents itself.
func (u *UI) commandsDoc() string {
	var p fyne.Preferences
	if u != nil && u.app != nil {
		p = u.app.Preferences()
	}
	return commandsDocWith(p)
}

func commandsDocWith(p fyne.Preferences) string {
	doc := strings.Replace(docCommands, commandsMarker, command.HelpMarkdown(), 1)
	return strings.Replace(doc, shortcutsMarker, keybindHelpMarkdown(p), 1)
}

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

	aboutPage := func() fyne.CanvasObject {
		info := widget.NewLabelWithStyle(
			"eco "+version+"\n\nA simple, modern IRC client.\nBuilt with ergochat/irc-go and Fyne.\nFull IRCv3 + Ergo draft feature support.\n\nDeveloped with AI assistance.",
			fyne.TextAlignCenter, fyne.TextStyle{})
		return container.NewCenter(info)
	}
	licensePage := func() fyne.CanvasObject {
		lic := widget.NewLabel(licenseText)
		lic.Wrapping = fyne.TextWrapWord
		lic.TextStyle = fyne.TextStyle{Monospace: true}
		return container.NewPadded(container.NewVScroll(lic))
	}

	// tabs render lazily: parsing every document up front made opening
	// the dialog visibly lag
	names := []string{"Getting Started", "IRC Basics", "Commands", "Power Users", "Ergo Guide", "FAQ", "About", "License"}
	builders := []func() fyne.CanvasObject{
		func() fyne.CanvasObject { return page(docGettingStarted) },
		func() fyne.CanvasObject { return page(docIRCBasics) },
		func() fyne.CanvasObject { return page(u.commandsDoc()) },
		func() fyne.CanvasObject { return page(docPowerUsers) },
		func() fyne.CanvasObject { return page(docErgoGuide) },
		func() fyne.CanvasObject { return page(docFAQ) },
		aboutPage,
		licensePage,
	}
	items := make([]*container.TabItem, len(names))
	for i, n := range names {
		items[i] = container.NewTabItem(n, container.NewStack())
	}
	tabs := container.NewAppTabs(items...)
	rendered := make([]bool, len(names))
	render := func(i int) {
		if i >= 0 && i < len(rendered) && !rendered[i] {
			rendered[i] = true
			items[i].Content = builders[i]()
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
	d = dialog.NewCustomWithoutButtons("Documentation",
		container.NewGridWrap(u.dialogSize(780, 560), body), u.win)
	u.trackDialog(d)
	d.Show()
}
