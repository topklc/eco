package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"

	"github.com/toprakkilic/eco/internal/command"
)

// The command table's own behaviour is tested headless in internal/command,
// against a fake env. These are the tests of the WIRING: that the app's env
// really is hooked up to a session and a connection, that a hint really lands
// in the buffer the command was typed in, and that the two lists v1 kept by
// hand — Tab completion and the help page — now come from the table.

// statusText returns the client's own lines in a buffer, oldest first. It reads
// the projection under the session's lock: the pump goroutine writes to it and
// this suite runs under -race.
func statusText(s *Session, buffer string) []string {
	b := s.Buffer(buffer)
	if b == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range b.Lines {
		if l.Status {
			out = append(out, l.Text)
		}
	}
	return out
}

// lastStatus is the newest of those, or "".
func lastStatus(s *Session, buffer string) string {
	all := statusText(s, buffer)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// TestCommand_JoinRunsThroughTheTable is the wiring in one line: text typed
// into the composer reaches internal/command, which reaches the session, which
// reaches the wire.
func TestCommand_JoinRunsThroughTheTable(t *testing.T) {
	s, srv := newTestSession(t)
	testCommandUI(s, serverBuffer).handleInput("/join eco")
	srv.WaitFor("JOIN #eco") // and the "#" the table adds is on the wire
}

// TestCommand_UnknownIsInterceptedNotSent is M10's visible change. v1 wrote the
// typo to the server; now the buffer gets a sentence naming the command the
// user meant and the way to send it on purpose, and the wire stays quiet.
func TestCommand_UnknownIsInterceptedNotSent(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")

	testCommandUI(s, "#eco").handleInput("/jion #eco")
	srv.Sync()

	if got := sentMatching(srv, "JION*"); len(got) != 0 {
		t.Errorf("client sent %q: an unknown command must not reach the server", got)
	}
	hint := lastStatus(s, "#eco")
	for _, want := range []string{"Unknown command /jion", "/join", "/raw JION #eco"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the intercept said %q, which does not contain %q", hint, want)
		}
	}

	// ...and taking the offer sends exactly what v1 would have sent
	testCommandUI(s, "#eco").handleInput("/raw JION #eco")
	srv.WaitFor("JION #eco")
}

// TestCommand_UsageLandsInTheBufferItWasTypedIn: a hint is a line in the
// conversation, not a dialog, and it goes where the user was looking.
func TestCommand_UsageLandsInTheBufferItWasTypedIn(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")
	s.getBuffer("alice")

	testCommandUI(s, "alice").handleInput("/msg")
	srv.Sync()

	if got := lastStatus(s, "alice"); got != "Usage: /msg nick [message]" {
		t.Errorf("usage line in the conversation = %q, want the generated usage", got)
	}
	for _, l := range statusText(s, "#eco") {
		if strings.HasPrefix(l, "Usage: /msg") {
			t.Error("the usage line landed in a buffer nobody typed it in")
		}
	}
}

// TestCommand_HistoryAsksTheServerThroughTheIRCLayer closes the hole the M9
// entry named: /history was the last wire write commands.go made by hand.
func TestCommand_HistoryAsksTheServerThroughTheIRCLayer(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco") // consumes the cold-start CHATHISTORY

	testCommandUI(s, "#eco").handleInput("/history")
	srv.WaitFor("CHATHISTORY LATEST #eco * 200")
}

// TestCommand_HistoryRefusesTheNetworkLog keeps the guard the audit added.
func TestCommand_HistoryRefusesTheNetworkLog(t *testing.T) {
	s, srv := newTestSession(t)
	testCommandUI(s, serverBuffer).handleInput("/history")
	srv.Sync()

	if got := sentMatching(srv, "CHATHISTORY*"); len(got) != 0 {
		t.Errorf("client sent %q for the network log, want nothing", got)
	}
	if got := lastStatus(s, serverBuffer); !strings.Contains(got, "not the server log") {
		t.Errorf("network log says %q, want the refusal", got)
	}
}

// TestCommand_ChannelOnlyCommandsRefuseAConversation: the table's NeedsChannel
// guard, reaching the screen.
func TestCommand_ChannelOnlyCommandsRefuseAConversation(t *testing.T) {
	s, srv := newTestSession(t)
	s.getBuffer("alice")

	testCommandUI(s, "alice").handleInput("/kick bob")
	srv.Sync()

	if got := sentMatching(srv, "KICK*"); len(got) != 0 {
		t.Errorf("client sent %q from a conversation, want nothing", got)
	}
	if got := lastStatus(s, "alice"); got != "/kick only works in a channel." {
		t.Errorf("refusal = %q, want the channel-only sentence", got)
	}
}

// TestCommand_ServicesShortcutsAreNotEchoed: /ns is a message to a bot, not
// something typed into a buffer, so it goes out without a local copy.
func TestCommand_ServicesShortcutsAreNotEchoed(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")
	before := lineCount(s, "#eco")

	testCommandUI(s, "#eco").handleInput("/ns info eco")
	srv.WaitFor("PRIVMSG NickServ :info eco")
	srv.Sync()

	if got := lineCount(s, "#eco"); got != before {
		t.Errorf("#eco holds %d lines, want %d: a services command must not echo", got, before)
	}
}

// TestCommand_WithoutAConnectionDoesNothing pins the guard v1 had at the top of
// its switch, through the real env: a session that has never connected has no
// wire, and every command that needs one is silently dropped.
func TestCommand_WithoutAConnectionDoesNothing(t *testing.T) {
	pinTestSettings(t)
	_, d := newTestSink(t)
	s := NewSession(testConfig(), d) // never connected: no wire
	stopSession(t, s)
	u := testCommandUI(s, serverBuffer)
	before := len(statusText(s, serverBuffer))

	for _, text := range []string{"/join #eco", "/nick ada", "/quit"} {
		u.handleInput(text)
	}
	if got := len(statusText(s, serverBuffer)); got != before {
		t.Errorf("commands with no connection wrote %d lines, want none",
			got-before)
	}
	if s.wireClient() != nil {
		t.Error("a session that never connected reports a wire")
	}
}

// TestCompletionComesFromTheTable is the second source of truth, deleted: Tab
// completion used to walk a hand-written slice in entry.go.
func TestCompletionComesFromTheTable(t *testing.T) {
	for _, verb := range command.Verbs() {
		got := slashMatches(verb)
		if len(got) == 0 || got[0] != verb {
			t.Errorf("completing %q returned %q, want it to include itself", verb, got)
		}
	}
	if got := slashMatches("/j"); len(got) != 2 || got[0] != "/j" || got[1] != "/join" {
		t.Errorf("completing /j = %q, want [/j /join]", got)
	}
	if got := slashMatches("/zzz"); len(got) != 0 {
		t.Errorf("completing /zzz = %q, want nothing", got)
	}
	// the hidden diagnostic stays out of the way
	for _, v := range slashMatches("/test") {
		if v == "/testnotify" {
			t.Error("/testnotify is completable; it is meant to be hidden")
		}
	}
}

// TestCommandsPageIsGenerated: the documentation window's command section is
// the table, rendered. v1 kept a second copy of the list in the markdown, and
// it had already drifted.
func TestCommandsPageIsGenerated(t *testing.T) {
	doc := commandsDoc()
	if strings.Contains(doc, commandsMarker) {
		t.Fatal("the generated command section was not spliced into the page")
	}
	for _, c := range command.All() {
		if c.Hidden {
			continue
		}
		if !strings.Contains(doc, "`/"+c.Name) {
			t.Errorf("/%s is missing from the documentation page", c.Name)
		}
	}
	if !strings.Contains(doc, "Keyboard shortcuts") {
		t.Error("splicing the commands in ate the rest of the page")
	}
}

// --- the two derivations, proved rather than sampled -------------------------
//
// The two tests above ask whether today's completion list and today's page
// agree with today's table, which a frozen hand-written copy of either would
// also satisfy — that is exactly the failure mode M10 set out to delete, so
// "they agree right now" is not the property worth pinning. The two below pin
// the derivation itself: completion is the table's list filtered, and the page
// is the table's markdown spliced. internal/command's own
// TestAddingACommandChangesEverythingDerived supplies the other half — that a
// new entry really does change Verbs() and HelpMarkdown() — so between them, a
// command added to the table cannot fail to be completable and documented.

// TestATableEntryThisPackageHasNeverSeenIsCompletableAndDocumented is the drift
// gate itself, and the only form of it a copy cannot pass: a command is added
// to the table at runtime, and Tab completion and the documentation page have
// to show it without this package being told anything. A hand-written list here
// — however faithful on the day it was written, which is how v1's two lists
// began — cannot answer for a verb it has never heard of.
func TestATableEntryThisPackageHasNeverSeenIsCompletableAndDocumented(t *testing.T) {
	before := slashMatches("/zz")
	if len(before) != 0 {
		t.Fatalf("completing /zz already offers %q; pick another name for this test", before)
	}

	remove := command.AddForTest(&command.Command{
		Name: "zzdrift", Aliases: []string{"zzd"}, Group: command.GroupChat,
		Help: "prove the UI's lists are the table's",
		Args: command.ArgSpec{Usage: "argument", Min: 1},
		Run:  func(in command.Input, e command.Env) {},
	})
	defer remove()

	if got := slashMatches("/zz"); !reflect.DeepEqual(got, []string{"/zzd", "/zzdrift"}) {
		t.Errorf("completing /zz = %q, want the new command and its alias: "+
			"Tab completion is not reading the table", got)
	}
	doc := commandsDoc()
	for _, want := range []string{"`/zzdrift argument`", "prove the UI's lists are the table's", "`/zzd`"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the documentation page does not contain %q: "+
				"the Commands section is not the table", want)
		}
	}

	remove()
	if got := slashMatches("/zz"); len(got) != 0 {
		t.Errorf("after the entry left the table, completing /zz still offers %q", got)
	}
	if strings.Contains(commandsDoc(), "zzdrift") {
		t.Error("after the entry left the table, the page still documents it")
	}
}

// TestTabCompletesASlashCommandFromTheTable drives the actual key. Every other
// completion test here checks slashMatches, which is the helper; this is its
// only caller, and a composer that stopped calling it would leave all of them
// green while Tab did nothing.
func TestTabCompletesASlashCommandFromTheTable(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	u := testCommandUI(s, "#eco")
	u.app = fynetest.NewTempApp(t)
	e := newChatEntry(u)

	remove := command.AddForTest(&command.Command{
		Name: "zzdrift", Group: command.GroupChat, Help: "prove Tab reads the table",
		Run: func(in command.Input, e command.Env) {},
	})
	defer remove()

	// a verb the composer has never heard of, added a moment ago
	e.SetText("/zzdri")
	e.CursorRow, e.CursorColumn = 0, len("/zzdri")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if got := e.Text; got != "/zzdrift " {
		t.Errorf("Tab on %q gave %q, want %q: the composer is not completing from the table",
			"/zzdri", got, "/zzdrift ")
	}

	// ...and an ordinary one still cycles through the table's alternatives
	e.completions = nil
	e.SetText("/wh")
	e.CursorRow, e.CursorColumn = 0, len("/wh")
	var cycled []string
	for i := 0; i < 3; i++ {
		e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
		cycled = append(cycled, strings.TrimSpace(e.Text))
	}
	want := command.Complete("/wh")
	if !reflect.DeepEqual(cycled, want) {
		t.Errorf("Tab cycled through %q, want the table's %q", cycled, want)
	}

	// a nickname is still a nickname: Tab off the start of the line is not a verb
	e.completions = nil
	e.SetText("hello ali")
	e.CursorRow, e.CursorColumn = 0, len("hello ali")
	e.TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if got := e.Text; got != "hello alice " {
		t.Errorf("Tab on a nickname gave %q, want %q", got, "hello alice ")
	}
}

// TestCompletionIsTheTablesOwnAnswer: slashMatches is command.Complete and
// nothing else. Checked over every prefix of every verb, plus the ones that
// match nothing, so a reimplementation in this package cannot satisfy it by
// agreeing on the easy cases.
func TestCompletionIsTheTablesOwnAnswer(t *testing.T) {
	verbs := command.Verbs()

	check := func(prefix string) {
		t.Helper()
		if got, want := slashMatches(prefix), command.Complete(prefix); !reflect.DeepEqual(got, want) {
			t.Errorf("slashMatches(%q) = %q, want %q: the completion list is not the table's",
				prefix, got, want)
		}
	}

	// "/" asks for the whole list: this alone is the completion list against
	// the table's, element for element and in order.
	check("/")
	seen := map[string]bool{}
	for _, v := range verbs {
		for i := 1; i <= len(v); i++ {
			if p := v[:i]; !seen[p] {
				seen[p] = true
				check(p)
			}
		}
	}
	for _, p := range []string{"/zzz", "/j0", "//", ""} {
		check(p)
	}
}

// TestCommandsPageEmbedsTheGeneratedSectionVerbatim: the page is the hand-
// written prose with command.HelpMarkdown() dropped into it, byte for byte. A
// second renderer in this package — however faithful today — fails here.
func TestCommandsPageEmbedsTheGeneratedSectionVerbatim(t *testing.T) {
	md := command.HelpMarkdown()
	if md == "" {
		t.Fatal("the table generated no markdown at all")
	}
	doc := commandsDoc()
	if !strings.Contains(doc, md) {
		t.Error("the documentation page does not contain command.HelpMarkdown() verbatim: " +
			"the Commands section is a second rendering of the table, which is what can drift")
	}
	// and the splice is the marker's doing, not a coincidence of the prose
	if want := strings.Replace(docCommands, commandsMarker, md, 1); doc != want {
		t.Error("commandsDoc() is not docCommands with the generated section at the marker")
	}
}

// TestCommand_JoinPassesTheKeyToTheWire: /join #chan key is how a beginner gets
// into a +k channel, and the key survives the table, the env adapter and the
// session. The headless test proves the table hands the key over; this proves
// what the app does with it, which is where dropping it would be invisible.
func TestCommand_JoinPassesTheKeyToTheWire(t *testing.T) {
	s, srv := newTestSession(t)
	testCommandUI(s, serverBuffer).handleInput("/join eco sekrit")
	srv.WaitFor("JOIN #eco sekrit")
}

// TestCommand_MsgOpensAndShowsTheConversation. v1's /msg switched the view to
// the conversation it opened; a /msg that only created the buffer would leave
// the user typing into the channel they came from.
func TestCommand_MsgOpensAndShowsTheConversation(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")
	u := testCommandUI(s, "#eco")

	u.handleInput("/msg bob hello there")
	srv.WaitFor("PRIVMSG bob :hello there")

	if s.Buffer("bob") == nil {
		t.Fatal("/msg did not open the conversation")
	}
	if got := u.current(); got != "bob" {
		t.Errorf("after /msg the buffer on screen is %q, want bob", got)
	}
}

// TestCommand_HintStaysInTheBufferItWasTypedIn. cmdEnv captures the buffer with
// the session precisely so a command that moves the view cannot land its own
// sentence somewhere the user is no longer looking. Nothing in the table does
// both today, so the property is pinned here at the seam that promises it.
func TestCommand_HintStaysInTheBufferItWasTypedIn(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")
	s.getBuffer("bob")
	u := testCommandUI(s, "#eco")

	e := &cmdEnv{u: u, s: s, buffer: "#eco"}
	u.selectBuffer("bob") // the view moves after the command was parsed
	if got := u.current(); got != "bob" {
		t.Fatalf("the view did not move: current is %q", got)
	}
	e.ShowHint("Usage: /msg nick [message]")

	if got := lastStatus(s, "#eco"); got != "Usage: /msg nick [message]" {
		t.Errorf("#eco's last line is %q, want the hint", got)
	}
	for _, l := range statusText(s, "bob") {
		if strings.HasPrefix(l, "Usage:") {
			t.Errorf("the hint landed in %q, which is not where it was typed", "bob")
		}
	}
}

// TestCommand_IgnoreAndUnignoreReachThePreferenceList. /ignore is new in M10
// and its whole effect is on a preference-backed set, so the wiring is the
// command: an adapter that passed the flag through inverted would ignore on
// /unignore and nothing on the wire would look wrong.
func TestCommand_IgnoreAndUnignoreReachThePreferenceList(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")
	u := testCommandUI(s, "#eco")
	u.app = fynetest.NewTempApp(t)
	restoreIgnores(t)

	u.handleInput("/ignore Bob")
	if !isIgnored("bob") {
		t.Error("/ignore Bob did not ignore bob")
	}
	if got := lastStatus(s, "#eco"); got != "Ignoring Bob. Use /unignore Bob to stop." {
		t.Errorf("/ignore said %q", got)
	}

	u.handleInput("/ignore")
	if got := lastStatus(s, "#eco"); !strings.Contains(got, "Ignoring bob") {
		t.Errorf("/ignore with no argument said %q, want the list", got)
	}

	u.handleInput("/unignore Bob")
	if isIgnored("bob") {
		t.Error("/unignore Bob left bob ignored")
	}
	if got := lastStatus(s, "#eco"); got != "No longer ignoring Bob." {
		t.Errorf("/unignore said %q", got)
	}

	u.handleInput("/ignore")
	if got := lastStatus(s, "#eco"); got != "You are not ignoring anyone." {
		t.Errorf("/ignore with an empty list said %q", got)
	}
}

// restoreIgnores puts the process-wide ignore set back after a test, which
// would otherwise leak into every later one.
func restoreIgnores(t *testing.T) {
	t.Helper()
	ignoreMu.Lock()
	prev := make(map[string]bool, len(ignored))
	for k, v := range ignored {
		prev[k] = v
	}
	ignoreMu.Unlock()
	t.Cleanup(func() {
		ignoreMu.Lock()
		defer ignoreMu.Unlock()
		ignored = prev
	})
}

// TestCommand_QuitFlushesBeforeClosingTheWindow is the audit-era guard M10
// moved into the UI hook: QUIT has just been written, and closing the process
// on the same tick shuts the socket before the line leaves, so the user times
// out instead of leaving. The wait is the behaviour, so this watches when the
// window is actually closed rather than that it eventually is.
func TestCommand_QuitFlushesBeforeClosingTheWindow(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")

	// The number matters, not just the shape: a wait of zero is a wait, and it
	// closes the socket on the same tick the QUIT was written.
	if quitFlush < 300*time.Millisecond {
		t.Errorf("quitFlush is %v; v1 gave the QUIT 300ms to reach the server", quitFlush)
	}

	closed := make(chan time.Duration, 1)
	prev := closeApp
	closeApp = func(fyne.App) { closed <- 0 }
	t.Cleanup(func() { closeApp = prev })

	u := testCommandUI(s, "#eco")
	u.app = fynetest.NewTempApp(t)

	start := time.Now()
	u.handleInput("/quit see you")
	srv.WaitFor("QUIT :see you") // the parting message goes out first

	select {
	case <-closed:
		if waited := time.Since(start); waited < quitFlush {
			t.Errorf("the window closed after %v, before the %v flush: the QUIT would be cut off",
				waited, quitFlush)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window never closed after /quit")
	}
}

// TestCommand_QuitWithNoWindowIsSafe: the same hook on a UI that has no app —
// which is every headless test in this package — parts the networks and returns
// rather than dereferencing nothing.
func TestCommand_QuitWithNoWindowIsSafe(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco")

	prev := closeApp
	closeApp = func(fyne.App) { t.Error("a window that does not exist was closed") }
	t.Cleanup(func() { closeApp = prev })

	testCommandUI(s, "#eco").handleInput("/quit bye")
	srv.WaitFor("QUIT :bye")
}
