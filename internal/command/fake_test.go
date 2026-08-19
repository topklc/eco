package command

import (
	"fmt"
	"strconv"
	"strings"
)

// fakeEnv is the whole world a command gets in these tests. Every call it can
// make is recorded, in order, as one line of text — so a command's entire
// effect is a slice comparison, and a command that quietly does something extra
// (a second write, a hint nobody asked for) fails.
//
// It implements all three of Env's interfaces itself. That is the point of the
// env being narrow: the fake is one struct, there is no window, no store, no
// socket, and nothing here needs a goroutine.
type fakeEnv struct {
	log []string

	// offline makes Wire() nil (a session with no connection), sessionless
	// makes both Session() and Wire() nil (no network at all).
	offline     bool
	sessionless bool

	caps    map[string]bool
	nick    string
	ignores []string
}

func (f *fakeEnv) add(format string, args ...any) {
	f.log = append(f.log, fmt.Sprintf(format, args...))
}

// wire records one write to the server. Parameters that are empty or contain a
// space are quoted, so "KICK #eco bob" and "KICK #eco bob \"\"" cannot be
// confused for each other.
func (f *fakeEnv) wire(verb string, params ...string) error {
	out := make([]string, 0, len(params)+1)
	out = append(out, verb)
	for _, p := range params {
		if p == "" || strings.ContainsAny(p, " \"") {
			p = strconv.Quote(p)
		}
		out = append(out, p)
	}
	f.log = append(f.log, strings.Join(out, " "))
	return nil
}

func (f *fakeEnv) Session() Session {
	if f.sessionless {
		return nil
	}
	return f
}

func (f *fakeEnv) Wire() Wire {
	if f.sessionless || f.offline {
		return nil
	}
	return fakeWire{f}
}

// fakeWire records the writes to the server. It is its own type because a
// single fake cannot have Quit mean both "QUIT this connection" and "close the
// app" — which is exactly the split the real program has, where Wire is
// *irc.Client and UIHooks is the window.
type fakeWire struct{ env *fakeEnv }

func (f *fakeEnv) UI() UIHooks { return f }

// --- Session ------------------------------------------------------------------

func (f *fakeEnv) Send(buffer, text string)   { f.add("send %s %q", buffer, text) }
func (f *fakeEnv) Action(buffer, text string) { f.add("action %s %q", buffer, text) }
func (f *fakeEnv) Join(channel, key string)   { f.add("join %s key=%q", channel, key) }
func (f *fakeEnv) Part(channel string)        { f.add("part %s", channel) }
func (f *fakeEnv) Cycle(channel string)       { f.add("cycle %s", channel) }
func (f *fakeEnv) Close(buffer string)        { f.add("closebuffer %s", buffer) }
func (f *fakeEnv) Clear(buffer string)        { f.add("clear %s", buffer) }
func (f *fakeEnv) Whois(from, nick string)    { f.add("whois %s from=%s", nick, from) }
func (f *fakeEnv) List()                      { f.add("list") }
func (f *fakeEnv) FetchHistory(buffer string) { f.add("fetchhistory %s", buffer) }
func (f *fakeEnv) HasCap(name string) bool    { return f.caps[name] }
func (f *fakeEnv) Ignored() []string          { return f.ignores }

func (f *fakeEnv) SetIgnore(nick string, on bool) { f.add("setignore %s on=%v", nick, on) }

func (f *fakeEnv) Nick() string {
	if f.nick == "" {
		return "eco"
	}
	return f.nick
}

// --- Wire ---------------------------------------------------------------------

func (w fakeWire) SetNick(nick string) error  { return w.env.wire("NICK", nick) }
func (w fakeWire) Topic(channel string) error { return w.env.wire("TOPIC", channel) }
func (w fakeWire) SetTopic(channel, topic string) error {
	return w.env.wire("TOPIC", channel, topic)
}
func (w fakeWire) Away(reason string) error          { return w.env.wire("AWAY", reason) }
func (w fakeWire) Notice(target, text string) error  { return w.env.wire("NOTICE", target, text) }
func (w fakeWire) Privmsg(target, text string) error { return w.env.wire("PRIVMSG", target, text) }
func (w fakeWire) Ctcp(target, request string) error { return w.env.wire("CTCP", target, request) }
func (w fakeWire) Names(channel string) error        { return w.env.wire("NAMES", channel) }
func (w fakeWire) Invite(nick, channel string) error { return w.env.wire("INVITE", nick, channel) }
func (w fakeWire) SetName(realname string) error     { return w.env.wire("SETNAME", realname) }
func (w fakeWire) Verify(account, code string) error { return w.env.wire("VERIFY", account, code) }
func (w fakeWire) Oper(name, password string) error  { return w.env.wire("OPER", name, password) }
func (w fakeWire) Who(target string) error           { return w.env.wire("WHO", target) }
func (w fakeWire) Whowas(nick string) error          { return w.env.wire("WHOWAS", nick) }
func (w fakeWire) Knock(channel string) error        { return w.env.wire("KNOCK", channel) }
func (w fakeWire) Wallops(text string) error         { return w.env.wire("WALLOPS", text) }
func (w fakeWire) Kill(nick, reason string) error    { return w.env.wire("KILL", nick, reason) }
func (w fakeWire) Quit(reason string) error          { return w.env.wire("QUIT", reason) }
func (w fakeWire) SendRaw(line string) error         { return w.env.wire("RAW", line) }

func (w fakeWire) Mode(target string, args ...string) error {
	return w.env.wire("MODE", append([]string{target}, args...)...)
}
func (w fakeWire) Kick(channel, nick, reason string) error {
	return w.env.wire("KICK", channel, nick, reason)
}
func (w fakeWire) Redact(target, msgid, reason string) error {
	return w.env.wire("REDACT", target, msgid, reason)
}
func (w fakeWire) Register(account, email, password string) error {
	return w.env.wire("REGISTER", account, email, password)
}

// ServerQuery upper-cases its verb exactly as internal/irc does, so what a test
// asserts is what would go on the wire.
func (w fakeWire) ServerQuery(command string, params ...string) error {
	return w.env.wire(strings.ToUpper(command), params...)
}

// --- UIHooks ------------------------------------------------------------------

func (f *fakeEnv) OpenBuffer(name string)    { f.add("ui open %s", name) }
func (f *fakeEnv) ShowHint(text string)      { f.add("hint %s", text) }
func (f *fakeEnv) Notify(title, body string) { f.add("ui notify %s: %s", title, body) }
func (f *fakeEnv) Refresh()                  { f.add("ui refresh") }
func (f *fakeEnv) Disconnect()               { f.add("ui disconnect") }
func (f *fakeEnv) Quit()                     { f.add("ui quit") }

func (f *fakeEnv) ShowDialog(d Dialog) {
	switch d {
	case DialogNetworks:
		f.add("ui dialog networks")
	case DialogHelp:
		f.add("ui dialog help")
	default:
		f.add("ui dialog %d", int(d))
	}
}

// the three places a command can be typed from
var (
	inChannel = Context{Buffer: "#eco"}
	inQuery   = Context{Buffer: "alice"}
	inServer  = Context{Buffer: "server", Server: true}
)
