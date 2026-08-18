package irc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

func TestBackoffDelayLadder(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: 8 * time.Second, Factor: 2, Jitter: 0}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := b.Delay(i+1, nil); got != w {
			t.Errorf("Delay(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := b.Delay(0, nil); got != time.Second {
		t.Errorf("Delay(0) = %v, want the first rung", got)
	}
	if got := b.Delay(1000, nil); got != 8*time.Second {
		t.Errorf("Delay(1000) = %v, want the cap (no overflow)", got)
	}
}

func TestBackoffJitterStaysInsideItsWindow(t *testing.T) {
	b := Backoff{Initial: 10 * time.Second, Max: time.Minute, Factor: 2, Jitter: 0.25}
	if got := b.Delay(1, func() float64 { return 0.5 }); got != 10*time.Second {
		t.Errorf("jitter at the centre = %v, want the un-jittered delay", got)
	}
	if got := b.Delay(1, func() float64 { return 0 }); got != 7500*time.Millisecond {
		t.Errorf("jitter at the low end = %v, want 7.5s", got)
	}
	// rnd is documented as [0,1), so 1 is the open end; assert the bound.
	if got := b.Delay(1, func() float64 { return 0.999999 }); got <= 10*time.Second || got > 12500*time.Millisecond {
		t.Errorf("jitter at the high end = %v, want just under 12.5s", got)
	}
}

func TestBackoffDefaultsApply(t *testing.T) {
	var zero Backoff
	if got := zero.Delay(1, nil); got != DefaultBackoffInitial {
		t.Errorf("zero Backoff first delay = %v, want %v", got, DefaultBackoffInitial)
	}
	if got := zero.Delay(50, nil); got != DefaultBackoffMax {
		t.Errorf("zero Backoff capped delay = %v, want %v", got, DefaultBackoffMax)
	}
}

func TestReconnectClimbsTheLadderThenRegisters(t *testing.T) {
	var dials *dialCounter
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		var dial func(context.Context, string, string) (net.Conn, error)
		dial, dials = failingDialer(3, srv.Dial)
		cfg.Dial = dial
		cfg.Backoff = Backoff{Initial: time.Second, Max: time.Minute, Factor: 2, Jitter: 0.25}
	})

	// Three refused dials, each reported with the attempt it was and the wait
	// before the next one. The jitter source is pinned to the centre of the
	// window, so the ladder is exactly geometric.
	for i, wantDelay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		attempt := i + 1
		conn := h.waitConn(model.ConnConnecting)
		if conn.Attempt != attempt {
			t.Fatalf("attempt %d reported as %d", attempt, conn.Attempt)
		}
		down := h.waitConn(model.ConnDisconnected)
		if down.Attempt != attempt {
			t.Fatalf("failure %d reported as attempt %d", attempt, down.Attempt)
		}
		if down.RetryIn != wantDelay {
			t.Fatalf("attempt %d retry = %v, want %v", attempt, down.RetryIn, wantDelay)
		}
		if down.Err == "" {
			t.Errorf("attempt %d reported no reason", attempt)
		}
	}

	if conn := h.waitConn(model.ConnConnecting); conn.Attempt != 4 {
		t.Fatalf("fourth attempt reported as %d", conn.Attempt)
	}
	h.srv.Register("eco")
	h.waitConn(model.ConnReady)

	if got := dials.get(); got != 4 {
		t.Errorf("dials = %d, want 4", got)
	}
	if got := h.timer.recorded(); len(got) != 3 ||
		got[0] != time.Second || got[1] != 2*time.Second || got[2] != 4*time.Second {
		t.Errorf("waits = %v, want [1s 2s 4s]", got)
	}
}

func TestReconnectAfterADropResetsTheLadderAndReregisters(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.Backoff = Backoff{Initial: 2 * time.Second, Max: time.Minute, Factor: 2, Jitter: 0}
	})
	h.register()

	// The server drops the link the way a netsplit does.
	h.srv.Close()

	down := h.waitConn(model.ConnDisconnected)
	if down.RetryIn != 2*time.Second {
		t.Fatalf("retry after a dropped connection = %v, want the first rung", down.RetryIn)
	}
	next := h.waitConn(model.ConnConnecting)
	if next.Attempt != 1 {
		t.Fatalf("attempt after a healthy connection = %d, want the ladder reset to 1", next.Attempt)
	}

	// A fresh registration on the new connection, with fresh properties: a
	// reconnect may land on a different server in the same pool.
	h.srv.Register("eco")
	h.waitConn(model.ConnReady)
	if h.c.Props().Network != "TestNet" {
		t.Errorf("properties not rebuilt after the reconnect: %+v", h.c.Props())
	}
	if !h.c.Enabled("batch") {
		t.Error("capabilities not renegotiated after the reconnect")
	}
}

func TestReconnectStopsWhenClosed(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		dial, _ := failingDialer(1000, srv.Dial)
		cfg.Dial = dial
	})
	h.waitConn(model.ConnDisconnected) // at least one failed attempt
	h.c.Close()
	// The stream closes; if the supervisor were still climbing the ladder it
	// would never get here.
	for range h.events {
	}
}
