//go:build live

package app

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// TestLivePart covers "leaving channels doesn't work": a normal part must
// close the buffer, and parting a stale buffer for a channel we are not
// actually on (the server refuses with 442/403) must close it too.
func TestLivePart(t *testing.T) {
	lines := make(chan string, 500)
	ch := "#ecoparttest" + randSuffix()
	cfg := Config{Server: "testnet.ergo.chat", Port: "6697", TLS: true, Nick: "ecoci" + randSuffix(), Channels: ch}
	s := newLiveSession(t, cfg, &testUI{topics: map[string]string{}, unread: map[string]viewmodel.Counts{},
		onLine:       func(buf string, l Line) { lines <- buf + " | " + l.Text },
		onBufferList: func() { lines <- "(buffer list changed)" }})
	s.Connect()

	wait := func(what string, done func() bool) {
		deadline := time.After(25 * time.Second)
		for !done() {
			select {
			case l := <-lines:
				t.Log(l)
			case <-deadline:
				t.Fatal("timed out waiting for " + what)
			}
		}
	}

	wait("connect+join", func() bool { return s.Connected() && s.Buffer(ch) != nil })
	t.Log("joined, now parting")
	s.Part(ch)
	wait("part to close the buffer", func() bool { return s.Buffer(ch) == nil })

	// stale buffer: we never joined this channel, so the server refuses
	// the PART; the refusal must still close the buffer
	stale := "#econotjoined" + randSuffix()
	s.getBuffer(stale)
	s.Part(stale)
	wait("refused part to close the stale buffer", func() bool { return s.Buffer(stale) == nil })

	// being kicked must auto-leave: rejoin a channel and kick ourselves
	ch2 := "#ecokicktest" + randSuffix()
	s.irc.Join(ch2)
	wait("join of kick-test channel", func() bool { return s.Buffer(ch2) != nil })
	s.irc.Send("KICK", ch2, s.irc.CurrentNick())
	wait("self-kick to close the buffer", func() bool { return s.Buffer(ch2) == nil })

	t.Log("all buffers closed")
	s.Quit()
}
