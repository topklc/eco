package state

import (
	"slices"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// memberNicks returns a buffer's roster in rank order.
func memberNicks(s *Store, id model.BufferID) []string {
	ms := s.Members(id)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Prefixes+m.Nick)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSelfJoinOpensTheBuffer pins the join path: the buffer exists before
// anything refers to it, we are in the roster, the rejoin plan knows about it,
// and history is still wanted.
func TestSelfJoinOpensTheBuffer(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.Join{EventMeta: h.meta(), Channel: "#eco", Sender: selfUser()})

	if got := changeNames(out); len(got) < 3 || got[0] != "buffercreated" {
		t.Fatalf("changes = %v, want BufferCreated first", got)
	}
	created := mustFind[model.BufferCreated](t, out)
	if created.Info.Kind != model.BufferChannel || created.Info.Name != "#eco" {
		t.Errorf("BufferCreated = %+v, want a channel named #eco", created.Info)
	}
	if created.Restored {
		t.Error("a live join was reported as restored")
	}
	members := mustFind[model.MembersChanged](t, out)
	if len(members.Added) != 1 || members.Added[0].Nick != testNick {
		t.Errorf("MembersChanged = %+v, want ourselves added", members)
	}
	status := mustFind[model.MessageAdded](t, out)
	if status.Message.Text != "You joined #eco" || status.Message.Kind != model.MsgStatus {
		t.Errorf("status line = %q (%v)", status.Message.Text, status.Message.Kind)
	}
	ch := h.id("#eco")
	if got := h.s.RejoinTargets(h.n); len(got) != 1 || got[0].Channel != "#eco" {
		t.Errorf("RejoinTargets = %+v, want #eco", got)
	}
	if !h.s.NeedsHistory(ch) {
		t.Error("a freshly joined channel does not want history")
	}
}

// TestPlaybackJoinsTouchNothing pins the JOIN-storm fix. A chathistory replay
// of a busy channel used to rebuild the member list and request history per
// line; replayed membership is display-only.
func TestPlaybackJoinsTouchNothing(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	before := memberNicks(h.s, ch)

	out := h.apply(model.Join{EventMeta: h.playback(), Channel: "#eco", Sender: user("ghost")})
	if _, ok := findChange[model.MembersChanged](out); ok {
		t.Errorf("a playback JOIN changed the roster: %v", changeNames(out))
	}
	status := mustFind[model.MessageAdded](t, out)
	if status.Message.Text != "ghost joined" || !status.Message.Playback {
		t.Errorf("playback join line = %+v", status.Message)
	}
	if got := memberNicks(h.s, ch); !equalStrings(got, before) {
		t.Errorf("roster = %v, want it unchanged at %v", got, before)
	}

	// our own replayed join must not even leave a line
	if out := h.apply(model.Join{EventMeta: h.playback(), Channel: "#eco", Sender: selfUser()}); len(out) != 0 {
		t.Errorf("our own replayed join emitted %v", changeNames(out))
	}
	// a replayed PART likewise leaves the roster alone
	out = h.apply(model.Part{EventMeta: h.playback(), Channel: "#eco", Sender: user("alice")})
	if _, ok := findChange[model.MembersChanged](out); ok {
		t.Errorf("a playback PART changed the roster: %v", changeNames(out))
	}
	if got := memberNicks(h.s, ch); !equalStrings(got, before) {
		t.Errorf("roster = %v, want it unchanged at %v", got, before)
	}
}

// TestReplayedStatusLinesDedupe pins that a status line carries its event's
// msgid: with draft/event-playback the same JOIN comes back on every reconnect,
// and v1 drew it again every time.
func TestReplayedStatusLinesDedupe(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	replay := withMsgID(h.playback(), "j1")

	first := h.apply(model.Join{EventMeta: replay, Channel: "#eco", Sender: user("alice")})
	if countChanges[model.MessageAdded](first) != 1 {
		t.Fatalf("replayed join = %v", changeNames(first))
	}
	again := h.apply(model.Join{EventMeta: replay, Channel: "#eco", Sender: user("alice")})
	if len(again) != 0 {
		t.Errorf("the same replayed join was drawn twice: %v", changeNames(again))
	}
	if got := len(h.s.Messages(ch)); got != 2 { // "You joined" plus the one replay
		t.Errorf("buffer holds %d messages, want 2", got)
	}
}

// TestPartClosesOurBuffer pins the leave paths: our own PART closes the buffer
// and drops it from the rejoin plan; someone else's removes them from the
// roster and leaves a line.
func TestPartClosesOurBuffer(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	out := h.apply(model.Part{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice"), Reason: "bye"})
	members := mustFind[model.MembersChanged](t, out)
	if len(members.Removed) != 1 || members.Removed[0].Nick != "alice" {
		t.Errorf("MembersChanged = %+v, want alice removed", members)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "alice left" {
		t.Errorf("part line = %q", line.Message.Text)
	}

	h.s.RequestPart(ch, "")
	out = h.apply(model.Part{EventMeta: h.meta(), Channel: "#eco", Sender: selfUser()})
	if _, ok := findChange[model.BufferClosed](out); !ok {
		t.Fatalf("our own PART did not close the buffer: %v", changeNames(out))
	}
	if _, ok := h.s.BufferInfo(ch); ok {
		t.Error("the buffer survived our own PART")
	}
	if got := h.s.RejoinTargets(h.n); len(got) != 0 {
		t.Errorf("RejoinTargets = %+v, want nothing after we left", got)
	}
}

// TestCycleKeepsTheBuffer pins /cycle: the PART echo empties the roster but
// keeps the buffer, its scrollback and its place in the rail.
func TestCycleKeepsTheBuffer(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("k1"), Target: "#eco", Sender: user("alice"), Text: "keep me"})
	before := len(h.s.Messages(ch))

	h.s.RequestCycle(ch)
	out := h.apply(model.Part{EventMeta: h.meta(), Channel: "#eco", Sender: selfUser()})
	if _, ok := findChange[model.BufferClosed](out); ok {
		t.Fatalf("/cycle closed the buffer: %v", changeNames(out))
	}
	reset := mustFind[model.MembersChanged](t, out)
	if reset.Reset == nil || len(reset.Reset) != 0 {
		t.Errorf("MembersChanged = %+v, want an empty roster reset", reset)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "Rejoining #eco" {
		t.Errorf("cycle line = %q", line.Message.Text)
	}
	if got := len(h.s.Messages(ch)); got <= before {
		t.Errorf("scrollback shrank to %d from %d", got, before)
	}
	// the rejoin puts us back
	h.apply(model.Join{EventMeta: h.meta(), Channel: "#eco", Sender: selfUser()})
	if got := memberNicks(h.s, ch); len(got) != 1 || got[0] != testNick {
		t.Errorf("roster after rejoin = %v", got)
	}
}

// TestSelfKickLeaves pins the auto-leave: being removed from a channel closes
// its buffer instead of keeping a room we are not in, and says so in the
// network log where the user will see it.
func TestSelfKickLeaves(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	out := h.apply(model.Kick{
		EventMeta: h.meta(), Channel: "#eco", Sender: user("alice"),
		Target: testNick, Reason: "spam",
	})
	line := mustFind[model.MessageAdded](t, out)
	if line.Meta().Buffer != h.server() {
		t.Errorf("self-kick line landed in %v, want the network log", line.Meta().Buffer)
	}
	if line.Message.Text != "You were kicked from #eco by alice (spam)" {
		t.Errorf("self-kick line = %q", line.Message.Text)
	}
	if _, ok := findChange[model.BufferClosed](out); !ok {
		t.Fatalf("a self-kick did not close the buffer: %v", changeNames(out))
	}
	if _, ok := h.s.BufferInfo(ch); ok {
		t.Error("the buffer survived a self-kick")
	}
}

// TestKickRemovesTheVictim pins the ordinary kick.
func TestKickRemovesTheVictim(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice", "bob")
	out := h.apply(model.Kick{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice"), Target: "bob"})
	members := mustFind[model.MembersChanged](t, out)
	if len(members.Removed) != 1 || members.Removed[0].Nick != "bob" {
		t.Errorf("MembersChanged = %+v", members)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "bob was kicked by alice" {
		t.Errorf("kick line = %q", line.Message.Text)
	}
	if got := memberNicks(h.s, ch); len(got) != 2 {
		t.Errorf("roster = %v, want us and alice", got)
	}
}

// TestQuitLeavesEveryChannel pins that a QUIT is network-wide, and that a
// netsplit reports one bulk delta per buffer instead of a line per user.
func TestQuitLeavesEveryChannel(t *testing.T) {
	h := newHarness(t)
	a := h.joinChannel("#one", "alice")
	b := h.joinChannel("#two", "alice")

	out := h.apply(model.Quit{EventMeta: h.meta(), Sender: user("alice"), Reason: "bye"})
	if got := countChanges[model.MembersChanged](out); got != 2 {
		t.Fatalf("MembersChanged count = %d, want one per channel", got)
	}
	if got := countChanges[model.MessageAdded](out); got != 2 {
		t.Errorf("quit lines = %d, want one per channel", got)
	}
	for _, id := range []model.BufferID{a, b} {
		if got := memberNicks(h.s, id); len(got) != 1 {
			t.Errorf("%v roster = %v, want only us", id, got)
		}
	}

	// a netsplit: bulk membership, no per-buffer flood
	h.apply(model.Join{EventMeta: h.meta(), Channel: "#one", Sender: user("carol")})
	h.apply(model.BatchOpen{EventMeta: h.meta(), Ref: "ns", Type: "netsplit", Params: []string{"a.example", "b.example"}})
	split := model.EventMeta{Network: h.n, Batch: "ns"}
	out = h.apply(model.Quit{EventMeta: split, Sender: user("carol"), Reason: "*.net *.split"})
	members := mustFind[model.MembersChanged](t, out)
	if !members.Bulk {
		t.Errorf("netsplit quit was not marked bulk: %+v", members)
	}
	if got := countChanges[model.MessageAdded](out); got != 0 {
		t.Errorf("a netsplit wrote %d status lines; the UI collapses bulk deltas instead", got)
	}
	h.apply(model.BatchClose{EventMeta: h.meta(), Ref: "ns", Type: "netsplit"})
}

// TestNickMovesTheMemberKey pins the folded-identity fix: a nick change moves
// the member entry instead of leaving the ghost v1 left behind, and our own
// nick change updates the identity highlights match against.
func TestNickMovesTheMemberKey(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	out := h.apply(model.Nick{EventMeta: h.meta(), Sender: user("alice"), NewNick: "ALICE"})
	members := mustFind[model.MembersChanged](t, out)
	if len(members.Updated) != 1 || members.Updated[0].Nick != "ALICE" {
		t.Errorf("MembersChanged = %+v", members)
	}
	if got := memberNicks(h.s, ch); len(got) != 2 {
		t.Fatalf("roster = %v, want two entries (a re-cased nick left a ghost)", got)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "alice is now known as ALICE" {
		t.Errorf("nick line = %q", line.Message.Text)
	}

	// A re-casing folds to the same key, so it overwrites the entry whether or
	// not the old one is deleted — which means it cannot see the delete at all.
	// A change to a genuinely different nick is the case that can, and it is the
	// one v1 got wrong: it wrote the new key and kept the old, leaving a ghost
	// who could never leave, because every later PART/QUIT names only the new
	// nick. (Found at M7 verification: dropping the delete left this test green.)
	out = h.apply(model.Nick{EventMeta: h.meta(), Sender: user("ALICE"), NewNick: "bob"})
	members = mustFind[model.MembersChanged](t, out)
	if len(members.Updated) != 1 || members.Updated[0].Nick != "bob" {
		t.Errorf("MembersChanged = %+v, want the entry updated to bob", members)
	}
	got := memberNicks(h.s, ch)
	if len(got) != 2 {
		t.Fatalf("roster = %v, want two entries: the old nick was left behind as a ghost", got)
	}
	if !slices.Contains(got, "bob") || slices.Contains(got, "ALICE") {
		t.Errorf("roster = %v, want bob present and ALICE gone", got)
	}

	// our own nick change updates highlight matching
	h.apply(model.Nick{EventMeta: h.meta(), Sender: selfUser(), NewNick: "eco2"})
	self := mustFind[model.SelfChanged](t, h.changes)
	if self.Nick != "eco2" {
		t.Errorf("SelfChanged = %+v, want the new nick", self)
	}
	added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
		EventMeta: h.meta(), Target: "#eco", Sender: user("ALICE"), Text: "eco2: ping",
	}))
	if !added.Message.Highlight {
		t.Error("a mention of our new nick did not highlight")
	}
}

// TestNamesRebuildsTheRoster pins NAMES accumulation: entries arrive with
// prefixes and optional userhosts, the roster is replaced wholesale at
// ENDOFNAMES, and ranks come from PREFIX rather than a hardcoded "~&@%+".
func TestNamesRebuildsTheRoster(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")

	h.apply(model.Names{EventMeta: h.meta(), Channel: "#eco", Names: []string{
		"@+alice", "bob!bob@host", "~carol", "%dave", testNick,
	}})
	if got := len(h.s.Members(ch)); got != 1 {
		t.Fatalf("roster changed before ENDOFNAMES: %v", memberNicks(h.s, ch))
	}
	out := h.apply(model.Names{EventMeta: h.meta(), Channel: "#eco", End: true})
	reset := mustFind[model.MembersChanged](t, out)
	if reset.Reset == nil {
		t.Fatalf("MembersChanged = %+v, want a full reset", reset)
	}
	want := []string{"~carol", "@+alice", "%dave", "bob", testNick}
	if got := memberNicks(h.s, ch); !equalStrings(got, want) {
		t.Errorf("roster = %v, want %v (PREFIX rank order)", got, want)
	}
	for _, m := range h.s.Members(ch) {
		if m.Nick == "alice" && m.Prefixes != "@+" {
			t.Errorf("alice prefixes = %q, want @+ from multi-prefix", m.Prefixes)
		}
		if m.Nick == "bob" && m.Folded != "bob" {
			t.Errorf("userhost-in-names entry parsed as %+v", m)
		}
	}

	// ENDOFNAMES with no reply body must not wipe a roster
	out = h.apply(model.Names{EventMeta: h.meta(), Channel: "#eco", End: true})
	if _, ok := findChange[model.MembersChanged](out); ok {
		t.Errorf("a bare ENDOFNAMES rebuilt the roster: %v", changeNames(out))
	}
	if got := len(h.s.Members(ch)); got != 5 {
		t.Errorf("roster size = %d after a bare ENDOFNAMES, want 5", got)
	}
}

// TestJoinPartVisibility pins the join/part filter: the network default and the
// per-buffer override both suppress the noise without touching the roster.
func TestJoinPartVisibility(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.HideJoinParts = true })
	ch := h.joinChannel("#eco")

	out := h.apply(model.Join{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice")})
	if _, ok := findChange[model.MessageAdded](out); ok {
		t.Errorf("a hidden join wrote a line: %v", changeNames(out))
	}
	if _, ok := findChange[model.MembersChanged](out); !ok {
		t.Errorf("hiding join lines also hid the roster update: %v", changeNames(out))
	}
	hidden := false
	h.s.SetHideJoinParts(ch, hidden)
	out = h.apply(model.Part{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice")})
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "alice left" {
		t.Errorf("per-buffer override did not show the part line: %v", changeNames(out))
	}
	if h.s.HideJoinParts(ch) {
		t.Error("HideJoinParts still reports hidden after the override")
	}
	h.s.ClearHideJoinParts(ch)
	if !h.s.HideJoinParts(ch) {
		t.Error("clearing the override did not fall back to the network setting")
	}
}

// TestAwayAndAccountUpdateMembers pins away-notify, account-notify and setname,
// which v1 requested and then ignored.
func TestAwayAndAccountUpdateMembers(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	out := h.apply(model.AwayChange{EventMeta: h.meta(), Sender: user("alice"), Away: true, Message: "bbl"})
	if m := mustFind[model.MembersChanged](t, out); len(m.Updated) != 1 || !m.Updated[0].Away {
		t.Errorf("MembersChanged = %+v, want alice away", m)
	}
	// no change means no event
	if out := h.apply(model.AwayChange{EventMeta: h.meta(), Sender: user("alice"), Away: true}); len(out) != 0 {
		t.Errorf("a repeated AWAY emitted %v", changeNames(out))
	}
	h.apply(model.AccountChange{EventMeta: h.meta(), Sender: user("alice"), Account: "alice_"})
	h.apply(model.SetName{EventMeta: h.meta(), Sender: user("alice"), RealName: "Alice Example"})
	for _, m := range h.s.Members(ch) {
		if m.Nick != "alice" {
			continue
		}
		if m.Account != "alice_" || m.RealName != "Alice Example" || !m.Away {
			t.Errorf("alice = %+v, want account, realname and away all tracked", m)
		}
	}

	// our own away state is identity, not roster
	out = h.apply(model.AwayChange{EventMeta: h.meta(), Sender: selfUser(), Away: true})
	if self := mustFind[model.SelfChanged](t, out); !self.Away {
		t.Errorf("SelfChanged = %+v, want away", self)
	}
}

// TestInviteLandsInTheNetworkLog pins the invite line, including the command
// that accepts it.
func TestInviteLandsInTheNetworkLog(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.Invite{EventMeta: h.meta(), Channel: "#secret", Sender: user("alice"), Target: testNick})
	line := mustFind[model.MessageAdded](t, out)
	if line.Meta().Buffer != h.server() {
		t.Errorf("invite landed in %v, want the network log", line.Meta().Buffer)
	}
	want := "alice invited you to #secret. Use /join #secret to accept."
	if line.Message.Text != want {
		t.Errorf("invite line = %q, want %q", line.Message.Text, want)
	}
	// an invite for somebody else is not our business
	if out := h.apply(model.Invite{EventMeta: h.meta(), Channel: "#secret", Sender: user("alice"), Target: "bob"}); len(out) != 0 {
		t.Errorf("someone else's invite emitted %v", changeNames(out))
	}
}
