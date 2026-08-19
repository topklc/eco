package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// CHATHISTORY TARGETS is the one CHATHISTORY line a server sends TO us: one row
// per conversation with activity in the window we asked about. It is how a
// client discovers the private messages that arrived while it was offline - a
// channel announces itself by being rejoined, a query has nobody to announce
// it - and before M9 the decoder dropped it on the floor.
func TestChatHistoryTargetsDecode(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	if _, err := h.c.HistoryTargets(testClock.Add(-time.Hour), testClock, 50); err != nil {
		t.Fatalf("CHATHISTORY TARGETS: %v", err)
	}
	line := h.srv.WaitFor("CHATHISTORY TARGETS")
	if !strings.Contains(line, "timestamp=") {
		t.Errorf("TARGETS request = %q, want a timestamp window", line)
	}

	h.srv.Send(":irc.test BATCH +t draft/chathistory-targets")
	h.srv.Send("@batch=t :irc.test CHATHISTORY TARGETS carol 2026-08-19T10:00:00.000Z")
	h.srv.Send(":irc.test BATCH -t")

	ev := h.waitFor("historytarget").(model.HistoryTarget)
	if ev.Target != "carol" {
		t.Errorf("target = %q, want carol", ev.Target)
	}
	if want := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC); !ev.At.Equal(want) {
		t.Errorf("at = %v, want %v", ev.At, want)
	}
	// The targets batch has no registry entry on purpose: its contents are
	// ordinary live lines, so each row is an event of its own and nothing has
	// to be assembled before the client can act on one.
	if ev.Playback {
		t.Error("a TARGETS row was flagged playback; it is not replayed conversation")
	}
}

// A CHATHISTORY line that is not a TARGETS row is not an event. Servers do not
// send the other subcommands to clients, and turning an unknown one into a
// half-filled event would put a buffer named "" on somebody's sidebar.
func TestChatHistoryOtherSubcommandsAreIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":irc.test CHATHISTORY LATEST #eco 100")
	h.srv.Send(":irc.test CHATHISTORY TARGETS")
	h.srv.Send("@msgid=probe :alice!alice@test PRIVMSG #eco :after the noise")

	ev := h.next()
	msg, ok := ev.(model.PrivMsg)
	if !ok {
		t.Fatalf("first event after the malformed lines = %T (%v), want the PRIVMSG that followed them",
			ev, ev.EventName())
	}
	if msg.Text != "after the noise" {
		t.Errorf("text = %q, want the probe message", msg.Text)
	}
}
