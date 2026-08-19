package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// The rebinding surface: a press-to-record field per action, and the live
// conflict line under it (blueprint §4.3).
//
// # Why a capture widget rather than two dropdowns
//
// v1 asked for a modifier from one Select and a key from another, which is a
// form for describing a chord rather than a way to choose one: it could express
// combos the toolkit never delivers, could not express Alt+Shift at all, and
// made the user translate their own fingers into a vocabulary. Press-to-record
// is the gesture everyone already knows, and it can only ever record a chord
// the toolkit actually produces.
//
// # The modifiers are QUERIED, not tracked
//
// This is the shiftHeld fix §4.3 asks for, and it is worth being precise about
// what was wrong. v1's composer tracked Shift with KeyDown/KeyUp handlers and a
// bool. That bool desyncs for a reason no amount of care fixes: a KeyUp
// delivered while another widget has focus - alt-tab away with Shift down, or
// press Shift inside a dialog - never arrives, and the bool stays true until
// the next tap of that key. The state then reads "Shift is held" for as long as
// the user does not press Shift again, which is why v1 occasionally sent a
// message when someone meant a newline.
//
// Fyne 2.8's desktop driver can be ASKED (desktop.Driver.CurrentKeyModifiers),
// so nothing here remembers anything: at the moment a key arrives, the
// modifiers are read from the driver. There is no state to go stale.

// currentModifiers returns the modifier keys held right now, or 0 when the
// driver cannot say (the test driver, and any future non-desktop one).
func currentModifiers(a fyne.App) fyne.KeyModifier {
	if a == nil {
		return 0
	}
	if d, ok := a.Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()
	}
	return 0
}

// keyCapture is a one-line field that records the next chord pressed into it.
type keyCapture struct {
	widget.BaseWidget

	// mods answers "which modifiers are held right now". It is a function
	// rather than a call to the driver so a test can hold Shift down, which no
	// test driver can do - the same seam the composer uses (UI.heldModifiers).
	mods func() fyne.KeyModifier
	// onChange fires with the recorded combo. It fires for a REFUSED combo too
	// - with ok false - because the refusal is what the surface has to explain.
	onChange func(c combo, ok bool)

	label     *widget.Label
	recording bool
	current   combo
}

func newKeyCapture(a fyne.App, current combo, onChange func(combo, bool)) *keyCapture {
	k := &keyCapture{
		mods:     func() fyne.KeyModifier { return currentModifiers(a) },
		current:  current,
		onChange: onChange,
	}
	k.label = widget.NewLabel(current.String())
	k.ExtendBaseWidget(k)
	return k
}

func (k *keyCapture) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(k.label))
}

// Tapped starts recording. A second tap stops it, so the widget can always be
// left alone again without pressing a key into it.
func (k *keyCapture) Tapped(*fyne.PointEvent) {
	k.recording = !k.recording
	k.redraw()
}

func (k *keyCapture) FocusGained() { k.redraw() }

// FocusLost stops recording: a field that is still listening after the keyboard
// has gone somewhere else is how a stray keystroke rebinds something.
func (k *keyCapture) FocusLost() {
	k.recording = false
	k.redraw()
}

func (k *keyCapture) TypedRune(rune) {}

// TypedKey records the chord: the key that arrived, plus the modifiers QUERIED
// at that instant. See the file comment for why the second half is a question
// and not a remembered bool.
func (k *keyCapture) TypedKey(e *fyne.KeyEvent) {
	if !k.recording {
		return
	}
	c := combo{Mod: k.mods(), Key: e.Name}
	if isModifierKey(e.Name) {
		// A modifier on its own is the user still assembling the chord; keep
		// listening rather than recording "Shift".
		if k.onChange != nil {
			k.onChange(c, false)
		}
		return
	}
	k.recording = false
	if comboRefusal(c) != "" {
		if k.onChange != nil {
			k.onChange(c, false)
		}
		k.redraw()
		return
	}
	k.current = c
	k.redraw()
	if k.onChange != nil {
		k.onChange(c, true)
	}
}

// TypedShortcut catches the chords the canvas would rather treat as shortcuts.
// Without it, recording Ctrl+K would fire Ctrl+K's action instead of being
// recorded - the one thing a rebinding field must never do.
func (k *keyCapture) TypedShortcut(s fyne.Shortcut) {
	if !k.recording {
		return
	}
	if cs, ok := s.(*desktop.CustomShortcut); ok {
		k.TypedKey(&fyne.KeyEvent{Name: cs.KeyName})
	}
}

// setCombo puts a value in without recording one (the Reset button).
func (k *keyCapture) setCombo(c combo) {
	k.current = c
	k.recording = false
	k.redraw()
}

func (k *keyCapture) redraw() {
	if k.recording {
		k.label.SetText("press a key…")
		return
	}
	k.label.SetText(k.current.String())
}

// --- the Preferences page -----------------------------------------------------------

// keybindsPane is the Shortcuts page: one row per rebindable action, the fixed
// keys listed underneath because they are part of the keyboard whether or not
// they can be moved, and one conflict line that says what is in the way.
func (u *UI) keybindsPane() fyne.CanvasObject {
	p := u.app.Preferences()
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord

	var items []*widget.FormItem
	var resets []func()
	for _, a := range keyActions {
		if a.Fixed {
			continue
		}
		a := a
		list := combosFor(p, a.ID)
		first := combo{}
		if len(list) > 0 {
			first = list[0]
		}
		var field *keyCapture
		field = newKeyCapture(u.app, first, func(c combo, ok bool) {
			if !ok {
				if why := comboRefusal(c); why != "" {
					status.SetText(why)
				}
				return
			}
			if other, taken := conflictFor(p, a.ID, c); taken {
				what := "used by “" + other.Label + "”"
				if other.Fixed {
					what = "reserved: it is “" + other.Label + "”"
				}
				status.SetText(c.String() + " is already " + what + ". Pick another one.")
				field.setCombo(first)
				return
			}
			// The recorded chord replaces the FIRST combo and keeps the rest,
			// so an action with a second way in (Alt+Up and Ctrl+PageUp) does
			// not silently lose it the moment the first is changed.
			next := append([]combo{c}, dropCombo(combosFor(p, a.ID), first)...)
			setCombos(p, a.ID, next)
			first = c
			u.applyKeybinds()
			status.SetText(a.Label + " is now " + comboWords(next) + ".")
		})
		field.mods = u.heldModifiers
		items = append(items, widget.NewFormItem(a.Label, field))
		resets = append(resets, func() {
			def := parseCombos(a.Combos)
			setCombos(p, a.ID, def)
			if len(def) > 0 {
				first = def[0]
				field.setCombo(def[0])
			}
		})
	}

	reset := widget.NewButton("Reset to defaults", func() {
		for _, r := range resets {
			r()
		}
		u.applyKeybinds()
		status.SetText("Back to the defaults.")
	})

	hint := widget.NewLabel("Click a shortcut, then press the keys you want.")
	hint.TextStyle = fyne.TextStyle{Italic: true}

	fixed := widget.NewRichTextFromMarkdown(fixedKeysMarkdown(p))
	fixed.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		hint,
		widget.NewForm(items...),
		status,
		reset,
		widget.NewSeparator(),
		fixed,
	)
}

// dropCombo removes one combo from a list.
func dropCombo(list []combo, drop combo) []combo {
	out := make([]combo, 0, len(list))
	for _, c := range list {
		if c != drop {
			out = append(out, c)
		}
	}
	return out
}

// comboWords writes a combo list the way a sentence would.
func comboWords(list []combo) string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.String())
	}
	switch len(out) {
	case 0:
		return "unbound"
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " or " + out[len(out)-1]
}

// fixedKeysMarkdown lists the keys that cannot be moved, from the same table
// the rest of the page is built from - so the page cannot claim a key is fixed
// when it is not, or forget one that is.
func fixedKeysMarkdown(p fyne.Preferences) string {
	var b strings.Builder
	b.WriteString("**These keys are fixed.**\n\n")
	for _, a := range keyActions {
		if !a.Fixed {
			continue
		}
		b.WriteString("- **")
		b.WriteString(actionCombos(p, a))
		b.WriteString("** - ")
		b.WriteString(a.Label)
		b.WriteString("\n")
	}
	return b.String()
}
