// Package viewmodel is the application state the window renders: which buffer
// the viewer is on, what the user has typed but not sent, what still wants
// their attention, and which rows the sidebar puts where.
//
// It holds no widgets. That is the whole point of the package: before M8 all of
// this lived in fields of internal/app's UI struct, next to the *widget.List
// that drew it, so the only way to ask "does a rename move the draft with it?"
// was to run a Fyne canvas and look. Everything here is answerable in a table
// test with no toolkit in the process.
//
// # What it owns, and what it does not
//
// It owns: the sidebar's rows (live sessions, their buffers, and the pinned
// profiles that have no session yet), the buffer on screen, the draft slot and
// which buffer owns it, the attention counts, the roster the member pane draws,
// and whether the message pane is following the tail.
//
// It does not own: the model (internal/state, behind internal/app's Session),
// the pixels (internal/app), or the connection. It never dials, never writes to
// a socket and never touches the store — Hooks is the one narrow way out, and
// it carries exactly one effect (see Hooks.MarkRead).
//
// # Threading
//
// One thread: the Fyne thread. Sink deliveries arrive there (internal/ui/dispatch
// makes that true), and every widget callback runs there, so nothing here is
// locked. A Model must not be touched from a session goroutine.
//
// # Unread is the store's, not this package's
//
// There is exactly one derivation of "does this message count as unread" in the
// program and it is state.countsUnread, evaluated against the buffer's read
// marker. This package holds the number the store last reported (SetUnread) and
// applies §4.4's last evaluation step — suppression while the buffer is on
// screen — when it hands a row to the sidebar. It never counts a line itself.
//
// Until M8 internal/app derived its own counts from Line flags while the store
// kept counters nothing read, which is the divergence the M7 verification
// recorded: a mutation that broke state.countsUnread left internal/app's suite
// green because internal/app was not asking. It asks now.
//
// The marker is what zeroes a count, so this package asks for it to move
// (Hooks.MarkRead) whenever the user shows a buffer or leaves one, and treats
// the zero as already applied — the round trip through the store and back
// through the funnel is one coalescing tick away, and a badge that lingers for
// a tick after the click is a badge that flickers.
package viewmodel
