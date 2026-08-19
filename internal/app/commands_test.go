package app

import (
	"strings"
	"testing"

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
