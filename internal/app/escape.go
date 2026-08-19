package app

// The Escape priority stack — blueprint §4.3, dispatched from one place.
//
// Escape means "put the topmost thing away", and the whole difficulty is that
// "topmost" is a claim about five different widgets that do not know about each
// other. v1 had one `activeDialog` slot and a chain of ifs, which is why a
// dialog opened from a dialog closed the wrong one and why Escape in the
// scrollback sometimes closed a popup instead of coming back to the present.
//
// So the order is a value, decided by a pure function over what is open, and
// the acting on it is three lines below that. The table test is the spec
// (escape_test.go): every combination of open things has exactly one answer.

// escLayer is one rung of the stack. They are declared in priority order and
// the zero value means "nothing to dismiss", so a new layer is added by putting
// it in the right place here and handling it in dismiss.
type escLayer uint8

const (
	// escNone: Escape does nothing.
	escNone escLayer = iota
	// escDialog: the topmost dialog. It is a STACK, not v1's single slot — a
	// dialog opened from a dialog must close itself first.
	escDialog
	// escCompletion: the composer is mid tab-cycle. Escape cancels the cycle
	// and leaves the text alone.
	escCompletion
	// escOverlay: the Switcher, the Finder, or the in-buffer search bar.
	escOverlay
	// escReader: Reader mode (F6). It is not in §4.3's list because §4.3 lists
	// the overlays; it goes here rather than above them because an overlay
	// opened OVER the reader is the newer thing on screen.
	escReader
	// escBottom: nothing is open and the reader is up in the scrollback —
	// Escape is "back to the present", the same as End.
	escBottom
)

// escState is everything the order depends on, and nothing else. It is a value
// so the decision can be tested without a window.
type escState struct {
	// Dialogs is how many are open. Only "any" matters, but the count is what
	// the caller has and reads better at the call site than a bool.
	Dialogs int
	// Completing reports an unfinished Tab cycle in the composer.
	Completing bool
	// Overlay reports the Switcher, the Finder or the search bar being open.
	Overlay bool
	// Reader reports Reader mode being open.
	Reader bool
	// ScrolledUp reports that the Timeline is not following the newest message.
	ScrolledUp bool
}

// escapeTarget is the whole of the priority rule.
func escapeTarget(s escState) escLayer {
	switch {
	case s.Dialogs > 0:
		return escDialog
	case s.Completing:
		return escCompletion
	case s.Overlay:
		return escOverlay
	case s.Reader:
		return escReader
	case s.ScrolledUp:
		return escBottom
	}
	return escNone
}

// escapeState reads the window's current answer to each rung.
func (u *UI) escapeState() escState {
	s := escState{Dialogs: len(u.dialogs), Reader: u.readerOn}
	if u.input != nil {
		s.Completing = len(u.input.completions) > 0
	}
	s.Overlay = u.overlayOpen()
	if u.timeline != nil {
		s.ScrolledUp = !u.timeline.NearBottom()
	}
	return s
}

// handleEscape dismisses the topmost thing, and reports whether there was one.
// It is the ONLY place Escape is acted on.
func (u *UI) handleEscape() bool {
	switch escapeTarget(u.escapeState()) {
	case escDialog:
		u.dialogs[len(u.dialogs)-1].Hide()
	case escCompletion:
		u.input.cancelCompletion()
	case escOverlay:
		u.closeOverlays()
	case escReader:
		u.closeReader()
	case escBottom:
		u.timeline.ScrollToBottom()
		u.focusComposer()
	default:
		return false
	}
	return true
}

// --- the dialog stack ----------------------------------------------------------

// dialogHandle is what this layer needs of a dialog: a way to close it and a
// way to be told it closed.
type dialogHandle interface {
	Hide()
	SetOnClosed(func())
}

// trackDialog pushes a dialog onto the stack so Escape can close the topmost
// one, and pops it again however it was closed — its own button, Escape, or
// code. v1 kept ONE dialog here, so a form opened from a browser left the
// browser unclosable and the form leaked out of the slot on the way out.
func (u *UI) trackDialog(d dialogHandle) {
	u.dialogs = append(u.dialogs, d)
	d.SetOnClosed(func() { u.untrackDialog(d) })
}

// untrackDialog removes one dialog wherever it sits in the stack — not just off
// the top, because dialogs can be dismissed in any order.
func (u *UI) untrackDialog(d dialogHandle) {
	for i, x := range u.dialogs {
		if x == d {
			u.dialogs = append(u.dialogs[:i], u.dialogs[i+1:]...)
			return
		}
	}
}

// --- the one focus-restore helper -----------------------------------------------

// focusComposer is §4.1's "every close restores composer focus through one
// shared helper". Every overlay's close path, the reader's, and Escape's all
// end here, so there is exactly one answer to "where does the keyboard go
// afterwards" and no surface can forget to give it back.
func (u *UI) focusComposer() {
	if u.input == nil || u.win == nil {
		return
	}
	u.win.Canvas().Focus(u.input)
}
