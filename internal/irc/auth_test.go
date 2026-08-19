package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// manualRegister drives registration by hand so a test can answer
// AUTHENTICATE however it likes. It returns when the client finished
// registering or gave up and quit.
func manualRegister(t *testing.T, srv *testsrv.Server, nick string, auth func(arg string)) {
	t.Helper()
	srv.WaitFor("CAP LS")
	srv.Send(":irc.test CAP * LS :%s", strings.Join(testsrv.DefaultCaps, " "))
	for {
		// TryNext, not Expect: a terminal auth failure makes the client hang up
		// in the middle of this handshake, and whether its QUIT beats the socket
		// closing is a race the test has no reason to care about. The outcome is
		// asserted on the event stream, which has its own deadline.
		line, ok := srv.TryNext()
		if !ok {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "QUIT":
			return
		case "AUTHENTICATE":
			if len(fields) > 1 && auth != nil {
				auth(fields[1])
			}
		case "CAP":
			if len(fields) < 2 {
				continue
			}
			switch strings.ToUpper(fields[1]) {
			case "REQ":
				srv.Send(":irc.test CAP * ACK :%s",
					strings.TrimPrefix(strings.Join(fields[2:], " "), ":"))
			case "END":
				srv.Welcome(nick)
				return
			}
		}
	}
}

func TestSASLPlainReportsSuccessBeforeReady(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.SASL = SASL{Mechanism: MechPlain, User: "eco", Pass: "hunter2"}
	})
	h.srv.Register("eco")

	var sawAuth bool
	deadline := time.After(eventTimeout)
loop:
	for {
		select {
		case ev := <-h.events:
			switch e := ev.(type) {
			case model.AuthResult:
				if !e.Success {
					t.Fatalf("auth failed: %+v", e)
				}
				if e.Mechanism != MechPlain {
					t.Errorf("mechanism = %q", e.Mechanism)
				}
				if e.Account != "eco" {
					t.Errorf("account = %q, want eco", e.Account)
				}
				sawAuth = true
			case model.ConnState:
				if e.Status == model.ConnReady {
					break loop
				}
			}
		case <-deadline:
			t.Fatal("never reached ready")
		}
	}
	if !sawAuth {
		t.Fatal("ready was reported without an AuthResult; authentication must be observable")
	}
}

func TestSASLFailureIsTerminalAndNotRetried(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.SASL = SASL{Mechanism: MechPlain, User: "eco", Pass: "wrong"}
	})
	manualRegister(t, h.srv, "eco", func(arg string) {
		if strings.EqualFold(arg, "PLAIN") {
			h.srv.Send("AUTHENTICATE +")
			return
		}
		h.srv.Send(":irc.test 904 eco :SASL authentication failed")
	})

	res, ok := h.waitFor("authresult").(model.AuthResult)
	if !ok || res.Success {
		t.Fatalf("auth result = %+v, want a failure", res)
	}
	if res.Message == "" {
		t.Error("the server's own reason was dropped")
	}

	failed := h.waitConn(model.ConnFailed)
	if failed.Err == "" {
		t.Error("terminal failure reported no reason")
	}
	// Bad credentials are not a transient fault: retrying them is how an
	// account gets throttled, so the ladder is never entered.
	if got := h.timer.recorded(); len(got) != 0 {
		t.Errorf("backoff waits = %v, want none after an auth failure", got)
	}
	for range h.events {
	}
	if h.c.Status() != model.ConnFailed {
		t.Errorf("status = %v, want failed", h.c.Status())
	}
}

func TestSASLOptionalFailureStillReachesReady(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.SASL = SASL{Mechanism: MechPlain, User: "eco", Pass: "wrong", Optional: true}
	})
	manualRegister(t, h.srv, "eco", func(arg string) {
		if strings.EqualFold(arg, "PLAIN") {
			h.srv.Send("AUTHENTICATE +")
			return
		}
		h.srv.Send(":irc.test 904 eco :SASL authentication failed")
	})

	res := h.waitFor("authresult").(model.AuthResult)
	if res.Success {
		t.Fatal("want a failure")
	}
	// Optional means the session continues - but never silently: the failure
	// was already an event of its own.
	h.waitConn(model.ConnReady)
}

func TestSCRAMRunsAfterRegistrationAndGatesReady(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.SASL = SASL{Mechanism: MechScram, User: "eco", Pass: "hunter2"}
	})
	// SCRAM is not part of registration, so the ordinary handshake applies.
	h.srv.Register("eco")

	// End of MOTD: registered, but not ready - authentication is sequenced
	// first so nothing above can join a +R channel a moment too early.
	h.waitConn(model.ConnAuthenticating)

	if got := untagged(h.srv.WaitFor("AUTHENTICATE*")); got != "AUTHENTICATE SCRAM-SHA-256" {
		t.Fatalf("first authenticate = %q", got)
	}
	h.srv.Send("AUTHENTICATE +")
	first := h.srv.WaitFor("AUTHENTICATE*")
	if payload := strings.Fields(first)[1]; payload == "+" || payload == "" {
		t.Fatalf("client-first message = %q, want a SCRAM payload", payload)
	}

	select {
	case ev := <-h.events:
		if cs, ok := ev.(model.ConnState); ok && cs.Status == model.ConnReady {
			t.Fatal("ready was reported while SCRAM was still in flight")
		}
	case <-time.After(50 * time.Millisecond):
	}

	h.srv.Send(":irc.test 900 eco eco!eco@test eco :You are now logged in as eco")
	h.srv.Send(":irc.test 903 eco :SASL authentication successful")

	res := h.waitFor("authresult").(model.AuthResult)
	if !res.Success || res.Mechanism != MechScram || res.Account != "eco" {
		t.Fatalf("auth result = %+v", res)
	}
	h.waitConn(model.ConnReady)
}

func TestSCRAMWithoutTheSaslCapAbortsRatherThanHangs(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.SASL = SASL{Mechanism: MechScram, User: "eco", Pass: "hunter2"}
		// Everything but sasl: the server will not let us authenticate.
		srv.Caps = []string{"batch", "message-tags", "labeled-response", "server-time"}
	})
	h.srv.Register("eco")

	res := h.waitFor("authresult").(model.AuthResult)
	if res.Success || !res.Aborted {
		t.Fatalf("auth result = %+v, want an aborted attempt", res)
	}
	// A server that refuses the capability must not leave the client stuck
	// in "authenticating" forever.
	h.waitConn(model.ConnReady)
}
