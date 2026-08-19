package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/command"
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
	compHead    string // text before the word being completed
	compTail    string // text after the cursor, preserved across the cycle

	curRows int // visible rows, grows with the draft up to maxInputRows
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
		// The composer is a widget, but the text in it is state: the viewmodel
		// owns it, so switching buffers can park it under the buffer it was
		// typed in without this widget being asked (and without the switch
		// having to remember to ask).
		if e.u.vm != nil {
			e.u.vm.SetDraft(t)
		}
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

// shiftDown reports whether Shift is held RIGHT NOW. It is a question, never a
// remembered bool - see keycapture.go for the whole of why. The short version
// is that v1 tracked Shift with KeyDown/KeyUp and the pair desyncs whenever a
// KeyUp is delivered somewhere else (alt-tab with Shift down), after which the
// composer believed Shift was held until the next tap of it and sent messages
// where a newline was meant.
func (e *chatEntry) shiftDown() bool {
	return e.u.heldModifiers()&fyne.KeyModifierShift != 0
}

func (e *chatEntry) submit() {
	e.cancelCompletion()
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
	// Focus lives here, so the app's bare keys have to pass through the
	// composer to reach the app at all (blueprint §4.3). Anything handleKey
	// does not claim carries on into the text box.
	if e.u.handleKey(k) {
		return
	}
	switch k.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if e.shiftDown() {
			e.Entry.TypedKey(k) // newline
		} else {
			e.submit()
		}
		return
	case fyne.KeyTab:
		// Shift+Tab arrives here as a plain Tab: Fyne hands the focused widget
		// the key and the modifier separately, and a widget that AcceptsTab
		// gets both directions. Which one this is, is a question for the driver.
		if e.shiftDown() {
			e.complete(-1)
		} else {
			e.complete(1)
		}
		return
	case fyne.KeyUp:
		if strings.Contains(e.Text, "\n") {
			e.Entry.TypedKey(k) // navigating a multiline draft
			return
		}
		if e.histPos > 0 {
			e.cancelCompletion()
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
			e.cancelCompletion()
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
	e.cancelCompletion()
	e.Entry.TypedKey(k)
}

func (e *chatEntry) TypedRune(r rune) {
	e.cancelCompletion()
	e.Entry.TypedRune(r)
}

// cancelCompletion ends a Tab cycle and takes the candidate strip down with it.
// Every path that used to write `completions = nil` goes through here, because
// a strip left on screen after the cycle it describes is over is a lie about
// what Tab will do next.
func (e *chatEntry) cancelCompletion() {
	if len(e.completions) == 0 {
		return
	}
	e.completions = nil
	e.compIdx = 0
	if e.u != nil && e.u.candidates != nil {
		e.u.candidates.Clear()
	}
}

// complete runs the Tab cycle in a direction: +1 for Tab, -1 for Shift+Tab.
//
// The first press collects the candidates for the word before the cursor and
// takes the first one; every press after that steps the SAME list, forwards or
// backwards, wrapping at both ends. Backwards from the first candidate is the
// last one, which is what makes Shift+Tab useful for a list of thirty nicks -
// one keystroke to the end of it instead of twenty-nine.
//
// The row of candidates goes on screen for the whole cycle (§4.3's "visible
// candidate popup"), because a cycle you cannot see is a cycle you have to
// count in your head.
func (e *chatEntry) complete(dir int) {
	sess := e.u.session()
	if sess == nil {
		return
	}

	if len(e.completions) == 0 {
		runes := []rune(e.Text)
		cur := e.cursorOffset()
		if cur > len(runes) {
			cur = len(runes)
		}
		// the word being completed is the run of non-space characters
		// immediately before the cursor
		start := cur
		for start > 0 && runes[start-1] != ' ' && runes[start-1] != '\n' {
			start--
		}
		word := string(runes[start:cur])
		if word == "" {
			return
		}
		prefix := strings.ToLower(word)
		nicks := func(pfx, decorate string) {
			for _, m := range sess.SortedMembers(e.u.current()) {
				nick := strings.TrimLeft(m, "~&@%+")
				if strings.HasPrefix(strings.ToLower(nick), pfx) {
					e.completions = append(e.completions, decorate+nick)
				}
			}
		}
		switch {
		case start == 0 && strings.HasPrefix(word, "/"):
			// slash command at the start of the line
			e.completions = append(e.completions, slashMatches(prefix)...)
		case strings.HasPrefix(word, "#"):
			// channels: joined buffers first, then the last LIST scan
			seen := map[string]bool{}
			for _, b := range sess.Buffers() {
				if strings.HasPrefix(b.Name, "#") && strings.HasPrefix(strings.ToLower(b.Name), prefix) {
					e.completions = append(e.completions, b.Name)
					seen[strings.ToLower(b.Name)] = true
				}
			}
			for _, ci := range sess.ChannelList() {
				if strings.HasPrefix(strings.ToLower(ci.Name), prefix) && !seen[strings.ToLower(ci.Name)] {
					e.completions = append(e.completions, ci.Name)
				}
			}
		case strings.HasPrefix(word, "@"):
			// @mention: complete the nick, keep the @
			nicks(strings.TrimPrefix(prefix, "@"), "@")
		default:
			nicks(prefix, "")
		}
		if len(e.completions) == 0 {
			return
		}
		e.compIdx = 0
		if dir < 0 {
			// Shift+Tab as the FIRST press means "start at the other end",
			// which is the only reading of it that is not just Tab.
			e.compIdx = len(e.completions) - 1
		}
		e.compHead = string(runes[:start])
		e.compTail = string(runes[cur:])
	} else {
		n := len(e.completions)
		e.compIdx = ((e.compIdx+dir)%n + n) % n
	}
	e.showCandidates()

	word := e.completions[e.compIdx]
	suffix := " "
	if e.compHead == "" && !strings.ContainsAny(word[:1], "/#@") {
		suffix = ": " // addressing someone at the start of a message
	}
	e.SetText(e.compHead + word + suffix + e.compTail)
	if e.compTail == "" {
		e.cursorToEndExact()
	} else {
		e.setCursor(len([]rune(e.compHead + word + suffix)))
	}
}

// showCandidates puts the running cycle on the strip above the composer.
func (e *chatEntry) showCandidates() {
	if e.u == nil || e.u.candidates == nil {
		return
	}
	if len(e.completions) < 2 {
		// One candidate is not a cycle: the word is already completed and a
		// strip saying so is chrome for its own sake.
		e.u.candidates.Clear()
		return
	}
	e.u.candidates.Set(e.completions, e.compIdx)
}

// cursorToEndExact moves the cursor to the absolute end using Fyne's own
// key navigation, which is correct even across soft-wrapped rows where
// logical row/column math is not.
func (e *chatEntry) cursorToEndExact() {
	for i := 0; i < 32; i++ {
		e.Entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	}
	e.Entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
}

// cursorOffset converts the entry's cursor into a rune offset. With word
// wrap on, Fyne's CursorRow counts soft-wrapped VISUAL rows, which cannot
// be mapped back to text offsets from public API. Row 0 is always exact
// (a first visual row starts at offset 0); for anything beyond it we fall
// back to end-of-text, i.e. completing the final word, rather than
// corrupting the middle of a wrapped draft.
func (e *chatEntry) cursorOffset() int {
	if e.CursorRow == 0 {
		return e.CursorColumn
	}
	return len([]rune(e.Text))
}

// setCursor places the visible cursor at a rune offset.
func (e *chatEntry) setCursor(off int) {
	row, col := 0, 0
	for _, r := range e.Text {
		if off == 0 {
			break
		}
		off--
		if r == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	e.CursorRow, e.CursorColumn = row, col
	e.Refresh()
}

// slashMatches returns the commands whose verb starts with prefix, which
// carries the leading slash the user typed.
//
// It is a delegation and deliberately nothing more. v1 kept a hand-written
// slice here - the second source of truth M10 deleted, already missing /ctcp,
// /kickban and /userhost's neighbours by the time anyone looked - and even
// walking command.Verbs() here would leave a filter in this package that could
// be quietly replaced by a literal. There is no list of commands in the UI at
// all: the table answers the question.
func slashMatches(prefix string) []string { return command.Complete(prefix) }

// cursorToEnd places the cursor after the last character, correctly for
// multiline text (a recalled multiline history entry spans several rows).
func (e *chatEntry) cursorToEnd() {
	lines := strings.Split(e.Text, "\n")
	e.CursorRow = len(lines) - 1
	e.CursorColumn = len(lines[len(lines)-1])
	e.Refresh()
}
