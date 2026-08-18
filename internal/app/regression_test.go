package app

import (
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// The M5 regression contract: a real Session driven against the scripted fake
// server in internal/testsrv, asserting on session/buffer state — never on
// rendered display strings. Everything here is offline and deterministic;
// synchronisation is srv.Sync(), never a sleep.
//
// newTestSession and friends are the shared fixture for the whole
// TestRegression_* family, so keep them boring and general.

const testNick = "eco"

// testConfig is the Session config the fixture uses: a plain, TLS-less
// connection whose address is never dialled (the seam replaces the dialer).
func testConfig() Config {
	return Config{Server: "irc.test", Port: "6667", Nick: testNick}
}

// newTestSession returns a Session connected to a fake server and fully
// registered — the state every regression test starts from. Tests must not
// run in parallel: the fixture pins the global settings snapshot.
func newTestSession(t *testing.T) (*Session, *testsrv.Server) {
	t.Helper()
	s, srv, _ := newTestSessionUI(t)
	return s, srv
}

// newTestSessionUI is newTestSession plus the recorder holding every UI
// callback the session fired.
func newTestSessionUI(t *testing.T) (*Session, *testsrv.Server, *testUI) {
	t.Helper()
	s, srv, ui := startTestSession(t, testConfig())
	srv.Register(testNick)
	return s, srv, ui
}

// startTestSession builds a Session on a fake server and calls Connect, but
// leaves the handshake to the caller — the entry point for tests that script
// registration itself (SASL, capability refusals, STS).
func startTestSession(t *testing.T, cfg Config) (*Session, *testsrv.Server, *testUI) {
	t.Helper()
	pinTestSettings(t)

	srv := testsrv.New(t)
	ui := &testUI{topics: map[string]string{}}
	s := NewSession(cfg, ui.callbacks())
	s.Dial = srv.Dial      // the test seam
	srv.Barrier = s.settle // the other half of it (see below)
	s.Connect()
	stopSession(t, s)
	return s, srv, ui
}

// Since the M7 cutover the Session decodes on the client's goroutine and applies
// on its own, so the fake server's PING/PONG barrier only proves the client has
// *read* every earlier line. srv.Barrier closes that gap: Sync runs it after the
// PONG, and it returns once the adapter has folded everything in. Every fixture
// in this package wires it, which is why no individual test had to change how it
// synchronises.

// stopSession registers the goleak-style teardown every fixture must use: it
// quits the session and then WAITS for its connection goroutine to return.
//
// Quit is asynchronous — it only sets the quit flag and writes QUIT, and the
// server closing the connection is what actually unwinds ircevent's Loop — so
// a cleanup that merely called Quit proved nothing. Waiting on the seam turns
// "no test leaves a Session goroutine running" into an assertion rather than a
// hope, and it is a channel receive, not a sleep: it costs nothing when the
// session shuts down cleanly and fails loudly when it does not.
//
// Register it AFTER srv's own t.Cleanup so it runs BEFORE the server is torn
// down (cleanups unwind last-in-first-out): the QUIT needs a live server to
// answer it by closing the pipe.
func stopSession(t *testing.T, s *Session) {
	t.Helper()
	t.Cleanup(func() {
		s.Quit()
		if s.stopped == nil {
			return // Connect was never called; there is no goroutine to wait for
		}
		select {
		case <-s.stopped:
		case <-time.After(stopTimeout):
			t.Errorf("session goroutine still running %v after Quit — the test leaks it", stopTimeout)
		}
	})
}

// stopTimeout bounds the teardown wait. Everything here is a net.Pipe in the
// same process, so this is a "something is wedged" budget, not a latency one.
const stopTimeout = 10 * time.Second

// joinChannel drives a complete user-initiated JOIN of ch and returns its
// buffer, once the session has processed the whole burst — the JOIN echo, a
// topic, the NAMES list and its terminator — and once the requests the join
// itself provokes (MODE, CHATHISTORY) have been consumed. Draining those
// matters: an unconsumed one would satisfy a later Expect by accident.
//
// members are 353-style entries ("@alice", "+bob"); our own nick is always
// included. This is the one join fixture for the whole TestRegression_*
// family — the four suites each grew their own near-identical copy, and three
// slightly different handshakes are three slightly different things to debug.
func joinChannel(t *testing.T, s *Session, srv *testsrv.Server, ch string, members ...string) *Buffer {
	t.Helper()
	s.JoinUser(ch)
	srv.WaitFor("JOIN " + ch)
	srv.Send(":%s!%s@test JOIN %s", testNick, testNick, ch)
	srv.Send(":irc.test 332 %s %s :topic of %s", testNick, ch, ch)
	srv.Send(":irc.test 353 %s = %s :%s", testNick, ch, strings.Join(append([]string{testNick}, members...), " "))
	srv.Send(":irc.test 366 %s %s :End of /NAMES list.", testNick, ch)
	srv.Sync()
	srv.WaitFor("MODE " + ch)
	srv.WaitFor("CHATHISTORY LATEST " + ch)
	b := s.Buffer(ch)
	if b == nil {
		t.Fatalf("no %s buffer after the JOIN handshake", ch)
	}
	return b
}

// pinTestSettings makes the global settings snapshot deterministic for the
// duration of the test: no channel scan on connect, no auto-join storm, no
// hidden join/part lines and unbounded scrollback.
func pinTestSettings(t *testing.T) {
	t.Helper()
	prev := config.Current()
	next := *prev
	next.AutoScanChannels = false
	next.JoinAllChannels = false
	next.HideJoinParts = false
	next.ScrollbackCap = 0
	config.Publish(&next)
	t.Cleanup(func() { config.Publish(prev) })
}

func TestRegression_JoinChannelAndReceiveMessage(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	if !s.Connected() {
		t.Fatal("session reports not connected after registration")
	}
	if got := s.CurrentNick(); got != testNick {
		t.Fatalf("CurrentNick = %q, want %q", got, testNick)
	}
	if got := srv.Nick(); got != testNick {
		t.Fatalf("server saw NICK %q, want %q", got, testNick)
	}

	s.JoinUser("#eco")
	srv.Expect("JOIN #eco")
	srv.Replay(`
		# the nick is testNick; the fixture registers as "eco"
		S: :eco!eco@test JOIN #eco
		S: :irc.test 332 eco #eco :a channel for testing
		S: :irc.test 353 eco = #eco :@alice eco +bob
		S: :irc.test 366 eco #eco :End of /NAMES list.
	`)
	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG #eco :hello eco, welcome")
	srv.Sync()

	b := s.Buffer("#eco")
	if b == nil {
		t.Fatal("no #eco buffer after JOIN")
	}
	if b.Topic != "a channel for testing" {
		t.Errorf("topic = %q, want %q", b.Topic, "a channel for testing")
	}
	wantMembers := map[string]string{"alice": "@", "eco": "", "bob": "+"}
	if !maps.Equal(b.Members, wantMembers) {
		t.Errorf("members = %v, want %v", b.Members, wantMembers)
	}

	msgs := messageLines(b)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages in #eco, want 1: %+v", len(msgs), msgs)
	}
	got := msgs[0]
	switch {
	case got.Nick != "alice":
		t.Errorf("nick = %q, want %q", got.Nick, "alice")
	case got.Text != "hello eco, welcome":
		t.Errorf("text = %q, want %q", got.Text, "hello eco, welcome")
	case got.MsgID != "m1":
		t.Errorf("msgid = %q, want %q", got.MsgID, "m1")
	case !got.Highlight:
		t.Error("mention of our nick did not set Highlight")
	case got.Self || got.Playback || got.Action || got.Notice || got.Status:
		t.Errorf("wrong line flags: %+v", got)
	}

	// the same msgid replayed must not duplicate the message (memory.md:
	// per-buffer msgid seen-set)
	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG #eco :hello eco, welcome")
	srv.Sync()
	if n := len(messageLines(s.Buffer("#eco"))); n != 1 {
		t.Errorf("after replaying msgid m1 the buffer holds %d messages, want 1", n)
	}

	// the UI saw the same line the buffer holds
	shown := ui.Lines("#eco")
	if len(shown) == 0 || shown[len(shown)-1] != got {
		t.Errorf("last line handed to the UI = %+v, want %+v", shown, got)
	}

	// joining asked the server for the channel's modes and its scrollback
	srv.WaitFor("MODE #eco")
	srv.WaitFor("CHATHISTORY LATEST #eco")
}

// TestTestsrv_ReconnectSeam proves the harness's disconnect/redial seam
// works, so the reconnect regressions can be written against it. ircevent
// reconnects immediately the FIRST time a connection drops and only then
// waits ReconnectFreq (30s, set in Connect), so a test may drop the
// connection once for free; dropping it twice needs a longer srv.Timeout.
func TestTestsrv_ReconnectSeam(t *testing.T) {
	s, srv := newTestSession(t)
	s.JoinUser("#eco")
	srv.Expect("JOIN #eco")
	srv.Send(":eco!eco@test JOIN #eco")
	srv.Sync()

	srv.Close()    // server-side disconnect
	srv.NextConn() // the client redials
	srv.Register(testNick)

	// the open channel is rejoined on the new connection (memory.md)
	srv.WaitFor("JOIN #eco")
	if s.Buffer("#eco") == nil {
		t.Error("#eco buffer did not survive the reconnect")
	}
}

// messageLines returns a buffer's real messages, dropping status lines
// (joins, mode notices) so assertions do not depend on their wording.
func messageLines(b *Buffer) []Line {
	var out []Line
	for _, l := range b.Lines {
		if !l.Status {
			out = append(out, l)
		}
	}
	return out
}

// recordedLine is one AddLine callback.
type recordedLine struct {
	Buffer string
	Line   Line
}

// testUI records every UICallbacks event a Session fires. The session calls
// back from ircevent's read goroutine, so every field is guarded; it never
// touches *testing.T, which keeps it safe when the connection goroutine
// outlives the test body.
type testUI struct {
	mu sync.Mutex

	lines      []recordedLine
	amended    []string
	members    []string
	topics     map[string]string
	joinFails  []JoinFailure
	renames    [][2]string
	redactions []string
	highlights []recordedLine // Buffer + a Line carrying Nick and Text
	stsPorts   []string
	bufferList int
	listDone   int
	connected  bool
	status     string
}

// JoinFailure is one JoinFailed callback.
type JoinFailure struct {
	Channel string
	Reason  string
	NeedKey bool
}

func (u *testUI) callbacks() UICallbacks {
	return UICallbacks{
		AddLine: func(buffer string, l Line) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.lines = append(u.lines, recordedLine{buffer, l})
		},
		AmendBuffer: func(buffer string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.amended = append(u.amended, buffer)
		},
		BufferList: func() {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.bufferList++
		},
		MembersChange: func(buffer string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.members = append(u.members, buffer)
		},
		TopicChange: func(buffer, topic string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.topics[buffer] = topic
		},
		StatusChange: func(connected bool, status string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.connected, u.status = connected, status
		},
		RedactMsg: func(buffer, msgid string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.redactions = append(u.redactions, buffer+"|"+msgid)
		},
		STS: func(port string, durationSecs int64) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.stsPorts = append(u.stsPorts, port)
		},
		RenameBuffer: func(oldName, newName string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.renames = append(u.renames, [2]string{oldName, newName})
		},
		HighlightNote: func(buffer, nick, text string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.highlights = append(u.highlights, recordedLine{buffer, Line{Nick: nick, Text: text}})
		},
		JoinFailed: func(channel, reason string, needKey bool) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.joinFails = append(u.joinFails, JoinFailure{channel, reason, needKey})
		},
		ChannelListDone: func() {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.listDone++
		},
	}
}

// Lines returns the lines the session handed the UI for one buffer.
func (u *testUI) Lines(buffer string) []Line {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []Line
	for _, r := range u.lines {
		if r.Buffer == buffer {
			out = append(out, r.Line)
		}
	}
	return out
}

// AllLines returns every AddLine callback, in order.
func (u *testUI) AllLines() []recordedLine {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedLine(nil), u.lines...)
}

// MemberChanges returns the buffers MembersChange fired for, in order.
func (u *testUI) MemberChanges() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.members...)
}

// AmendedBuffers returns the buffers AmendBuffer fired for, in order.
func (u *testUI) AmendedBuffers() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.amended...)
}

// Topic returns the last topic the UI was told about for a buffer.
func (u *testUI) Topic(buffer string) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.topics[buffer]
}

// Status returns the last StatusChange.
func (u *testUI) Status() (connected bool, text string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.connected, u.status
}

// JoinFailures returns the refused joins the UI was asked to surface.
func (u *testUI) JoinFailures() []JoinFailure {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]JoinFailure(nil), u.joinFails...)
}

// Renames returns the {old, new} buffer renames, in order.
func (u *testUI) Renames() [][2]string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([][2]string(nil), u.renames...)
}

// Redactions returns "buffer|msgid" for each RedactMsg callback.
func (u *testUI) Redactions() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.redactions...)
}

// Highlights returns the mentions the UI was notified about.
func (u *testUI) Highlights() []recordedLine {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedLine(nil), u.highlights...)
}

// STSPorts returns the ports advertised through the STS callback.
func (u *testUI) STSPorts() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.stsPorts...)
}

// BufferListCalls counts the times the buffer list was invalidated.
func (u *testUI) BufferListCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.bufferList
}

// ChannelListDoneCalls counts completed channel scans.
func (u *testUI) ChannelListDoneCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.listDone
}
