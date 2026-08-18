package irc

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// testClock pins every client-side timestamp, so an event's Time is either the
// server's server-time tag or exactly this instant, and a test can tell which.
var testClock = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

// eventTimeout bounds every wait on the event stream. Nothing here sleeps;
// this is a "something is wrong" budget.
const eventTimeout = 5 * time.Second

// fakeTimer stands in for the backoff sleep: it fires immediately and records
// what it was asked to wait, so the schedule is asserted instead of endured.
type fakeTimer struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeTimer) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func (f *fakeTimer) recorded() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.waits...)
}

// harness is a Client wired to a scripted fake server.
type harness struct {
	t      *testing.T
	srv    *testsrv.Server
	c      *Client
	timer  *fakeTimer
	events <-chan model.Event
}

// newHarness builds a client against a fresh fake server. mutate, when given,
// adjusts the config (and may reach the server through h.srv) before Start.
func newHarness(t *testing.T, mutate func(cfg *Config, srv *testsrv.Server)) *harness {
	t.Helper()
	srv := testsrv.New(t)
	timer := &fakeTimer{}

	cfg := Config{
		Network:     "testnet",
		Server:      "irc.test",
		Port:        "6667",
		Nick:        "eco",
		QuitMessage: "bye",
		Version:     "eco-test",
		EventBuffer: 4096,
		// A decode bug must fail the test, not be swallowed by the library's
		// callback recover and logged to a discarded logger.
		AllowPanic: true,
		// The fake server never answers a keepalive PING, so the keepalive is
		// pushed well past any test's lifetime; the registration timeout stays
		// short enough that a hung handshake fails fast.
		Timeout:   10 * time.Second,
		KeepAlive: 10 * time.Minute,
		Dial:      srv.Dial,
		Clock:     model.FixedClock(testClock),
		Timer:     timer.After,
		// 0.5 is the exact centre of the jitter window, so the ladder is the
		// un-jittered geometric one and can be asserted exactly.
		Rand: func() float64 { return 0.5 },
	}
	if mutate != nil {
		mutate(&cfg, srv)
	}

	c := New(cfg)
	h := &harness{t: t, srv: srv, c: c, timer: timer, events: c.Events()}
	t.Cleanup(func() {
		c.Close()
		// The channel is closed by then; draining it proves nothing was left
		// mid-flight and that emit never blocked forever.
		for range h.events {
		}
	})
	c.Start()
	return h
}

// next returns the next event, failing the test if none arrives.
func (h *harness) next() model.Event {
	h.t.Helper()
	select {
	case ev, ok := <-h.events:
		if !ok {
			h.t.Fatalf("event stream closed while waiting for an event")
		}
		return ev
	case <-time.After(eventTimeout):
		h.t.Fatalf("no event within %v", eventTimeout)
		return nil
	}
}

// waitFor consumes events until one has the given EventName, and returns it.
// Skipped events are reported on failure so the test says what it did see.
func (h *harness) waitFor(name string) model.Event {
	h.t.Helper()
	var seen []string
	deadline := time.After(eventTimeout)
	for {
		select {
		case ev, ok := <-h.events:
			if !ok {
				h.t.Fatalf("event stream closed waiting for %q (saw %v)", name, seen)
			}
			if ev.EventName() == name {
				return ev
			}
			seen = append(seen, ev.EventName())
		case <-deadline:
			h.t.Fatalf("no %q event within %v (saw %v)", name, eventTimeout, seen)
			return nil
		}
	}
}

// waitConn consumes events until a ConnState with the given status.
func (h *harness) waitConn(status model.ConnStatus) model.ConnState {
	h.t.Helper()
	var seen []string
	deadline := time.After(eventTimeout)
	for {
		select {
		case ev, ok := <-h.events:
			if !ok {
				h.t.Fatalf("event stream closed waiting for conn %v (saw %v)", status, seen)
			}
			if cs, isConn := ev.(model.ConnState); isConn {
				if cs.Status == status {
					return cs
				}
				seen = append(seen, "conn:"+cs.Status.String())
				continue
			}
			seen = append(seen, ev.EventName())
		case <-deadline:
			h.t.Fatalf("no conn %v within %v (saw %v)", status, eventTimeout, seen)
			return model.ConnState{}
		}
	}
}

// register drives the whole handshake and waits until the client reports
// itself ready.
func (h *harness) register() {
	h.t.Helper()
	h.srv.Register("eco")
	h.waitConn(model.ConnReady)
}

// join walks the client through joining a channel so message tests have a
// buffer to talk about.
func (h *harness) join(channel string) {
	h.t.Helper()
	if _, err := h.c.Join(channel, ""); err != nil {
		h.t.Fatalf("join %s: %v", channel, err)
	}
	h.srv.WaitFor("*JOIN " + channel)
	h.srv.Send(":eco!eco@test JOIN %s", channel)
	h.srv.Send(":irc.test 353 eco = %s :@eco alice", channel)
	h.srv.Send(":irc.test 366 eco %s :End of /NAMES list.", channel)
	h.srv.Sync()
}

// failingDialer refuses the first n dials and then hands over to the real one,
// which is how consecutive connection failures are produced without a network.
func failingDialer(n int, onward func(context.Context, string, string) (net.Conn, error)) (func(context.Context, string, string) (net.Conn, error), *dialCounter) {
	c := &dialCounter{}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if int(c.add()) <= n {
			return nil, errors.New("connection refused")
		}
		return onward(ctx, network, addr)
	}, c
}

type dialCounter struct {
	mu sync.Mutex
	n  int
}

func (c *dialCounter) add() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

func (c *dialCounter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// hasTag reports whether a client line carries tag=value.
func hasTag(line, tag, value string) bool {
	if !strings.HasPrefix(line, "@") {
		return false
	}
	tags, _, _ := strings.Cut(line[1:], " ")
	for _, kv := range strings.Split(tags, ";") {
		k, v, _ := strings.Cut(kv, "=")
		if k == tag && (value == "" || v == value) {
			return true
		}
	}
	return false
}

// untagged strips the message-tag prefix from a client line.
func untagged(line string) string {
	if !strings.HasPrefix(line, "@") {
		return line
	}
	_, rest, _ := strings.Cut(line, " ")
	return rest
}

// tagValue returns the value of a tag on a client line, "" if absent.
func tagValue(line, tag string) string {
	if !strings.HasPrefix(line, "@") {
		return ""
	}
	tags, _, _ := strings.Cut(line[1:], " ")
	for _, kv := range strings.Split(tags, ";") {
		k, v, _ := strings.Cut(kv, "=")
		if k == tag {
			return v
		}
	}
	return ""
}
