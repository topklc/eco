package app

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/command"
	"github.com/toprakkilic/eco/internal/irc"
)

// handleInput processes what the user typed into the message box.
//
// There is no switch here any more. internal/command parses the line against
// the one table and runs it against the narrow env below - which is the whole
// of what a command can touch: the session on screen, its connection, and seven
// screen effects. The table is also what Tab completion and /help read, so a
// command cannot exist without being completable and documented (M10).
//
// The session and buffer a command applies to are the ones on screen: they are
// read once, here, rather than re-asked per command.
func (u *UI) handleInput(text string) {
	sess, cur := u.session(), u.current()
	ctx := command.Context{Buffer: cur, Server: cur == serverBuffer}
	command.Run(command.Parse(text, ctx), &cmdEnv{u: u, s: sess, buffer: cur})
}

// cmdEnv is internal/command's Env for this window. It is the ONLY door between
// the command table and the app: everything a command does arrives through one
// of the three interfaces below, which is what makes the table testable with no
// window, no store and no socket (see internal/command's tests).
type cmdEnv struct {
	u *UI
	s *Session // the session on screen, nil when there is no network
	// buffer is where the command was typed, and so where its hints go. It is
	// captured with the session rather than re-read, so a command that changes
	// the buffer on screen (/msg) cannot land its own usage error somewhere the
	// user was not looking.
	buffer string
}

// Session hands the command layer the chat side of this network, or nil when
// there is no network at all.
func (e *cmdEnv) Session() command.Session {
	if e.s == nil {
		return nil
	}
	return sessionCmds{u: e.u, s: e.s}
}

// Wire hands over the connection, or nil when there is none - which is what
// Command.NeedsConn is checked against. *irc.Client implements the interface
// itself: every wire write a command makes is a method in internal/irc.
func (e *cmdEnv) Wire() command.Wire {
	if e.s == nil {
		return nil
	}
	c := e.s.wireClient()
	if c == nil {
		return nil
	}
	return c
}

// UI is this window.
func (e *cmdEnv) UI() command.UIHooks { return e }

// wireIsTheClient states the layering: if internal/irc stops covering what the
// table needs, the verb is added there, not worked around here.
var _ command.Wire = (*irc.Client)(nil)

// --- UIHooks ------------------------------------------------------------------

// OpenBuffer creates a conversation if it does not exist and shows it (/msg).
func (e *cmdEnv) OpenBuffer(name string) {
	if e.s == nil {
		return
	}
	e.s.getBuffer(name)
	e.u.selectBuffer(name)
}

// ShowDialog opens one of the app's windows.
func (e *cmdEnv) ShowDialog(d command.Dialog) {
	switch d {
	case command.DialogNetworks:
		e.u.ShowNetworkManager()
	case command.DialogHelp:
		e.u.showHelp()
	}
}

// ShowHint puts one of the client's own sentences in the buffer the command was
// typed in. With no session there is nowhere to put it, and nothing is lost:
// the only commands that run without one are the two that open a window.
func (e *cmdEnv) ShowHint(text string) {
	if e.s == nil {
		return
	}
	e.s.status(e.buffer, text)
}

// Notify raises a desktop notification (/testnotify).
func (e *cmdEnv) Notify(title, body string) { e.u.notify(title, body) }

// Refresh repaints the buffer on screen (/clear).
func (e *cmdEnv) Refresh() { e.u.renderBuffer() }

// Disconnect drops this network and leaves the app running.
func (e *cmdEnv) Disconnect() {
	if e.s == nil {
		return
	}
	e.u.closeSession(e.s)
}

// quitFlush is how long the QUIT lines are given to reach the server before the
// process exits. Closing the socket first would make the user appear to time
// out rather than to leave, which is why v1 waited and why this still does.
const quitFlush = 300 * time.Millisecond

// closeApp closes the application. It is a variable, not a call, so a headless
// test can watch WHEN it happens: the wait above is the whole behaviour of the
// hook below, and a test that cannot see the close cannot see the wait.
var closeApp = func(a fyne.App) { fyne.Do(func() { a.Quit() }) }

// Quit parts every network and closes the window.
//
// The delay is the point: QUIT has just been written, and exiting the process
// immediately would close the socket before it reached the server. This is
// v1's 300ms, kept where it belongs - the command says "quit", the window knows
// what quitting costs.
func (e *cmdEnv) Quit() {
	for _, s := range e.u.sessions {
		s.Quit()
	}
	app := e.u.app
	if app == nil {
		return // a window built without one: the headless tests. Nothing to close.
	}
	go func() {
		time.Sleep(quitFlush)
		closeApp(app)
	}()
}

// --- Session ------------------------------------------------------------------

// sessionCmds adapts one Session to internal/command's Session interface. It is
// a translation and nothing else: no decision lives in here.
type sessionCmds struct {
	u *UI
	s *Session
}

func (c sessionCmds) Send(buffer, text string)   { c.s.SendMessage(buffer, text) }
func (c sessionCmds) Action(buffer, text string) { c.s.SendAction(buffer, text) }
func (c sessionCmds) Join(channel, key string)   { c.s.JoinWithKey(channel, key) }
func (c sessionCmds) Part(channel string)        { c.s.Part(channel) }
func (c sessionCmds) Cycle(channel string)       { c.s.Cycle(channel) }
func (c sessionCmds) Close(buffer string)        { c.s.CloseBuffer(buffer) }
func (c sessionCmds) Clear(buffer string)        { c.s.ClearBuffer(buffer) }
func (c sessionCmds) Whois(from, nick string)    { c.s.Whois(from, nick) }
func (c sessionCmds) List()                      { c.s.RequestList() }
func (c sessionCmds) FetchHistory(buffer string) { c.s.FetchHistory(buffer) }
func (c sessionCmds) Nick() string               { return c.s.CurrentNick() }
func (c sessionCmds) HasCap(name string) bool    { return c.s.hasCap(name) }

// SetIgnore edits the client-side ignore list, which is a preference and so
// belongs to the app rather than to the connection.
func (c sessionCmds) SetIgnore(nick string, on bool) {
	setIgnore(c.u.app.Preferences(), nick, on)
}

// Ignored lists the ignored nicks, sorted.
func (c sessionCmds) Ignored() []string { return ignoredList() }
