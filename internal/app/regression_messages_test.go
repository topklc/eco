package app

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// M5 regression contract, message-pipeline half: echo dedupe, msgid dedupe,
// highlight rules, multiline merging, redaction and ignore. Every test names
// the bug it prevents — these are bugs v1 actually shipped (devdocs/memory.md
// invariants, devdocs/engineering-audit.md, devdocs/v2/research.md top-10).
//
// Everything drives a real Session against internal/testsrv over a net.Pipe:
// no network, no sleeps, no wall-clock dependence. Assertions are on
// Session/Buffer state and callback invocations, never on rendered strings.
//
// Helpers here are prefixed msg* so they cannot collide with the other
// TestRegression_* files sharing this package. The JOIN handshake itself is
// the shared joinChannel fixture in regression_test.go.

// --- fixtures -------------------------------------------------------------

// msgTexts returns the text of every real (non-status) line in a buffer.
func msgTexts(t *testing.T, s *Session, buffer string) []string {
	t.Helper()
	b := s.Buffer(buffer)
	if b == nil {
		t.Fatalf("no %s buffer", buffer)
	}
	var out []string
	for _, l := range messageLines(b) {
		out = append(out, l.Text)
	}
	return out
}

// msgOnly returns the single message line in a buffer whose text matches,
// failing when there is not exactly one. "Exactly one" is the whole point of
// every dedupe test below, so the helper asserts it rather than picking one.
func msgOnly(t *testing.T, s *Session, buffer, text string) Line {
	t.Helper()
	b := s.Buffer(buffer)
	if b == nil {
		t.Fatalf("no %s buffer", buffer)
	}
	var found []Line
	for _, l := range messageLines(b) {
		if l.Text == text {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("buffer %s holds %d lines with text %q, want exactly 1 (all: %q)",
			buffer, len(found), text, msgTexts(t, s, buffer))
	}
	return found[0]
}

// msgUILines returns the real (non-status) lines the session handed the UI
// for one buffer, so counting them is not thrown off by "You joined #eco".
func msgUILines(ui *testUI, buffer string) []Line {
	var out []Line
	for _, l := range ui.Lines(buffer) {
		if !l.Status {
			out = append(out, l)
		}
	}
	return out
}

// msgByID returns the message line carrying msgid, or fails.
func msgByID(t *testing.T, s *Session, buffer, msgid string) Line {
	t.Helper()
	b := s.Buffer(buffer)
	if b == nil {
		t.Fatalf("no %s buffer", buffer)
	}
	var found []Line
	for _, l := range b.Lines {
		if l.MsgID == msgid {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("buffer %s holds %d lines with msgid %q, want exactly 1", buffer, len(found), msgid)
	}
	return found[0]
}

// --- echo-message dedupe --------------------------------------------------
//
// CHARACTERIZATION, expected to change at M6/M7. The three tests in this
// section pin *that* a sent message is displayed exactly once, which is a real
// requirement — but they pin it over v1's mechanism, `pendingEcho`, which
// matches the echo by its TEXT. Blueprint §3 MUST 3 deletes that outright:
// labeled-response correlation renders the send as Pending and resolves it in
// place when the labelled echo arrives, carrying the server's real msgid and
// time. What must survive the rewrite is the observable contract below (one
// line, Self set, no duplicate however late or however batched the echo is);
// what must NOT survive is text matching, so two identical messages sent in a
// row stop being indistinguishable. See also the own-message-has-no-MsgID
// expectation in TestRegression_LineFlagsDriveUnreadAndHighlight, which is the
// same bug seen from the buffer side and flips at the same milestone.

// Bug: own messages double-rendered. eco displays a sent message locally at
// once (so typing feels instant) and the server, with echo-message
// negotiated, sends the very same PRIVMSG back. Without the pendingEcho
// suppression every message the user sent appeared twice.
// (memory.md: "Own messages display locally first; server echoes and history
// replays are deduped".)
func TestRegression_EchoMessageDedupe(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	s.SendMessage("#eco", "hello from eco")
	srv.WaitFor("PRIVMSG #eco :hello from eco")

	// the local echo is on screen before the server has said anything
	local := msgOnly(t, s, "#eco", "hello from eco")
	if !local.Self {
		t.Errorf("locally echoed line is not marked Self: %+v", local)
	}
	if local.Nick != testNick {
		t.Errorf("locally echoed nick = %q, want %q", local.Nick, testNick)
	}

	// ...and the server's echo-message copy must not add a second one
	srv.Send("@msgid=e1 :%s!%s@test PRIVMSG #eco :hello from eco", testNick, testNick)
	srv.Sync()

	msgOnly(t, s, "#eco", "hello from eco")
	if got := len(msgTexts(t, s, "#eco")); got != 1 {
		t.Fatalf("#eco holds %d messages after the echo, want 1: %q", got, msgTexts(t, s, "#eco"))
	}
	if shown := msgUILines(ui, "#eco"); len(shown) != 1 {
		t.Errorf("UI was handed %d messages for #eco, want 1: %+v", len(shown), shown)
	}
}

// Bug: the echo was only suppressed when it came back immediately. A busy
// channel interleaves other traffic between the send and its echo, and a
// dedupe that only looked at the last line let the delayed copy through.
// (QA top-10: "delayed-echo dedupe".)
func TestRegression_EchoMessageDedupeDelayed(t *testing.T) {
	s, srv, _ := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	s.SendMessage("#eco", "delayed hello")
	srv.WaitFor("PRIVMSG #eco :delayed hello")

	// a burst of unrelated traffic lands between the send and its echo
	srv.Send("@msgid=a1 :alice!alice@test PRIVMSG #eco :unrelated chatter")
	srv.Send(":carol!carol@test JOIN #eco")
	srv.Send("@msgid=a2 :bob!bob@test PRIVMSG #eco :more chatter")
	srv.Sync()

	// only now does our own message come back
	srv.Send("@msgid=e1 :%s!%s@test PRIVMSG #eco :delayed hello", testNick, testNick)
	srv.Sync()

	msgOnly(t, s, "#eco", "delayed hello")
	want := []string{"delayed hello", "unrelated chatter", "more chatter"}
	got := msgTexts(t, s, "#eco")
	if len(got) != len(want) {
		t.Fatalf("#eco holds %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("#eco holds %q, want %q", got, want)
		}
	}
}

// Bug: a multiline message is displayed locally as ONE block but echoed by
// the server as one PRIVMSG per line inside a draft/multiline batch. Each
// echoed part has to be suppressed separately, or a three-line paste came
// back as three extra messages under the block already on screen.
func TestRegression_EchoMessageDedupeMultilineParts(t *testing.T) {
	s, srv, _ := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	s.SendMessage("#eco", "first line\nsecond line")
	srv.WaitFor("BATCH +*draft/multiline #eco")
	srv.WaitFor("*PRIVMSG #eco :first line")
	srv.WaitFor("*PRIVMSG #eco :second line")

	// one local block, not two lines
	block := msgOnly(t, s, "#eco", "first line\nsecond line")
	if !block.Self {
		t.Errorf("local multiline block is not marked Self: %+v", block)
	}

	// the server echoes it back part by part, inside a real batch
	srv.Replay(`
		S: :irc.test BATCH +eml draft/multiline #eco
		S: @batch=eml :eco!eco@test PRIVMSG #eco :first line
		S: @batch=eml :eco!eco@test PRIVMSG #eco :second line
		S: :irc.test BATCH -eml
	`)
	srv.Sync()

	if got := msgTexts(t, s, "#eco"); len(got) != 1 || got[0] != "first line\nsecond line" {
		t.Fatalf("#eco holds %q after the echoed batch, want one merged block", got)
	}
}

// --- msgid dedupe ---------------------------------------------------------

// Bug: replayed history duplicated what was already on screen. Reconnects
// clear historyRequested and refetch CHATHISTORY, and a user can ask for
// /history at any time; without a per-buffer msgid seen-set the overlap
// between the replay and the live messages was rendered twice.
// (memory.md: "Reconnect rejoins open channels and refetches history; dedupe
// makes that safe".)
func TestRegression_MsgIDDedupeAcrossPlayback(t *testing.T) {
	s, srv, _ := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	// a live message, seen once
	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG #eco :hello eco")
	srv.Sync()
	if got := msgTexts(t, s, "#eco"); len(got) != 1 {
		t.Fatalf("#eco holds %q after the live message, want 1", got)
	}

	// chathistory replays it alongside an older message we never saw
	playback := `
		S: :irc.test BATCH +hist chathistory #eco
		S: @batch=hist;msgid=m0;time=2020-01-01T10:00:00.000Z :alice!alice@test PRIVMSG #eco :older line
		S: @batch=hist;msgid=m1;time=2020-01-01T10:00:01.000Z :alice!alice@test PRIVMSG #eco :hello eco
		S: :irc.test BATCH -hist
	`
	srv.Replay(playback)
	srv.Sync()

	if got := msgTexts(t, s, "#eco"); len(got) != 2 {
		t.Fatalf("#eco holds %q after playback, want the live line plus one new one", got)
	}
	msgOnly(t, s, "#eco", "hello eco")

	// the replay must not have rewritten the live line's flags
	live := msgByID(t, s, "#eco", "m1")
	if live.Playback {
		t.Error("the live message was re-flagged as playback by the replay")
	}
	if !live.Highlight {
		t.Error("the live message lost its highlight when the replay arrived")
	}
	if old := msgByID(t, s, "#eco", "m0"); !old.Playback {
		t.Errorf("replayed message not flagged Playback: %+v", old)
	}

	// a second identical fetch (the /history button, or a reconnect refetch)
	// still adds nothing
	srv.Replay(playback)
	srv.Sync()
	if got := msgTexts(t, s, "#eco"); len(got) != 2 {
		t.Fatalf("#eco holds %q after the second replay, want 2", got)
	}

	// dedupe is per buffer: the same msgid in another buffer is a different
	// message and must still be shown
	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG %s :a private line", testNick)
	srv.Sync()
	if got := msgTexts(t, s, "alice"); len(got) != 1 || got[0] != "a private line" {
		t.Fatalf("PM buffer holds %q, want the private line — msgid dedupe leaked across buffers", got)
	}
}

// --- highlights -----------------------------------------------------------

// Bug: substring highlighting. Matching the nick anywhere in the text made
// "economy" ping the user called eco; the fix is containsWord's nick-char
// boundaries, which must keep letting punctuation-adjacent mentions
// ("@eco", "eco:", "ECO") through.
func TestRegression_HighlightWholeWordOnly(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	cases := []struct {
		text string
		want bool
	}{
		{"hello eco, welcome", true},
		{"@eco ping", true},
		{"eco: are you there", true},
		{"shout at ECO", true},            // matching folds case
		{"the economy is booming", false}, // the bug: substring match
		{"nested in an ecosystem", false},
		{"talking about neco", false},
	}
	for _, c := range cases {
		srv.Send(":alice!alice@test PRIVMSG #eco :%s", c.text)
	}
	srv.Sync()

	for _, c := range cases {
		l := msgOnly(t, s, "#eco", c.text)
		if l.Highlight != c.want {
			t.Errorf("Highlight = %v for %q, want %v", l.Highlight, c.text, c.want)
		}
		if l.Self || l.Status || l.Playback {
			t.Errorf("wrong flags on an incoming channel message: %+v", l)
		}
	}

	// plain channel mentions are notified by the UI's AddLine path; the
	// session's HighlightNote callback is reserved for mentions that arrive
	// as a multiline continuation, where there is no new line to notice
	if hl := ui.Highlights(); len(hl) != 0 {
		t.Errorf("HighlightNote fired %d times for plain lines, want 0: %+v", len(hl), hl)
	}
}

// Bug: a private message that did not happen to contain your nick did not
// highlight, so DMs sat unread with no badge. A PM is a mention by
// definition — but a services NOTICE is not, or NickServ's routine chatter
// raised a desktop notification on every connect.
func TestRegression_HighlightPrivateMessages(t *testing.T) {
	s, srv, _ := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	srv.Send("@msgid=p1 :alice!alice@test PRIVMSG %s :are you around?", testNick)
	srv.Send("@msgid=n1 :NickServ!NickServ@services.test NOTICE %s :Your nick is registered", testNick)
	srv.Sync()

	pm := msgOnly(t, s, "alice", "are you around?")
	if !pm.Highlight {
		t.Errorf("a private message did not highlight: %+v", pm)
	}
	if pm.Notice || pm.Self || pm.Playback || pm.Status {
		t.Errorf("wrong flags on a private message: %+v", pm)
	}

	note := msgOnly(t, s, "NickServ", "Your nick is registered")
	if note.Highlight {
		t.Errorf("a services notice highlighted: %+v", note)
	}
	if !note.Notice {
		t.Errorf("a NOTICE was not flagged as one: %+v", note)
	}
}

// Bug: replayed history and join/part noise raised notifications. Opening a
// channel replayed 100 messages, and every old mention in them pinged again;
// a nick like eco also matched status text ("ecobot joined"). Playback and
// status lines must never notify. Nor must your own messages, which is why
// the highlight test runs !self first.
func TestRegression_HighlightNeverFromPlaybackOrStatus(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	// history playback carrying a mention
	srv.Replay(`
		S: :irc.test BATCH +hist chathistory #eco
		S: @batch=hist;msgid=h1;time=2020-01-01T10:00:00.000Z :alice!alice@test PRIVMSG #eco :ping eco please
		S: :irc.test BATCH -hist
	`)
	// status noise whose text contains our nick
	srv.Send(":ecobot!ecobot@test JOIN #eco")
	srv.Send(":ecobot!ecobot@test PART #eco :bye eco")
	// and our own message mentioning ourselves
	srv.Sync()
	s.SendMessage("#eco", "eco reporting in")
	srv.WaitFor("PRIVMSG #eco :eco reporting in")
	srv.Sync()

	replayed := msgOnly(t, s, "#eco", "ping eco please")
	if replayed.Highlight {
		t.Errorf("a replayed mention highlighted: %+v", replayed)
	}
	if !replayed.Playback {
		t.Errorf("a chathistory message was not flagged Playback: %+v", replayed)
	}

	own := msgOnly(t, s, "#eco", "eco reporting in")
	if own.Highlight {
		t.Errorf("our own message highlighted on our own nick: %+v", own)
	}
	if !own.Self {
		t.Errorf("our own message is not flagged Self: %+v", own)
	}

	b := s.Buffer("#eco")
	statusLines := 0
	for _, l := range b.Lines {
		if !l.Status {
			continue
		}
		statusLines++
		if l.Highlight {
			t.Errorf("a status line highlighted: %+v", l)
		}
		if l.Nick != "" {
			t.Errorf("a status line carries a nick: %+v", l)
		}
	}
	if statusLines == 0 {
		t.Fatal("no status lines were recorded; the join/part noise never arrived")
	}
	if hl := ui.Highlights(); len(hl) != 0 {
		t.Errorf("HighlightNote fired %d times for playback/status, want 0: %+v", len(hl), hl)
	}
}

// --- multiline ------------------------------------------------------------

// Bug: a draft/multiline message rendered as one message per part, so a
// pasted code block became N separate rows with N nick columns. The parts of
// one batch merge into a single line; draft/multiline-concat joins without a
// newline (a soft-wrapped continuation), everything else with one.
func TestRegression_MultilineMergesIntoOneLine(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	srv.Replay(`
		S: :irc.test BATCH +ml draft/multiline #eco
		S: @batch=ml :alice!alice@test PRIVMSG #eco :first part
		S: @batch=ml :alice!alice@test PRIVMSG #eco :second part
		S: @batch=ml;draft/multiline-concat :alice!alice@test PRIVMSG #eco : glued on
		S: :irc.test BATCH -ml
	`)
	srv.Sync()

	got := msgTexts(t, s, "#eco")
	want := "first part\nsecond part glued on"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("#eco holds %q, want one merged line %q", got, want)
	}

	l := msgOnly(t, s, "#eco", want)
	if l.Nick != "alice" {
		t.Errorf("merged line nick = %q, want alice", l.Nick)
	}
	// audit bug #2: a live multiline batch is NOT history playback, or it
	// silently loses highlighting and unread counting
	if l.Playback {
		t.Errorf("a live multiline message was classified as playback: %+v", l)
	}

	// CHARACTERIZATION, expected to change at M6/M7: the incremental-amend
	// COUNT is v1's mechanism, not a requirement. v1 appends the first part and
	// then rewrites the line in place for each further part, so the UI sees two
	// AmendBuffer calls here. Blueprint §3 MUST 2 assembles multiline at batch
	// CLOSE, which emits one finished line and no amends at all. The merged
	// text, nick and flags asserted above are the part that must survive; when
	// the rewrite lands, replace this count with "no amends".
	amends := 0
	for _, buf := range ui.AmendedBuffers() {
		if buf == "#eco" {
			amends++
		}
	}
	if amends != 2 {
		t.Errorf("AmendBuffer fired %d times for the two continuations, want 2", amends)
	}
	if shown := msgUILines(ui, "#eco"); len(shown) != 1 {
		t.Errorf("UI was handed %d lines for a single multiline message, want 1", len(shown))
	}
}

// Bug (engineering-audit #11): "Mentions inside multiline continuations were
// silent". The first part opens the message, so only it was tested for the
// nick; a mention in part two never highlighted, never badged and never
// notified. The continuation must promote the merged line AND fire
// HighlightNote — exactly once, however many later parts also mention us.
func TestRegression_MultilineMentionHighlightsOnce(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	srv.Replay(`
		S: :irc.test BATCH +ml2 draft/multiline #eco
		S: @batch=ml2 :bob!bob@test PRIVMSG #eco :quick question
		S: @batch=ml2 :bob!bob@test PRIVMSG #eco :are you around eco?
		S: @batch=ml2 :bob!bob@test PRIVMSG #eco :ping eco again
		S: :irc.test BATCH -ml2
	`)
	srv.Sync()

	merged := msgOnly(t, s, "#eco", "quick question\nare you around eco?\nping eco again")
	if !merged.Highlight {
		t.Errorf("a mention in a multiline continuation did not highlight: %+v", merged)
	}
	if merged.Playback {
		t.Errorf("a live multiline mention was classified as playback: %+v", merged)
	}

	// CHARACTERIZATION, expected to change at M6/M7: HighlightNote exists only
	// because v1 amends a line already handed to the UI, so a mention arriving
	// in a continuation has no new AddLine to ride on and needs a second
	// callback. Assembling at batch close (blueprint §3 MUST 2) emits the
	// finished line once, with Highlight already set, and M8's Dispatcher
	// deletes the 12-callback surface this belongs to. The requirement that
	// survives is "one notification for one message, naming the part that
	// mentioned us" — not this particular callback.
	hl := ui.Highlights()
	if len(hl) != 1 {
		t.Fatalf("HighlightNote fired %d times, want exactly 1: %+v", len(hl), hl)
	}
	if hl[0].Buffer != "#eco" || hl[0].Line.Nick != "bob" || hl[0].Line.Text != "are you around eco?" {
		t.Errorf("HighlightNote = %+v, want #eco/bob/the mentioning part", hl[0])
	}
}

// --- redaction ------------------------------------------------------------

// msgRedactUI applies an incoming REDACT to the buffer the way the Fyne UI
// does (ui.go's RedactMsg: tombstone the matching line so it stops being a
// message). The mutation lives in the UI layer in v1 — it is not reachable
// headlessly — so the fixture wires it into the session's callback and the
// test can assert on Buffer state.
//
// CHARACTERIZATION, expected to change at M6/M7: only this stand-in is v1's,
// not the behaviour it stands in for. When the core owns redaction (M7, with
// the archive purging row + FTS at M9) this helper is deleted and the test
// asserts directly on the session's own state.
func msgRedactUI(s *Session, buffer, msgid string) {
	if msgid == "" {
		return // an empty id would match every locally echoed line
	}
	b := s.Buffer(buffer)
	if b == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range b.Lines {
		if b.Lines[i].MsgID == msgid {
			b.Lines[i].Text = "(message deleted)"
			b.Lines[i].Status = true
			b.Lines[i].Nick = ""
		}
	}
}

// msgNewRedactSession is newTestSessionUI with the redaction half of the UI
// attached, so a REDACT can be observed as buffer state and not only as a
// callback. It mirrors startTestSession; the callbacks are composed before
// Connect, so the read goroutine never sees them change.
func msgNewRedactSession(t *testing.T) (*Session, *testsrv.Server, *testUI) {
	t.Helper()
	pinTestSettings(t)

	srv := testsrv.New(t)
	ui := &testUI{topics: map[string]string{}}
	cb := ui.callbacks()
	record := cb.RedactMsg
	var s *Session
	cb.RedactMsg = func(buffer, msgid string) {
		record(buffer, msgid)
		msgRedactUI(s, buffer, msgid)
	}
	s = NewSession(testConfig(), cb)
	s.Dial = srv.Dial
	s.Connect()
	stopSession(t, s)
	srv.Register(testNick)
	return s, srv, ui
}

// Bug: draft/message-redaction was negotiated and the REDACT never removed
// anything — a message deleted by an op stayed on screen forever. The
// session must route REDACT to the buffer and msgid it names (and only
// those), which in turn requires every incoming line to carry its msgid.
func TestRegression_RedactRemovesMessage(t *testing.T) {
	s, srv, ui := msgNewRedactSession(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")

	srv.Send("@msgid=r1 :alice!alice@test PRIVMSG #eco :a keeper")
	srv.Send("@msgid=r2 :mallory!mallory@test PRIVMSG #eco :something regrettable")
	srv.Send("@msgid=r3 :alice!alice@test PRIVMSG %s :same id space, other buffer", testNick)
	srv.Sync()
	if got := msgTexts(t, s, "#eco"); len(got) != 2 {
		t.Fatalf("#eco holds %q before the redaction, want 2", got)
	}

	srv.Send(":mallory!mallory@test REDACT #eco r2 :oops")
	srv.Sync()

	if got := ui.Redactions(); len(got) != 1 || got[0] != "#eco|r2" {
		t.Fatalf("RedactMsg calls = %q, want one #eco|r2", got)
	}
	got := msgTexts(t, s, "#eco")
	if len(got) != 1 || got[0] != "a keeper" {
		t.Fatalf("#eco holds %q after the redaction, want only the untouched message", got)
	}
	if pm := msgTexts(t, s, "alice"); len(pm) != 1 {
		t.Errorf("the redaction reached the wrong buffer: alice holds %q", pm)
	}

	// a REDACT naming a msgid we never displayed changes nothing
	srv.Send(":mallory!mallory@test REDACT #eco nosuchid :oops")
	srv.Sync()
	if got := msgTexts(t, s, "#eco"); len(got) != 1 || got[0] != "a keeper" {
		t.Fatalf("#eco holds %q after an unknown-msgid REDACT, want it untouched", got)
	}
}

// --- ignore ---------------------------------------------------------------

// msgIgnore turns ignoring on for a nick through the production toggle and
// guarantees the process-wide ignore set is clean again when the test ends.
// toggleIgnore persists through fyne.Preferences; prefs comes from fyne's
// in-memory test app, so nothing touches the real preference store.
func msgIgnore(t *testing.T, prefs fyne.Preferences, nick string) {
	t.Helper()
	t.Cleanup(func() {
		ignoreMu.Lock()
		defer ignoreMu.Unlock()
		delete(ignored, strings.ToLower(nick))
	})
	if !toggleIgnore(prefs, nick) {
		t.Fatalf("toggleIgnore(%q) did not turn ignoring on", nick)
	}
}

// Bug: /ignore only greyed the member list. An ignored nick's PRIVMSGs still
// landed in the buffer, still counted as unread and — for a PM — still
// opened a new buffer, which is exactly the pop-up a user ignores someone to
// stop. Unignoring must restore delivery immediately, with no restart.
func TestRegression_IgnoredNickNeverReachesABuffer(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice", "+bob")
	// NewTempApp, not NewApp: both set fyne's process-wide current app, but
	// only this one puts a fresh one back afterwards, so the ignore test
	// cannot hand its app (and its preferences) to whatever runs next.
	prefs := fynetest.NewTempApp(t).Preferences()
	msgIgnore(t, prefs, "spammer")

	srv.Send("@msgid=s1 :spammer!spammer@test PRIVMSG #eco :buy my coins")
	srv.Send("@msgid=s2 :spammer!spammer@test PRIVMSG %s :buy my coins, %s", testNick, testNick)
	srv.Send("@msgid=k1 :alice!alice@test PRIVMSG #eco :still here")
	srv.Sync()

	if got := msgTexts(t, s, "#eco"); len(got) != 1 || got[0] != "still here" {
		t.Fatalf("#eco holds %q, want only alice's message", got)
	}
	if s.Buffer("spammer") != nil {
		t.Error("an ignored nick's private message opened a buffer")
	}
	for _, r := range ui.AllLines() {
		if strings.Contains(r.Line.Text, "buy my coins") {
			t.Fatalf("an ignored nick's message reached the UI: %+v", r)
		}
	}

	// unignoring restores delivery at once
	if toggleIgnore(prefs, "spammer") {
		t.Fatal("toggleIgnore did not turn ignoring off")
	}
	srv.Send("@msgid=s3 :spammer!spammer@test PRIVMSG #eco :back again")
	srv.Sync()

	got := msgTexts(t, s, "#eco")
	if len(got) != 2 || got[1] != "back again" {
		t.Fatalf("#eco holds %q after unignoring, want alice's line then the spammer's", got)
	}
}
