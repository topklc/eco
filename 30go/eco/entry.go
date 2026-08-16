package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// chatEntry is the message input: Tab completes nicknames, Up and Down
// walk through previously sent messages.
type chatEntry struct {
	widget.Entry
	u *UI

	history []string
	histPos int // len(history) means "editing a new message"
	draft   string

	completions []string // current Tab cycle
	compIdx     int
	compPrefix  string // what the user had typed before the first Tab

	shiftHeld bool // Shift+Enter inserts a newline instead of sending
	curRows   int  // visible rows, grows with the draft up to maxInputRows
}

// maxInputRows caps how tall the input grows before it scrolls internally.
const maxInputRows = 5

func newChatEntry(u *UI) *chatEntry {
	e := &chatEntry{u: u}
	e.histPos = 0
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	e.curRows = 1
	e.SetMinRowsVisible(1)
	e.OnChanged = func(t string) {
		lines := strings.Count(t, "\n") + 1
		if e.u.composeHint != nil {
			e.u.updateComposeHint(lines)
		}
		rows := lines
		if rows > maxInputRows {
			rows = maxInputRows
		}
		if rows != e.curRows {
			e.curRows = rows
			e.SetMinRowsVisible(rows)
			if e.u.centerBox != nil {
				e.u.centerBox.Refresh()
			}
		}
	}
	return e
}

func (e *chatEntry) AcceptsTab() bool { return true }

// TypedShortcut lets app-wide combos work while the input is focused,
// which is nearly always.
func (e *chatEntry) TypedShortcut(s fyne.Shortcut) {
	if cs, ok := s.(*desktop.CustomShortcut); ok && e.u.handleShortcut(cs) {
		return
	}
	e.Entry.TypedShortcut(s)
}

func (e *chatEntry) KeyDown(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shiftHeld = true
	}
	e.Entry.KeyDown(k)
}

func (e *chatEntry) KeyUp(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shiftHeld = false
	}
	e.Entry.KeyUp(k)
}

func (e *chatEntry) submit() {
	e.completions = nil
	text := e.Text
	e.recordHistory(text)
	e.SetText("")
	e.u.handleInput(text)
}

func (e *chatEntry) recordHistory(text string) {
	if text != "" {
		e.history = append(e.history, text)
		if len(e.history) > 100 {
			e.history = e.history[1:]
		}
	}
	e.histPos = len(e.history)
	e.draft = ""
}

func (e *chatEntry) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if e.shiftHeld {
			e.Entry.TypedKey(k) // newline
		} else {
			e.submit()
		}
		return
	case fyne.KeyTab:
		e.complete()
		return
	case fyne.KeyUp:
		if strings.Contains(e.Text, "\n") {
			e.Entry.TypedKey(k) // navigating a multiline draft
			return
		}
		if e.histPos > 0 {
			e.completions = nil
			if e.histPos == len(e.history) {
				e.draft = e.Text
			}
			e.histPos--
			e.SetText(e.history[e.histPos])
			e.cursorToEnd()
		}
		return
	case fyne.KeyDown:
		if strings.Contains(e.Text, "\n") {
			e.Entry.TypedKey(k)
			return
		}
		if e.histPos < len(e.history) {
			e.completions = nil
			e.histPos++
			if e.histPos == len(e.history) {
				e.SetText(e.draft)
			} else {
				e.SetText(e.history[e.histPos])
			}
			e.cursorToEnd()
		}
		return
	}
	e.completions = nil
	e.Entry.TypedKey(k)
}

func (e *chatEntry) TypedRune(r rune) {
	e.completions = nil
	e.Entry.TypedRune(r)
}

// complete cycles through nicknames in the current buffer matching the
// last word in the entry.
func (e *chatEntry) complete() {
	if e.u.sess == nil {
		return
	}

	if len(e.completions) == 0 {
		text := e.Text
		start := strings.LastIndexAny(text, " \n") + 1
		prefix := strings.ToLower(text[start:])
		if prefix == "" {
			return
		}
		for _, m := range e.u.sess.SortedMembers(e.u.current) {
			nick := strings.TrimLeft(m, "~&@%+")
			if strings.HasPrefix(strings.ToLower(nick), prefix) {
				e.completions = append(e.completions, nick)
			}
		}
		if len(e.completions) == 0 {
			return
		}
		e.compIdx = 0
		e.compPrefix = text[:start]
	} else {
		e.compIdx = (e.compIdx + 1) % len(e.completions)
	}

	nick := e.completions[e.compIdx]
	suffix := " "
	if e.compPrefix == "" {
		suffix = ": "
	}
	e.SetText(e.compPrefix + nick + suffix)
	e.cursorToEnd()
}

// cursorToEnd places the cursor after the last character, correctly for
// multiline text (a recalled multiline history entry spans several rows).
func (e *chatEntry) cursorToEnd() {
	lines := strings.Split(e.Text, "\n")
	e.CursorRow = len(lines) - 1
	e.CursorColumn = len(lines[len(lines)-1])
}
