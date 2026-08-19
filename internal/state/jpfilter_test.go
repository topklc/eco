package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// smartHarness is a store with the filter on and a clock the test drives.
func smartHarness(t *testing.T, mods ...func(*Options)) *harness {
	t.Helper()
	return newHarness(t, append([]func(*Options){func(o *Options) {
		o.SmartJoinParts = true
	}}, mods...)...)
}

// says makes nick speak in a channel at a time.
func (h *harness) says(ch, nick string, at time.Time) {
	h.t.Helper()
	meta := h.meta()
	meta.Time = at
	h.apply(model.PrivMsg{EventMeta: meta, Target: ch, Sender: user(nick), Text: "hello"})
}

// jpLines returns the status lines a membership event produced.
func jpLines(cs []model.Change) []string {
	var out []string
	for _, c := range cs {
		if added, ok := c.(model.MessageAdded); ok && added.Message.Kind == model.MsgStatus {
			out = append(out, added.Message.Text)
		}
	}
	return out
}

// The filter's whole point, as one table: a join, a part or a quit is shown
// when that nick has said something here recently, and is dropped when they
// have not. This is what makes a busy channel readable — and the reason it is a
// rule about the NICK and not about the channel is that the blunt per-channel
// switch already existed and hid the people you were talking to along with the
// strangers.
func TestSmartFilterShowsRecentSpeakers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoke time.Duration // how long before the event they last spoke; -1 means never
		want  bool
	}{
		{"spoke a moment ago", 1 * time.Second, true},
		{"spoke a minute ago", 1 * time.Minute, true},
		{"spoke just inside the window", DefaultSpeakerWindow - time.Second, true},
		{"spoke exactly at the window edge", DefaultSpeakerWindow, true},
		{"spoke just outside the window", DefaultSpeakerWindow + time.Second, false},
		{"spoke an hour ago", time.Hour, false},
		{"never spoke", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := smartHarness(t)
			h.joinChannel("#eco", "alice", "stranger")
			now := baseTime.Add(2 * time.Hour)
			if tc.spoke >= 0 {
				h.says("#eco", "stranger", now.Add(-tc.spoke))
			}
			meta := h.meta()
			meta.Time = now
			got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#eco", Sender: user("stranger")}))
			if (len(got) > 0) != tc.want {
				t.Errorf("part lines = %v, want shown = %v", got, tc.want)
			}
		})
	}
}

// All three events go through the same gate, and each one of them is a separate
// call site that could be left behind.
func TestSmartFilterCoversJoinPartAndQuit(t *testing.T) {
	h := smartHarness(t)
	h.joinChannel("#eco", "alice", "stranger")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "alice", now)
	meta := func() model.EventMeta {
		m := h.meta()
		m.Time = now
		return m
	}

	// the stranger: three events, no lines
	if got := jpLines(h.apply(model.Join{EventMeta: meta(), Channel: "#eco", Sender: user("nobody")})); len(got) != 0 {
		t.Errorf("a silent nick's join was shown: %v", got)
	}
	if got := jpLines(h.apply(model.Part{EventMeta: meta(), Channel: "#eco", Sender: user("stranger")})); len(got) != 0 {
		t.Errorf("a silent nick's part was shown: %v", got)
	}
	if got := jpLines(h.apply(model.Quit{EventMeta: meta(), Sender: user("nobody")})); len(got) != 0 {
		t.Errorf("a silent nick's quit was shown: %v", got)
	}

	// alice, who has been talking: shown
	if got := jpLines(h.apply(model.Quit{EventMeta: meta(), Sender: user("alice")})); len(got) != 1 {
		t.Errorf("a recent speaker's quit = %v, want one line", got)
	}
}

// The two existing overrides keep working, and keep their priority: "hide
// joins/parts here" is an instruction and beats the filter, while turning the
// filter off restores v1's behaviour exactly.
func TestSmartFilterKeepsTheExistingOverrides(t *testing.T) {
	h := smartHarness(t)
	ch := h.joinChannel("#eco", "alice")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "alice", now)
	meta := h.meta()
	meta.Time = now

	// hidden wins over "they spoke"
	h.s.SetHideJoinParts(ch, true)
	if got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#eco", Sender: user("alice")})); len(got) != 0 {
		t.Errorf("the per-buffer hide was overruled by the filter: %v", got)
	}

	// filter off: everyone's lines come back
	h2 := newHarness(t)
	h2.joinChannel("#eco", "alice", "stranger")
	m2 := h2.meta()
	m2.Time = now
	if got := jpLines(h2.apply(model.Part{EventMeta: m2, Channel: "#eco", Sender: user("stranger")})); len(got) != 1 {
		t.Errorf("with the filter off, a silent nick's part = %v, want one line", got)
	}

	// filter on, then switched off at runtime
	h3 := smartHarness(t)
	h3.joinChannel("#eco", "alice", "stranger")
	h3.s.SetNetworkSmartJoinParts(h3.n, false)
	if !h3.s.SmartJoinParts(h3.n) == false {
		t.Error("SmartJoinParts did not report the setting")
	}
	m3 := h3.meta()
	m3.Time = now
	if got := jpLines(h3.apply(model.Part{EventMeta: m3, Channel: "#eco", Sender: user("stranger")})); len(got) != 1 {
		t.Errorf("switching the filter off did not restore the line: %v", got)
	}
}

// A private conversation has no churn to filter: the person leaving IS the
// conversation, and dropping that line would leave someone talking to a ghost.
// The same goes for the network's own log, where a quit is the server's news.
//
// The gate is asserted directly because no event reaches it today — a QUIT only
// writes a line into buffers whose member list contained the nick, and a query
// buffer keeps no roster (applyQuit). That is v1's behaviour and this slice does
// not change it; what this pins is that when a line for a non-channel does get
// here, the recency filter is not what stops it.
func TestSmartFilterLeavesConversationsAlone(t *testing.T) {
	h := smartHarness(t)
	h.apply(model.PrivMsg{EventMeta: h.meta(), Target: testNick, Sender: user("alice"), Text: "hi"})
	n := h.s.nets[h.n]
	meta := h.meta()
	meta.Time = baseTime.Add(3 * time.Hour) // long past alice's last word

	query := n.bufs[n.props.Fold("alice")]
	if query == nil || query.kind != model.BufferQuery {
		t.Fatalf("no query buffer for alice: %+v", query)
	}
	if !h.s.showJoinPart(n, query, "alice", meta) {
		t.Error("the filter silenced a private conversation")
	}
	if !h.s.showJoinPart(n, n.server(), "alice", meta) {
		t.Error("the filter silenced the network log")
	}
	// and it is still a filter where it is supposed to be one
	ch := n.bufs[n.props.Fold("#eco")]
	if ch == nil {
		h.joinChannel("#eco", "alice")
		ch = n.bufs[n.props.Fold("#eco")]
	}
	if h.s.showJoinPart(n, ch, "alice", meta) {
		t.Error("the filter let a stale speaker through in a channel")
	}
}

// The window is configurable, and the configured value is what is applied.
func TestSmartFilterWindowIsConfigurable(t *testing.T) {
	h := smartHarness(t, func(o *Options) { o.SpeakerWindow = time.Minute })
	h.joinChannel("#eco", "alice")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "alice", now.Add(-90*time.Second))
	meta := h.meta()
	meta.Time = now
	if got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#eco", Sender: user("alice")})); len(got) != 0 {
		t.Errorf("a one-minute window kept a 90-second-old speaker: %v", got)
	}

	h.s.SetNetworkSpeakerWindow(h.n, 2*time.Minute)
	if got := jpLines(h.apply(model.Join{EventMeta: meta, Channel: "#eco", Sender: user("alice")})); len(got) != 1 {
		t.Errorf("widening the window did not bring the line back: %v", got)
	}
}

// Speech is recorded per buffer. Someone chatting in #eco is not thereby
// interesting when they leave #linux — which is the difference between a filter
// that reads the room and one that just tracks activity.
func TestSmartFilterIsPerBuffer(t *testing.T) {
	h := smartHarness(t)
	h.joinChannel("#eco", "alice")
	h.joinChannel("#linux", "alice")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "alice", now)
	meta := h.meta()
	meta.Time = now

	if got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#linux", Sender: user("alice")})); len(got) != 0 {
		t.Errorf("speech in another channel kept the line: %v", got)
	}
	if got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#eco", Sender: user("alice")})); len(got) != 1 {
		t.Errorf("speech in this channel did not keep the line: %v", got)
	}
}

// Folding is the server's: on an rfc1459 network "Alice[afk]" and "alice{afk}"
// are one person, and the filter has to agree with the member list about that.
func TestSmartFilterFoldsTheNick(t *testing.T) {
	h := smartHarness(t)
	h.joinChannel("#eco", "Alice[afk]")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "ALICE{afk}", now)
	meta := h.meta()
	meta.Time = now
	if got := jpLines(h.apply(model.Part{EventMeta: meta, Channel: "#eco", Sender: user("Alice[afk]")})); len(got) != 1 {
		t.Errorf("a folded nick was not recognised as the speaker: %v", got)
	}
	if !h.s.SpokeRecently(h.id("#eco"), "alice[afk]") {
		t.Error("SpokeRecently disagreed with the filter")
	}
}

// A netsplit is still one bulk delta and no lines, filter or no filter: the
// batch rule runs first and is not something the filter may re-decide.
func TestSmartFilterDoesNotResurrectNetsplitLines(t *testing.T) {
	h := smartHarness(t)
	h.joinChannel("#eco", "alice")
	now := baseTime.Add(time.Hour)
	h.says("#eco", "alice", now)
	h.apply(model.BatchOpen{EventMeta: h.meta(), Ref: "ns", Type: "netsplit", Params: []string{"a.example", "b.example"}})
	meta := h.meta()
	meta.Time, meta.Batch = now, "ns"
	if got := jpLines(h.apply(model.Quit{EventMeta: meta, Sender: user("alice")})); len(got) != 0 {
		t.Errorf("a netsplit quit wrote a line: %v", got)
	}
}

// The speaker table is bounded. A channel where thousands of distinct people
// speak must not grow one map entry per person for the life of the process.
func TestSmartFilterSpeakerTableIsBounded(t *testing.T) {
	h := smartHarness(t)
	id := h.joinChannel("#eco")
	at := baseTime
	for i := 0; i < speakerCap*3; i++ {
		at = at.Add(time.Second)
		h.says("#eco", "nick"+itoa(i), at)
	}
	b := h.s.buffer(id)
	if b == nil {
		t.Fatal("no buffer")
	}
	if len(b.spoke) > speakerCap {
		t.Errorf("speaker table = %d entries, want at most %d", len(b.spoke), speakerCap)
	}
	// and the newest speaker is still in it: pruning by age must not throw away
	// the people the filter exists to keep
	if !b.spokeSince(h.s.nets[h.n].props.Fold("nick"+itoa(speakerCap*3-1)), at.Add(-time.Minute)) {
		t.Error("the newest speaker was pruned")
	}
}

// itoa avoids pulling strconv into the test for one call.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
