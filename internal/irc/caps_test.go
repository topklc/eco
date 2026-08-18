package irc

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// restrictedCaps is a deliberately small advertisement: everything else eco
// asks for comes back NAKed, which is the case a client meets on most
// networks that are not Ergo.
var restrictedCaps = []string{
	"batch", "message-tags", "labeled-response", "server-time",
	"multi-prefix", "sts=duration=2592000,preload",
}

func TestCapNegotiationRecordsAckAndNak(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = restrictedCaps
	})
	h.srv.Register("eco")

	var (
		sawLS   bool
		sawNak  bool
		enabled []string
	)
	deadline := time.After(eventTimeout)
collect:
	for {
		select {
		case ev := <-h.events:
			switch e := ev.(type) {
			case model.CapChange:
				switch e.Op {
				case model.CapLS:
					sawLS = true
					if e.More {
						t.Error("the fake server sends one LS line; More must be false")
					}
				case model.CapNAK:
					sawNak = true
				}
				enabled = e.Enabled
			case model.ConnState:
				if e.Status == model.ConnReady {
					break collect
				}
			}
		case <-deadline:
			t.Fatal("never reached ready")
		}
	}
	if !sawLS || !sawNak {
		t.Fatalf("LS seen=%v NAK seen=%v", sawLS, sawNak)
	}
	// CapChange.Enabled is the whole enabled set after the change, so a
	// consumer never has to replay CAP state itself.
	want := map[string]bool{"batch": true, "message-tags": true, "labeled-response": true,
		"server-time": true, "multi-prefix": true}
	got := map[string]bool{}
	for _, n := range enabled {
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("cap %q missing from the enabled set %v", n, enabled)
		}
	}
	if got["echo-message"] || got["draft/multiline"] {
		t.Errorf("a NAKed cap ended up enabled: %v", enabled)
	}
	if h.c.Enabled("draft/multiline") {
		t.Error("Enabled disagrees with the NAK")
	}
	if _, ok := h.c.MultilineLimits(); ok {
		t.Error("multiline limits reported for a cap that was refused")
	}
}

func TestSTSPolicyIsSurfacedFromTheAdvertisedSet(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = restrictedCaps
	})
	h.register()

	// sts is never requested, so it only ever appears in what the server
	// advertised — which is exactly where it is read from.
	policy, ok, err := h.c.STS()
	if err != nil {
		t.Fatalf("sts: %v", err)
	}
	if !ok {
		t.Fatal("no sts policy seen")
	}
	if !policy.HasDuration || policy.Duration != 30*24*time.Hour {
		t.Errorf("sts duration = %v", policy.Duration)
	}
	if !policy.Preload {
		t.Error("sts preload flag lost")
	}
	if policy.Revocation() {
		t.Error("duration=2592000 is not a revocation")
	}
	if h.c.Enabled("sts") {
		t.Error("sts must not be in the enabled set: a client never requests it")
	}
}

func TestCapNewIsRequestedAndCapDelStandsDown(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = restrictedCaps
	})
	h.register()

	// cap-notify: a capability that appears later is asked for, not ignored.
	h.srv.Send(":irc.test CAP eco NEW :draft/chathistory")
	req := h.srv.WaitFor("CAP REQ*")
	if got := untagged(req); got != "CAP REQ draft/chathistory" && got != "CAP REQ :draft/chathistory" {
		t.Fatalf("cap req = %q", got)
	}
	newCap := h.waitFor("capchange").(model.CapChange)
	if newCap.Op != model.CapNEW || len(newCap.Caps) != 1 || newCap.Caps[0].Name != "draft/chathistory" {
		t.Fatalf("cap new = %+v", newCap)
	}

	h.srv.Send(":irc.test CAP eco ACK :draft/chathistory")
	ack := h.waitFor("capchange").(model.CapChange)
	if ack.Op != model.CapACK {
		t.Fatalf("want an ACK, got %v", ack.Op)
	}
	if !h.c.Enabled("draft/chathistory") {
		t.Error("draft/chathistory not enabled after the ACK")
	}

	h.srv.Send(":irc.test CAP eco DEL :draft/chathistory")
	del := h.waitFor("capchange").(model.CapChange)
	if del.Op != model.CapDEL {
		t.Fatalf("want a DEL, got %v", del.Op)
	}
	if h.c.Enabled("draft/chathistory") {
		t.Error("a DELed capability is still enabled")
	}
	for _, n := range del.Enabled {
		if n == "draft/chathistory" {
			t.Error("the DELed capability is still in CapChange.Enabled")
		}
	}
}

func TestCapLSContinuationIsFlagged(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = restrictedCaps
	})
	h.register()

	h.srv.Send(":irc.test CAP eco LS * :draft/relaymsg")
	first := h.waitFor("capchange").(model.CapChange)
	if !first.More {
		t.Error("a CAP LS continuation line must set More")
	}
	h.srv.Send(":irc.test CAP eco LS :draft/persistence")
	second := h.waitFor("capchange").(model.CapChange)
	if second.More {
		t.Error("the final CAP LS line must clear More")
	}
}
