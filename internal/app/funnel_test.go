package app

import (
	"slices"
	"testing"
)

// The M8 contract where it meets the app: a real Session, a real
// dispatch.Dispatcher and the recorder as its Sink, driven by the fake server.
//
// internal/ui/dispatch's own tests pin the funnel's mechanics (queue,
// coalescing, liveness, ordering, shutdown) headlessly. These pin the wiring:
// that the session emits into it, that the app's teardown is what retires a
// session, and that what the screen is handed is what the buffer holds, in the
// order the buffer holds it.

// A session the app has torn down stops reaching the screen — and does so
// without the twelve hand-rolled guards that used to ask the question, once per
// callback, inside a fyne.Do.
func TestFunnel_ClosedSessionStopsReachingTheScreen(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	d := s.disp
	joinChannel(t, s, srv, "#eco", "@alice")

	srv.Send("@msgid=live1 :alice!alice@test PRIVMSG #eco :before the teardown")
	srv.Sync()
	if got := ui.Delivered("#eco", "before the teardown"); len(got) != 1 {
		t.Fatalf("a live session's line reached the screen %d times, want 1", len(got))
	}

	// This is exactly what UI.closeSession does, and the only thing that makes
	// a session stale.
	d.Close(s.ID())

	srv.Send("@msgid=live2 :alice!alice@test PRIVMSG #eco :after the teardown")
	srv.Sync()

	if got := ui.Delivered("#eco", "after the teardown"); len(got) != 0 {
		t.Errorf("a torn-down session still painted: %+v", got)
	}
	// The core kept working: only the delivery was dropped, so nothing here is
	// hiding a session that quietly stopped applying events.
	if b := s.Buffer("#eco"); b == nil || len(messageLines(b)) != 2 {
		t.Errorf("the store stopped recording when the screen stopped listening: %+v", b)
	}
}

// A session's identity in the funnel is its own, for its whole life. Two
// sessions to the SAME server — which is what a reconnect and the STS plaintext
// upgrade both produce — must never share one: retiring the first would
// otherwise take the second off the screen with it, and the window would go
// quiet while the wire kept talking.
//
// This is the rule dispatch.SessionID exists for, asserted where the id is
// minted. internal/ui/dispatch cannot see how the app derives one, and every
// other test in this package uses a single session — so deriving it from
// s.Config.Server (or from the profile name, which the user may edit) passed
// the whole suite until this test existed.
func TestFunnel_EachSessionHasItsOwnIdentity(t *testing.T) {
	ui, d := newTestSink(t)
	cfg := Config{Server: "irc.test", Port: "6667", Nick: testNick}

	first := NewSession(cfg, d)
	second := NewSession(cfg, d) // the reconnect: same profile, same server
	if first.ID() == second.ID() {
		t.Fatalf("two sessions to %s share the funnel identity %q; retiring one silences the other",
			cfg.Server, first.ID())
	}

	d.Close(first.ID()) // the old connection is torn down

	second.status(serverBuffer, "the new connection is talking")
	first.status(serverBuffer, "the old connection is not")
	settleUI(second)

	if got := ui.Delivered(serverBuffer, "the new connection is talking"); len(got) != 1 {
		t.Errorf("the live session reached the screen %d times, want 1", len(got))
	}
	if got := ui.Delivered(serverBuffer, "the old connection is not"); len(got) != 0 {
		t.Errorf("the retired session still painted: %+v", got)
	}
}

// Tearing a session down is what retires it from the funnel: the app half of
// the one liveness rule.
//
// The assertion is on UI.retireSession — closeSession's model half — because
// "the teardown calls dispatch.Close" is exactly the line a refactor drops, and
// nothing else would notice: the funnel would keep delivering a dead session's
// traffic to a window that no longer lists it. closeSession's other half is
// widgets, and stays where a canvas is needed to see it.
func TestFunnel_TearingDownASessionRetiresIt(t *testing.T) {
	u, s, ui := testWindowUI(t)

	s.status(serverBuffer, "before the teardown")
	settleUI(s)
	if got := ui.Delivered(serverBuffer, "before the teardown"); len(got) != 1 {
		t.Fatalf("a live session's line reached the screen %d times, want 1", len(got))
	}

	next, move := u.retireSession(s)
	if !move || !next.IsZero() {
		t.Errorf("retiring the only session sent the viewer to %+v (moved=%v), want the idle window", next, move)
	}
	if len(u.sessions) != 0 {
		t.Errorf("the torn-down session is still in the window's list: %+v", u.sessions)
	}
	if p := u.vm.Profile(s.ID()); p != "" {
		t.Errorf("the viewmodel still knows the torn-down session as %q", p)
	}

	s.status(serverBuffer, "after the teardown")
	settleUI(s)
	if got := ui.Delivered(serverBuffer, "after the teardown"); len(got) != 0 {
		t.Errorf("a torn-down session still painted: %+v", got)
	}
}

// A burst costs one turn of the UI thread, not one per line. This is the
// milestone's headline, measured where it matters: the old code opened a
// fyne.Do per callback, so forty lines meant forty of them.
func TestFunnel_ABurstIsOneBatch(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	const burst = 40
	before := ui.Batches()
	for i := range burst {
		srv.Send("@msgid=b%d :alice!alice@test PRIVMSG #eco :line %d", i, i)
	}
	srv.Sync()

	if n := len(ui.Lines("#eco")) - 1; n < burst { // -1: the join's status line
		t.Fatalf("the screen was handed %d lines for a burst of %d", n, burst)
	}
	// One is the honest answer and the usual one; the tick may legitimately
	// split a burst that the machine stalls in the middle of, so the assertion
	// is "a handful", not "forty".
	if got := ui.Batches() - before; got > 3 {
		t.Errorf("a burst of %d lines took %d turns of the UI thread; coalescing is gone", burst, got)
	}
}

// What the screen is handed is what the buffer holds, in the buffer's order —
// messages and the app's own status sentences alike.
//
// This is the invariant that lets a status line ("you joined", a usage error)
// be a Change like any other instead of a side channel: a sentence printed
// because of a message has to stay after it.
func TestFunnel_TheScreenSeesTheBufferInOrder(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	srv.Send("@msgid=o1 :alice!alice@test PRIVMSG #eco :first")
	srv.Send(":bob!bob@test JOIN #eco")
	srv.Send("@msgid=o2 :bob!bob@test PRIVMSG #eco :second")
	srv.Send(":bob!bob@test PART #eco :bye")
	srv.Send("@msgid=o3 :alice!alice@test PRIVMSG #eco :third")
	srv.Sync()

	// ...and one of the app's own sentences, which is what every /command
	// prints. It is a Change like any other and keeps its place in the stream;
	// a status line that skipped the funnel would leave the buffer and the
	// screen disagreeing about what was said and when.
	s.status("#eco", "the client had something to say")
	settleUI(s)

	b := s.Buffer("#eco")
	if b == nil {
		t.Fatal("no #eco buffer")
	}
	want := make([]string, 0, len(b.Lines))
	for _, l := range b.Lines {
		want = append(want, l.Text)
	}
	got := make([]string, 0, len(want))
	for _, l := range ui.Lines("#eco") {
		got = append(got, l.Text)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the screen was handed\n %q\nbut the buffer holds\n %q", got, want)
	}
	if len(want) < 6 {
		t.Fatalf("the fixture produced only %d rows (%q); it is not testing interleaving", len(want), want)
	}
}
