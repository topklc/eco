package main

import (
	"strings"
	"testing"
	"time"
)

func TestSendMessageEcho(t *testing.T) {
	lines := make(chan string, 200)
	cfg := Config{Server: "testnet.ergo.chat", Port: "6697", TLS: true, Nick: "sendci" + randSuffix(), Channels: "#chat"}
	s := NewSession(cfg, UICallbacks{
		AddLine: func(buf string, l Line) { lines <- buf + " | " + l.Nick + " | " + l.Text },
	})
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
	s.SendMessage("#chat", "hello from test harness")
	deadline = time.After(15 * time.Second)
	for {
		select {
		case l := <-lines:
			t.Log(l)
			if strings.Contains(l, "hello from test harness") {
				t.Log("SUCCESS: own message displayed via echo")
				s.Quit()
				return
			}
		case <-deadline:
			t.Fatal("sent message never displayed")
		}
	}
}
