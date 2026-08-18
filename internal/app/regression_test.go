package app

import (
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
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
	ui, d := newTestSink(t)
	s := NewSession(cfg, d)
	s.Dial = srv.Dial      // the test seam
	srv.Barrier = s.settle // the other half of it (see below)
	s.Connect()
	stopSession(t, s)
	return s, srv, ui
}

// newTestSink builds the funnel a test drives a Session through: the real
// dispatch.Dispatcher, on its real 80ms window, with the recorder as its Sink
// and fyne.Do replaced by a direct call — there is no Fyne app in a test, and
// the funnel's own seam is what makes that possible.
//
// The window is the production one on purpose: what makes the suite
// deterministic is not a shortened tick but srv.Sync()'s barrier, which runs
// through Session.settle into Dispatcher.Flush.
//
// The Stop cleanup is registered here, BEFORE stopSession registers its own, so
// it runs after it: the session's goroutine has to be gone before the funnel it
// emits into is.
func newTestSink(t *testing.T) (*testUI, *dispatch.Dispatcher) {
	t.Helper()
	ui := &testUI{topics: map[string]string{}}
	d := dispatch.New(dispatch.Options{Sink: ui, Do: func(f func()) { f() }})
	t.Cleanup(d.Stop)
	return ui, d
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

// newLiveSession builds a Session on a real server through the same funnel the
// offline suite uses: the live tests watch the stream through the recorder's
// hooks rather than through callbacks of their own, so there is exactly one
// Sink implementation in this package's tests.
func newLiveSession(t *testing.T, cfg Config, ui *testUI) *Session {
	t.Helper()
	d := dispatch.New(dispatch.Options{Sink: ui, Do: func(f func()) { f() }})
	t.Cleanup(d.Stop)
	return NewSession(cfg, d)
}

// settleUI waits until everything the session has already produced has reached
// the screen. srv.Sync() does this for anything the server said — its barrier
// runs through Session.settle — and this is its counterpart for a verb the user
// invoked locally (CloseBuffer, SendMessage, Part), which since M8 reaches the
// screen through the same coalescing funnel rather than through a synchronous
// callback. It is a channel handshake, not a sleep.
func settleUI(s *Session) { s.settle() }

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

// recordedLine is one row the funnel handed the screen.
type recordedLine struct {
	Buffer string
	Line   Line
}

// testUI is the offline suite's screen: a dispatch.Sink, the same interface the
// real UI implements, so every assertion below is made about the production
// path — session → funnel → sink — and not about a stub of it.
//
// It is written to from the dispatcher's goroutine, so every field is guarded;
// it never touches *testing.T, which keeps it safe when a batch is still
// landing as the test body returns.
type testUI struct {
	mu sync.Mutex

	lines      []recordedLine
	amended    []string
	members    []string
	topics     map[string]string
	joinFails  []JoinFailure
	renames    [][2]string
	redactions []string
	stsPorts   []string
	bufferList int
	listDone   int // scans the browser was asked to open for
	scans      int // scans that finished at all
	batches    int // BatchDone calls: one per coalesced batch
	conn       dispatch.ConnState

	// Hooks let a test watch the stream as it arrives rather than after the
	// fact; the live suite drives its waits off them. They are read under the
	// lock and called outside it, so a hook may block on a channel.
	onLine       func(buffer string, l Line)
	onTopic      func(buffer, topic string)
	onBufferList func()
	onSTS        func(port string, seconds int64)
}

// JoinFailure is one refused join the UI was asked to surface.
type JoinFailure struct {
	Channel string
	Reason  string
	NeedKey bool
}

func (u *testUI) LinesAdded(_ dispatch.SessionID, b dispatch.Buffer, lines []Line) {
	u.mu.Lock()
	for _, l := range lines {
		u.lines = append(u.lines, recordedLine{b.Name, l})
	}
	hook := u.onLine
	u.mu.Unlock()
	if hook != nil {
		for _, l := range lines {
			hook(b.Name, l)
		}
	}
}

func (u *testUI) LineAmended(_ dispatch.SessionID, b dispatch.Buffer) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.amended = append(u.amended, b.Name)
}

func (u *testUI) LineRedacted(_ dispatch.SessionID, b dispatch.Buffer, msgID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.redactions = append(u.redactions, b.Name+"|"+msgID)
}

func (u *testUI) BufferListChanged(dispatch.SessionID) {
	u.mu.Lock()
	u.bufferList++
	hook := u.onBufferList
	u.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (u *testUI) BufferRenamed(_ dispatch.SessionID, from, to dispatch.Buffer) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.renames = append(u.renames, [2]string{from.Name, to.Name})
}

func (u *testUI) MembersChanged(_ dispatch.SessionID, b dispatch.Buffer) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.members = append(u.members, b.Name)
}

func (u *testUI) TopicChanged(_ dispatch.SessionID, b dispatch.Buffer, topic string) {
	u.mu.Lock()
	u.topics[b.Name] = topic
	hook := u.onTopic
	u.mu.Unlock()
	if hook != nil {
		hook(b.Name, topic)
	}
}

func (u *testUI) ConnStateChanged(_ dispatch.SessionID, c dispatch.ConnState) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.conn = c
}

func (u *testUI) JoinRefused(_ dispatch.SessionID, channel, reason string, needKey bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.joinFails = append(u.joinFails, JoinFailure{channel, reason, needKey})
}

func (u *testUI) STSOffered(_ dispatch.SessionID, port string, seconds int64) {
	u.mu.Lock()
	u.stsPorts = append(u.stsPorts, port)
	hook := u.onSTS
	u.mu.Unlock()
	if hook != nil {
		hook(port, seconds)
	}
}

func (u *testUI) ChannelListDone(_ dispatch.SessionID, requested bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.scans++
	if requested {
		u.listDone++
	}
}

func (u *testUI) BatchDone() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.batches++
}

// Lines returns the lines the funnel handed the screen for one buffer.
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

// Delivered returns every delivery of one buffer's line with this text. One
// message must be one delivery: two of them is two desktop notifications for
// one thing, which is the failure v1's second-chance mention callback caused
// and M7's batch assembly removed the need for.
func (u *testUI) Delivered(buffer, text string) []Line {
	var out []Line
	for _, l := range u.Lines(buffer) {
		if l.Text == text {
			out = append(out, l)
		}
	}
	return out
}

// AllLines returns every delivered line, in order.
func (u *testUI) AllLines() []recordedLine {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]recordedLine(nil), u.lines...)
}

// MemberChanges returns the buffers whose roster the UI was told about, in order.
func (u *testUI) MemberChanges() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.members...)
}

// AmendedBuffers returns the buffers redrawn in place, in order.
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

// Status returns the last connection state as the status dot reads it.
func (u *testUI) Status() (connected bool, retrying bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.conn.Status == model.ConnReady, u.conn.Retrying
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

// Redactions returns "buffer|msgid" for each withdrawn message.
func (u *testUI) Redactions() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.redactions...)
}

// STSPorts returns the ports advertised through the STS path.
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

// ChannelListDoneCalls counts the scans the browser was asked to open for.
func (u *testUI) ChannelListDoneCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.listDone
}

// Batches counts coalesced batches: one turn of the UI thread each.
func (u *testUI) Batches() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.batches
}
