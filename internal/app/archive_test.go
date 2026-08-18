package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// M9's contract, driven the same way the rest of this package is: a real
// Session, a real archive on a temp path, the scripted fake server, and
// srv.Sync() as the only synchronisation. What is new is that Session.settle
// now drains the archive's worker too, so an assertion made after Sync can be
// about the FILE and not only about the window.
//
// The rules of the file are the M5 contract's: assert on state and on what the
// funnel told the screen, never on rendered strings; never sleep.

// archiveSession is newTestSessionUI with a history file. The path is returned
// so a test can close the session and open a SECOND one over the same archive,
// which is the whole of "does history survive a restart?".
func archiveSession(t *testing.T) (*Session, *testsrv.Server, *testUI, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.db")
	s, srv, ui := archiveSessionAt(t, path)
	return s, srv, ui, path
}

// archiveSessionAt is the same over a given path, with optional preference
// tweaks applied BEFORE the session is built — the store reads its scrollback
// and its notification level once, at construction, so a test that needs them
// different cannot publish afterwards.
func archiveSessionAt(t *testing.T, path string, tweak ...func(*config.Settings)) (*Session, *testsrv.Server, *testUI) {
	t.Helper()
	pinTestSettings(t)
	if len(tweak) > 0 {
		prev := config.Current()
		next := *prev
		for _, f := range tweak {
			f(&next)
		}
		config.Publish(&next)
		t.Cleanup(func() { config.Publish(prev) })
	}

	srv := testsrv.New(t)
	ui, d := newTestSink(t)
	cfg := testConfig()
	cfg.Archive = path
	s := NewSession(cfg, d)
	if s.arcErr != nil {
		t.Fatalf("opening the archive at %s: %v", path, s.arcErr)
	}
	if !s.Archiving() {
		t.Fatal("the session did not attach an archive")
	}
	// Registered BEFORE stopSession's cleanup, so it runs AFTER it: the pump
	// has to be gone before the file it writes to is closed.
	t.Cleanup(func() { _ = s.closeArchive() })
	s.Dial = srv.Dial
	srv.Barrier = s.settle
	s.Connect()
	stopSession(t, s)
	srv.Register(testNick)
	return s, srv, ui
}

// arcRows reads a buffer's stored messages straight out of the file, bypassing
// everything the session holds. It is what "on disk" means in this file.
func arcRows(t *testing.T, path, folded string) []archive.Message {
	t.Helper()
	db, err := archive.Open(path, archive.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("opening %s read-only: %v", path, err)
	}
	defer db.Close()
	rows, err := db.Latest(context.Background(), folded, 500)
	if err != nil {
		t.Fatalf("reading %s: %v", folded, err)
	}
	return rows
}

func arcTexts(ms []archive.Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Body)
	}
	return out
}

// arcHas reports whether a body is among the stored rows.
func arcHas(ms []archive.Message, body string) bool {
	for _, m := range ms {
		if m.Body == body {
			return true
		}
	}
	return false
}

// TestArchive_LiveMessagesReachTheDisk pins the live path (§5.4): every message
// the store accepts is recorded, write-behind, as the same change is folded
// into the projection. Without this nothing else in M9 has anything to read.
func TestArchive_LiveMessagesReachTheDisk(t *testing.T) {
	s, srv, _, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	srv.Send("@msgid=a1 :alice!alice@test PRIVMSG #eco :the build is green")
	srv.Send("@msgid=a2 :alice!alice@test PRIVMSG #eco :shipping it")
	srv.Sync()

	rows := arcRows(t, path, "#eco")
	if !arcHas(rows, "the build is green") || !arcHas(rows, "shipping it") {
		t.Fatalf("stored rows = %q, want both messages", arcTexts(rows))
	}
	// The msgid is the dedupe key and the pagination anchor; a row without one
	// is a row no replay can be merged against.
	for _, m := range rows {
		if m.Body == "the build is green" && m.MsgID != "a1" {
			t.Errorf("stored msgid = %q, want a1", m.MsgID)
		}
	}

	// A replay of the same msgid must not add a second row (ADR-3).
	before := len(rows)
	srv.Send("@msgid=a1 :alice!alice@test PRIVMSG #eco :the build is green")
	srv.Sync()
	if got := len(arcRows(t, path, "#eco")); got != before {
		t.Errorf("a replayed msgid grew the archive from %d to %d rows", before, got)
	}
}

// TestArchive_OurOwnMessageIsStoredOnceWithItsMsgID pins the one message the
// live path cannot record when it is created: our own. It is rendered
// immediately as a pending echo with no msgid, and it becomes durable — and
// dedupable, and redactable — only when the labelled echo resolves it. Record
// it at both moments and every message you send is in the file twice.
func TestArchive_OurOwnMessageIsStoredOnceWithItsMsgID(t *testing.T) {
	s, srv, _, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	s.SendMessage("#eco", "on it")
	sent := srv.WaitFor("PRIVMSG #eco :on it")
	srv.Sync()
	srv.Send("@label=%s;msgid=own1 :%s!%s@test PRIVMSG #eco :on it",
		bufLabelOf(t, sent), testNick, testNick)
	srv.Sync()

	var found []archive.Message
	for _, m := range arcRows(t, path, "#eco") {
		if m.Body == "on it" {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		t.Fatalf("our own message is stored %d times, want once", len(found))
	}
	if found[0].MsgID != "own1" {
		t.Errorf("stored msgid = %q, want the server's own1", found[0].MsgID)
	}
	if found[0].Sender != testNick {
		t.Errorf("stored sender = %q, want %q", found[0].Sender, testNick)
	}
}

// TestArchive_HistorySurvivesARestart is the milestone, end to end: record,
// close, reopen, and the buffer has its history back — with our own messages
// still ours, and without the archive's own rows being written back to it.
func TestArchive_HistorySurvivesARestart(t *testing.T) {
	first, srv, _, path := archiveSession(t)
	joinChannel(t, first, srv, "#eco", "@alice")
	srv.Send("@msgid=h1 :alice!alice@test PRIVMSG #eco :yesterday's news")
	srv.Send("@msgid=h2 :bob!bob@test PRIVMSG #eco :and the day before")
	srv.Sync()
	first.Quit()
	if err := first.closeArchive(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}

	// A NEW process, over the same file.
	second, srv2, ui2, _ := restartOver(t, path)
	joinChannel(t, second, srv2, "#eco", "@alice")
	srv2.Sync()

	b := second.Buffer("#eco")
	if b == nil {
		t.Fatal("no #eco buffer after rejoining")
	}
	texts := bufTexts(messageLines(b))
	if !containsText(texts, "yesterday's news") || !containsText(texts, "and the day before") {
		t.Fatalf("after a restart #eco holds %q, want both archived messages", texts)
	}
	// Order: the restored block is ABOVE whatever this session has said.
	if i, j := indexOfText(texts, "yesterday's news"), indexOfText(texts, "and the day before"); i > j {
		t.Errorf("restored messages are out of order: %q", texts)
	}
	// The window's rows reached the screen as a prepend, not as news.
	if got := ui2.Prepended("#eco"); len(got) < 2 {
		t.Errorf("the restored block was delivered as %d prepended rows, want at least 2", len(got))
	}
	// Restoring must not write the archive back into itself: two rows in, two
	// rows out, whatever the msgid situation.
	rows := arcRows(t, path, "#eco")
	n := 0
	for _, m := range rows {
		if m.Body == "yesterday's news" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the restored message is now stored %d times: restoring wrote it back", n)
	}
}

// restartOver opens a second session over an existing archive, as a restart
// does. It is deliberately not a helper that hides the close: the point of
// every test that uses it is that the FIRST session is gone.
func restartOver(t *testing.T, path string) (*Session, *testsrv.Server, *testUI, string) {
	t.Helper()
	s, srv, ui := archiveSessionAt(t, path)
	return s, srv, ui, path
}

// TestArchive_UnreadSurvivesARestart is the other half of the milestone: the
// badge comes back too, because it is derived from a read marker that was
// stored — not from counting the lines this process happened to see.
func TestArchive_UnreadSurvivesARestart(t *testing.T) {
	first, srv, _, path := archiveSession(t)
	joinChannel(t, first, srv, "#eco", "@alice")
	srv.Send("@msgid=u1 :alice!alice@test PRIVMSG #eco :one")
	srv.Sync()
	first.MarkRead("#eco") // the user read up to here
	settleUI(first)
	srv.Send("@msgid=u2 :alice!alice@test PRIVMSG #eco :two")
	srv.Send("@msgid=u3 :alice!alice@test PRIVMSG #eco :three")
	srv.Sync()
	first.Quit()
	if err := first.closeArchive(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}

	second, srv2, ui2, _ := restartOver(t, path)
	joinChannel(t, second, srv2, "#eco", "@alice")
	srv2.Sync()

	if got := ui2.Unread("#eco").Unread; got != 2 {
		t.Errorf("unread after the restart = %d, want 2 (the two messages after the stored marker)", got)
	}
}

// TestArchive_ScrollUpPagesFromTheArchive pins the backward path: opening a
// buffer restores its newest page, reaching the top asks the archive for the
// one above it, the rows arrive ABOVE what is on screen, and the "Loading
// older…" state brackets the wait.
//
// The history is seeded straight into the file rather than driven through the
// server, because what is under test is the reader and not the writer, and
// because it has to be longer than one page for a page boundary to exist.
func TestArchive_ScrollUpPagesFromTheArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	const total = hydrateLimit + 40
	seedArchive(t, path, "#eco", total)

	s, srv, ui := archiveSessionAt(t, path)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Sync()

	before := bufTexts(messageLines(s.Buffer("#eco")))
	if containsText(before, "seeded 1") {
		t.Fatalf("the cold page already holds the oldest line; there is no page boundary to test")
	}
	if !containsText(before, "seeded "+itoa(total)) {
		t.Fatalf("the cold page does not hold the newest line: %q", tail(before, 3))
	}

	s.LoadOlder("#eco")
	settleUI(s)

	after := bufTexts(messageLines(s.Buffer("#eco")))
	if len(after) <= len(before) {
		t.Fatalf("scroll-up added nothing (%d rows before and after)", len(after))
	}
	if !containsText(after, "seeded 1") {
		t.Errorf("after paging the oldest seeded line is still missing: %q", tail(after, 3))
	}
	if i, j := indexOfText(after, "seeded 1"), indexOfText(after, "seeded "+itoa(total)); i > j {
		t.Errorf("the older page landed below the newer one (%d > %d)", i, j)
	}
	if got := ui.Prepended("#eco"); len(got) == 0 {
		t.Error("the older page was not delivered as a prepend")
	}
	if got := ui.LoadingStates("#eco"); len(got) < 2 || !got[0] || got[len(got)-1] {
		t.Errorf("loading states = %v, want a true bracketed by a final false", got)
	}

	// Paging past the beginning stops asking rather than looping: the archive
	// is exhausted and there is nothing on the wire to ask either (this server
	// never advertised chathistory).
	s.LoadOlder("#eco")
	settleUI(s)
	if got := bufTexts(messageLines(s.Buffer("#eco"))); len(got) != len(after) {
		t.Errorf("paging past the beginning changed the buffer from %d to %d rows", len(after), len(got))
	}
}

// TestArchive_RestoringNeverDoublesWhatIsHere pins the two rules that keep the
// live path and the cold path from describing the same message twice.
//
// A restore reads back rows THIS session wrote a moment ago. The ones with a
// msgid recognise themselves; a status line — "You joined #eco" — has neither a
// msgid nor an id the archive minted, so it would arrive as a second, identical
// row above the first. And a restore must not be written back: for a row with a
// msgid that is a slow way of achieving nothing, and for one without it is a
// duplicate that grows every time the profile is opened.
func TestArchive_RestoringNeverDoublesWhatIsHere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	seedArchive(t, path, "#eco", 3)
	// A second buffer, so that the archive holds rows #eco's window does NOT —
	// which is what makes "this row is already here" and "this row is from an
	// older run" two different questions below.
	seedArchive(t, path, "dave", 2)

	s, srv, _ := archiveSessionAt(t, path)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Sync()

	before := bufTexts(s.Buffer("#eco").Lines)
	rowsBefore := len(arcRows(t, path, "#eco"))

	// Hydrate the same buffer a second time, with the join notice this session
	// wrote now certainly on disk. Nothing may change: not the window, not the
	// file.
	id := model.BufferID{Network: s.netID, Folded: "#eco"}
	s.mu.Lock()
	delete(s.hydrated, "#eco")
	s.hydrateLocked(id)
	s.mu.Unlock()
	settleUI(s)

	after := bufTexts(s.Buffer("#eco").Lines)
	if len(after) != len(before) {
		t.Errorf("a second restore changed the window from %d to %d rows:\n%q", len(before), len(after), after)
	}
	if got := len(arcRows(t, path, "#eco")); got != rowsBefore {
		t.Errorf("a restore wrote itself back: %d rows on disk, was %d", got, rowsBefore)
	}

	// The two rules, stated directly, because the ids the store mints and the
	// rowids the archive assigns usually happen to line up — so a restore that
	// broke both would still look fine here until the day they did not (a
	// pending echo is recorded when it resolves, not when it is minted, and
	// that is all it takes).
	s.mu.Lock()
	items := s.restoreLocked(id, []archive.Message{
		{RowID: 4, MsgID: "restored-with-a-msgid", Buffer: "#eco",
			Time: time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC), Sender: "alice", Body: "from an older run"},
		{RowID: s.arcBase + 500, Buffer: "#eco",
			Time: time.Date(2026, 8, 1, 11, 30, 0, 0, time.UTC), Sender: "", Body: "written by this session"},
	}, true)
	s.mu.Unlock()
	s.emit(items)
	settleUI(s)

	window := bufTexts(s.Buffer("#eco").Lines)
	if !containsText(window, "from an older run") {
		t.Errorf("a row from a previous run was not restored: %q", window)
	}
	if containsText(window, "written by this session") {
		t.Errorf("a msgid-less row THIS session wrote was restored on top of itself: %q", window)
	}
	if got := len(arcRows(t, path, "#eco")); got != rowsBefore {
		t.Errorf("restoring wrote %d new rows to disk", got-rowsBefore)
	}
}

// TestArchive_ReopeningAClosedBufferRestoresItAgain pins the other half of the
// cold path. Closing a conversation throws its window away; opening it again
// has to fill it from the file, or "history survives a restart" quietly stops
// applying to every buffer you have closed since.
func TestArchive_ReopeningAClosedBufferRestoresItAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	seedArchive(t, path, "carol", 5)

	s, srv, _ := archiveSessionAt(t, path)
	srv.Send("@msgid=live1 :carol!carol@test PRIVMSG %s :hello again", testNick)
	srv.Sync()
	if got := bufTexts(messageLines(s.Buffer("carol"))); !containsText(got, "seeded 5") {
		t.Fatalf("the conversation opened without its history: %q", got)
	}

	s.CloseBuffer("carol")
	settleUI(s)
	if s.Buffer("carol") != nil {
		t.Fatal("the buffer did not close")
	}

	srv.Send("@msgid=live2 :carol!carol@test PRIVMSG %s :still here?", testNick)
	srv.Sync()
	got := bufTexts(messageLines(s.Buffer("carol")))
	if !containsText(got, "seeded 5") {
		t.Errorf("the reopened conversation is empty of history: %q", got)
	}
	if !containsText(got, "still here?") {
		t.Errorf("the reopened conversation lost the live message: %q", got)
	}
}

// TestArchive_ScrollUpMissGoesToTheServer pins the miss path: with nothing
// older in the archive, the scroll-up becomes CHATHISTORY BEFORE the oldest
// message we hold, and the reply merges above the window and is recorded — so
// the same scroll-up next time is served from disk.
func TestArchive_ScrollUpMissGoesToTheServer(t *testing.T) {
	s, srv, ui, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG #eco :the newest thing")
	srv.Sync()

	s.LoadOlder("#eco")
	line := srv.WaitFor("CHATHISTORY BEFORE #eco")

	// The reply carries the request's label, as a real server's does: the batch
	// itself says only which target it is for, so the DIRECTION — and with it
	// "this block goes above what is on screen" — is remembered from the
	// question and nowhere else.
	srv.Send("@label=%s :irc.test BATCH +old chathistory #eco", bufLabelOf(t, line))
	srv.Send("@batch=old;msgid=m0 :bob!bob@test PRIVMSG #eco :something older")
	srv.Send(":irc.test BATCH -old")
	srv.Sync()

	texts := bufTexts(messageLines(s.Buffer("#eco")))
	if len(texts) < 2 || texts[0] != "something older" {
		t.Errorf("#eco holds %q, want the fetched page above the live message", texts)
	}
	if got := ui.LoadingStates("#eco"); len(got) == 0 || got[len(got)-1] {
		t.Errorf("loading states = %v, want the wait to have ended", got)
	}
	if rows := arcRows(t, path, "#eco"); !arcHas(rows, "something older") {
		t.Errorf("the fetched page was not recorded: %q", arcTexts(rows))
	}
}

// TestArchive_ScrollUpStopsAskingWhenNobodyCanAnswer pins the other end of the
// backward path. The UI fires LoadOlder from a scroll POSITION — a thing that
// happens dozens of times a second while a reader holds the wheel — so a
// question nobody can answer has to be asked once, not once per pixel.
//
// It runs on a session that was never connected, which is also the offline half
// of the milestone: history opens from the file with no server in the picture.
func TestArchive_ScrollUpStopsAskingWhenNobodyCanAnswer(t *testing.T) {
	pinTestSettings(t)
	path := filepath.Join(t.TempDir(), "profile.db")
	seedArchive(t, path, "#eco", 3)

	ui, d := newTestSink(t)
	cfg := testConfig()
	cfg.Archive = path
	s := NewSession(cfg, d)
	t.Cleanup(func() { _ = s.closeArchive() })

	s.getBuffer("#eco")
	settleUI(s)
	if got := bufTexts(messageLines(s.Buffer("#eco"))); !containsText(got, "seeded 3") {
		t.Fatalf("an unconnected session did not open the buffer from the file: %q", got)
	}

	s.LoadOlder("#eco")
	settleUI(s)
	states := ui.LoadingStates("#eco")
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("loading states = %v, want exactly [true false]", states)
	}

	s.LoadOlder("#eco")
	s.LoadOlder("#eco")
	settleUI(s)
	if got := ui.LoadingStates("#eco"); len(got) != len(states) {
		t.Errorf("scroll-up asked again with nobody left to ask: %v", got)
	}
}

// TestArchive_ReconnectFillsTheGapAndSaysSo pins §5.4's reconnect path and the
// beginner-visible line the blueprint asks for: CHATHISTORY AFTER the newest
// message we hold, per buffer, and ONE quiet sentence instead of a replay
// flood.
func TestArchive_ReconnectFillsTheGapAndSaysSo(t *testing.T) {
	s, srv, _, _ := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=g1 :alice!alice@test PRIVMSG #eco :before the drop")
	srv.Sync()

	srv.Close()
	srv.NextConn()
	srv.Register(testNick)
	srv.WaitFor("JOIN #eco")

	// TARGETS goes first, so a conversation it discovers is asked about before
	// the counter can reach zero.
	if line := srv.WaitFor("CHATHISTORY TARGETS"); !strings.Contains(line, "TARGETS") {
		t.Fatalf("no TARGETS request after the reconnect: %q", line)
	}
	after := srv.WaitFor("CHATHISTORY AFTER #eco")
	if !strings.Contains(after, "msgid=g1") {
		t.Errorf("CHATHISTORY AFTER anchored on %q, want msgid=g1", after)
	}

	srv.Send(":irc.test BATCH +t draft/chathistory-targets")
	srv.Send(":irc.test BATCH -t")
	srv.Send(":irc.test BATCH +gap chathistory #eco")
	srv.Send("@batch=gap;msgid=g2 :alice!alice@test PRIVMSG #eco :you missed this")
	srv.Send("@batch=gap;msgid=g3 :bob!bob@test PRIVMSG #eco :and this")
	srv.Send(":irc.test BATCH -gap")
	srv.Sync()

	log := bufTexts(s.Buffer(serverBuffer).Lines)
	if !containsText(log, "Reconnected — caught up 2 messages.") {
		t.Errorf("the server log says %q, want the caught-up line", log)
	}
	// One sentence, not one per buffer and not one per message.
	n := 0
	for _, l := range log {
		if strings.HasPrefix(l, "Reconnected — caught up") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the caught-up line was written %d times, want once", n)
	}
}

// TestArchive_TargetsOpenAConversationThatHappenedOffline pins the other half
// of the gap: a channel announces itself by being rejoined, a private message
// has nobody to announce it. Without TARGETS it is simply lost.
func TestArchive_TargetsOpenAConversationThatHappenedOffline(t *testing.T) {
	s, srv, _, _ := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=t1 :alice!alice@test PRIVMSG #eco :hello")
	srv.Sync()

	srv.Close()
	srv.NextConn()
	srv.Register(testNick)
	srv.WaitFor("CHATHISTORY TARGETS")

	srv.Send(":irc.test BATCH +t draft/chathistory-targets")
	srv.Send("@batch=t :irc.test CHATHISTORY TARGETS carol 2026-08-19T10:00:00.000Z")
	srv.Send("@batch=t :irc.test CHATHISTORY TARGETS #eco 2026-08-19T10:00:00.000Z")
	srv.Send(":irc.test BATCH -t")
	srv.Sync()

	if s.Buffer("carol") == nil {
		t.Fatal("TARGETS named carol and no buffer was opened for the conversation")
	}
	// A channel from TARGETS must NOT reopen: leaving one and having every
	// reconnect drag it back is the failure mode that rule exists to avoid. The
	// #eco buffer here is the one the rejoin made, so ask about a channel we
	// are not in.
	srv.Send(":irc.test BATCH +t2 draft/chathistory-targets")
	srv.Send("@batch=t2 :irc.test CHATHISTORY TARGETS #left 2026-08-19T10:00:00.000Z")
	srv.Send(":irc.test BATCH -t2")
	srv.Sync()
	if s.Buffer("#left") != nil {
		t.Error("a channel named by TARGETS was reopened")
	}
	// The conversation it did open is fetched, which is what makes it useful.
	srv.WaitFor("CHATHISTORY LATEST carol")
}

// TestArchive_MarkReadGoesOnTheWireAndComesBack pins the marker as one fact in
// three places: the store derives the count from it, the archive stores it, the
// server carries it to the user's other devices — and a marker arriving FROM
// another device lowers the count here.
func TestArchive_MarkReadGoesOnTheWireAndComesBack(t *testing.T) {
	s, srv, ui, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=r1;time=2026-08-19T10:00:00.000Z :alice!alice@test PRIVMSG #eco :one")
	srv.Send("@msgid=r2;time=2026-08-19T10:00:01.000Z :alice!alice@test PRIVMSG #eco :two")
	srv.Sync()
	if got := ui.Unread("#eco").Unread; got != 2 {
		t.Fatalf("unread = %d before reading, want 2", got)
	}

	s.MarkRead("#eco")
	line := srv.WaitFor("MARKREAD #eco")
	if !strings.Contains(line, "timestamp=2026-08-19T10:00:01") {
		t.Errorf("MARKREAD sent %q, want the newest message's timestamp", line)
	}
	settleUI(s)
	if got := ui.Unread("#eco").Unread; got != 0 {
		t.Errorf("unread after reading = %d, want 0", got)
	}
	// It is on disk, so the next process starts where this one stopped.
	db, err := archive.Open(path, archive.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	m, err := db.ReadMarker(context.Background(), "#eco")
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	if m.At.IsZero() {
		t.Error("the read marker was not stored")
	}

	// Another device reads further; the count here follows it.
	srv.Send("@msgid=r3;time=2026-08-19T10:00:02.000Z :alice!alice@test PRIVMSG #eco :three")
	srv.Sync()
	if got := ui.Unread("#eco").Unread; got != 1 {
		t.Fatalf("unread after a new message = %d, want 1", got)
	}
	srv.Send(":irc.test MARKREAD #eco timestamp=2026-08-19T10:00:02.000Z")
	srv.Sync()
	if got := ui.Unread("#eco").Unread; got != 0 {
		t.Errorf("unread after another device's MARKREAD = %d, want 0", got)
	}
}

// TestArchive_PerBufferOptOutStopsTheDisk pins the privacy contract's first
// promise: logging is on by default, and turning it off for one buffer means
// nothing from that buffer reaches the file — while everything else still does.
func TestArchive_PerBufferOptOutStopsTheDisk(t *testing.T) {
	s, srv, _, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	joinChannel(t, s, srv, "#other", "@alice")

	if !s.Logging("#eco") {
		t.Fatal("logging is not on by default")
	}
	s.SetLogging("#eco", false)
	settleUI(s)
	if s.Logging("#eco") {
		t.Fatal("logging is still on after opting out")
	}

	srv.Send("@msgid=o1 :alice!alice@test PRIVMSG #eco :secret")
	srv.Send("@msgid=o2 :alice!alice@test PRIVMSG #other :not secret")
	srv.Sync()

	if rows := arcRows(t, path, "#eco"); arcHas(rows, "secret") {
		t.Errorf("a buffer with logging off was written to disk: %q", arcTexts(rows))
	}
	if rows := arcRows(t, path, "#other"); !arcHas(rows, "not secret") {
		t.Errorf("opting one buffer out stopped another: %q", arcTexts(rows))
	}
	// It is on screen either way: opting out of the FILE is not opting out of
	// the conversation.
	if texts := bufTexts(messageLines(s.Buffer("#eco"))); !containsText(texts, "secret") {
		t.Errorf("#eco on screen = %q, want the message still shown", texts)
	}

	// And it survives: the setting lives in the file it governs.
	s.Quit()
	if err := s.closeArchive(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	again, _, _ := archiveSessionAt(t, path)
	if again.Logging("#eco") {
		t.Error("the per-buffer opt-out did not survive a restart")
	}
}

// TestArchive_ExcludingDMsStopsTheDisk pins the global switch: channels are
// still recorded, private messages are not, and the answer is written through
// to the archive so it is enforced there too.
func TestArchive_ExcludingDMsStopsTheDisk(t *testing.T) {
	s, srv, _, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	pinExcludeDMs(t, true)

	srv.Send("@msgid=d1 :carol!carol@test PRIVMSG %s :just between us", testNick)
	srv.Send("@msgid=d2 :alice!alice@test PRIVMSG #eco :in public")
	srv.Sync()

	if rows := arcRows(t, path, "carol"); arcHas(rows, "just between us") {
		t.Errorf("a private message was written to disk with DM logging off: %q", arcTexts(rows))
	}
	if rows := arcRows(t, path, "#eco"); !arcHas(rows, "in public") {
		t.Errorf("excluding DMs also stopped a channel: %q", arcTexts(rows))
	}
	if s.Logging("carol") {
		t.Error("the DM buffer's stored setting still says logging is on")
	}
}

// TestArchive_FirstDMRaisesThePrompt pins the other half of §5.4's DM rule: the
// question is asked when a private conversation first opens, once, and never
// for a channel.
func TestArchive_FirstDMRaisesThePrompt(t *testing.T) {
	s, srv, ui, _ := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	if got := ui.DMPrompts(); len(got) != 0 {
		t.Fatalf("joining a channel raised the private-message prompt: %v", got)
	}

	srv.Send("@msgid=d1 :carol!carol@test PRIVMSG %s :hello", testNick)
	srv.Send("@msgid=d2 :dave!dave@test PRIVMSG %s :hello too", testNick)
	srv.Sync()

	if got := ui.DMPrompts(); len(got) != 1 || got[0] != "carol" {
		t.Errorf("prompts = %v, want exactly one, for carol", got)
	}

	// A profile that has already answered is never asked again.
	_, srv2, ui2 := archiveSessionAt(t, filepath.Join(t.TempDir(), "second.db"),
		func(c *config.Settings) { c.DMLogAnswered = true })
	srv2.Send("@msgid=d3 :erin!erin@test PRIVMSG %s :hi", testNick)
	srv2.Sync()
	if got := ui2.DMPrompts(); len(got) != 0 {
		t.Errorf("the prompt was raised again after it had been answered: %v", got)
	}
}

// TestArchive_RedactPurgesTheStoredRow pins §5.4's redaction rule from the
// outside: a withdrawn message leaves no body in the file, not even when it was
// still in the write-behind queue when the REDACT arrived.
func TestArchive_RedactPurgesTheStoredRow(t *testing.T) {
	s, srv, _, path := archiveSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	srv.Send("@msgid=x1 :alice!alice@test PRIVMSG #eco :something regrettable")
	srv.Send(":alice!alice@test REDACT #eco x1 :typo")
	srv.Sync()

	rows := arcRows(t, path, "#eco")
	if arcHas(rows, "something regrettable") {
		t.Errorf("the withdrawn message is still in the archive: %q", arcTexts(rows))
	}
	// The tombstone stays, so the conversation around it still reads.
	found := false
	for _, m := range rows {
		if m.MsgID == "x1" {
			found = true
			if !m.Redacted || m.Body != "" {
				t.Errorf("the tombstone row = %+v, want redacted with no body", m)
			}
		}
	}
	if !found {
		t.Error("no tombstone left where the withdrawn message was")
	}
	// The search index has to lose it too, or the text is still readable.
	db, err := archive.Open(path, archive.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	hits, err := db.Search(context.Background(), "regrettable", archive.Filters{}, 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("the withdrawn text is still findable: %d hits", len(hits))
	}
}

// TestArchive_ListStreamsIntoTheCache pins §5.4's LIST rule: the scan goes to
// the cache as it arrives, the browser reads it from there, and the store does
// not keep a second copy once the scan has ended.
func TestArchive_ListStreamsIntoTheCache(t *testing.T) {
	s, srv, _, path := archiveSession(t)

	s.RequestList()
	srv.WaitFor("LIST")
	srv.Send(":irc.test 321 %s Channel :Users  Name", testNick)
	srv.Send(":irc.test 322 %s #alpha 42 :the alpha channel", testNick)
	srv.Send(":irc.test 322 %s #beta 7 :the beta channel", testNick)
	srv.Send(":irc.test 323 %s :End of /LIST", testNick)
	srv.Sync()

	got := s.Channels("", 50)
	if len(got) != 2 {
		t.Fatalf("the browser sees %d channels, want 2: %+v", len(got), got)
	}
	if got[0].Name != "#alpha" || got[0].Users != 42 {
		t.Errorf("first row = %+v, want #alpha with 42 users (most populated first)", got[0])
	}
	if match := s.Channels("beta", 50); len(match) != 1 || match[0].Name != "#beta" {
		t.Errorf("searching the cache for beta = %+v, want only #beta", match)
	}

	// It is in the file, so the browser has something to show before the first
	// scan of the NEXT session finishes.
	db, err := archive.Open(path, archive.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	cached, err := db.Channels(context.Background(), "", 50)
	if err != nil {
		t.Fatalf("reading the cache: %v", err)
	}
	if len(cached) != 2 {
		t.Errorf("the cache holds %d channels, want 2", len(cached))
	}
	// And the store is not holding a second copy of it.
	if n := len(s.store.ChannelList(s.netID)); n != 0 {
		t.Errorf("the store still holds %d LIST rows after the scan ended", n)
	}
}

// TestArchive_ASessionWithoutAnArchiveStillWorks pins the fallback the whole
// design rests on: eco without a writable archive is eco before M9, not eco
// refusing to connect. Everything below is the offline suite's own fixture,
// which has no archive at all — this test only says so out loud.
func TestArchive_ASessionWithoutAnArchiveStillWorks(t *testing.T) {
	s, srv := newTestSession(t)
	if s.Archiving() {
		t.Fatal("the plain fixture grew an archive")
	}
	joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=n1 :alice!alice@test PRIVMSG #eco :still works")
	srv.Sync()
	if texts := bufTexts(messageLines(s.Buffer("#eco"))); !containsText(texts, "still works") {
		t.Errorf("#eco = %q, want the message", texts)
	}
	// Scroll-up with no archive goes straight to the server.
	s.LoadOlder("#eco")
	srv.WaitFor("CHATHISTORY BEFORE #eco")
	// And the per-buffer privacy controls say honestly that there is nothing
	// to control.
	if s.Logging("#eco") {
		t.Error("a session with no archive reports that it is logging")
	}
}

// --- fixtures ----------------------------------------------------------------

// seedArchive writes n messages into a buffer's history, as a previous run of
// eco would have left them. The timestamps step by a second so the pager's
// (ts, rowid) ordering is exercised with distinct times, and every row carries
// a msgid so a page can be anchored on one.
func seedArchive(t *testing.T, path, buffer string, n int) {
	t.Helper()
	db, err := archive.Open(path, archive.Options{})
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer db.Close()
	rec, err := archive.NewRecorder(context.Background(), db, archive.RecorderOptions{})
	if err != nil {
		t.Fatalf("starting a recorder: %v", err)
	}
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		if !rec.Record(archive.Message{
			MsgID:  "seed" + itoa(i),
			Buffer: buffer,
			Time:   base.Add(time.Duration(i) * time.Second),
			Sender: "alice",
			Kind:   model.MsgPrivmsg,
			Body:   "seeded " + itoa(i),
		}) {
			t.Fatalf("the recorder refused seeded message %d", i)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("closing the recorder: %v", err)
	}
}

// pinExcludeDMs turns the "never save private messages" preference on or off
// for the rest of the test. It must run AFTER the fixture, whose own pin would
// otherwise unwind it.
func pinExcludeDMs(t *testing.T, on bool) {
	t.Helper()
	prev := config.Current()
	next := *prev
	next.ExcludeDMLogs = on
	config.Publish(&next)
	t.Cleanup(func() { config.Publish(prev) })
}

func containsText(texts []string, want string) bool {
	return indexOfText(texts, want) >= 0
}

func indexOfText(texts []string, want string) int {
	for i, s := range texts {
		if s == want {
			return i
		}
	}
	return -1
}

func tail(texts []string, n int) []string {
	if len(texts) <= n {
		return texts
	}
	return texts[len(texts)-n:]
}

func itoa(n int) string { return strconv.Itoa(n) }
