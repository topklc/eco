package command

import (
	"sort"
	"strings"
)

// Command is one entry in the table: everything the program needs to know about
// a verb, in one place. Tab completion, the generated help and the
// unknown-command intercept all read these fields, so a command is added or
// changed here and nowhere else.
type Command struct {
	// Name is the canonical verb without its slash.
	Name string
	// Aliases are the other spellings that reach this entry.
	Aliases []string
	// Help is one line, lower-case, imperative: what the command does.
	Help string
	// Group is where the command appears in the generated help.
	Group Group
	// Args is the shape of the arguments, and the usage line when they are
	// missing.
	Args ArgSpec
	// NeedsConn refuses the command when there is no live connection, which is
	// what every command that writes to the server or reads the session needs.
	// A command that clears it must be runnable with no network at all, and so
	// may touch UIHooks only.
	NeedsConn bool
	// NeedsChannel refuses the command outside a channel buffer. It is for the
	// commands whose whole meaning is "this channel": /kick, /op, /cycle. A
	// command that can be given an explicit target (/part #other, /mode #other)
	// checks for itself instead.
	NeedsChannel bool
	// Hidden keeps a command out of the generated help and out of Tab
	// completion. It still runs when typed.
	Hidden bool
	// Run does it. It is called only after the guards above have passed and
	// after Args.Min/Max were satisfied, so it may index Args.Fields up to Min
	// and call Env.Wire without checking either.
	Run func(in Input, e Env)
}

// Verb is the command as the user types it, with its slash.
func (c *Command) Verb() string { return "/" + c.Name }

// Usage is the sentence shown when the arguments are wrong. It is generated
// from the spec, so it cannot drift from what the parser actually accepts.
func (c *Command) Usage() string {
	if c.Args.Usage == "" {
		return "Usage: " + c.Verb()
	}
	return "Usage: " + c.Verb() + " " + c.Args.Usage
}

// Describe is one command's help line: how to type it, what it does, and what
// else it can be called.
func (c *Command) Describe() string {
	line := c.Verb()
	if c.Args.Usage != "" {
		line += " " + c.Args.Usage
	}
	line += " - " + c.Help
	if len(c.Aliases) > 0 {
		line += " (also /" + strings.Join(c.Aliases, ", /") + ")"
	}
	return line
}

// Group is where a command appears in the generated help.
type Group int

const (
	GroupChat Group = iota
	GroupYou
	GroupOps
	GroupInfo
	GroupServices
	GroupOper
	GroupApp
)

// groupNames are the headings the help is built from, in order.
var groupNames = []string{
	GroupChat:     "Chat",
	GroupYou:      "You",
	GroupOps:      "Channel operators",
	GroupInfo:     "Information",
	GroupServices: "Services",
	GroupOper:     "Server operators",
	GroupApp:      "Application",
}

// String names a group for the help.
func (g Group) String() string {
	if int(g) < len(groupNames) {
		return groupNames[g]
	}
	return "Other"
}

// asChannel gives a bare name the "#" a user leaves off. It only knows about
// "#", exactly as v1 did: guessing a "&" or "!" prefix would rewrite a name the
// user typed on purpose.
func asChannel(name string) string {
	if name == "" || strings.HasPrefix(name, "#") {
		return name
	}
	return "#" + name
}

// prefixCmd builds one of the mode-prefix commands (/op, /voice, …), which all
// do the same thing to a list of nicks: "+o" for three nicks is "+ooo".
func prefixCmd(name, mode, help string) *Command {
	return &Command{
		Name: name, Help: help, Group: GroupOps,
		NeedsConn: true, NeedsChannel: true,
		Args: ArgSpec{Usage: "nick [nick...]", Words: AllWords, Min: 1},
		Run: func(in Input, e Env) {
			nicks := in.Args.Fields
			flag := string(mode[0]) + strings.Repeat(string(mode[1]), len(nicks))
			_ = e.Wire().Mode(in.Ctx.Buffer, append([]string{flag}, nicks...)...)
		},
	}
}

// queryCmd builds one of the informational commands that take no arguments and
// answer in numerics.
func queryCmd(name, help string) *Command {
	return &Command{
		Name: name, Help: help, Group: GroupInfo, NeedsConn: true,
		Run: func(in Input, e Env) { _ = e.Wire().ServerQuery(name) },
	}
}

// queryArgsCmd is queryCmd for the ones that take parameters.
func queryArgsCmd(name, usage, help string) *Command {
	return &Command{
		Name: name, Help: help, Group: GroupInfo, NeedsConn: true,
		Args: ArgSpec{Usage: usage, Words: AllWords},
		Run:  func(in Input, e Env) { _ = e.Wire().ServerQuery(name, in.Args.Fields...) },
	}
}

// serviceCmd builds one of the services shortcuts: /ns is a PRIVMSG to
// NickServ, and nothing more.
func serviceCmd(name, service, help string) *Command {
	return &Command{
		Name: name, Help: help, Group: GroupServices, NeedsConn: true,
		Args: ArgSpec{Usage: "[command]"},
		Run:  func(in Input, e Env) { _ = e.Wire().Privmsg(service, in.Args.Raw) },
	}
}

// table is THE list of commands. Its order is the order the help prints in.
//
// Every guard the engineering audit added is in here as data or as the first
// lines of a Run: /part refuses a non-channel before anything reaches the wire,
// /join gives a bare name its "#", /cycle keeps the buffer, /history refuses
// the network log, /quit flushes before the window closes (in the UI hook,
// which is where the delay belongs).
//
// It is filled in init and not from a variable's own expression because /help's
// entry reaches back into the table - it can explain any other command - and
// the initialisation-order analysis reads that, correctly, as a cycle.
var table []*Command

func init() {
	table = []*Command{
		// --- chat -----------------------------------------------------------------
		{
			Name: "join", Aliases: []string{"j"}, Group: GroupChat, NeedsConn: true,
			Help: "join a channel",
			Args: ArgSpec{Usage: "#channel [key]", Words: 1, Min: 1},
			Run: func(in Input, e Env) {
				e.Session().Join(asChannel(in.Args.Field(0)), strings.TrimSpace(in.Args.Rest))
			},
		},
		{
			Name: "part", Aliases: []string{"leave"}, Group: GroupChat, NeedsConn: true,
			Help: "leave a channel",
			Args: ArgSpec{Usage: "[#channel]"},
			Run: func(in Input, e Env) {
				target := in.Ctx.Buffer
				if in.Args.Raw != "" {
					target = asChannel(in.Args.Raw)
				}
				// Only a channel can be parted. Without this, /part in the network
				// log or in a private conversation asks the server to leave
				// something it has never heard of, and the 442/403 refusal path
				// then destroys that buffer.
				if !strings.HasPrefix(target, "#") {
					e.UI().ShowHint("Usage: /part [#channel]")
					return
				}
				e.Session().Part(target)
			},
		},
		{
			Name: "close", Group: GroupChat, NeedsConn: true,
			Help: "close this buffer, leaving the channel if it is one",
			Run: func(in Input, e Env) {
				if in.Ctx.Server {
					return // the network log is not closeable
				}
				if in.Ctx.IsChannel() {
					e.Session().Part(in.Ctx.Buffer)
					return
				}
				e.Session().Close(in.Ctx.Buffer)
			},
		},
		{
			Name: "msg", Aliases: []string{"query", "q"}, Group: GroupChat, NeedsConn: true,
			Help: "open a private conversation, and send a message if you give one",
			Args: ArgSpec{Usage: "nick [message]", Words: 1, Min: 1},
			Run: func(in Input, e Env) {
				target := in.Args.Field(0)
				e.UI().OpenBuffer(target)
				if in.Args.Rest != "" {
					e.Session().Send(target, in.Args.Rest)
				}
			},
		},
		{
			Name: "me", Group: GroupChat, NeedsConn: true,
			Help: "send an action, as in \"* you wave\"",
			Args: ArgSpec{Usage: "does something", Min: 1},
			Run:  func(in Input, e Env) { e.Session().Action(in.Ctx.Buffer, in.Args.Raw) },
		},
		{
			Name: "notice", Group: GroupChat, NeedsConn: true,
			Help: "send a notice, which most clients show differently from a message",
			Args: ArgSpec{Usage: "target message", Words: 1, Min: 2},
			Run:  func(in Input, e Env) { _ = e.Wire().Notice(in.Args.Field(0), in.Args.Rest) },
		},
		{
			Name: "ctcp", Group: GroupChat, NeedsConn: true,
			Help: "ask another client a question, such as VERSION or PING",
			Args: ArgSpec{Usage: "nick request", Words: 1, Min: 2},
			Run:  func(in Input, e Env) { _ = e.Wire().Ctcp(in.Args.Field(0), in.Args.Rest) },
		},
		{
			Name: "clear", Group: GroupChat, NeedsConn: true,
			Help: "clear this buffer on screen",
			Run: func(in Input, e Env) {
				e.Session().Clear(in.Ctx.Buffer)
				e.UI().Refresh()
			},
		},
		{
			Name: "cycle", Aliases: []string{"rejoin"}, Group: GroupChat,
			NeedsConn: true, NeedsChannel: true,
			Help: "leave and rejoin this channel, keeping its history",
			Run:  func(in Input, e Env) { e.Session().Cycle(in.Ctx.Buffer) },
		},
		{
			Name: "ignore", Group: GroupChat, NeedsConn: true,
			Help: "hide someone's messages, or list who is hidden",
			Args: ArgSpec{Usage: "[nick]"},
			Run: func(in Input, e Env) {
				if in.Args.Raw == "" {
					list := e.Session().Ignored()
					if len(list) == 0 {
						e.UI().ShowHint("You are not ignoring anyone.")
						return
					}
					e.UI().ShowHint("Ignoring " + strings.Join(list, ", ") +
						". Use /unignore nick to stop.")
					return
				}
				e.Session().SetIgnore(in.Args.Raw, true)
				e.UI().ShowHint("Ignoring " + in.Args.Raw + ". Use /unignore " +
					in.Args.Raw + " to stop.")
			},
		},
		{
			Name: "unignore", Group: GroupChat, NeedsConn: true,
			Help: "stop hiding someone's messages",
			Args: ArgSpec{Usage: "nick", Min: 1},
			Run: func(in Input, e Env) {
				e.Session().SetIgnore(in.Args.Raw, false)
				e.UI().ShowHint("No longer ignoring " + in.Args.Raw + ".")
			},
		},

		// --- you ------------------------------------------------------------------
		{
			Name: "nick", Group: GroupYou, NeedsConn: true,
			Help: "change your nickname",
			Args: ArgSpec{Usage: "newnickname", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().SetNick(in.Args.Raw) },
		},
		{
			Name: "away", Group: GroupYou, NeedsConn: true,
			Help: "say you are away, with an optional reason",
			Args: ArgSpec{Usage: "[reason]"},
			Run: func(in Input, e Env) {
				_ = e.Wire().Away(in.Args.Raw)
				if in.Args.Raw == "" {
					e.UI().ShowHint("You are no longer away.")
					return
				}
				e.UI().ShowHint("You are now away: " + in.Args.Raw)
			},
		},
		{
			Name: "back", Group: GroupYou, NeedsConn: true,
			Help: "say you are back",
			Run: func(in Input, e Env) {
				_ = e.Wire().Away("")
				e.UI().ShowHint("You are no longer away.")
			},
		},
		{
			Name: "setname", Group: GroupYou, NeedsConn: true,
			Help: "change the real name shown in /whois",
			Args: ArgSpec{Usage: "your real name", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().SetName(in.Args.Raw) },
		},
		{
			Name: "register", Group: GroupYou, NeedsConn: true,
			Help: "register your nickname as an account",
			Args: ArgSpec{Usage: "password [email]", Words: AllWords, Min: 1},
			Run: func(in Input, e Env) {
				// The standard command where the server supports it, NickServ
				// where it does not - the fallback is what makes this work on the
				// networks that have not caught up.
				if e.Session().HasCap("draft/account-registration") {
					email := "*"
					if len(in.Args.Fields) > 1 {
						email = in.Args.Field(1)
					}
					_ = e.Wire().Register("*", email, in.Args.Field(0))
					e.UI().ShowHint("Registering your current nickname as an account...")
					return
				}
				_ = e.Wire().Privmsg("NickServ", "REGISTER "+strings.Join(in.Args.Fields, " "))
				e.UI().ShowHint("Sent registration request to NickServ.")
			},
		},
		{
			Name: "verify", Group: GroupYou, NeedsConn: true,
			Help: "finish registering with the code the server sent you",
			Args: ArgSpec{Usage: "[account] code", Words: AllWords, Min: 1, Max: 2},
			Run: func(in Input, e Env) {
				if len(in.Args.Fields) == 1 {
					_ = e.Wire().Verify(e.Session().Nick(), in.Args.Field(0))
					return
				}
				_ = e.Wire().Verify(in.Args.Field(0), in.Args.Field(1))
			},
		},
		{
			Name: "identify", Group: GroupYou, NeedsConn: true,
			Help: "log in to your account with NickServ",
			Args: ArgSpec{Usage: "[account] password"},
			Run:  func(in Input, e Env) { _ = e.Wire().Privmsg("NickServ", "IDENTIFY "+in.Args.Raw) },
		},

		// --- channel operators ----------------------------------------------------
		{
			Name: "topic", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "show the topic, or set it",
			Args: ArgSpec{Usage: "[new topic]"},
			Run: func(in Input, e Env) {
				if in.Args.Raw == "" {
					_ = e.Wire().Topic(in.Ctx.Buffer)
					return
				}
				_ = e.Wire().SetTopic(in.Ctx.Buffer, in.Args.Raw)
			},
		},
		{
			Name: "mode", Group: GroupOps, NeedsConn: true,
			Help: "show or change modes on a channel or on yourself",
			Args: ArgSpec{Usage: "[target] [modes]", Words: AllWords},
			Run: func(in Input, e Env) {
				if len(in.Args.Fields) == 0 {
					_ = e.Wire().Mode(in.Ctx.Buffer)
					return
				}
				_ = e.Wire().Mode(in.Args.Field(0), in.Args.Fields[1:]...)
			},
		},
		prefixCmd("op", "+o", "give channel operator status"),
		prefixCmd("deop", "-o", "take channel operator status away"),
		prefixCmd("voice", "+v", "let someone speak in a moderated channel"),
		prefixCmd("devoice", "-v", "take voice away"),
		prefixCmd("hop", "+h", "give half-operator status"),
		prefixCmd("dehop", "-h", "take half-operator status away"),
		{
			Name: "ban", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "ban a nick or a mask from this channel",
			Args: ArgSpec{Usage: "nick-or-mask", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Mode(in.Ctx.Buffer, "+b", banMask(in.Args.Raw)) },
		},
		{
			Name: "unban", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "lift a ban",
			Args: ArgSpec{Usage: "nick-or-mask", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Mode(in.Ctx.Buffer, "-b", banMask(in.Args.Raw)) },
		},
		{
			Name: "kick", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "remove someone from this channel",
			Args: ArgSpec{Usage: "nick [reason]", Words: 1, Min: 1},
			Run: func(in Input, e Env) {
				_ = e.Wire().Kick(in.Ctx.Buffer, in.Args.Field(0), in.Args.Rest)
			},
		},
		{
			Name: "kickban", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "ban someone and remove them",
			Args: ArgSpec{Usage: "nick [reason]", Words: 1, Min: 1},
			Run: func(in Input, e Env) {
				nick := in.Args.Field(0)
				// Ban first: kicked and then banned leaves a window to rejoin in.
				_ = e.Wire().Mode(in.Ctx.Buffer, "+b", nick+"!*@*")
				_ = e.Wire().Kick(in.Ctx.Buffer, nick, in.Args.Rest)
			},
		},
		{
			Name: "invite", Group: GroupOps, NeedsConn: true, NeedsChannel: true,
			Help: "invite someone to this channel",
			Args: ArgSpec{Usage: "nick", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Invite(in.Args.Raw, in.Ctx.Buffer) },
		},
		{
			Name: "knock", Group: GroupOps, NeedsConn: true,
			Help: "ask for an invitation to an invite-only channel",
			Args: ArgSpec{Usage: "#channel", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Knock(in.Args.Raw) },
		},

		// --- information ----------------------------------------------------------
		{
			Name: "whois", Group: GroupInfo, NeedsConn: true,
			Help: "look someone up; the answer lands in this buffer",
			Args: ArgSpec{Usage: "nick", Min: 1},
			Run:  func(in Input, e Env) { e.Session().Whois(in.Ctx.Buffer, in.Args.Raw) },
		},
		{
			Name: "whowas", Group: GroupInfo, NeedsConn: true,
			Help: "look up someone who has already left",
			Args: ArgSpec{Usage: "nick", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Whowas(in.Args.Raw) },
		},
		{
			Name: "who", Group: GroupInfo, NeedsConn: true,
			Help: "list who matches a channel, a nick or a mask",
			Args: ArgSpec{Usage: "[target]"},
			Run: func(in Input, e Env) {
				target := in.Args.Raw
				if target == "" {
					target = in.Ctx.Buffer
				}
				_ = e.Wire().Who(target)
			},
		},
		{
			Name: "list", Group: GroupInfo, NeedsConn: true,
			Help: "browse the channels on this network",
			Run:  func(in Input, e Env) { e.Session().List() },
		},
		{
			Name: "names", Group: GroupInfo, NeedsConn: true, NeedsChannel: true,
			Help: "list who is in this channel",
			Run:  func(in Input, e Env) { _ = e.Wire().Names(in.Ctx.Buffer) },
		},
		{
			Name: "history", Group: GroupInfo, NeedsConn: true,
			Help: "fetch more of this conversation's history",
			Run: func(in Input, e Env) {
				if in.Ctx.Server {
					e.UI().ShowHint("History works in channels and conversations, not the server log.")
					return
				}
				e.Session().FetchHistory(in.Ctx.Buffer)
			},
		},
		queryCmd("motd", "show the server's message of the day"),
		queryCmd("time", "ask the server for its local time"),
		queryCmd("version", "ask the server what software it runs"),
		queryCmd("admin", "show who runs this server"),
		queryCmd("info", "show the server's information page"),
		queryCmd("links", "list the servers on this network"),
		queryCmd("lusers", "show how many users are connected"),
		queryCmd("map", "show the network's shape"),
		queryCmd("rules", "show the network's rules"),
		queryArgsCmd("stats", "[letter]", "ask the server for a statistics report"),
		queryArgsCmd("ison", "nick [nick...]", "ask which of these nicks are online"),
		queryArgsCmd("userhost", "nick [nick...]", "ask where these nicks are connecting from"),
		queryArgsCmd("monitor", "[+|-|C|L|S] [nicks]", "watch nicks for going online and offline"),

		// --- services -------------------------------------------------------------
		serviceCmd("ns", "NickServ", "send a command to NickServ, which owns nicknames"),
		serviceCmd("cs", "ChanServ", "send a command to ChanServ, which owns channels"),
		serviceCmd("hs", "HostServ", "send a command to HostServ, which owns cloaks"),

		// --- server operators -----------------------------------------------------
		{
			Name: "oper", Group: GroupOper, NeedsConn: true,
			Help: "authenticate as a server operator",
			Args: ArgSpec{Usage: "name password", Words: AllWords, Min: 2},
			Run:  func(in Input, e Env) { _ = e.Wire().Oper(in.Args.Field(0), in.Args.Field(1)) },
		},
		{
			Name: "kill", Group: GroupOper, NeedsConn: true,
			Help: "disconnect a user from the server",
			Args: ArgSpec{Usage: "nick [reason]", Words: 1, Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Kill(in.Args.Field(0), in.Args.Rest) },
		},
		{
			Name: "wallops", Group: GroupOper, NeedsConn: true,
			Help: "message everyone who is listening for operator notices",
			Args: ArgSpec{Usage: "message", Min: 1},
			Run:  func(in Input, e Env) { _ = e.Wire().Wallops(in.Args.Raw) },
		},

		// --- application ----------------------------------------------------------
		{
			Name: "server", Aliases: []string{"servers", "network", "networks", "connect"},
			Group: GroupApp,
			Help:  "open the network manager, to connect or edit a network",
			Run:   func(in Input, e Env) { e.UI().ShowDialog(DialogNetworks) },
		},
		{
			Name: "disconnect", Group: GroupApp, NeedsConn: true,
			Help: "leave this network but keep eco open",
			Run:  func(in Input, e Env) { e.UI().Disconnect() },
		},
		{
			Name: "redact", Group: GroupApp, NeedsConn: true,
			Help: "delete one of your own messages, where the server allows it",
			Args: ArgSpec{Usage: "msgid [reason]", Words: 1, Min: 1},
			Run: func(in Input, e Env) {
				_ = e.Wire().Redact(in.Ctx.Buffer, in.Args.Field(0), in.Args.Rest)
			},
		},
		{
			Name: "raw", Aliases: []string{"quote"}, Group: GroupApp, NeedsConn: true,
			Help: "send a line to the server exactly as typed",
			Args: ArgSpec{Usage: "[line]"},
			Run:  func(in Input, e Env) { _ = e.Wire().SendRaw(in.Args.Raw) },
		},
		{
			Name: "quit", Aliases: []string{"exit"}, Group: GroupApp, NeedsConn: true,
			Help: "leave every network and close eco",
			Args: ArgSpec{Usage: "[message]"},
			Run: func(in Input, e Env) {
				if in.Args.Raw != "" {
					_ = e.Wire().Quit(in.Args.Raw)
				}
				e.UI().Quit()
			},
		},
		{
			Name: "help", Aliases: []string{"docs", "helpwindow"}, Group: GroupApp,
			Help: "open the documentation, or explain one command",
			Args: ArgSpec{Usage: "[command]"},
			Run: func(in Input, e Env) {
				if in.Args.Raw != "" {
					e.UI().ShowHint(explain(in.Args.Raw))
					return
				}
				e.UI().ShowDialog(DialogHelp)
				e.UI().ShowHint(Summary())
			},
		},
		{
			Name: "testnotify", Group: GroupApp, Hidden: true,
			Help: "raise a test desktop notification",
			Run:  func(in Input, e Env) { e.UI().Notify("eco", "Notifications are working.") },
		},
	}
	index()
}

// banMask turns a bare nick into a mask, leaving a real mask alone.
func banMask(target string) string {
	if strings.ContainsAny(target, "!*@") {
		return target
	}
	return target + "!*@*"
}

// byVerb indexes the table by every name and alias, and verbs is the
// completion list: every name and alias that is not hidden, with its slash,
// sorted. Tab completion reads exactly that, so a command added to the table is
// completable without anyone remembering to say so.
var (
	byVerb map[string]*Command
	verbs  []string
)

// index builds those two from the table. It runs once, at init, because both
// are read on the keystroke path: every Enter looks a verb up and every Tab
// walks the list.
func index() {
	byVerb = make(map[string]*Command, len(table)*2)
	for _, c := range table {
		for _, v := range append([]string{c.Name}, c.Aliases...) {
			if _, dup := byVerb[v]; dup {
				panic("command: duplicate verb /" + v)
			}
			byVerb[v] = c
		}
	}
	verbs = make([]string, 0, len(byVerb))
	for v, c := range byVerb {
		if !c.Hidden {
			verbs = append(verbs, "/"+v)
		}
	}
	sort.Strings(verbs)
}

// Lookup finds the command a verb names, or nil. The verb is without its slash
// and is matched case-insensitively.
func Lookup(verb string) *Command { return byVerb[strings.ToLower(verb)] }

// All returns the table in help order.
func All() []*Command { return append([]*Command(nil), table...) }

// Verbs returns every completable verb, with its leading slash, sorted.
func Verbs() []string { return append([]string(nil), verbs...) }

// Complete returns the verbs a half-typed one could become, in Tab order.
// prefix carries the leading slash the user typed.
//
// The filtering lives here rather than in the composer because the composer is
// where v1 kept its hand-written copy of the list: with this, the UI holds no
// list of commands at all, and the only way to answer "what can this become" is
// to ask the table.
func Complete(prefix string) []string {
	var out []string
	for _, v := range verbs {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}
