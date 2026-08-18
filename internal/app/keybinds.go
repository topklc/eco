package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Rebindable app shortcuts. Alt+1..9 (jump to sidebar row) stays fixed.
type keyAction struct {
	ID    string
	Label string
	Def   string // default combo, e.g. "Ctrl+W"
}

var keyActions = []keyAction{
	{"close", "Close current buffer", "Ctrl+W"},
	{"prev", "Previous buffer", "Ctrl+PageUp"},
	{"next", "Next buffer", "Ctrl+PageDown"},
	{"unread", "Jump to next unread", "Alt+A"},
	{"quit", "Quit eco", "Ctrl+Q"},
}

var keybindModifiers = []string{"Ctrl", "Alt", "Ctrl+Shift", "Ctrl+Alt"}

var keybindKeys = func() []string {
	keys := []string{}
	for c := 'A'; c <= 'Z'; c++ {
		keys = append(keys, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		keys = append(keys, string(c))
	}
	keys = append(keys, "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9",
		"F10", "F11", "F12", "PageUp", "PageDown", "Home", "End", "Tab", "Space")
	return keys
}()

func modifierMask(name string) (fyne.KeyModifier, bool) {
	switch name {
	case "Ctrl":
		return fyne.KeyModifierControl, true
	case "Alt":
		return fyne.KeyModifierAlt, true
	case "Ctrl+Shift":
		return fyne.KeyModifierControl | fyne.KeyModifierShift, true
	case "Ctrl+Alt":
		return fyne.KeyModifierControl | fyne.KeyModifierAlt, true
	}
	return 0, false
}

// parseCombo turns "Ctrl+PageUp" into a shortcut; the key is the part
// after the last '+' so modifier names may themselves contain one.
func parseCombo(s string) (*desktop.CustomShortcut, bool) {
	i := strings.LastIndex(s, "+")
	if i <= 0 || i == len(s)-1 {
		return nil, false
	}
	mod, ok := modifierMask(s[:i])
	if !ok {
		return nil, false
	}
	key := s[i+1:]
	// display names differ from Fyne's KeyName constants for these
	switch key {
	case "PageUp":
		key = string(fyne.KeyPageUp) // "Prior"
	case "PageDown":
		key = string(fyne.KeyPageDown) // "Next"
	}
	return &desktop.CustomShortcut{KeyName: fyne.KeyName(key), Modifier: mod}, true
}

// comboFor returns the user's combo for an action, or the default.
func comboFor(p fyne.Preferences, id string) string {
	for _, a := range keyActions {
		if a.ID == id {
			return p.StringWithFallback("kb_"+id, a.Def)
		}
	}
	return ""
}

// matchesAction reports whether a received shortcut is the current binding
// for the given action.
func (u *UI) matchesAction(cs *desktop.CustomShortcut, id string) bool {
	b, ok := parseCombo(comboFor(u.app.Preferences(), id))
	return ok && b.KeyName == cs.KeyName && b.Modifier == cs.Modifier
}

// applyKeybinds (re)registers the rebindable canvas shortcuts. The fixed
// Alt+digit row jumps are registered once in registerShortcuts.
func (u *UI) applyKeybinds() {
	cv := u.win.Canvas()
	for _, old := range u.boundKeys {
		cv.RemoveShortcut(old)
	}
	u.boundKeys = u.boundKeys[:0]
	for _, a := range keyActions {
		cs, ok := parseCombo(comboFor(u.app.Preferences(), a.ID))
		if !ok {
			continue
		}
		u.boundKeys = append(u.boundKeys, cs)
		bound := cs
		cv.AddShortcut(bound, func(fyne.Shortcut) { u.handleShortcut(bound) })
	}
}

// keybindsPane is the Preferences page for rebinding shortcuts.
func (u *UI) keybindsPane() fyne.CanvasObject {
	p := u.app.Preferences()
	items := []*widget.FormItem{}
	var resets []func()
	for _, a := range keyActions {
		a := a
		combo := comboFor(p, a.ID)
		i := strings.LastIndex(combo, "+")
		curMod, curKey := "Ctrl", "W"
		if i > 0 && i < len(combo)-1 {
			curMod, curKey = combo[:i], combo[i+1:]
		}
		mod := widget.NewSelect(keybindModifiers, nil)
		mod.SetSelected(curMod)
		key := widget.NewSelect(keybindKeys, nil)
		key.SetSelected(curKey)
		save := func(string) {
			if mod.Selected == "" || key.Selected == "" {
				return
			}
			p.SetString("kb_"+a.ID, mod.Selected+"+"+key.Selected)
			u.applyKeybinds()
		}
		mod.OnChanged = save
		key.OnChanged = save
		items = append(items, widget.NewFormItem(a.Label, container.NewGridWithColumns(2, mod, key)))
		resets = append(resets, func() {
			di := strings.LastIndex(a.Def, "+")
			mod.SetSelected(a.Def[:di])
			key.SetSelected(a.Def[di+1:])
		})
	}
	note := widget.NewLabel("Alt+1…9 always jumps to the numbered sidebar entry.")
	note.TextStyle = fyne.TextStyle{Italic: true}
	reset := widget.NewButton("Reset to defaults", func() {
		for _, r := range resets {
			r() // SetSelected triggers save, which persists and re-applies
		}
	})
	return container.NewVBox(widget.NewForm(items...), note, reset)
}

// showIgnoredUsers lists ignored nicks with an Unignore button each, so
// an old ignore is always discoverable and reversible.
func (u *UI) showIgnoredUsers() {
	var d dialog.Dialog
	body := container.NewVBox()
	var rebuild func()
	rebuild = func() {
		body.Objects = nil
		nicks := ignoredList()
		if len(nicks) == 0 {
			body.Add(widget.NewLabel("Nobody is ignored."))
		}
		for _, n := range nicks {
			n := n
			btn := widget.NewButton("Unignore", func() {
				toggleIgnore(u.app.Preferences(), n)
				u.memberList.Refresh()
				rebuild()
			})
			body.Add(container.NewBorder(nil, nil, nil, btn, widget.NewLabel(n)))
		}
		body.Refresh()
	}
	rebuild()
	d = dialog.NewCustom("Ignored users", "Close",
		container.NewGridWrap(u.dialogSize(340, 300), container.NewVScroll(body)), u.win)
	u.trackDialog(d)
	d.Show()
}
