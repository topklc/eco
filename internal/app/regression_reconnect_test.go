package app

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// M5 regression contract, connection- and state-tracking half: reconnect,
// history playback, channel rename, mode tracking, the member list and the
// STS policy record. Each test names the v1 bug it exists to prevent.
//
// The fixture is the shared one in regression_test.go (newTestSession /
// startTestSession / testUI): no network, no sleeps — srv.Sync() is the
// barrier — and every assertion looks at Session/Buffer state or at callback
// invocations, never at a rendered string. Tests must not run in parallel:
// the fixture pins the global settings snapshot.

// rcJoinBatchMax is the largest number of channels the rejoin path may put on
// one JOIN line. Sending one line per channel is what the batching exists to
// avoid (40 open channels used to mean 40 protocol lines on every reconnect).
const rcJoinBatchMax = 10

// TestRegression_ReconnectRejoinsOpenChannelsExactlyOnce pins the reconnect
// invariant from devdocs/memory.md ("reconnect rejoins open channels and
// refetches history; dedupe makes that safe") and the two bugs v1 shipped
// around it:
//
//  1. the library rejoins nothing itself and join-all only runs on the
//     session's first scan, so a dropped connection left every open buffer
//     dead — present but not actually joined;
//  2. once the rejoin landed, an autojoin channel was joined TWICE (once by
//     the autojoin loop, once by the rejoin loop), duplicating the join line
//     and the history fetch for it.
//
// It also pins the batching path and the buffer bookkeeping: multi-target
// JOINs, and a buffer set that comes out of the reconnect unchanged.
func TestRegression_ReconnectRejoinsOpenChannelsExactlyOnce(t *testing.T) {
	cfg := testConfig()
	cfg.Channels = "#auto" // the channel both loops used to claim
	s, srv, ui := startTestSession(t, cfg)
	srv.Register(testNick)

	// the autojoin channel comes up on connect
	srv.WaitFor("JOIN #auto")
	srv.Send(":eco!eco@test JOIN #auto")

	// eleven more the user opened by hand. One is mixed case: the rejoin has
	// to use the buffer's own name while deduping against the (lowercased)
	// autojoin list, so both halves of the casemapping are exercised.
	opened := []string{"#Cafe"}
	for i := 1; i <= 10; i++ {
		opened = append(opened, fmt.Sprintf("#ch%02d", i))
	}
	for _, ch := range opened {
		s.JoinUser(ch)
		srv.WaitFor("JOIN " + ch)
		srv.Send(":eco!eco@test JOIN " + ch)
	}
	srv.Sync()

	all := append([]string{"#auto"}, opened...)
	linesBefore := map[string]int{}
	memberCallsBefore := map[string]int{}
	for _, ch := range all {
		b := s.Buffer(ch)
		if b == nil {
			t.Fatalf("no %s buffer before the reconnect", ch)
		}
		linesBefore[ch] = len(b.Lines)
		memberCallsBefore[ch] = rcCount(ui.MemberChanges(), ch)
	}
	buffersBefore := rcBufferNames(t, s)

	// the server drops us; ircevent redials immediately the first time
	srv.Close()
	srv.NextConn()
	srv.Register(testNick)

	if !s.Connected() {
		t.Error("session reports not connected after the reconnect")
	}

	// Pending() on the fresh connection is exactly what the client sent
	// after re-registering: the rejoin and nothing else.
	joins := rcJoinCommands(srv.Pending())
	var targets []string
	for _, cmd := range joins {
		if len(cmd) > rcJoinBatchMax {
			t.Errorf("JOIN carried %d targets (%v), want at most %d per line", len(cmd), cmd, rcJoinBatchMax)
		}
		targets = append(targets, cmd...)
	}
	got, want := slices.Clone(targets), slices.Clone(all)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("rejoin sent JOIN for %v, want each open channel exactly once: %v", got, want)
	}
	if len(joins) >= len(all) {
		t.Errorf("rejoin used %d JOIN commands for %d channels; the batching path is not being taken", len(joins), len(all))
	}

	// answer every JOIN the client actually sent: a duplicated request would
	// produce a duplicated echo here, and so a duplicated join line
	for _, ch := range targets {
		srv.Send(":eco!eco@test JOIN " + ch)
	}
	srv.Sync()

	for _, ch := range all {
		b := s.Buffer(ch)
		if b == nil {
			t.Fatalf("%s buffer did not survive the reconnect", ch)
		}
		if n := len(b.Lines) - linesBefore[ch]; n != 1 {
			t.Errorf("%s gained %d lines across the reconnect, want exactly 1 (the join)", ch, n)
		}
		if n := rcCount(ui.MemberChanges(), ch) - memberCallsBefore[ch]; n != 1 {
			t.Errorf("%s fired MembersChange %d times across the reconnect, want exactly 1", ch, n)
		}
	}
	if got := rcBufferNames(t, s); !slices.Equal(got, buffersBefore) {
		t.Errorf("buffer set after the reconnect = %v, want %v (no buffer added, dropped or duplicated)", got, buffersBefore)
	}

	// history is refetched once per channel — never twice (the dedupe makes
	// the refetch safe, it does not make a doubled fetch free)
	pending := srv.Pending()
	for _, ch := range all {
		if n := rcCountPrefix(pending, "CHATHISTORY LATEST "+ch+" "); n != 1 {
			t.Errorf("%s requested history %d times after the rejoin, want 1", ch, n)
		}
	}
}

// TestRegression_PlaybackJoinsDoNotTouchMembersOrRefetchHistory pins the
// isPlayback rule (memory.md: "batch-tagged events never mutate member state
// or trigger history requests"). v1's storm: a server replaying joins inside
// a chathistory batch re-ran the live JOIN path, so every replayed self-join
// asked for the channel's modes and another page of history — which arrived
// as more playback — while replayed joins/parts of other people corrupted the
// member list with users who left hours ago.
func TestRegression_PlaybackJoinsDoNotTouchMembersOrRefetchHistory(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	s.JoinUser("#eco")
	srv.Expect("JOIN #eco")
	srv.Replay(`
		S: :eco!eco@test JOIN #eco
		S: :irc.test 353 eco = #eco :@alice eco +bob
		S: :irc.test 366 eco #eco :End of /NAMES list.
	`)
	srv.Sync()
	srv.WaitFor("MODE #eco")                // consume the live join's requests
	srv.WaitFor("CHATHISTORY LATEST #eco ") // so anything below is new

	// resetting the per-channel history guard is exactly what the disconnect
	// callback does, so a playback join that leaked into the live path really
	// would fire another CHATHISTORY here
	s.ClearBuffer("#eco")

	b := s.Buffer("#eco")
	membersBefore := maps.Clone(b.Members)
	linesBefore := len(b.Lines)
	memberCallsBefore := rcCount(ui.MemberChanges(), "#eco")

	// a chathistory batch replaying a join storm. ircevent buffers batched
	// messages and dispatches them when the batch closes, each still carrying
	// its batch tag — which is what isPlayback keys on.
	srv.Replay(`
		S: :irc.test BATCH +hist chathistory #eco
		S: @batch=hist :mallory!m@test JOIN #eco
		S: @batch=hist :eco!eco@test JOIN #eco
		S: @batch=hist :alice!alice@test PART #eco :bye
		S: :irc.test BATCH -hist
	`)
	srv.Sync()

	if !maps.Equal(b.Members, membersBefore) {
		t.Errorf("playback mutated the member list: %v, want %v", b.Members, membersBefore)
	}
	if n := rcCount(ui.MemberChanges(), "#eco") - memberCallsBefore; n != 0 {
		t.Errorf("playback fired MembersChange %d times, want 0", n)
	}
	// the events did reach the session (otherwise the assertions above pass
	// vacuously): both foreign events are shown, our own replayed join is not
	if n := len(b.Lines) - linesBefore; n != 2 {
		t.Errorf("playback added %d lines, want 2 (mallory joined, alice left; our own replayed join stays silent)", n)
	}
	for _, l := range srv.Pending() {
		if strings.HasPrefix(l, "CHATHISTORY") || strings.HasPrefix(l, "MODE ") {
			t.Errorf("playback made the client send %q; batched events must not re-request history or modes", l)
		}
	}
}

// TestRegression_ChannelRenameMigratesBuffer covers draft/channel-rename:
// the buffer has to move to the new name keeping everything it holds. v1 bugs:
// a rename left the buffer under its old key (messages kept arriving into a
// buffer the user could no longer see) and the display order kept the stale
// key, so the sidebar walked into a nil buffer.
func TestRegression_ChannelRenameMigratesBuffer(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	s.JoinUser("#eco")
	srv.Expect("JOIN #eco")
	srv.Replay(`
		S: :eco!eco@test JOIN #eco
		S: :irc.test 332 eco #eco :the old room
		S: :irc.test 353 eco = #eco :@alice eco
		S: :irc.test 366 eco #eco :End of /NAMES list.
		S: @msgid=m1 :alice!alice@test PRIVMSG #eco :before the rename
	`)
	srv.Sync()

	old := s.Buffer("#eco")
	if old == nil {
		t.Fatal("no #eco buffer to rename")
	}
	membersBefore := maps.Clone(old.Members)
	linesBefore := len(old.Lines)
	listCallsBefore := ui.BufferListCalls()

	srv.Send(":alice!alice@test RENAME #eco #eco-two :spring cleaning")
	srv.Sync()

	if b := s.Buffer("#eco"); b != nil {
		t.Errorf("the old name still resolves to a buffer after the rename: %+v", b)
	}
	b := s.Buffer("#eco-two")
	if b == nil {
		t.Fatal("no buffer under the new name after the rename")
	}
	if b != old {
		t.Error("rename created a new buffer instead of migrating the existing one")
	}
	if b.Name != "#eco-two" {
		t.Errorf("buffer name = %q, want %q", b.Name, "#eco-two")
	}
	if b.Topic != "the old room" {
		t.Errorf("topic lost in the rename: %q", b.Topic)
	}
	if !maps.Equal(b.Members, membersBefore) {
		t.Errorf("members after the rename = %v, want %v", b.Members, membersBefore)
	}
	// every line survives; the rename itself adds one status line
	if n := len(b.Lines) - linesBefore; n != 1 {
		t.Errorf("line count changed by %d over the rename, want +1 (the rename notice)", n)
	}
	msgs := messageLines(b)
	if len(msgs) != 1 || msgs[0].Text != "before the rename" || msgs[0].MsgID != "m1" {
		t.Errorf("messages did not survive the rename: %+v", msgs)
	}
	if got := ui.Renames(); !slices.Equal(got, [][2]string{{"#eco", "#eco-two"}}) {
		t.Errorf("RenameBuffer callbacks = %v, want one #eco -> #eco-two", got)
	}
	if ui.BufferListCalls() == listCallsBefore {
		t.Error("the rename did not invalidate the buffer list")
	}
	// the display order moved with the buffer: a stale key would surface as a
	// nil entry here, and the new name must not appear twice
	names := rcBufferNames(t, s)
	if n := rcCount(names, "#eco-two"); n != 1 {
		t.Errorf("buffer order holds %s %d times: %v", "#eco-two", n, names)
	}
	if rcCount(names, "#eco") != 0 {
		t.Errorf("buffer order still holds the old name: %v", names)
	}

	// traffic addressed to the new name lands in the migrated buffer
	srv.Send("@msgid=m2 :alice!alice@test PRIVMSG #eco-two :after the rename")
	srv.Sync()
	if got := len(messageLines(b)); got != 2 {
		t.Errorf("messages in the renamed buffer = %d, want 2", got)
	}
}

// TestRegression_ChannelModeTrackingFromTheWire pins mode tracking end to end
// (modes_test.go covers applyModeDelta as a pure function; this covers the
// wiring). v1 bugs it prevents: member modes leaking into the channel's mode
// string so the settings dialog offered to remove "+o"; a mode argument being
// mistaken for a mode letter, which corrupted everything after it on the line;
// -k leaving the stale key behind so a rejoin sent the wrong one; and 324
// merging into stale state instead of replacing it.
func TestRegression_ChannelModeTrackingFromTheWire(t *testing.T) {
	s, srv := newTestSession(t)

	s.JoinUser("#eco")
	srv.Expect("JOIN #eco")
	srv.Send(":eco!eco@test JOIN #eco")
	srv.Sync()

	b := s.Buffer("#eco")
	if b == nil {
		t.Fatal("no #eco buffer")
	}

	srv.Send(":alice!alice@test MODE #eco +k sekrit")
	srv.Sync()
	if !strings.ContainsRune(b.Modes, 'k') || b.Key != "sekrit" {
		t.Errorf("after +k: modes %q key %q, want k tracked and the key kept", b.Modes, b.Key)
	}

	srv.Send(":alice!alice@test MODE #eco -k sekrit")
	srv.Sync()
	if strings.ContainsRune(b.Modes, 'k') || b.Key != "" {
		t.Errorf("after -k: modes %q key %q, want k and the key gone", b.Modes, b.Key)
	}

	srv.Send(":alice!alice@test MODE #eco +ims")
	srv.Sync()
	if got := rcModeSet(b.Modes); got != "ims" {
		t.Errorf("after +ims: modes %q, want i, m and s", b.Modes)
	}

	// member modes take a nick argument and are not channel state
	srv.Send(":alice!alice@test MODE #eco +o bob")
	srv.Send(":alice!alice@test MODE #eco +v carol")
	srv.Sync()
	if strings.ContainsAny(b.Modes, "ov") {
		t.Errorf("member modes leaked into the channel modes: %q", b.Modes)
	}
	if got := rcModeSet(b.Modes); got != "ims" {
		t.Errorf("member modes disturbed the channel modes: %q", b.Modes)
	}

	// +l takes an argument, -i on the same line must still be applied
	srv.Send(":alice!alice@test MODE #eco +l-i 50")
	srv.Sync()
	if got := rcModeSet(b.Modes); got != "lms" {
		t.Errorf("after +l-i 50: modes %q, want l, m and s", b.Modes)
	}

	// 324 is the full state: it replaces what we tracked, it does not merge
	srv.Send(":irc.test 324 eco #eco +nt")
	srv.Sync()
	if got := rcModeSet(b.Modes); got != "nt" {
		t.Errorf("after 324 +nt: modes %q, want exactly n and t", b.Modes)
	}
	if b.Key != "" {
		t.Errorf("324 without +k left key %q", b.Key)
	}

	srv.Send(":irc.test 324 eco #eco +ntk topsecret")
	srv.Sync()
	if got := rcModeSet(b.Modes); got != "knt" {
		t.Errorf("after 324 +ntk: modes %q, want k, n and t", b.Modes)
	}
	if b.Key != "topsecret" {
		t.Errorf("324 key = %q, want %q", b.Key, "topsecret")
	}
}

// TestRegression_MemberListTracksNamesJoinPartQuit pins the member list
// against the events that maintain it. v1 bugs: NAMES entries were parsed
// without allowing for multi-prefix or userhost-in-names, so "@+dan" and
// "erica!erica@host" became members under those literal names; and QUIT was
// handled per visible channel, leaving the quitter in the member list of
// every other channel they shared with us.
func TestRegression_MemberListTracksNamesJoinPartQuit(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	for _, ch := range []string{"#eco", "#other"} {
		s.JoinUser(ch)
		// WaitFor, not Expect: the previous channel's join already queued a
		// MODE and a CHATHISTORY request ahead of this line
		srv.WaitFor("JOIN " + ch)
		srv.Send(":eco!eco@test JOIN " + ch)
	}
	srv.Replay(`
		S: :irc.test 353 eco = #eco :@alice eco +bob @+dan erica!erica@test
		S: :irc.test 366 eco #eco :End of /NAMES list.
		S: :irc.test 353 eco = #other :alice eco @dave
		S: :irc.test 366 eco #other :End of /NAMES list.
	`)
	srv.Sync()

	eco, other := s.Buffer("#eco"), s.Buffer("#other")
	if eco == nil || other == nil {
		t.Fatal("missing channel buffers")
	}
	wantEco := map[string]string{"alice": "@", "eco": "", "bob": "+", "dan": "@+", "erica": ""}
	if !maps.Equal(eco.Members, wantEco) {
		t.Errorf("#eco members = %v, want %v", eco.Members, wantEco)
	}
	wantOther := map[string]string{"alice": "", "eco": "", "dave": "@"}
	if !maps.Equal(other.Members, wantOther) {
		t.Errorf("#other members = %v, want %v", other.Members, wantOther)
	}

	// a live join adds an unprefixed member and tells the UI
	callsBefore := rcCount(ui.MemberChanges(), "#eco")
	srv.Send(":frank!f@test JOIN #eco")
	srv.Sync()
	if got, ok := eco.Members["frank"]; !ok || got != "" {
		t.Errorf("frank after JOIN = (%q, %v), want present with no prefix", got, ok)
	}
	if n := rcCount(ui.MemberChanges(), "#eco") - callsBefore; n != 1 {
		t.Errorf("JOIN fired MembersChange %d times, want 1", n)
	}

	// a part removes exactly one member
	callsBefore = rcCount(ui.MemberChanges(), "#eco")
	srv.Send(":bob!b@test PART #eco :bye")
	srv.Sync()
	if _, ok := eco.Members["bob"]; ok {
		t.Errorf("bob survived his PART: %v", eco.Members)
	}
	if n := rcCount(ui.MemberChanges(), "#eco") - callsBefore; n != 1 {
		t.Errorf("PART fired MembersChange %d times, want 1", n)
	}

	// a quit removes the nick from EVERY channel they were in, and only there
	ecoCalls, otherCalls := rcCount(ui.MemberChanges(), "#eco"), rcCount(ui.MemberChanges(), "#other")
	ecoLines, otherLines := len(eco.Lines), len(other.Lines)
	srv.Send(":alice!alice@test QUIT :leaving")
	srv.Sync()
	if _, ok := eco.Members["alice"]; ok {
		t.Errorf("alice survived her QUIT in #eco: %v", eco.Members)
	}
	if _, ok := other.Members["alice"]; ok {
		t.Errorf("alice survived her QUIT in #other: %v", other.Members)
	}
	if n := rcCount(ui.MemberChanges(), "#eco") - ecoCalls; n != 1 {
		t.Errorf("QUIT fired MembersChange %d times for #eco, want 1", n)
	}
	if n := rcCount(ui.MemberChanges(), "#other") - otherCalls; n != 1 {
		t.Errorf("QUIT fired MembersChange %d times for #other, want 1", n)
	}
	if n := len(eco.Lines) - ecoLines; n != 1 {
		t.Errorf("#eco gained %d lines on the QUIT, want 1", n)
	}
	if n := len(other.Lines) - otherLines; n != 1 {
		t.Errorf("#other gained %d lines on the QUIT, want 1", n)
	}

	// a quit from someone we share no channel with touches nothing
	ecoCalls, otherCalls = rcCount(ui.MemberChanges(), "#eco"), rcCount(ui.MemberChanges(), "#other")
	ecoLines, otherLines = len(eco.Lines), len(other.Lines)
	srv.Send(":ghost!g@test QUIT :never here")
	srv.Sync()
	if rcCount(ui.MemberChanges(), "#eco") != ecoCalls || rcCount(ui.MemberChanges(), "#other") != otherCalls {
		t.Error("a stranger's QUIT fired MembersChange")
	}
	if len(eco.Lines) != ecoLines || len(other.Lines) != otherLines {
		t.Error("a stranger's QUIT added lines to channels they were never in")
	}
}

// TestRegression_STSPolicyInCapLSIsRecorded pins the STS pickup: a policy
// advertised in CAP LS must be parsed (port and duration) and handed to the
// UI, which stores it in the config store, and duration=0 in a later CAP NEW
// must revoke it. No reconnect is involved — the bug was that the
// advertisement was only ever noticed during the upgrade dance, so a policy
// seen on an already-secure connection was never remembered, and the client
// happily connected in plaintext next time.
//
// Persisting only from a TLS connection is ui.go's rule (memory.md: "STS
// policies persist only from TLS connections") and stays there; the offline
// pipe carries no TLS handshake, so the sink here stands in for that branch.
func TestRegression_STSPolicyInCapLSIsRecorded(t *testing.T) {
	prefs := fynetest.NewTempApp(t).Preferences()
	cfg := testConfig()
	caps := append(slices.Clone(testsrv.DefaultCaps), "sts=duration=300,port=6697")

	s, srv, ui := rcStartSession(t, cfg, caps, rcSTSSink(prefs, cfg.Server))
	srv.Register(testNick)

	if !s.Connected() {
		t.Error("advertising sts broke registration")
	}
	if got := ui.STSPorts(); !slices.Equal(got, []string{"6697"}) {
		t.Fatalf("STS callback saw ports %v, want [6697] exactly once", got)
	}
	pol, ok := config.STSLookup(prefs, cfg.Server)
	if !ok {
		t.Fatalf("no STS policy recorded for %s", cfg.Server)
	}
	if pol.Port != "6697" {
		t.Errorf("recorded port = %q, want %q", pol.Port, "6697")
	}
	if pol.Expires <= time.Now().Unix() {
		t.Errorf("policy expires at %d, which is not in the future", pol.Expires)
	}

	// duration=0 revokes, per the spec
	srv.Send(":irc.test CAP * NEW :sts=duration=0")
	srv.Sync()
	if _, ok := config.STSLookup(prefs, cfg.Server); ok {
		t.Error("a duration=0 advertisement did not revoke the stored policy")
	}
}

// --- helpers (local to this file; the shared fixture stays untouched) ---

// rcStartSession is startTestSession with the two knobs this file needs and
// the shared fixture does not expose: the capability set the fake server
// advertises (an "sts" policy has to be in CAP LS) and an extra STS sink.
func rcStartSession(t *testing.T, cfg Config, caps []string, sts func(port string, durationSecs int64)) (*Session, *testsrv.Server, *testUI) {
	t.Helper()
	pinTestSettings(t)

	srv := testsrv.New(t)
	srv.Caps = caps // set before the client connects, as Server documents
	ui := &testUI{topics: map[string]string{}}
	cb := ui.callbacks()
	record := cb.STS
	cb.STS = func(port string, durationSecs int64) {
		record(port, durationSecs)
		if sts != nil {
			sts(port, durationSecs)
		}
	}
	s := NewSession(cfg, cb)
	s.Dial = srv.Dial // the test seam
	s.Connect()
	stopSession(t, s)
	return s, srv, ui
}

// rcSTSSink mirrors what ui.go does with an STS advertisement on a secure
// connection: store the policy for the server's host, letting duration=0
// revoke it. It stores the ADVERTISED port so the assertion covers the CAP LS
// parse rather than the session's own configuration.
func rcSTSSink(prefs fyne.Preferences, host string) func(string, int64) {
	return func(port string, durationSecs int64) {
		config.STSRecord(prefs, host, port, durationSecs)
	}
}

// rcJoinCommands returns one entry per JOIN the client sent, each holding
// that command's channel targets.
func rcJoinCommands(lines []string) [][]string {
	var out [][]string
	for _, l := range lines {
		rest, ok := strings.CutPrefix(l, "JOIN ")
		if !ok {
			continue
		}
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			rest = rest[:i] // drop keys; the rejoin path sends none
		}
		out = append(out, strings.Split(rest, ","))
	}
	return out
}

// rcCount counts exact occurrences of v in list.
func rcCount(list []string, v string) int {
	n := 0
	for _, s := range list {
		if s == v {
			n++
		}
	}
	return n
}

// rcCountPrefix counts the lines starting with prefix.
func rcCountPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// rcBufferNames returns every buffer name the session would display, sorted.
// A nil entry means a stale key survived in the display order, which is a
// failure in itself.
func rcBufferNames(t *testing.T, s *Session) []string {
	t.Helper()
	var out []string
	for i, b := range s.Buffers() {
		if b == nil {
			t.Fatalf("Buffers()[%d] is nil: a stale key survived in the buffer order", i)
		}
		out = append(out, b.Name)
	}
	slices.Sort(out)
	return out
}

// rcModeSet sorts a mode string so assertions do not depend on the order the
// flags happened to be applied in.
func rcModeSet(modes string) string {
	r := []rune(modes)
	slices.Sort(r)
	return string(r)
}
