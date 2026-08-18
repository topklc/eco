//go:build live

package main

import (
	"testing"
	"time"
)

func TestLiveErgoTestnet(t *testing.T) {
	lines := make(chan string, 100)
	cfg := Config{Server: "testnet.ergo.chat", Port: "6697", TLS: true, Nick: "ecoci" + randSuffix(), Channels: "#chat"}
	s := NewSession(cfg, UICallbacks{
		AddLine: func(buf string, l Line) { lines <- buf + " | " + l.Text },
	})
	s.Connect()
	deadline := time.After(25 * time.Second)
	connected := false
	for !connected {
		select {
		case l := <-lines:
			t.Log(l)
			if s.Connected() && s.Buffer("#chat") != nil {
				connected = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for connect+join")
		}
	}
	t.Log("caps:", s.irc.AcknowledgedCaps())
	if !s.hasCap("draft/chathistory") {
		t.Error("expected draft/chathistory cap from Ergo")
	}
	s.Quit()
}
