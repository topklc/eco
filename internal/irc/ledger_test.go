package irc

import (
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// movingClock is a Clock a test can advance by hand.
type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (m *movingClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *movingClock) advance(d time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(d)
	m.mu.Unlock()
}

func TestLedgerLabelsAreUniqueAndCarryTheirRequest(t *testing.T) {
	clock := &movingClock{now: testClock}
	l := newLedger(clock.Now)

	a := l.next(Sent{Kind: CmdMessage, Target: "#eco"})
	b := l.next(Sent{Kind: CmdHistory, Target: "#eco", Direction: model.HistoryBefore, Limit: 50})
	if a == b || a == "" {
		t.Fatalf("labels %q and %q must differ and be non-empty", a, b)
	}
	// Not a bare number: the library underneath reads numeric labels as its
	// own, and eco's correlation is visibly eco's.
	if a[0] != 'e' {
		t.Errorf("label %q does not carry the client's prefix", a)
	}

	got, ok := l.lookup(b)
	if !ok || got.Kind != CmdHistory || got.Direction != model.HistoryBefore || got.Limit != 50 {
		t.Fatalf("lookup(%q) = %+v ok=%v", b, got, ok)
	}
	if _, ok := l.lookup(""); ok {
		t.Error("the empty label must never resolve")
	}
	l.forget(b)
	if _, ok := l.lookup(b); ok {
		t.Error("forget did not drop the entry")
	}
}

func TestLedgerExpiresUnansweredLabels(t *testing.T) {
	clock := &movingClock{now: testClock}
	l := newLedger(clock.Now)

	stale := l.next(Sent{Kind: CmdMessage, Target: "#eco"})
	clock.advance(ledgerTTL + time.Minute)
	// The expiry scan runs on a schedule, so drive allocation past it.
	for i := 0; i < ledgerSweepEvery; i++ {
		l.next(Sent{Kind: CmdMessage, Target: "#eco"})
	}
	if _, ok := l.lookup(stale); ok {
		t.Error("a label the server never answered was remembered forever")
	}
}

func TestLedgerEvictsRatherThanClearing(t *testing.T) {
	clock := &movingClock{now: testClock}
	l := newLedger(clock.Now)

	var labels []string
	for i := 0; i < ledgerMax*2; i++ {
		labels = append(labels, l.next(Sent{Kind: CmdMessage, Target: "#eco"}))
	}
	if n := l.len(); n > ledgerMax {
		t.Fatalf("ledger holds %d entries, want at most %d", n, ledgerMax)
	}
	// The most recent send must still resolve: a bound that forgets the newest
	// entries is worse than no bound at all.
	if _, ok := l.lookup(labels[len(labels)-1]); !ok {
		t.Error("the newest label was evicted")
	}
	if _, ok := l.lookup(labels[0]); ok {
		t.Error("the oldest label survived the cap")
	}
}

func TestNoLabeledResponseMeansNoLabels(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		// A server from before IRCv3's labeled-response.
		srv.Caps = []string{"multi-prefix", "server-time"}
	})
	h.register()

	if h.c.Enabled("labeled-response") {
		t.Fatal("labeled-response should have been NAKed")
	}
	handle, err := h.c.SendMessage("#eco", "plain old irc")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if handle.Label() != "" || len(handle.Labels) != 0 {
		t.Fatalf("handle = %+v, want no labels", handle)
	}
	line := h.srv.WaitFor("*PRIVMSG #eco*")
	if line[0] == '@' {
		t.Errorf("tagged a server that never agreed to read tags: %q", line)
	}
	if h.c.ledger.len() != 0 {
		t.Error("labels were allocated for a server that cannot answer them")
	}
}
