//go:build live

package app

import (
	"testing"
	"time"
)

// Sanity-check the create-with-options sequence: JOIN then MODE/TOPIC
// immediately after (the creator is op, so both must be accepted).
func TestLiveCreateOptions(t *testing.T) {
	lines := make(chan string, 500)
	ch := "#ecocreate" + randSuffix()
	cfg := Config{Server: "testnet.ergo.chat", Port: "6697", TLS: true, Nick: "ecoci" + randSuffix()}
	s := NewSession(cfg, UICallbacks{
		AddLine:     func(buf string, l Line) { lines <- buf + " | " + l.Text },
		TopicChange: func(buf, topic string) { lines <- "TOPIC " + buf + " = " + topic },
	})
	s.Connect()
	deadline := time.After(25 * time.Second)
	for !s.Connected() {
		select {
		case l := <-lines:
			t.Log(l)
		case <-deadline:
			t.Fatal("timed out connecting")
		}
	}
	s.irc.Join(ch)
	s.irc.Send("MODE", ch, "+imsk", "sekrit")
	s.irc.Send("TOPIC", ch, "options test")
	deadline = time.After(20 * time.Second)
	gotTopic, gotMode := false, false
	for !gotTopic || !gotMode {
		select {
		case l := <-lines:
			t.Log(l)
			if containsAll(l, "TOPIC "+ch, "options test") {
				gotTopic = true
			}
			if containsAll(l, "mode", "+imsk") {
				gotMode = true
			}
		case <-deadline:
			t.Fatalf("timed out; gotTopic=%v gotMode=%v", gotTopic, gotMode)
		}
	}
	s.Quit()
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !contains(s, x) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
