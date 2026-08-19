package irc

import (
	"testing"

	"github.com/toprakkilic/eco/internal/testsrv"
)

// TestCommandVerbsGoOnTheWire pins the protocol lines internal/command's table
// produces. The table itself is tested against a fake wire, which proves it
// calls the right method with the right arguments and nothing about what those
// methods write — this is that other half, and it is here because this is the
// package where the bytes are decided.
func TestCommandVerbsGoOnTheWire(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	for _, tc := range []struct {
		name string
		call func() error
		want string
	}{
		{"privmsg", func() error { return h.c.Privmsg("NickServ", "IDENTIFY hunter2") },
			"PRIVMSG NickServ :IDENTIFY hunter2"},
		{"notice", func() error { return h.c.Notice("alice", "heads up") },
			"NOTICE alice :heads up"},
		{"ctcp upper-cases the request", func() error { return h.c.Ctcp("alice", "version") },
			"PRIVMSG alice :\x01VERSION\x01"},
		{"topic query", func() error { return h.c.Topic("#eco") }, "TOPIC #eco"},
		{"topic set", func() error { return h.c.SetTopic("#eco", "new topic") },
			"TOPIC #eco :new topic"},
		{"away", func() error { return h.c.Away("lunch") }, "AWAY :lunch"},
		{"back is an empty away", func() error { return h.c.Away("") }, "AWAY"},
		{"setname", func() error { return h.c.SetName("Ada Lovelace") }, "SETNAME :Ada Lovelace"},
		{"register", func() error { return h.c.Register("*", "*", "hunter2") },
			"REGISTER * * :hunter2"},
		{"verify", func() error { return h.c.Verify("ada", "code123") }, "VERIFY ada code123"},
		{"oper", func() error { return h.c.Oper("ada", "hunter2") }, "OPER ada hunter2"},
		{"who", func() error { return h.c.Who("#eco") }, "WHO #eco"},
		{"whowas", func() error { return h.c.Whowas("ada") }, "WHOWAS ada"},
		{"knock", func() error { return h.c.Knock("#secret") }, "KNOCK #secret"},
		{"wallops", func() error { return h.c.Wallops("server going down") },
			"WALLOPS :server going down"},
		{"kill with a reason", func() error { return h.c.Kill("spammer", "bye now") },
			"KILL spammer :bye now"},
		{"kill without one", func() error { return h.c.Kill("spammer", "") }, "KILL spammer"},
		{"server query", func() error { return h.c.ServerQuery("motd") }, "MOTD"},
		{"server query with parameters", func() error { return h.c.ServerQuery("stats", "u") },
			"STATS u"},
		{"mode", func() error { return h.c.Mode("#eco", "+o", "alice") }, "MODE #eco +o alice"},
		{"kick", func() error { return h.c.Kick("#eco", "bob", "spam") }, "KICK #eco bob :spam"},
		{"invite", func() error { return h.c.Invite("bob", "#eco") }, "INVITE bob #eco"},
		{"names", func() error { return h.c.Names("#eco") }, "NAMES #eco"},
		{"nick", func() error { return h.c.SetNick("ada") }, "NICK ada"},
		// Last: the server hangs up on a QUIT, so nothing can be written after it.
		{"quit", func() error { return h.c.Quit("see you") }, "QUIT :see you"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			h.srv.WaitFor(tc.want)
		})
	}
}

// TestAnOmittedReasonIsOmittedNotEmpty. "/kick bob" and "/redact id" carry no
// reason, and there is a difference between not sending the parameter and
// sending it empty: ForceTrailing turns "" into a literal ":" at the end of the
// line, which a server shows as a kick with a blank comment. v1 sent two
// parameters, so this asserts the exact line rather than a prefix of it —
// testsrv.Match is happy with "KICK #eco bob" as a prefix of "KICK #eco bob :",
// which is precisely the difference at stake.
func TestAnOmittedReasonIsOmittedNotEmpty(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	for _, tc := range []struct {
		name string
		call func() error
		want string
	}{
		{"kick with no reason", func() error { return h.c.Kick("#eco", "bob", "") },
			"KICK #eco bob"},
		{"kick with one", func() error { return h.c.Kick("#eco", "bob", "spam") },
			"KICK #eco bob :spam"},
		{"redact with no reason", func() error { return h.c.Redact("#eco", "abc123", "") },
			"REDACT #eco abc123"},
		{"redact with one", func() error { return h.c.Redact("#eco", "abc123", "mistake") },
			"REDACT #eco abc123 :mistake"},
		{"kill with no reason", func() error { return h.c.Kill("bob", "") }, "KILL bob"},
		{"away with no reason is a bare AWAY", func() error { return h.c.Away("") }, "AWAY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			line := testsrv.StripTags(h.srv.WaitFor(tc.want))
			if line != tc.want {
				t.Errorf("%s put %q on the wire, want exactly %q", tc.name, line, tc.want)
			}
		})
	}
}

// TestCommandVerbsFailWithoutAConnection is the other guard the command table
// leans on: a verb written with nothing to write to reports it instead of
// panicking, so "not connected" stays one check in one place.
func TestCommandVerbsFailWithoutAConnection(t *testing.T) {
	c := New(Config{Network: "n", Server: "irc.test", Port: "6667", Nick: "eco"})
	t.Cleanup(c.Close)
	for name, call := range map[string]func() error{
		"privmsg":     func() error { return c.Privmsg("NickServ", "x") },
		"notice":      func() error { return c.Notice("alice", "x") },
		"ctcp":        func() error { return c.Ctcp("alice", "version") },
		"away":        func() error { return c.Away("lunch") },
		"setname":     func() error { return c.SetName("Ada") },
		"register":    func() error { return c.Register("*", "*", "pw") },
		"wallops":     func() error { return c.Wallops("x") },
		"kill":        func() error { return c.Kill("bob", "x") },
		"quit":        func() error { return c.Quit("bye") },
		"serverquery": func() error { return c.ServerQuery("motd") },
		"topic":       func() error { return c.Topic("#eco") },
	} {
		if err := call(); err != ErrNotConnected {
			t.Errorf("%s before connect = %v, want ErrNotConnected", name, err)
		}
	}
}
