package state

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// TestDisconnectKeepsTheUsersStateAndDropsTheConnectionsPins the split that
// makes a reconnect correct: buffers, scrollback and "channels I am in" belong
// to the user and survive; in-flight requests and the batch registry belonged
// to the connection and do not.
func TestDisconnectKeepsTheUsersStateAndDropsTheConnections(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.Mode{EventMeta: h.meta(), Target: "#eco", ModeString: "+k", Args: []string{"sekrit"}, Full: true})
	h.apply(model.PrivMsg{EventMeta: h.msg("last"), Target: "#eco", Sender: user("alice"), Text: "before the drop"})
	h.s.RequestPart(h.id("#other"), "")
	h.s.RequestJoin(h.n, "#pending")
	h.s.MarkHistoryRequested(ch)

	out := h.apply(model.ConnState{EventMeta: h.meta(), Status: model.ConnDisconnected, Err: "connection reset"})
	state := mustFind[model.ConnStateChanged](t, out)
	if state.Status != model.ConnDisconnected || state.Err != "connection reset" {
		t.Errorf("ConnStateChanged = %+v", state)
	}
	line := mustFind[model.MessageAdded](t, out)
	if line.Message.Text != "Disconnected from server: connection reset" {
		t.Errorf("disconnect line = %q", line.Message.Text)
	}

	if h.s.PendingPart(h.n, "#other") || h.s.PendingJoin(h.n, "#pending") {
		t.Error("an unanswered request survived the disconnect: a later 442 could close a live buffer")
	}
	if !h.s.NeedsHistory(ch) {
		t.Error("history was not re-armed for the reconnect")
	}
	if _, ok := h.s.BufferInfo(ch); !ok {
		t.Fatal("the channel buffer did not survive the disconnect")
	}
	if got := len(h.s.Messages(ch)); got == 0 {
		t.Error("scrollback did not survive the disconnect")
	}

	// the reconnect plan: rejoin with the key we learned, gap-fill from the
	// last msgid we actually hold
	rejoin := h.s.RejoinTargets(h.n)
	if len(rejoin) != 1 || rejoin[0].Channel != "#eco" || rejoin[0].Key != "sekrit" {
		t.Errorf("RejoinTargets = %+v, want #eco with its key", rejoin)
	}
	gaps := h.s.GapFillTargets(h.n)
	if len(gaps) != 1 || gaps[0].MsgID != "last" || gaps[0].Buffer != ch {
		t.Errorf("GapFillTargets = %+v, want #eco anchored at the last msgid", gaps)
	}

	// reconnecting replays the join; the msgid dedupe stops the refetch from
	// duplicating anything
	h.apply(model.ConnState{EventMeta: h.meta(), Status: model.ConnReady, Nick: testNick})
	h.apply(model.Join{EventMeta: h.meta(), Channel: "#eco", Sender: selfUser()})
	out = h.apply(model.PrivMsg{EventMeta: h.msg("last"), Target: "#eco", Sender: user("alice"), Text: "before the drop"})
	if len(out) != 0 {
		t.Errorf("the gap-fill duplicated a message we already had: %v", changeNames(out))
	}
}

// TestConnStateReportsSelf pins that the nick the server actually gave us
// becomes our identity (433 renames are visible), and that a repeat Ready does
// not re-announce.
func TestConnStateReportsSelf(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.ConnState{EventMeta: h.meta(), Status: model.ConnReady, Nick: "eco1"})
	if self := mustFind[model.SelfChanged](t, out); self.Nick != "eco1" {
		t.Errorf("SelfChanged = %+v, want the server's nick", self)
	}
	if nick, _, _ := h.s.Self(h.n); nick != "eco1" {
		t.Errorf("Self nick = %q", nick)
	}
	if _, ok := findChange[model.MessageAdded](out); ok {
		t.Error("a repeated Ready re-announced the connection")
	}
}

// TestJoinRefusalOnlyPromptsWhenAsked pins the refusal UX: a join the user just
// asked for may interrupt them; an autojoin or reconnect refusal stays in the
// log, or reconnecting to a network with three banned channels opens three
// dialogs.
func TestJoinRefusalOnlyPromptsWhenAsked(t *testing.T) {
	h := newHarness(t)

	out := h.apply(model.JoinRefused{
		EventMeta: h.meta(), Channel: "#locked", Code: "475",
		Reason: "Cannot join channel (+k)", NeedKey: true,
	})
	if _, ok := findChange[model.JoinFailed](out); ok {
		t.Errorf("an unrequested refusal prompted: %v", changeNames(out))
	}
	line := mustFind[model.MessageAdded](t, out)
	if line.Message.Text != "Cannot join #locked: Cannot join channel (+k)" {
		t.Errorf("refusal line = %q", line.Message.Text)
	}

	h.s.RequestJoin(h.n, "#locked")
	out = h.apply(model.JoinRefused{
		EventMeta: h.meta(), Channel: "#locked", Code: "475",
		Reason: "Cannot join channel (+k)", NeedKey: true,
	})
	failed := mustFind[model.JoinFailed](t, out)
	if failed.Channel != "#locked" || failed.Code != "475" || !failed.NeedKey {
		t.Errorf("JoinFailed = %+v", failed)
	}
	if h.s.PendingJoin(h.n, "#locked") {
		t.Error("the refusal did not clear the pending join")
	}
}

// TestPartRefusalClosesTheBuffer pins the 442/403 path. The buffer normally
// closes on the PART echo, but a server that thinks we are not on the channel
// answers a numeric instead, and v1 left the buffer open forever.
func TestPartRefusalClosesTheBuffer(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#stale")

	// a 442 about a channel nobody asked to leave must not close anything
	out := h.apply(model.Numeric{EventMeta: h.meta(), Code: "442", Params: []string{"#stale"}, Text: "You're not on that channel"})
	if _, ok := findChange[model.BufferClosed](out); ok {
		t.Fatalf("an unrelated 442 closed a live buffer: %v", changeNames(out))
	}

	h.s.RequestPart(ch, "")
	out = h.apply(model.Numeric{EventMeta: h.meta(), Code: "442", Params: []string{"#stale"}, Text: "You're not on that channel"})
	if _, ok := findChange[model.BufferClosed](out); !ok {
		t.Fatalf("a refused PART did not close the buffer: %v", changeNames(out))
	}
	if _, ok := h.s.BufferInfo(ch); ok {
		t.Error("the buffer survived the refusal")
	}

	// with labeled-response the label is authoritative, so a 442 answering some
	// other command cannot close a buffer at all
	other := h.joinChannel("#live")
	h.s.RequestPart(other, "part-1")
	out = h.apply(model.Numeric{
		EventMeta: model.EventMeta{Network: h.n, Label: "topic-9"},
		Code:      "442", Params: []string{"#live"}, Text: "You're not on that channel",
	})
	if _, ok := findChange[model.BufferClosed](out); ok {
		t.Errorf("a numeric answering another command closed the buffer: %v", changeNames(out))
	}
	out = h.apply(model.Numeric{
		EventMeta: model.EventMeta{Network: h.n, Label: "part-1"},
		Code:      "442", Params: []string{"#live"}, Text: "You're not on that channel",
	})
	if _, ok := findChange[model.BufferClosed](out); !ok {
		t.Errorf("the labeled refusal did not close the buffer: %v", changeNames(out))
	}
}

// TestWhoisRoutesPerRequest inverts the v1 characterization test: v1 kept one
// global whoisTarget that was never cleared, so replies landed in whichever
// buffer last ran the command. Correlation is per nick and clears on 318.
func TestWhoisRoutesPerRequest(t *testing.T) {
	h := newHarness(t)
	one := h.joinChannel("#one")
	two := h.joinChannel("#two")

	h.s.TrackWhois(h.n, "alice", one)
	h.s.TrackWhois(h.n, "bob", two)

	out := h.apply(model.Numeric{
		EventMeta: h.meta(), Code: "311",
		Params: []string{"alice", "alice", "host.example", "*"}, Text: "Alice Example",
	})
	line := mustFind[model.MessageAdded](t, out)
	if line.Meta().Buffer != one {
		t.Errorf("alice's whois landed in %v, want %v", line.Meta().Buffer, one)
	}
	if line.Message.Text != "alice is alice@host.example (Alice Example)" {
		t.Errorf("311 line = %q", line.Message.Text)
	}

	out = h.apply(model.Numeric{EventMeta: h.meta(), Code: "330", Params: []string{"bob", "bobaccount"}, Text: "is logged in as"})
	if line := mustFind[model.MessageAdded](t, out); line.Meta().Buffer != two {
		t.Errorf("bob's whois landed in %v, want %v", line.Meta().Buffer, two)
	}

	// 318 ends the correlation; later replies fall back to the network log
	h.apply(model.Numeric{EventMeta: h.meta(), Code: "318", Params: []string{"alice"}, Text: "End of /WHOIS list"})
	out = h.apply(model.Numeric{EventMeta: h.meta(), Code: "313", Params: []string{"alice"}, Text: "is an IRC operator"})
	if line := mustFind[model.MessageAdded](t, out); line.Meta().Buffer != h.server() {
		t.Errorf("a whois reply after 318 landed in %v, want the network log", line.Meta().Buffer)
	}
}

// TestNumericFallback pins that no numeric is silently lost, and that error
// numerics are marked as errors (v1's raw-callback rule).
func TestNumericFallback(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.Numeric{EventMeta: h.meta(), Code: "381", Text: "You are now an IRC operator"})
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "You are now an IRC operator" {
		t.Errorf("381 line = %q", line.Message.Text)
	}
	out = h.apply(model.Numeric{EventMeta: h.meta(), Code: "421", Params: []string{"NOPE"}, Text: "Unknown command"})
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "Error: NOPE Unknown command" {
		t.Errorf("421 line = %q", line.Message.Text)
	}
}

// TestListStreamsAndCaches pins the LIST path: rows stream out in chunks as
// they arrive, RPL_LISTEND closes the scan, and the cache is sorted by
// population for the quick switcher.
func TestListStreamsAndCaches(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < listChunk; i++ {
		h.apply(model.ListItem{EventMeta: h.meta(), Channel: "#c" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Users: i})
	}
	if _, ok := findChange[model.ListUpdated](h.changes); !ok {
		t.Fatalf("a full chunk was not reported: %v", changeNames(h.changes))
	}
	h.apply(model.ListItem{EventMeta: h.meta(), Channel: "#busy", Users: 900})
	out := h.apply(model.ListEnd{EventMeta: h.meta()})
	done := mustFind[model.ListUpdated](t, out)
	if !done.Done {
		t.Error("ListEnd did not close the scan")
	}
	if len(done.Items) != 1 || done.Items[0].Name != "#busy" {
		t.Errorf("final chunk = %+v, want the trailing row only", done.Items)
	}
	cached := h.s.ChannelList(h.n)
	if len(cached) != listChunk+1 {
		t.Fatalf("cache holds %d rows, want %d", len(cached), listChunk+1)
	}
	if cached[0].Name != "#busy" {
		t.Errorf("cache is not sorted by population: %+v", cached[0])
	}
	// a new scan replaces the cache rather than appending to it
	h.apply(model.ListItem{EventMeta: h.meta(), Channel: "#fresh", Users: 1})
	if got := h.s.ChannelList(h.n); len(got) != 1 || got[0].Name != "#fresh" {
		t.Errorf("a new scan did not reset the cache: %+v", got)
	}
}

// TestAuthResultIsVisible pins that a SASL failure is a first-class change, not
// a log line nobody reads (v1's SASLOptional silently degraded).
func TestAuthResultIsVisible(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.AuthResult{
		EventMeta: h.meta(), Success: false, Mechanism: "PLAIN", Message: "Invalid credentials",
	})
	auth := mustFind[model.AuthStateChanged](t, out)
	if auth.Success || auth.Mechanism != "PLAIN" {
		t.Errorf("AuthStateChanged = %+v", auth)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "Authentication failed: Invalid credentials" {
		t.Errorf("auth line = %q", line.Message.Text)
	}

	out = h.apply(model.AuthResult{EventMeta: h.meta(), Success: true, Mechanism: "PLAIN", Account: "eco"})
	if self := mustFind[model.SelfChanged](t, out); self.Account != "eco" {
		t.Errorf("SelfChanged = %+v, want the account", self)
	}
	if _, account, _ := h.s.Self(h.n); account != "eco" {
		t.Errorf("Self account = %q", account)
	}
}

// TestCapsAreTracked pins that the store owns the capability set, since it is
// what answers "is multiline on".
func TestCapsAreTracked(t *testing.T) {
	h := newHarness(t)
	h.apply(model.CapChange{EventMeta: h.meta(), Op: model.CapLS, Caps: []model.Cap{
		{Name: "draft/multiline", Value: "max-bytes=4096,max-lines=8"},
		{Name: "server-time"},
	}})
	h.apply(model.CapChange{EventMeta: h.meta(), Op: model.CapACK, Caps: []model.Cap{
		{Name: "draft/multiline"}, {Name: "server-time"},
	}})
	if !h.s.HasCap(h.n, "server-time") {
		t.Error("an acked cap is not enabled")
	}
	if v, ok := h.s.CapValue(h.n, "draft/multiline"); !ok || v != "max-bytes=4096,max-lines=8" {
		t.Errorf("multiline value = %q ok=%v", v, ok)
	}
	h.apply(model.CapChange{EventMeta: h.meta(), Op: model.CapDEL, Caps: []model.Cap{{Name: "server-time"}}})
	if h.s.HasCap(h.n, "server-time") {
		t.Error("a deleted cap is still enabled")
	}
}

// TestCasemappingChangeRefoldsKeys pins the awkward case: a server that
// advertises a different CASEMAPPING after buffers already exist. Every folded
// key is rebuilt, and the UI is told the ids moved.
func TestCasemappingChangeRefoldsKeys(t *testing.T) {
	h := newHarness(t)
	old := h.joinChannel("#Test[Room]", "Alice[]")
	if old.Folded != "#test{room}" {
		t.Fatalf("rfc1459 fold = %q, want #test{room}", old.Folded)
	}
	out := h.apply(model.ISupport{EventMeta: h.meta(), Tokens: []string{"CASEMAPPING=ascii"}})
	renamed := mustFind[model.BufferRenamed](t, out)
	if renamed.From != old {
		t.Errorf("BufferRenamed.From = %v, want %v", renamed.From, old)
	}
	newID := h.id("#Test[Room]")
	if newID.Folded != "#test[room]" {
		t.Fatalf("ascii fold = %q", newID.Folded)
	}
	if _, ok := h.s.BufferInfo(newID); !ok {
		t.Fatal("the buffer did not move to its new id")
	}
	// members refolded too, so a message from Alice[] still finds her
	found := false
	for _, m := range h.s.Members(newID) {
		if m.Folded == "alice[]" {
			found = true
		}
	}
	if !found {
		t.Errorf("members were not refolded: %v", memberNicks(h.s, newID))
	}
}

// TestServerErrorAndStatusLines pins that the network log gets the two things
// v1 dropped on the floor: ERROR, and the state of the connection.
func TestServerErrorAndStatusLines(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.ServerError{EventMeta: h.meta(), Message: "Closing link: throttled"})
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "Error: Closing link: throttled" {
		t.Errorf("ERROR line = %q", line.Message.Text)
	}
	if got := h.s.Status(h.n); got != model.ConnReady {
		t.Errorf("status = %v, want ready", got)
	}
}

// TestGapFillSkipsEmptyBuffers pins that a buffer with nothing in it has no
// anchor: LATEST covers it, and inventing an anchor would refetch the world.
func TestGapFillSkipsEmptyBuffers(t *testing.T) {
	h := newHarness(t)
	h.s.OpenBuffer(h.n, "#empty")
	if got := h.s.GapFillTargets(h.n); len(got) != 0 {
		t.Errorf("GapFillTargets = %+v, want nothing for an empty buffer", got)
	}
	id, _ := h.s.OpenBuffer(h.n, "bob")
	h.apply(model.PrivMsg{EventMeta: h.msg("q1"), Target: testNick, Sender: user("bob"), Text: "hi"})
	got := h.s.GapFillTargets(h.n)
	if len(got) != 1 || got[0].Buffer != id || got[0].MsgID != "q1" {
		t.Errorf("GapFillTargets = %+v, want the query anchored at q1", got)
	}
	if got[0].At.IsZero() {
		t.Error("gap-fill anchor has no timestamp fallback")
	}
}
