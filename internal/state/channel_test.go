package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestModeDeltaTracking pins channel mode tracking against the network's
// CHANMODES and PREFIX. v1 hardcoded the letters, so a mode it did not know
// consumed the wrong parameter; and it answered every MODE with a NAMES, which
// on a busy channel is a self-inflicted flood.
func TestModeDeltaTracking(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	steps := []struct {
		name      string
		modes     string
		args      []string
		full      bool
		wantModes string
		wantKey   string
		wantLimit int
	}{
		{name: "paramless flags", modes: "+nt", wantModes: "nt"},
		{name: "key takes its parameter", modes: "+k", args: []string{"hunter2"}, wantModes: "knt", wantKey: "hunter2"},
		{name: "limit takes one only when set", modes: "+l", args: []string{"42"}, wantModes: "klnt", wantKey: "hunter2", wantLimit: 42},
		{name: "removing the limit takes none", modes: "-l", wantModes: "knt", wantKey: "hunter2"},
		{name: "removing the key clears it", modes: "-k", args: []string{"hunter2"}, wantModes: "nt"},
		{name: "a list mode consumes its mask", modes: "+bi", args: []string{"*!*@spam.example"}, wantModes: "int"},
		{name: "324 replaces rather than merges", modes: "+mk", args: []string{"other"}, full: true, wantModes: "km", wantKey: "other"},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			out := h.apply(model.Mode{
				EventMeta: h.meta(), Target: "#eco", Sender: user("alice"),
				ModeString: st.modes, Args: st.args, Full: st.full,
			})
			changed := mustFind[model.ModeChanged](t, out)
			if changed.Modes != st.wantModes {
				t.Errorf("modes = %q, want %q", changed.Modes, st.wantModes)
			}
			if changed.Key != st.wantKey {
				t.Errorf("key = %q, want %q", changed.Key, st.wantKey)
			}
			if changed.Limit != st.wantLimit {
				t.Errorf("limit = %d, want %d", changed.Limit, st.wantLimit)
			}
			modes, key, limit := h.s.Modes(ch)
			if modes != st.wantModes || key != st.wantKey || limit != st.wantLimit {
				t.Errorf("store holds %q/%q/%d, want %q/%q/%d",
					modes, key, limit, st.wantModes, st.wantKey, st.wantLimit)
			}
			// 324 is the server telling us the state; nobody just changed it
			_, hasLine := findChange[model.MessageAdded](out)
			if st.full && hasLine {
				t.Errorf("RPL_CHANNELMODEIS wrote a status line: %v", changeNames(out))
			}
			if !st.full && !hasLine {
				t.Errorf("a MODE change wrote no status line: %v", changeNames(out))
			}
		})
	}
	if got := h.s.RejoinTargets(h.n); len(got) != 1 || got[0].Key != "other" {
		t.Errorf("RejoinTargets = %+v, want the tracked key so a +k rejoin works", got)
	}
}

// TestMemberModeUpdatesPrefixes pins that +o/+v update the roster directly —
// no NAMES round trip, no full rebuild for one voice.
func TestMemberModeUpdatesPrefixes(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice", "bob")

	out := h.apply(model.Mode{
		EventMeta: h.meta(), Target: "#eco", Sender: user("alice"),
		ModeString: "+ov", Args: []string{"bob", "bob"},
	})
	if _, ok := findChange[model.ModeChanged](out); ok {
		t.Errorf("a pure member mode reported a channel mode change: %v", changeNames(out))
	}
	members := mustFind[model.MembersChanged](t, out)
	if len(members.Updated) == 0 {
		t.Fatalf("MembersChanged = %+v, want bob updated", members)
	}
	if got := memberNicks(h.s, ch); got[0] != "@+bob" {
		t.Errorf("roster = %v, want bob ranked with @+ first", got)
	}
	out = h.apply(model.Mode{
		EventMeta: h.meta(), Target: "#eco", Sender: user("alice"),
		ModeString: "-o", Args: []string{"bob"},
	})
	members = mustFind[model.MembersChanged](t, out)
	if len(members.Updated) != 1 || members.Updated[0].Prefixes != "+" {
		t.Errorf("MembersChanged = %+v, want bob down to +", members)
	}
	// a mode for someone we never saw join still lands
	h.apply(model.Mode{
		EventMeta: h.meta(), Target: "#eco", Sender: user("alice"),
		ModeString: "+o", Args: []string{"carol"},
	})
	found := false
	for _, m := range h.s.Members(ch) {
		if m.Nick == "carol" && m.Prefixes == "@" {
			found = true
		}
	}
	if !found {
		t.Errorf("roster = %v, want carol recorded as an op", memberNicks(h.s, ch))
	}
}

// TestTopicTracking pins the two topic sources: the numeric we learn on join
// (no status line, nobody changed anything) and the TOPIC command (a line).
func TestTopicTracking(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	setAt := baseTime.Add(-time.Hour)

	out := h.apply(model.Topic{
		EventMeta: h.meta(), Channel: "#eco", Topic: "the build is green",
		SetBy: "alice", SetAt: setAt,
	})
	changed := mustFind[model.TopicChanged](t, out)
	if changed.Topic != "the build is green" || changed.SetBy != "alice" || !changed.SetAt.Equal(setAt) {
		t.Errorf("TopicChanged = %+v", changed)
	}
	if _, ok := findChange[model.MessageAdded](out); ok {
		t.Errorf("RPL_TOPIC wrote a status line: %v", changeNames(out))
	}
	if topic, known := h.s.Topic(ch); topic != "the build is green" || !known {
		t.Errorf("Topic = %q known=%v", topic, known)
	}

	out = h.apply(model.Topic{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice"), Topic: "now red"})
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "alice set the topic: now red" {
		t.Errorf("topic line = %q", line.Message.Text)
	}

	out = h.apply(model.Topic{EventMeta: h.meta(), Channel: "#eco", Sender: user("alice"), Cleared: true})
	changed = mustFind[model.TopicChanged](t, out)
	if !changed.Cleared || changed.Topic != "" {
		t.Errorf("TopicChanged = %+v, want a cleared topic", changed)
	}
	if topic, known := h.s.Topic(ch); topic != "" || !known {
		t.Errorf("Topic = %q known=%v, want a known-empty topic", topic, known)
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "alice cleared the topic" {
		t.Errorf("clear line = %q", line.Message.Text)
	}
}

// TestChannelRenameMigratesTheBuffer pins draft/channel-rename: the buffer
// keeps its messages, roster and unread under a new id, and BufferRenamed
// carries the old id so the UI can migrate everything it keyed by it.
func TestChannelRenameMigratesTheBuffer(t *testing.T) {
	h := newHarness(t)
	old := h.joinChannel("#old", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("m1"), Target: "#old", Sender: user("alice"), Text: "before"})
	unreadBefore, _ := h.s.Unread(old)

	out := h.apply(model.ChannelRenamed{EventMeta: h.meta(), From: "#old", To: "#new", Reason: "tidying"})
	renamed := mustFind[model.BufferRenamed](t, out)
	if renamed.From != old {
		t.Errorf("BufferRenamed.From = %v, want %v", renamed.From, old)
	}
	newID := h.id("#new")
	if renamed.Meta().Buffer != newID || renamed.Name != "#new" {
		t.Errorf("BufferRenamed = %+v, want the new id and name", renamed)
	}
	if _, ok := h.s.BufferInfo(old); ok {
		t.Error("the old buffer id still resolves")
	}
	if got := len(h.s.Members(newID)); got != 2 {
		t.Errorf("roster after rename = %v", memberNicks(h.s, newID))
	}
	if unread, _ := h.s.Unread(newID); unread != unreadBefore {
		t.Errorf("unread after rename = %d, want %d", unread, unreadBefore)
	}
	for _, m := range h.s.Messages(newID) {
		if m.Buffer != newID {
			t.Fatalf("message %d still points at %v", m.ID, m.Buffer)
		}
	}
	if line := mustFind[model.MessageAdded](t, out); line.Message.Text != "Channel renamed from #old to #new (tidying)" {
		t.Errorf("rename line = %q", line.Message.Text)
	}
	if got := h.s.RejoinTargets(h.n); len(got) != 1 || got[0].Channel != "#new" {
		t.Errorf("RejoinTargets = %+v, want the new name", got)
	}

	// renaming onto an open buffer would merge two conversations: refuse
	h.joinChannel("#taken")
	out = h.apply(model.ChannelRenamed{EventMeta: h.meta(), From: "#new", To: "#taken"})
	if _, ok := findChange[model.BufferRenamed](out); ok {
		t.Errorf("a rename collided with an open buffer: %v", changeNames(out))
	}
	if _, ok := h.s.BufferInfo(newID); !ok {
		t.Error("the refused rename lost the original buffer")
	}
}

// TestUserModeLandsInTheNetworkLog pins that a MODE addressed to us is a log
// line, not an attempt to track channel state on a nick.
func TestUserModeLandsInTheNetworkLog(t *testing.T) {
	h := newHarness(t)
	out := h.apply(model.Mode{EventMeta: h.meta(), Target: testNick, ModeString: "+i"})
	line := mustFind[model.MessageAdded](t, out)
	if line.Meta().Buffer != h.server() || line.Message.Text != "mode +i" {
		t.Errorf("user mode line = %q in %v", line.Message.Text, line.Meta().Buffer)
	}
	if out := h.apply(model.Mode{EventMeta: h.meta(), Target: "someoneelse", ModeString: "+i"}); len(out) != 0 {
		t.Errorf("another user's mode emitted %v", changeNames(out))
	}
}
