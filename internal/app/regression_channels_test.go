package app

import (
	"maps"
	"testing"

	"github.com/toprakkilic/eco/internal/testsrv"
)

// M5 channel-lifecycle regressions. Every test here encodes a bug v1 actually
// shipped and paid to find (see devdocs/engineering-audit.md, the QA top-10 in
// devdocs/v2/research.md, and the part_live_test.go scenarios these replace).
//
// All of it runs against the scripted fake server in internal/testsrv: no
// network, no sleeps, srv.Sync() as the only barrier. Assertions are on
// Session/Buffer state and on which UI callbacks fired — never on the text of
// a rendered line.

// --- local helpers (kept in this file so the shared fixture stays general) ---

// membersOf copies a buffer's member map under the session lock, so state
// assertions never race the connection goroutine.
func membersOf(s *Session, ch string) map[string]string {
	b := s.Buffer(ch)
	if b == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(b.Members)
}

// pendingParts returns the channels awaiting an answer to a PART request. The
// bookkeeping moved into the store at the M7 cutover (it is what makes a
// labelled 442 attributable to the PART that caused it), so the helper reads it
// from there; the keys are folded, which for these tests is lowercase.
func pendingParts(s *Session) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return setOf(s.store.PendingParts(s.netID))
}

// pendingJoins returns the channels awaiting an answer to a user-initiated
// JOIN request.
func pendingJoins(s *Session) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return setOf(s.store.PendingJoins(s.netID))
}

func setOf(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// lineCount returns how many lines a buffer holds (0 if it is gone).
func lineCount(s *Session, name string) int {
	b := s.Buffer(name)
	if b == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(b.Lines)
}

// sentMatching returns the still-unconsumed lines the client sent that match
// pat. Call it after srv.Sync(): anything the client wrote before the PING was
// queued on the same write channel, so the queue is complete.
func sentMatching(srv *testsrv.Server, pat string) []string {
	var out []string
	for _, l := range srv.Pending() {
		if testsrv.Match(pat, l) {
			out = append(out, l)
		}
	}
	return out
}

// testCommandUI is the smallest UI that can run handleInput: the command layer
// touches only these two fields on the /part, /close and /cycle paths, and no
// Fyne object is constructed, so this stays headless.
func testCommandUI(s *Session, current string) *UI {
	return &UI{sess: s, current: current}
}

// --- PART ---

// Bug: "leaving channels doesn't work" (part_live_test.go scenario 1). The
// server's PART echo is what closes the buffer; before the fix the buffer
// stayed in the sidebar forever.
func TestRegression_PartEchoClosesBuffer(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	before := ui.BufferListCalls()
	s.Part("#eco")
	srv.WaitFor("PART #eco")
	if got := pendingParts(s); !got["#eco"] {
		t.Fatalf("pendingPart = %v, want #eco flagged while the request is unanswered", got)
	}
	srv.Send(":%s!%s@test PART #eco", testNick, testNick)
	srv.Sync()

	if b := s.Buffer("#eco"); b != nil {
		t.Errorf("#eco buffer still open after the PART echo: %+v", b)
	}
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty once the part was answered", got)
	}
	if ui.BufferListCalls() <= before {
		t.Error("closing a buffer did not invalidate the buffer list")
	}
}

// Bug: a PART for a channel we are not actually on (a stale buffer left by a
// failed rejoin) is answered with 442/403 instead of a PART echo, and v1 left
// that buffer stuck open forever — the second half of "leaving channels
// doesn't work" (part_live_test.go scenario 2).
func TestRegression_PartRefusalClosesBuffer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		numeric string
		text    string
	}{
		{"442 not on channel", "442", "You're not on that channel"},
		{"403 no such channel", "403", "No such channel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, srv := newTestSession(t)
			// a stale buffer: it exists locally, but we never joined
			s.getBuffer("#stale")
			if s.Buffer("#stale") == nil {
				t.Fatal("fixture failed to create the stale buffer")
			}

			s.Part("#stale")
			srv.WaitFor("PART #stale")
			srv.Send(":irc.test %s %s #stale :%s", tc.numeric, testNick, tc.text)
			srv.Sync()

			if b := s.Buffer("#stale"); b != nil {
				t.Errorf("#stale buffer survived the %s refusal: %+v", tc.numeric, b)
			}
			if got := pendingParts(s); len(got) != 0 {
				t.Errorf("pendingPart = %v, want empty after the refusal", got)
			}
		})
	}
}

// Bug: the refusal path must be keyed per channel. A 442/403 for a channel the
// user never asked to leave (an unrelated command, or a server that answers
// late) must not close anything — the refusal only means "you are not on it",
// which is not a reason to destroy a live buffer.
func TestRegression_PartRefusalForUnaskedChannelKeepsBuffer(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	joinChannel(t, s, srv, "#other", "@alice")

	// the user asked to leave #other only
	s.Part("#other")
	srv.WaitFor("PART #other")

	// ...and the server refuses for #eco, which nobody asked about
	srv.Send(":irc.test 442 %s #eco :You're not on that channel", testNick)
	srv.Send(":irc.test 403 %s #nonexistent :No such channel", testNick)
	srv.Sync()

	if s.Buffer("#eco") == nil {
		t.Error("a refusal for #eco closed it although no part was requested for it")
	}
	if s.Buffer("#other") == nil {
		t.Error("a refusal for a different channel closed the channel that was being parted")
	}
	if got := pendingParts(s); !got["#other"] {
		t.Errorf("pendingPart = %v, want #other still awaiting its answer", got)
	}

	// the answer for the channel that WAS asked about still closes it
	srv.Send(":irc.test 442 %s #other :You're not on that channel", testNick)
	srv.Sync()
	if b := s.Buffer("#other"); b != nil {
		t.Errorf("#other survived its own refusal: %+v", b)
	}
}

// Bug (guard added by the engineering audit): /part with no argument used the
// current buffer, so typing it in the server log or a PM sent "PART server" /
// "PART alice"; the server refused with 442/403 and the refusal path then
// destroyed that buffer. Non-channel buffers must be rejected before anything
// goes on the wire.
func TestRegression_PartInNonChannelBufferKeepsBuffer(t *testing.T) {
	s, srv := newTestSession(t)
	s.getBuffer("alice") // a PM buffer

	for _, current := range []string{serverBuffer, "alice"} {
		testCommandUI(s, current).handleInput("/part")
	}
	srv.Sync()

	if s.Buffer(serverBuffer) == nil {
		t.Error("/part in the server buffer destroyed the server buffer")
	}
	if s.Buffer("alice") == nil {
		t.Error("/part in a PM destroyed the PM buffer")
	}
	if got := sentMatching(srv, "PART*"); len(got) != 0 {
		t.Errorf("client sent %q, want no PART for a non-channel buffer", got)
	}
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty: a non-channel /part must not arm the refusal path", got)
	}

	// the guard must not be over-broad: naming a channel still parts it, and
	// a bare name is normalised to a channel
	joinChannel(t, s, srv, "#eco")
	testCommandUI(s, serverBuffer).handleInput("/part eco")
	srv.WaitFor("PART #eco")
	srv.Send(":%s!%s@test PART #eco", testNick, testNick)
	srv.Sync()
	if b := s.Buffer("#eco"); b != nil {
		t.Errorf("/part #channel from the server buffer left %s open", b.Name)
	}
}

// Bug: another user's PART must only update the member list. v1's self-part
// path is selected by nick, and getting that comparison wrong closes the
// buffer whenever anyone leaves.
func TestRegression_OtherUserPartOnlyRemovesMember(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	srv.Send(":bob!bob@test PART #eco :bye")
	srv.Sync()

	if s.Buffer("#eco") == nil {
		t.Fatal("another user's PART closed our buffer")
	}
	want := map[string]string{"alice": "@", testNick: ""}
	if got := membersOf(s, "#eco"); !maps.Equal(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
	if got := ui.MemberChanges(); len(got) == 0 || got[len(got)-1] != "#eco" {
		t.Errorf("MembersChange calls = %v, want the last one for #eco", got)
	}
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty: nobody asked to part", got)
	}
}

// --- KICK ---

// Bug: being kicked left a dead buffer we were no longer in, so the sidebar
// lied and messages typed into it vanished (part_live_test.go scenario 3). A
// self-KICK must auto-leave and close the buffer.
func TestRegression_SelfKickClosesBuffer(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	before := ui.BufferListCalls()

	srv.Send(":alice!alice@test KICK #eco %s :out you go", testNick)
	srv.Sync()

	if b := s.Buffer("#eco"); b != nil {
		t.Errorf("#eco buffer survived our own kick: %+v", b)
	}
	if ui.BufferListCalls() <= before {
		t.Error("the auto-leave did not invalidate the buffer list")
	}
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty after a self-kick", got)
	}
	// the kick is reported where the user can still read it
	if lineCount(s, serverBuffer) == 0 {
		t.Error("no line recorded in the server buffer for our own kick")
	}
}

// Bug: the self-kick auto-leave must not fire for anyone else. Kicking another
// user only removes them from the member list.
func TestRegression_KickOfOtherOnlyRemovesMember(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")
	linesBefore := lineCount(s, "#eco")

	srv.Send(":alice!alice@test KICK #eco bob :spam")
	srv.Sync()

	if s.Buffer("#eco") == nil {
		t.Fatal("kicking another user closed our buffer")
	}
	want := map[string]string{"alice": "@", testNick: ""}
	if got := membersOf(s, "#eco"); !maps.Equal(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
	if got := ui.MemberChanges(); len(got) == 0 || got[len(got)-1] != "#eco" {
		t.Errorf("MembersChange calls = %v, want the last one for #eco", got)
	}
	if got := lineCount(s, "#eco"); got != linesBefore+1 {
		t.Errorf("#eco holds %d lines, want %d: the kick should add exactly one note", got, linesBefore+1)
	}
}

// --- /cycle ---

// Bug: /cycle is a PART followed by a JOIN, and the PART echo is exactly what
// the close path listens for. Without keepOnPart the cycle destroyed the
// buffer (losing its scrollback and the user's place) and rebuilt an empty
// one.
func TestRegression_CycleKeepsBufferAndScrollback(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	before := joinChannel(t, s, srv, "#eco", "@alice", "+bob")
	srv.Send("@msgid=c1 :alice!alice@test PRIVMSG #eco :before the cycle")
	srv.Sync()
	msgsBefore := len(messageLines(s.Buffer("#eco")))
	listCallsBefore := ui.BufferListCalls()

	testCommandUI(s, "#eco").handleInput("/cycle")
	srv.WaitFor("PART #eco")
	srv.WaitFor("JOIN #eco")

	// the part echo must be absorbed, not treated as "the user left"
	srv.Send(":%s!%s@test PART #eco", testNick, testNick)
	srv.Sync()
	if s.Buffer("#eco") == nil {
		t.Fatal("the PART half of /cycle closed the buffer")
	}
	if got := membersOf(s, "#eco"); len(got) != 0 {
		t.Errorf("members = %v, want them cleared while off the channel", got)
	}

	srv.Send(":%s!%s@test JOIN #eco", testNick, testNick)
	srv.Send(":irc.test 353 %s = #eco :%s @alice +bob", testNick, testNick)
	srv.Send(":irc.test 366 %s #eco :End of /NAMES list.", testNick)
	srv.Sync()

	after := s.Buffer("#eco")
	if after == nil {
		t.Fatal("#eco is gone after the cycle")
	}
	if after != before {
		t.Error("the cycle replaced the buffer instead of keeping it (scrollback and unread would be lost)")
	}
	if got := len(messageLines(after)); got != msgsBefore {
		t.Errorf("messages after the cycle = %d, want %d preserved", got, msgsBefore)
	}
	want := map[string]string{testNick: "", "alice": "@", "bob": "+"}
	if got := membersOf(s, "#eco"); !maps.Equal(got, want) {
		t.Errorf("members after the rejoin = %v, want %v", got, want)
	}
	if ui.BufferListCalls() != listCallsBefore {
		t.Error("the cycle added or removed a buffer; it must do neither")
	}
	// a later refusal must not close the cycled channel either: the cycle
	// never armed the refusal path
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty after a cycle", got)
	}
}

// TestRegression_CyclePartRefusalKeepsBuffer pins the collision between the two
// PART outcomes. /cycle arms the refusal path (RequestCycle sets pendingPart, so
// a stale buffer still closes on a 442) AND asks for the buffer to be kept — and
// the second has to win, or a /cycle on a channel the server thinks we already
// left destroys the scrollback the /cycle existed to preserve. The JOIN half is
// already on the wire by then and still lands.
//
// The 442 here is deliberately unlabelled: a labelled one is answered from the
// PART ledger, and /cycle records no label, so only an unlabelled reply reaches
// the pendingPart/keepOnPart branch this test is about.
//
// Added at M7 verification. state.partRefused has an explicit keepOnPart branch
// with a comment explaining itself, and deleting that branch outright left every
// package in the repo green — internal/app, internal/state and internal/transcript.
func TestRegression_CyclePartRefusalKeepsBuffer(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	before := joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=c1 :alice!alice@test PRIVMSG #eco :before the cycle")
	srv.Sync()
	msgsBefore := len(messageLines(s.Buffer("#eco")))
	listCallsBefore := ui.BufferListCalls()

	testCommandUI(s, "#eco").handleInput("/cycle")
	srv.WaitFor("PART #eco")
	srv.WaitFor("JOIN #eco")

	// the server refuses the PART instead of echoing it
	srv.Send(":irc.test 442 %s #eco :You're not on that channel", testNick)
	srv.Sync()

	after := s.Buffer("#eco")
	if after == nil {
		t.Fatal("the refused PART of a /cycle closed the buffer")
	}
	if after != before {
		t.Error("the refusal replaced the buffer instead of keeping it")
	}
	if got := len(messageLines(after)); got != msgsBefore {
		t.Errorf("messages after the refusal = %d, want %d preserved", got, msgsBefore)
	}
	if ui.BufferListCalls() != listCallsBefore {
		t.Error("the refusal churned the buffer list; the buffer never left it")
	}
	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want empty once the refusal was consumed", got)
	}

	// the JOIN half still completes and refills the room
	srv.Send(":%s!%s@test JOIN #eco", testNick, testNick)
	srv.Send(":irc.test 353 %s = #eco :%s @alice", testNick, testNick)
	srv.Send(":irc.test 366 %s #eco :End of /NAMES list.", testNick)
	srv.Sync()
	want := map[string]string{testNick: "", "alice": "@"}
	if got := membersOf(s, "#eco"); !maps.Equal(got, want) {
		t.Errorf("members after the rejoin = %v, want %v", got, want)
	}

	// and the buffer is no longer armed: a later, unrelated 442 leaves it alone
	srv.Send(":irc.test 442 %s #eco :You're not on that channel", testNick)
	srv.Sync()
	if s.Buffer("#eco") == nil {
		t.Error("a second, unasked-for refusal closed the cycled channel")
	}
}

// --- staleness across a disconnect ---

// Bug: pendingPart/pendingJoin are answered by the server, so a request that
// is still unanswered when the connection drops can never be answered. Left
// behind, the stale flag lets an unrelated 442/403 on the NEW connection close
// a live, rejoined channel (and a stale join flag lets an autojoin refusal pop
// a dialog the user never asked for). Both maps must be cleared on disconnect.
func TestRegression_PendingRequestsClearedOnDisconnect(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	// two requests the server never answers
	s.Part("#eco")
	srv.WaitFor("PART #eco")
	s.JoinUser("#never")
	srv.WaitFor("JOIN #never")
	if got := pendingParts(s); !got["#eco"] {
		t.Fatalf("pendingPart = %v, want #eco armed before the drop", got)
	}
	if got := pendingJoins(s); !got["#never"] {
		t.Fatalf("pendingJoin = %v, want #never armed before the drop", got)
	}

	srv.Close()    // the connection dies with both requests outstanding
	srv.NextConn() // the client redials
	srv.Register(testNick)

	if got := pendingParts(s); len(got) != 0 {
		t.Errorf("pendingPart = %v, want cleared by the disconnect", got)
	}
	if got := pendingJoins(s); len(got) != 0 {
		t.Errorf("pendingJoin = %v, want cleared by the disconnect", got)
	}

	// the channel is rejoined and alive on the new connection
	srv.WaitFor("JOIN #eco")
	srv.Send(":%s!%s@test JOIN #eco", testNick, testNick)
	srv.Sync()
	if s.Buffer("#eco") == nil {
		t.Fatal("#eco did not survive the reconnect")
	}

	// the answers that arrive late (or for something else entirely) must not
	// act on the pre-disconnect requests
	srv.Send(":irc.test 442 %s #eco :You're not on that channel", testNick)
	srv.Send(":irc.test 473 %s #never :Cannot join channel (+i)", testNick)
	srv.Sync()

	if s.Buffer("#eco") == nil {
		t.Error("a stale pendingPart let a 442 close the live rejoined #eco buffer")
	}
	if got := ui.JoinFailures(); len(got) != 0 {
		t.Errorf("JoinFailed fired %v; a stale pendingJoin must not prompt after a reconnect", got)
	}
}

// --- join refusals ---

// Bug: join refusals only reached the server log, which a beginner never
// opens; the channel simply never appeared. They are surfaced to the UI now,
// and 475 (wrong/missing key) is the one that must ask for the key.
func TestRegression_JoinRefusalSurfacesToUI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		numeric string
		reason  string
		needKey bool
	}{
		{"475 needs a key", "475", "Cannot join channel (+k)", true},
		{"473 invite only", "473", "Cannot join channel (+i)", false},
		{"474 banned", "474", "Cannot join channel (+b)", false},
		{"471 channel full", "471", "Cannot join channel (+l)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, srv, ui := newTestSessionUI(t)

			s.JoinUser("#secret")
			srv.WaitFor("JOIN #secret")
			srv.Send(":irc.test %s %s #secret :%s", tc.numeric, testNick, tc.reason)
			srv.Sync()

			got := ui.JoinFailures()
			if len(got) != 1 {
				t.Fatalf("JoinFailed fired %d times (%v), want exactly 1", len(got), got)
			}
			want := JoinFailure{Channel: "#secret", Reason: tc.reason, NeedKey: tc.needKey}
			if got[0] != want {
				t.Errorf("JoinFailed = %+v, want %+v", got[0], want)
			}
			if b := s.Buffer("#secret"); b != nil {
				t.Errorf("a refused join left a phantom buffer: %+v", b)
			}
			// the refusal consumed the request; a repeat must not re-prompt
			srv.Send(":irc.test %s %s #secret :%s", tc.numeric, testNick, tc.reason)
			srv.Sync()
			if got := ui.JoinFailures(); len(got) != 1 {
				t.Errorf("JoinFailed fired %d times after a duplicate refusal, want 1: %v", len(got), got)
			}
		})
	}
}

// Bug: refusals must be gated on pendingJoin. Autojoin and post-reconnect
// rejoins are issued by the client, not the user, and a +k or +i channel in
// the autojoin list would otherwise pop a key prompt on every single connect,
// with no user action to explain it.
func TestRegression_AutojoinRefusalDoesNotPrompt(t *testing.T) {
	cfg := testConfig()
	cfg.Channels = "#auto"
	s, srv, ui := startTestSession(t, cfg)
	srv.Register(testNick)

	// the connect callback joined #auto on the client's own initiative
	srv.WaitFor("JOIN #auto")
	srv.Send(":irc.test 475 %s #auto :Cannot join channel (+k)", testNick)
	srv.Sync()

	if got := ui.JoinFailures(); len(got) != 0 {
		t.Fatalf("JoinFailed fired %v for an autojoin refusal; it must stay in the log", got)
	}
	if got := pendingJoins(s); len(got) != 0 {
		t.Errorf("pendingJoin = %v, want empty: autojoin must not arm the prompt", got)
	}

	// the same refusal for the same channel DOES prompt once the user asks
	s.JoinUser("#auto")
	srv.WaitFor("JOIN #auto")
	srv.Send(":irc.test 475 %s #auto :Cannot join channel (+k)", testNick)
	srv.Sync()

	got := ui.JoinFailures()
	if len(got) != 1 {
		t.Fatalf("JoinFailed fired %d times (%v), want exactly 1 for the user-initiated join", len(got), got)
	}
	if !got[0].NeedKey || got[0].Channel != "#auto" {
		t.Errorf("JoinFailed = %+v, want #auto with needKey", got[0])
	}
}
