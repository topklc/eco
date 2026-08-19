package command

import (
	"reflect"
	"testing"
)

// TestCommands runs every command in the table against the fake env and pins
// its whole effect: the calls it makes, in order, and nothing else.
//
// This is the milestone's claim made checkable. v1's 300-line switch could only
// be exercised through a window and a socket, so none of this was tested at
// all; the guards in here (a /part that refuses a non-channel, a /join that
// adds the "#", a /kickban that bans before it kicks) were regressions waiting
// to happen.
func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		ctx   Context
		env   fakeEnv
		want  []string
	}{
		// --- chat -------------------------------------------------------------
		{name: "join adds the hash a user leaves off", input: "/join eco", ctx: inChannel,
			want: []string{`join #eco key=""`}},
		{name: "join keeps a hash that is there", input: "/join #eco", ctx: inChannel,
			want: []string{`join #eco key=""`}},
		{name: "join takes a key", input: "/join #eco sekrit", ctx: inChannel,
			want: []string{`join #eco key="sekrit"`}},
		{name: "join trims the key", input: "/join #eco   sekrit  ", ctx: inChannel,
			want: []string{`join #eco key="sekrit"`}},
		{name: "join without a channel says how", input: "/join", ctx: inChannel,
			want: []string{"hint Usage: /join #channel [key]"}},
		{name: "j is join", input: "/j eco", ctx: inChannel,
			want: []string{`join #eco key=""`}},

		{name: "part leaves the channel on screen", input: "/part", ctx: inChannel,
			want: []string{"part #eco"}},
		{name: "part names a channel from anywhere", input: "/part eco", ctx: inServer,
			want: []string{"part #eco"}},
		{name: "part refuses the network log", input: "/part", ctx: inServer,
			want: []string{"hint Usage: /part [#channel]"}},
		{name: "part refuses a conversation", input: "/part", ctx: inQuery,
			want: []string{"hint Usage: /part [#channel]"}},
		{name: "leave is part", input: "/leave", ctx: inChannel,
			want: []string{"part #eco"}},

		{name: "close parts a channel", input: "/close", ctx: inChannel,
			want: []string{"part #eco"}},
		{name: "close closes a conversation", input: "/close", ctx: inQuery,
			want: []string{"closebuffer alice"}},
		{name: "close does nothing in the network log", input: "/close", ctx: inServer,
			want: nil},

		{name: "msg opens a conversation and sends", input: "/msg bob hello there", ctx: inChannel,
			want: []string{"ui open bob", `send bob "hello there"`}},
		{name: "msg with no message only opens", input: "/msg bob", ctx: inChannel,
			want: []string{"ui open bob"}},
		{name: "query is msg", input: "/query bob hi", ctx: inChannel,
			want: []string{"ui open bob", `send bob "hi"`}},
		{name: "msg without a nick says how", input: "/msg", ctx: inChannel,
			want: []string{"hint Usage: /msg nick [message]"}},

		{name: "me sends an action", input: "/me waves at everyone", ctx: inChannel,
			want: []string{`action #eco "waves at everyone"`}},
		{name: "me without text says how", input: "/me", ctx: inChannel,
			want: []string{"hint Usage: /me does something"}},

		{name: "notice", input: "/notice bob heads up", ctx: inChannel,
			want: []string{`NOTICE bob "heads up"`}},
		{name: "notice needs a message", input: "/notice bob", ctx: inChannel,
			want: []string{"hint Usage: /notice target message"}},

		{name: "ctcp", input: "/ctcp bob version", ctx: inChannel,
			want: []string{"CTCP bob version"}},
		{name: "ctcp needs a request", input: "/ctcp bob", ctx: inChannel,
			want: []string{"hint Usage: /ctcp nick request"}},

		{name: "clear wipes the buffer and repaints", input: "/clear", ctx: inChannel,
			want: []string{"clear #eco", "ui refresh"}},

		{name: "cycle keeps the buffer", input: "/cycle", ctx: inChannel,
			want: []string{"cycle #eco"}},
		{name: "rejoin is cycle", input: "/rejoin", ctx: inChannel,
			want: []string{"cycle #eco"}},
		{name: "cycle refuses a conversation", input: "/cycle", ctx: inQuery,
			want: []string{"hint /cycle only works in a channel."}},

		{name: "ignore with nobody ignored", input: "/ignore", ctx: inChannel,
			want: []string{"hint You are not ignoring anyone."}},
		{name: "ignore lists who is ignored", input: "/ignore", ctx: inChannel,
			env:  fakeEnv{ignores: []string{"alice", "bob"}},
			want: []string{"hint Ignoring alice, bob. Use /unignore nick to stop."}},
		{name: "ignore a nick", input: "/ignore bob", ctx: inChannel,
			want: []string{"setignore bob on=true", "hint Ignoring bob. Use /unignore bob to stop."}},
		{name: "unignore a nick", input: "/unignore bob", ctx: inChannel,
			want: []string{"setignore bob on=false", "hint No longer ignoring bob."}},
		{name: "unignore needs a nick", input: "/unignore", ctx: inChannel,
			want: []string{"hint Usage: /unignore nick"}},

		// --- you --------------------------------------------------------------
		{name: "nick", input: "/nick ada", ctx: inChannel, want: []string{"NICK ada"}},
		{name: "nick needs a name", input: "/nick", ctx: inChannel,
			want: []string{"hint Usage: /nick newnickname"}},

		{name: "away with a reason", input: "/away at lunch", ctx: inChannel,
			want: []string{`AWAY "at lunch"`, "hint You are now away: at lunch"}},
		{name: "away with none marks you back", input: "/away", ctx: inChannel,
			want: []string{`AWAY ""`, "hint You are no longer away."}},
		{name: "back", input: "/back", ctx: inChannel,
			want: []string{`AWAY ""`, "hint You are no longer away."}},

		{name: "setname", input: "/setname Ada Lovelace", ctx: inChannel,
			want: []string{`SETNAME "Ada Lovelace"`}},

		{name: "register uses the standard command when the server has it",
			input: "/register hunter2", ctx: inChannel,
			env: fakeEnv{caps: map[string]bool{"draft/account-registration": true}},
			want: []string{"REGISTER * * hunter2",
				"hint Registering your current nickname as an account..."}},
		{name: "register takes an email", input: "/register hunter2 ada@example.com", ctx: inChannel,
			env: fakeEnv{caps: map[string]bool{"draft/account-registration": true}},
			want: []string{"REGISTER * ada@example.com hunter2",
				"hint Registering your current nickname as an account..."}},
		{name: "register falls back to NickServ", input: "/register hunter2 ada@example.com",
			ctx: inChannel,
			want: []string{`PRIVMSG NickServ "REGISTER hunter2 ada@example.com"`,
				"hint Sent registration request to NickServ."}},
		{name: "register needs a password", input: "/register", ctx: inChannel,
			want: []string{"hint Usage: /register password [email]"}},

		{name: "verify with a code uses our own nick", input: "/verify abc123", ctx: inChannel,
			env: fakeEnv{nick: "ada"}, want: []string{"VERIFY ada abc123"}},
		{name: "verify with an account", input: "/verify someone abc123", ctx: inChannel,
			want: []string{"VERIFY someone abc123"}},
		{name: "verify with too many words says how", input: "/verify a b c", ctx: inChannel,
			want: []string{"hint Usage: /verify [account] code"}},

		{name: "identify", input: "/identify hunter2", ctx: inChannel,
			want: []string{`PRIVMSG NickServ "IDENTIFY hunter2"`}},

		// --- channel operators ------------------------------------------------
		{name: "topic with no argument asks", input: "/topic", ctx: inChannel,
			want: []string{"TOPIC #eco"}},
		{name: "topic sets", input: "/topic all welcome", ctx: inChannel,
			want: []string{`TOPIC #eco "all welcome"`}},
		{name: "topic refuses a conversation", input: "/topic", ctx: inQuery,
			want: []string{"hint /topic only works in a channel."}},

		{name: "mode with no argument asks about this buffer", input: "/mode", ctx: inChannel,
			want: []string{"MODE #eco"}},
		{name: "mode names its own target", input: "/mode #other +i", ctx: inChannel,
			want: []string{"MODE #other +i"}},

		{name: "op one", input: "/op alice", ctx: inChannel,
			want: []string{"MODE #eco +o alice"}},
		{name: "op several share one flag", input: "/op alice bob carol", ctx: inChannel,
			want: []string{"MODE #eco +ooo alice bob carol"}},
		{name: "deop", input: "/deop alice", ctx: inChannel,
			want: []string{"MODE #eco -o alice"}},
		{name: "voice", input: "/voice alice", ctx: inChannel,
			want: []string{"MODE #eco +v alice"}},
		{name: "devoice", input: "/devoice alice", ctx: inChannel,
			want: []string{"MODE #eco -v alice"}},
		{name: "hop", input: "/hop alice", ctx: inChannel,
			want: []string{"MODE #eco +h alice"}},
		{name: "dehop", input: "/dehop alice", ctx: inChannel,
			want: []string{"MODE #eco -h alice"}},
		{name: "op needs a nick", input: "/op", ctx: inChannel,
			want: []string{"hint Usage: /op nick [nick...]"}},
		{name: "op refuses a conversation", input: "/op alice", ctx: inQuery,
			want: []string{"hint /op only works in a channel."}},

		{name: "ban makes a mask from a nick", input: "/ban bob", ctx: inChannel,
			want: []string{"MODE #eco +b bob!*@*"}},
		{name: "ban leaves a real mask alone", input: "/ban *!*@evil.example", ctx: inChannel,
			want: []string{"MODE #eco +b *!*@evil.example"}},
		{name: "unban", input: "/unban bob", ctx: inChannel,
			want: []string{"MODE #eco -b bob!*@*"}},

		{name: "kick with a reason", input: "/kick bob being rude", ctx: inChannel,
			want: []string{`KICK #eco bob "being rude"`}},
		{name: "kick without one", input: "/kick bob", ctx: inChannel,
			want: []string{`KICK #eco bob ""`}},
		{name: "kickban bans before it kicks", input: "/kickban bob spam", ctx: inChannel,
			want: []string{"MODE #eco +b bob!*@*", "KICK #eco bob spam"}},

		{name: "invite", input: "/invite bob", ctx: inChannel,
			want: []string{"INVITE bob #eco"}},
		{name: "knock", input: "/knock #secret", ctx: inChannel,
			want: []string{"KNOCK #secret"}},

		// --- information ------------------------------------------------------
		{name: "whois answers where it was asked", input: "/whois ada", ctx: inQuery,
			want: []string{"whois ada from=alice"}},
		{name: "whowas", input: "/whowas ada", ctx: inChannel, want: []string{"WHOWAS ada"}},
		{name: "who defaults to this buffer", input: "/who", ctx: inChannel,
			want: []string{"WHO #eco"}},
		{name: "who takes a target", input: "/who ada", ctx: inChannel,
			want: []string{"WHO ada"}},
		{name: "list", input: "/list", ctx: inChannel, want: []string{"list"}},
		{name: "names", input: "/names", ctx: inChannel, want: []string{"NAMES #eco"}},
		{name: "names refuses the network log", input: "/names", ctx: inServer,
			want: []string{"hint /names only works in a channel."}},

		{name: "history asks for this buffer", input: "/history", ctx: inChannel,
			want: []string{"fetchhistory #eco"}},
		{name: "history works in a conversation", input: "/history", ctx: inQuery,
			want: []string{"fetchhistory alice"}},
		{name: "history refuses the network log", input: "/history", ctx: inServer,
			want: []string{"hint History works in channels and conversations, not the server log."}},

		{name: "motd", input: "/motd", ctx: inChannel, want: []string{"MOTD"}},
		{name: "version", input: "/version", ctx: inChannel, want: []string{"VERSION"}},
		{name: "lusers", input: "/lusers", ctx: inChannel, want: []string{"LUSERS"}},
		{name: "stats takes a letter", input: "/stats u", ctx: inChannel,
			want: []string{"STATS u"}},
		{name: "ison takes nicks", input: "/ison ada bob", ctx: inChannel,
			want: []string{"ISON ada bob"}},
		{name: "monitor", input: "/monitor + ada", ctx: inChannel,
			want: []string{"MONITOR + ada"}},

		// --- services ---------------------------------------------------------
		{name: "ns", input: "/ns info ada", ctx: inChannel,
			want: []string{`PRIVMSG NickServ "info ada"`}},
		{name: "cs", input: "/cs op #eco", ctx: inChannel,
			want: []string{`PRIVMSG ChanServ "op #eco"`}},
		{name: "hs", input: "/hs on", ctx: inChannel,
			want: []string{"PRIVMSG HostServ on"}},

		// --- server operators -------------------------------------------------
		{name: "oper", input: "/oper ada hunter2", ctx: inChannel,
			want: []string{"OPER ada hunter2"}},
		{name: "oper needs both", input: "/oper ada", ctx: inChannel,
			want: []string{"hint Usage: /oper name password"}},
		{name: "kill with a reason", input: "/kill bob go away", ctx: inChannel,
			want: []string{`KILL bob "go away"`}},
		{name: "kill without one", input: "/kill bob", ctx: inChannel,
			want: []string{`KILL bob ""`}},
		{name: "wallops", input: "/wallops server restart soon", ctx: inChannel,
			want: []string{`WALLOPS "server restart soon"`}},

		// --- application ------------------------------------------------------
		{name: "server opens the network manager", input: "/server", ctx: inChannel,
			want: []string{"ui dialog networks"}},
		{name: "networks is server", input: "/networks", ctx: inChannel,
			want: []string{"ui dialog networks"}},
		{name: "disconnect", input: "/disconnect", ctx: inChannel,
			want: []string{"ui disconnect"}},
		{name: "redact with a reason", input: "/redact abc123 posted by mistake", ctx: inChannel,
			want: []string{`REDACT #eco abc123 "posted by mistake"`}},
		{name: "redact without one", input: "/redact abc123", ctx: inChannel,
			want: []string{`REDACT #eco abc123 ""`}},
		{name: "redact needs a msgid", input: "/redact", ctx: inChannel,
			want: []string{"hint Usage: /redact msgid [reason]"}},
		{name: "raw", input: "/raw PING :hello", ctx: inChannel,
			want: []string{`RAW "PING :hello"`}},
		{name: "quote is raw", input: "/quote PING :hello", ctx: inChannel,
			want: []string{`RAW "PING :hello"`}},
		{name: "quit with a parting message", input: "/quit see you", ctx: inChannel,
			want: []string{`QUIT "see you"`, "ui quit"}},
		{name: "quit without one", input: "/quit", ctx: inChannel,
			want: []string{"ui quit"}},
		{name: "exit is quit", input: "/exit", ctx: inChannel,
			want: []string{"ui quit"}},
		{name: "help opens the window and lists the commands", input: "/help", ctx: inChannel,
			want: []string{"ui dialog help", "hint " + Summary()}},
		{name: "help explains one command", input: "/help join", ctx: inChannel,
			want: []string{"hint /join #channel [key] - join a channel (also /j)"}},
		{name: "help explains an alias", input: "/help /q", ctx: inChannel,
			want: []string{"hint /msg nick [message] - open a private conversation, " +
				"and send a message if you give one (also /query, /q)"}},
		{name: "help on nothing it knows", input: "/help zzzzz", ctx: inChannel,
			want: []string{"hint There is no /zzzzz. Type /help for the list."}},
		{name: "testnotify", input: "/testnotify", ctx: inChannel,
			want: []string{"ui notify eco: Notifications are working."}},

		// --- plain text -------------------------------------------------------
		{name: "a message goes to this buffer", input: "hello everyone", ctx: inChannel,
			want: []string{`send #eco "hello everyone"`}},
		{name: "a doubled slash escapes a command", input: "//me is not an action", ctx: inChannel,
			want: []string{`send #eco "/me is not an action"`}},
		{name: "an empty line does nothing", input: "", ctx: inChannel, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env
			Run(Parse(tc.input, tc.ctx), &env)
			if !reflect.DeepEqual(env.log, tc.want) {
				t.Errorf("%q in %q did:\n  %q\nwant:\n  %q", tc.input, tc.ctx.Buffer, env.log, tc.want)
			}
		})
	}
}

// TestCommandsWithoutAConnection pins the guard v1 had at the top of its
// switch: with no live connection a command does nothing at all, silently.
// Typing a message is the exception - that path reports "Not connected." for
// itself, one layer down, and always did.
func TestCommandsWithoutAConnection(t *testing.T) {
	for _, input := range []string{"/join #eco", "/nick ada", "/me waves", "/part", "/quit"} {
		env := fakeEnv{offline: true}
		Run(Parse(input, inChannel), &env)
		if len(env.log) != 0 {
			t.Errorf("%q with no connection did %q, want nothing", input, env.log)
		}
	}
	// ...and a message still goes down to the session, which is what says so.
	env := fakeEnv{offline: true}
	Run(Parse("hello", inChannel), &env)
	if want := []string{`send #eco "hello"`}; !reflect.DeepEqual(env.log, want) {
		t.Errorf("a message with no connection did %q, want %q", env.log, want)
	}
}

// TestCommandsWithoutASession is the same question one step further out: no
// network at all. The three commands that do not need one still work, because
// they touch nothing but the window.
func TestCommandsWithoutASession(t *testing.T) {
	for input, want := range map[string][]string{
		"/server":      {"ui dialog networks"},
		"/testnotify":  {"ui notify eco: Notifications are working."},
		"/help":        {"ui dialog help", "hint " + Summary()},
		"/join #eco":   nil,
		"hello nobody": nil,
	} {
		env := fakeEnv{sessionless: true}
		Run(Parse(input, Context{}), &env)
		if !reflect.DeepEqual(env.log, want) {
			t.Errorf("%q with no session did %q, want %q", input, env.log, want)
		}
	}
}

// TestEveryCommandIsReachableAndSafe walks the whole table and runs each
// command with no arguments in a channel. Nothing may panic - an entry whose
// Run indexes an argument the spec does not require would, and that is the one
// mistake this shape of table makes easy.
func TestEveryCommandIsReachableAndSafe(t *testing.T) {
	for _, c := range All() {
		if c.Run == nil {
			t.Errorf("/%s has no Run", c.Name)
			continue
		}
		if c.Help == "" {
			t.Errorf("/%s has no help line", c.Name)
		}
		if got := Lookup(c.Name); got != c {
			t.Errorf("/%s does not look up to itself", c.Name)
		}
		for _, a := range c.Aliases {
			if got := Lookup(a); got != c {
				t.Errorf("/%s is not an alias of /%s", a, c.Name)
			}
		}
		env := fakeEnv{caps: map[string]bool{"draft/account-registration": true}}
		Run(Parse("/"+c.Name, inChannel), &env)
		// and again with a couple of arguments, which is the other half of the
		// indexing risk
		env = fakeEnv{}
		Run(Parse("/"+c.Name+" one two", inChannel), &env)
	}
}
