//go:build live

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// TestSendMessageEcho is the live half of the cutover's headline change. Sending
// a message renders a *pending* row carrying the labelled-response label
// internal/irc put on the wire, and the server's echo resolves that row in
// place - so the message ends up with the server's real msgid.
//
// v1 matched the echo by buffer and text and dropped it, which is why its own
// messages had no msgid and could be neither redacted nor read-marked, and why
// two identical messages in a row ate one echo. That is the thing to watch on a
// real server: the offline suite proves the mechanism against internal/testsrv,
// and this proves Ergo plays the part testsrv scripts.
func TestSendMessageEcho(t *testing.T) {
	lines := make(chan string, 200)
	cfg := Config{Server: "testnet.ergo.chat", Port: "6697", TLS: true, Nick: "sendci" + randSuffix(), Channels: "#chat"}
	s := newLiveSession(t, cfg, &testUI{topics: map[string]string{}, unread: map[string]viewmodel.Counts{},
		onLine: func(buf string, l Line) { lines <- buf + " | " + l.Nick + " | " + l.Text }})
	s.Connect()
	deadline := time.After(25 * time.Second)
	for {
		select {
		case l := <-lines:
			if strings.Contains(l, "You joined #chat") {
				goto joined
			}
		case <-deadline:
			t.Fatal("never joined #chat")
		}
	}
joined:
	const sent = "hello from test harness"
	s.SendMessage("#chat", sent)
	deadline = time.After(15 * time.Second)
	for {
		select {
		case l := <-lines:
			t.Log(l)
			if strings.Contains(l, sent) {
				t.Log("SUCCESS: own message displayed")
				goto displayed
			}
		case <-deadline:
			t.Fatal("sent message never displayed")
		}
	}
displayed:
	// The row is displayed the moment it is sent, so its msgid arrives a round
	// trip later: poll rather than assert once.
	if !s.hasCap("labeled-response") || !s.hasCap("echo-message") {
		t.Skip("server negotiated neither labeled-response nor echo-message: there " +
			"is nothing to correlate an echo with, and the local copy IS the message")
	}
	for until := time.Now().Add(15 * time.Second); ; {
		if id := ownMsgID(s, "#chat", sent); id != "" {
			t.Log("SUCCESS: the labelled echo resolved our row, msgid ", id)
			break
		}
		if time.Now().After(until) {
			t.Fatal("our own message never gained the server's msgid: the labelled " +
				"echo did not resolve the pending row, so this message cannot be " +
				"redacted or read-marked")
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Quit()
}

// ownMsgID returns the msgid of our own copy of text in a buffer, or "".
//
// It reads the projection under the session's own lock rather than through
// Session.Buffer, which hands back a struct the pump goroutine is still writing
// to: this suite runs under -race, and the rest of it only ever compares that
// pointer against nil.
func ownMsgID(s *Session, buffer, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bufs[s.foldLocked(buffer)]
	if b == nil {
		return ""
	}
	for _, l := range b.Lines {
		if l.Self && strings.Contains(l.Text, text) {
			return l.MsgID
		}
	}
	return ""
}
