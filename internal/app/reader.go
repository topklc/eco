package app

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/config"
)

// Reader mode — blueprint §4.2's copy stance, R-5, and risk #3.
//
// Fyne 2.8 cannot select text across two widgets, and the Timeline is one
// widget per message. That is a real limit and it is not hidden anywhere: a
// single message is copied from its own row (hover strip or right-click), and
// a run of them is copied here, from a plain-text view that behaves like any
// text box — drag to select, Ctrl+A, Ctrl+C, arrows and Shift+arrows. F6
// opens and closes it, so does the toolbar's copy button, and so does Escape.
// Nothing in eco claims native selection in the message pane, because there
// is none.
//
// Two things about it are deliberate and are said out loud in the docs.
//
// It is a still picture. Messages keep arriving while it is open; they are in
// the pane when it closes. Re-laying a text box out per batch would cost more
// than the whole Timeline does (measured: 126ms for one arriving message over
// 500 lines) and would eat the selection the user is in the middle of making.
//
// It shows the part of the buffer you were reading, not all of it. Fyne lays
// a text widget out in time quadratic in its rows: 500 lines take 0.1s,
// 2,000 take 2.4s and 10,000 take 44s (measured on both widget.Entry and a
// selectable widget.Label — it is the text layout, not the widget). So the
// window is what the Timeline says is on screen plus context either side,
// capped, and the header says exactly how much of the buffer that is when it
// is not all of it.
const (
	// readerContext is how many messages of run-up the window keeps on each
	// side of what was on screen.
	readerContext = 120
	// readerMax is the hard cap, chosen so opening stays under about a fifth
	// of a second on the dev box.
	readerMax = 500
)

// readerView is the text box itself: an ordinary multi-line entry, because
// that is the one thing in Fyne that can be selected with the keyboard as
// well as the mouse. It is editable for the same reason — a disabled Entry
// answers no key at all — and nothing ever reads it back: it is rebuilt from
// the buffer every time it opens.
type readerView struct {
	widget.Entry
	u *UI
}

func newReaderView(u *UI) *readerView {
	r := &readerView{u: u}
	r.MultiLine = true
	r.Wrapping = fyne.TextWrapWord
	// monospace on purpose: this is where a pasted table or a column of
	// server output lines up again, which is the one thing the M11(c)
	// typography gate took away from the message body
	r.TextStyle = fyne.TextStyle{Monospace: true}
	r.ExtendBaseWidget(r)
	return r
}

// TypedKey keeps the two keys that mean "put this away" working while the
// text box has focus. Everything else is the Entry's, which is the whole
// point of using one.
func (r *readerView) TypedKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyF6, fyne.KeyEscape:
		r.u.closeReader()
		return
	}
	r.Entry.TypedKey(k)
}

// handleKey is the app's bare-key handler: keys with no modifier, dispatched
// from the canvas and from the composer (which is where focus lives, so the
// composer must pass them through — blueprint §4.3). Modifier combos are
// shortcuts and go through handleShortcut instead.
func (u *UI) handleKey(k *fyne.KeyEvent) bool {
	switch k.Name {
	case fyne.KeyF6:
		u.toggleReader()
		return true
	case fyne.KeyEscape:
		if u.activeDialog != nil {
			u.activeDialog.Hide()
			return true
		}
		if u.readerOn {
			u.closeReader()
			return true
		}
	}
	return false
}

// toggleReader opens Reader mode over the chat pane, or closes it.
func (u *UI) toggleReader() {
	if u.readerOn {
		u.closeReader()
		return
	}
	u.readerOn = true
	u.fillReader()
	u.timeline.Hide()
	u.readerPane.Show()
	u.centerBox.Refresh()
	u.win.Canvas().Focus(u.reader)
}

// closeReader puts the chat pane back and returns the keyboard to the
// composer, which is where focus lives.
func (u *UI) closeReader() {
	if !u.readerOn {
		return
	}
	u.readerOn = false
	u.readerPane.Hide()
	u.reader.SetText("") // a closed reader holds no copy of the conversation
	u.timeline.Show()
	u.centerBox.Refresh()
	u.win.Canvas().Focus(u.input)
}

// refreshReader rebuilds an open Reader from the buffer under it. It is
// called where the buffer itself changed — a switch, a redaction — and not
// on arrival: a withdrawn message must not survive in a view someone can
// copy, and a stream of new ones must not fight the selection being made.
func (u *UI) refreshReader() {
	if !u.readerOn {
		return
	}
	u.fillReader()
}

// fillReader puts the window of the buffer into the text box and says how
// much of the buffer that is.
func (u *UI) fillReader() {
	lines := u.timeline.Lines()
	first, last := u.timeline.VisibleLines()
	lo, hi := readerWindow(len(lines), first, last)
	u.reader.SetText(readerText(lines[lo:hi]))
	// land the view where the reader was, rather than on the run-up above it
	if first > lo {
		u.reader.CursorRow = first - lo
	}
	u.reader.Refresh()

	if hi-lo == len(lines) {
		u.readerNote.Hide()
		return
	}
	u.readerNote.SetText(fmt.Sprintf(
		"Showing %d of %d messages — the ones around where you were reading. Esc or F6 closes.",
		hi-lo, len(lines)))
	u.readerNote.Show()
}

// readerWindow is which slice of a buffer Reader mode shows: what was on
// screen, widened by the run-up, and never more than the cap. Split out
// because it is arithmetic with four edge cases and no widgets.
func readerWindow(n, first, last int) (lo, hi int) {
	if n == 0 {
		return 0, 0
	}
	if last <= first { // nothing on screen: the newest messages
		first, last = n-1, n
	}
	lo, hi = first-readerContext, last+readerContext
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if hi-lo > readerMax {
		lo = hi - readerMax
	}
	return lo, hi
}

// readerText is the buffer as text: the timestamp the user chose, the nick in
// the IRC convention, and the message with its formatting codes stripped.
// This is also what a per-row Copy puts on the clipboard, so it is what a log
// ought to look like pasted into anything else.
func readerText(lines []Line) string {
	layout := config.Current().TimeLayout
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.Time.Local().Format(layout))
		sb.WriteString("  ")
		switch {
		case l.Status:
			sb.WriteString("* ")
		case l.Action:
			sb.WriteString("* " + l.Nick + " ")
		case l.Nick != "":
			sb.WriteString("<" + l.Nick + "> ")
		}
		sb.WriteString(ircfmt.Strip(l.Text))
		sb.WriteString("\n")
	}
	return sb.String()
}

// newReaderPane assembles the pane: the note above, the text below.
func (u *UI) newReaderPane() *fyne.Container {
	u.reader = newReaderView(u)
	u.readerNote = widget.NewLabel("")
	u.readerNote.TextStyle = fyne.TextStyle{Italic: true}
	u.readerNote.Hide()
	pane := container.NewBorder(u.readerNote, nil, nil, nil, u.reader)
	pane.Hide()
	return pane
}
