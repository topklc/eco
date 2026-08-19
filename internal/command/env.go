package command

// Env is everything a command may touch. It is deliberately three small
// interfaces and nothing else: the command table cannot see the window, the
// store, the archive or the IRC library, so a fake env is a struct with a
// handful of methods and every command below is a unit test.
//
// Session and Wire may both be nil, and the difference matters:
//   - Session is nil when there is no network on screen at all,
//   - Wire is nil when there is a session but no live connection.
//
// [Command.NeedsConn] is checked against Wire, which is why no command has to
// ask.
type Env interface {
	// Session is the network the command was typed at: its buffers, its
	// sending, the client-side lists that belong to it.
	Session() Session
	// Wire is the connection. Every write to the server goes through it, and
	// its implementation lives in internal/irc.
	Wire() Wire
	// UI is the handful of screen effects a command may cause. Never nil.
	UI() UIHooks
}

// Session is the chat side of one network: the operations that mean something
// to the local model as well as to the server (opening and closing buffers,
// sending with a local echo, correlating a reply with the buffer that asked).
// Anything that is only a write goes to [Wire] instead.
type Session interface {
	// Send sends the composer's text to a buffer and echoes it locally.
	Send(buffer, text string)
	// Action sends a /me to a buffer.
	Action(buffer, text string)
	// Join joins a channel, with a key when one was given.
	Join(channel, key string)
	// Part leaves a channel and closes its buffer.
	Part(channel string)
	// Cycle leaves and rejoins a channel, keeping the buffer and its scrollback.
	Cycle(channel string)
	// Close removes a buffer without leaving anything.
	Close(buffer string)
	// Clear wipes a buffer's rows client-side.
	Clear(buffer string)
	// Whois asks about a nick; the reply lands in the buffer that asked.
	Whois(from, nick string)
	// List asks the server for its channel list on the user's behalf.
	List()
	// FetchHistory asks for the newest page of a buffer's history.
	FetchHistory(buffer string)
	// Nick is the nickname the server currently knows us by.
	Nick() string
	// HasCap reports whether a capability is in force on this connection.
	HasCap(name string) bool
	// SetIgnore adds or removes a nick from the client-side ignore list.
	SetIgnore(nick string, on bool)
	// Ignored lists the ignored nicks, sorted.
	Ignored() []string
}

// Wire is every write to the server the command table can make. It is
// implemented by *irc.Client itself — the table formats no protocol, which is
// what keeps ircevent behind internal/irc.
type Wire interface {
	SetNick(nick string) error
	Topic(channel string) error
	SetTopic(channel, topic string) error
	Away(reason string) error
	Notice(target, text string) error
	Privmsg(target, text string) error
	Ctcp(target, request string) error
	Mode(target string, args ...string) error
	Kick(channel, nick, reason string) error
	Invite(nick, channel string) error
	Names(channel string) error
	Redact(target, msgid, reason string) error
	Register(account, email, password string) error
	Verify(account, code string) error
	SetName(realname string) error
	Oper(name, password string) error
	Who(target string) error
	Whowas(nick string) error
	Knock(channel string) error
	Wallops(text string) error
	Kill(nick, reason string) error
	Quit(reason string) error
	// ServerQuery issues one of the informational commands whose reply is
	// numerics (MOTD, TIME, VERSION, STATS, …).
	ServerQuery(command string, params ...string) error
	// SendRaw writes an already-formatted protocol line: /raw, and the way an
	// unknown command is sent on purpose after the intercept.
	SendRaw(line string) error
}

// Dialog names one of the app's windows a command can open. It is an enum
// rather than a callback so this package names what it wants without knowing
// how any of it is drawn.
type Dialog int

const (
	// DialogNetworks is the network manager (/server).
	DialogNetworks Dialog = iota
	// DialogHelp is the documentation window (/help).
	DialogHelp
)

// UIHooks is what a command may do to the screen. Everything here is an
// effect, not a question: a command cannot read the UI, only nudge it.
type UIHooks interface {
	// OpenBuffer creates a buffer if it does not exist and shows it.
	OpenBuffer(name string)
	// ShowDialog opens one of the app's windows.
	ShowDialog(d Dialog)
	// ShowHint puts one of the client's own sentences in front of the user, in
	// the buffer the command was typed in. Usage errors, confirmations and the
	// unknown-command intercept all arrive this way.
	ShowHint(text string)
	// Notify raises a desktop notification (/testnotify).
	Notify(title, body string)
	// Refresh repaints the buffer on screen after a command changed it under
	// the UI's feet (/clear).
	Refresh()
	// Disconnect drops the session on screen but leaves the app running.
	Disconnect()
	// Quit parts every network and closes the app.
	Quit()
}
