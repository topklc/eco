package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// testNick is the nick every harness connects with.
const testNick = "eco"

// testNet is the network id every harness uses.
const testNet = model.NetworkID("testnet")

// baseTime is where the injected clock starts. Nothing in this package reads a
// clock it was not given, so every timestamp in these tests is chosen here.
var baseTime = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// harness drives a Store the way the dispatcher will: one event in, one slice
// of changes out, on one goroutine.
type harness struct {
	t       *testing.T
	s       *Store
	n       model.NetworkID
	changes []model.Change
}

func newHarness(t *testing.T, mods ...func(*Options)) *harness {
	t.Helper()
	o := Options{
		Clock: model.StepClock(baseTime, time.Second),
		IDs:   model.NewIDSeq(0),
	}
	for _, f := range mods {
		f(&o)
	}
	h := &harness{t: t, s: New(o), n: testNet}
	h.s.EnsureNetwork(h.n)
	// An Ergo-shaped 005 without CASEMAPPING, so the RFC 1459 default (the one
	// a client must assume) stays in force and the folding tests are honest.
	h.apply(model.ISupport{EventMeta: h.meta(), Reset: true, Tokens: []string{
		"CHANTYPES=#&", "PREFIX=(qaohv)~&@%+", "STATUSMSG=~&@%+",
		"CHANMODES=beI,k,l,imnpst", "MODES=3", "NETWORK=testnet",
	}})
	h.apply(model.ConnState{EventMeta: h.meta(), Status: model.ConnReady, Nick: testNick})
	return h
}

func (h *harness) meta() model.EventMeta { return model.EventMeta{Network: h.n} }

func (h *harness) playback() model.EventMeta {
	return model.EventMeta{Network: h.n, Playback: true, Batch: "hp"}
}

func (h *harness) msg(id string) model.EventMeta {
	return model.EventMeta{Network: h.n, MsgID: id}
}

func (h *harness) apply(ev model.Event) []model.Change {
	h.t.Helper()
	h.changes = h.s.Apply(ev)
	return h.changes
}

func (h *harness) id(name string) model.BufferID { return h.s.BufferID(h.n, name) }

func (h *harness) server() model.BufferID { return model.ServerBuffer(h.n) }

// user builds a sender with a full hostmask.
func user(nick string) model.Sender {
	return model.Sender{Nick: nick, User: nick, Host: "test.example"}
}

// self builds our own sender.
func selfUser() model.Sender {
	return model.Sender{Nick: testNick, User: testNick, Host: "test.example", Self: true}
}

// joinChannel puts us and one other member in a channel and returns its id.
func (h *harness) joinChannel(ch string, others ...string) model.BufferID {
	h.t.Helper()
	h.apply(model.Join{EventMeta: h.meta(), Channel: ch, Sender: selfUser()})
	for _, o := range others {
		h.apply(model.Join{EventMeta: h.meta(), Channel: ch, Sender: user(o)})
	}
	return h.id(ch)
}

// --- change helpers ----------------------------------------------------------

func changeNames(cs []model.Change) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ChangeName())
	}
	return out
}

func findChange[T model.Change](cs []model.Change) (T, bool) {
	var zero T
	for _, c := range cs {
		if v, ok := c.(T); ok {
			return v, true
		}
	}
	return zero, false
}

func allChanges[T model.Change](cs []model.Change) []T {
	var out []T
	for _, c := range cs {
		if v, ok := c.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func mustFind[T model.Change](t *testing.T, cs []model.Change) T {
	t.Helper()
	v, ok := findChange[T](cs)
	if !ok {
		t.Fatalf("no %T in %v", v, changeNames(cs))
	}
	return v
}

func countChanges[T model.Change](cs []model.Change) int { return len(allChanges[T](cs)) }

// --- tests -------------------------------------------------------------------

// TestStoreStartsWithAServerBuffer pins the one buffer that always exists: the
// network log survives every teardown path, so nothing has to nil-check it.
func TestStoreStartsWithAServerBuffer(t *testing.T) {
	s := New(Options{Clock: model.FixedClock(baseTime)})
	out := s.EnsureNetwork(testNet)
	created := mustFind[model.BufferCreated](t, out)
	if created.Info.ID != model.ServerBuffer(testNet) {
		t.Fatalf("server buffer id = %v, want %v", created.Info.ID, model.ServerBuffer(testNet))
	}
	if created.Info.Kind != model.BufferServer {
		t.Errorf("server buffer kind = %v, want server", created.Info.Kind)
	}
	if got := s.Buffers(testNet); len(got) != 1 {
		t.Fatalf("Buffers = %v, want only the server buffer", got)
	}
	if out := s.EnsureNetwork(testNet); out != nil {
		t.Errorf("EnsureNetwork twice emitted %v, want nothing", changeNames(out))
	}
	// the server buffer refuses to close
	if out := s.CloseBuffer(model.ServerBuffer(testNet)); out != nil {
		t.Errorf("closing the server buffer emitted %v, want nothing", changeNames(out))
	}
	if got := s.Buffers(testNet); len(got) != 1 {
		t.Fatalf("server buffer disappeared: %v", got)
	}
}

// TestApplyIsTotal pins that the reducer never panics and never invents
// changes: an event for an unknown network creates it, an unmodelled event is a
// no-op, and a nil event is ignored.
func TestApplyIsTotal(t *testing.T) {
	s := New(Options{Clock: model.FixedClock(baseTime)})
	if out := s.Apply(nil); out != nil {
		t.Errorf("Apply(nil) = %v, want nothing", changeNames(out))
	}
	if out := s.Apply(model.Raw{EventMeta: model.EventMeta{}, Line: "PING"}); out != nil {
		t.Errorf("Apply with no network = %v, want nothing", changeNames(out))
	}
	if out := s.Apply(model.Raw{EventMeta: model.EventMeta{Network: testNet}, Line: "PING"}); out != nil {
		t.Errorf("Apply(Raw) = %v, want nothing", changeNames(out))
	}
	if len(s.Networks()) != 1 {
		t.Errorf("Networks = %v, want the network created on sight", s.Networks())
	}
}

// TestBufferIdentityIsFolded pins the fix for v1's strings.ToLower keying:
// buffer identity is the server's casemapping, so on an RFC 1459 network
// "#Foo[]" and "#foo{}" are the same channel and a re-cased name never opens a
// second buffer.
func TestBufferIdentityIsFolded(t *testing.T) {
	h := newHarness(t)
	first := h.joinChannel("#Test[Room]")

	// same channel, different case and different bracket forms
	out := h.apply(model.PrivMsg{
		EventMeta: h.msg("m1"), Target: "#test{room}", Sender: user("alice"), Text: "hi",
	})
	if _, created := findChange[model.BufferCreated](out); created {
		t.Fatalf("a second buffer was opened for the same channel: %v", changeNames(out))
	}
	added := mustFind[model.MessageAdded](t, out)
	if added.Meta().Buffer != first {
		t.Errorf("message landed in %v, want %v", added.Meta().Buffer, first)
	}
	if got := len(h.s.Buffers(h.n)); got != 2 {
		t.Errorf("network holds %d buffers, want the server buffer plus one channel", got)
	}
	// the display name is the one the server first used, not the folded key
	info, ok := h.s.BufferInfo(first)
	if !ok || info.Name != "#Test[Room]" {
		t.Errorf("buffer name = %q, want the server's casing", info.Name)
	}
}

// TestScrollbackTrimForgetsItsMsgids pins the replacement for v1's shared
// 8192-entry seenIDs map, which emptied itself wholesale and let a busy buffer
// start duplicating old history. The index is per buffer and bounded by that
// buffer's own scrollback.
func TestScrollbackTrimForgetsItsMsgids(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Scrollback = 4 })
	ch := h.joinChannel("#eco")

	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		h.apply(model.PrivMsg{EventMeta: h.msg(id), Target: "#eco", Sender: user("alice"), Text: id})
	}
	msgs := h.s.Messages(ch)
	if len(msgs) != 4 {
		t.Fatalf("buffer holds %d messages, want the 4-message cap", len(msgs))
	}
	if got := msgs[len(msgs)-1].MsgID; got != "f" {
		t.Errorf("newest message msgid = %q, want f", got)
	}
	// a retained msgid still dedupes
	out := h.apply(model.PrivMsg{EventMeta: h.msg("f"), Target: "#eco", Sender: user("alice"), Text: "f"})
	if len(out) != 0 {
		t.Errorf("a retained msgid was displayed twice: %v", changeNames(out))
	}
	// a trimmed one no longer does, and that is correct: it is off screen
	out = h.apply(model.PrivMsg{EventMeta: h.msg("a"), Target: "#eco", Sender: user("alice"), Text: "a"})
	if countChanges[model.MessageAdded](out) != 1 {
		t.Errorf("a trimmed msgid was not accepted again: %v", changeNames(out))
	}
}

// TestUnlimitedScrollbackKeepsEverything pins the Unlimited setting.
func TestUnlimitedScrollbackKeepsEverything(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Scrollback = Unlimited })
	ch := h.joinChannel("#eco")
	for i := 0; i < 50; i++ {
		h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "x"})
	}
	if got := len(h.s.Messages(ch)); got < 50 {
		t.Errorf("buffer holds %d messages, want at least the 50 sent", got)
	}
}

// TestNetworksAreIsolated pins that two networks never share identity: the same
// channel name on two networks is two buffers, and ignore lists do not leak.
func TestNetworksAreIsolated(t *testing.T) {
	s := New(Options{Clock: model.FixedClock(baseTime)})
	a, b := model.NetworkID("a"), model.NetworkID("b")
	s.EnsureNetwork(a)
	s.EnsureNetwork(b)
	ida, _ := s.OpenBuffer(a, "#eco")
	idb, _ := s.OpenBuffer(b, "#eco")
	if ida == idb {
		t.Fatalf("the same buffer id %v on two networks", ida)
	}
	s.AddIgnore(a, "spammer")
	if !s.Ignored(a, user("spammer")) {
		t.Error("ignore did not take on its own network")
	}
	if s.Ignored(b, user("spammer")) {
		t.Error("an ignore leaked to another network")
	}
}
