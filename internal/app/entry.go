package app

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
	compHead    string // text before the word being completed
	compTail    string // text after the cursor, preserved across the cycle

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
			for _, c := range slashCommands {
				if strings.HasPrefix(c, prefix) {
					e.completions = append(e.completions, c)
				}
			}
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
		e.compHead = string(runes[:start])
		e.compTail = string(runes[cur:])
	} else {
		e.compIdx = (e.compIdx + 1) % len(e.completions)
	}

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

// slashCommands is the Tab-completion list: everything handleCommand
// accepts, with the leading slash.
var slashCommands = []string{
	"/admin", "/away", "/back", "/ban", "/clear", "/close", "/cs", "/ctcp",
	"/cycle", "/dehop", "/deop", "/devoice", "/disconnect", "/help",
	"/history", "/hop", "/hs", "/identify", "/info", "/invite", "/ison",
	"/join", "/kick", "/kickban", "/kill", "/knock", "/links", "/list",
	"/lusers", "/map", "/me", "/mode", "/monitor", "/motd", "/msg",
	"/names", "/nick", "/notice", "/ns", "/op", "/oper", "/part", "/query",
	"/quit", "/raw", "/redact", "/register", "/rejoin", "/rules", "/server",
	"/setname", "/stats", "/time", "/topic", "/unban", "/userhost",
	"/verify", "/version", "/voice", "/wallops", "/who", "/whois", "/whowas",
}

// cursorToEnd places the cursor after the last character, correctly for
// multiline text (a recalled multiline history entry spans several rows).
func (e *chatEntry) cursorToEnd() {
	lines := strings.Split(e.Text, "\n")
	e.CursorRow = len(lines) - 1
	e.CursorColumn = len(lines[len(lines)-1])
	e.Refresh()
}
