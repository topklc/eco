package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestOpenAndRestoreBuffers pins the intent API's buffer lifecycle.
func TestOpenAndRestoreBuffers(t *testing.T) {
	h := newHarness(t)

	id, out := h.s.OpenBuffer(h.n, "#eco")
	created := mustFind[model.BufferCreated](t, out)
	if created.Restored {
		t.Error("a buffer the user opened was marked restored")
	}
	if again, out := h.s.OpenBuffer(h.n, "#ECO"); again != id || len(out) != 0 {
		t.Errorf("reopening the same buffer = %v with %v, want the same id and no change", again, changeNames(out))
	}

	rid, out := h.s.RestoreBuffer(h.n, "#archived")
	if r := mustFind[model.BufferCreated](t, out); !r.Restored {
		t.Error("a restored buffer was not marked restored")
	}
	if _, ok := h.s.BufferInfo(rid); !ok {
		t.Error("the restored buffer does not resolve")
	}

	if out := h.s.CloseBuffer(h.id("#nothing")); out != nil {
		t.Errorf("closing an unknown buffer emitted %v", changeNames(out))
	}
	if out := h.s.CloseBuffer(rid); len(out) != 1 {
		t.Errorf("closing a buffer emitted %v, want one BufferClosed", changeNames(out))
	}
}

// TestLocalChannelsAreChannels pins a v1 bug the ISUPPORT layer kills: with
// CHANTYPES=#&, "&local" is a channel. v1 tested for a literal '#', so &
// channels were treated as private messages and never rejoined.
func TestLocalChannelsAreChannels(t *testing.T) {
	h := newHarness(t)
	h.apply(model.Join{EventMeta: h.meta(), Channel: "&local", Sender: selfUser()})
	info, ok := h.s.BufferInfo(h.id("&local"))
	if !ok {
		t.Fatal("no buffer for &local")
	}
	if info.Kind != model.BufferChannel {
		t.Errorf("&local kind = %v, want channel", info.Kind)
	}
	if got := h.s.RejoinTargets(h.n); len(got) != 1 || got[0].Channel != "&local" {
		t.Errorf("RejoinTargets = %+v, want &local", got)
	}
	// and a message to it routes to the channel, not to a query with the sender
	added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
		EventMeta: h.meta(), Target: "&local", Sender: user("alice"), Text: "hi",
	}))
	if added.Meta().Buffer != h.id("&local") {
		t.Errorf("message landed in %v", added.Meta().Buffer)
	}
}

// TestClearBuffer pins /clear: the log and the msgid index go, so a later
// /history refills instead of deduping against messages nobody can see.
func TestClearBuffer(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("c1"), Target: "#eco", Sender: user("alice"), Text: "hi"})
	h.s.MarkHistoryRequested(ch)

	out := h.s.ClearBuffer(ch)
	if u := mustFind[model.UnreadChanged](t, out); u.Unread != 0 {
		t.Errorf("UnreadChanged = %+v, want zeroed counts", u)
	}
	if got := len(h.s.Messages(ch)); got != 0 {
		t.Errorf("buffer holds %d messages after a clear", got)
	}
	if !h.s.NeedsHistory(ch) {
		t.Error("a cleared buffer does not want history back")
	}
	added := h.apply(model.PrivMsg{EventMeta: h.msg("c1"), Target: "#eco", Sender: user("alice"), Text: "hi"})
	if countChanges[model.MessageAdded](added) != 1 {
		t.Errorf("a cleared buffer still deduped the refetch: %v", changeNames(added))
	}
}

// TestUnreadSurvivesEqualTimestamps pins the marker resolving to a local
// position: a burst of messages sharing one server-time must still count, or a
// busy channel silently stops badging.
func TestUnreadSurvivesEqualTimestamps(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Clock = model.FixedClock(baseTime) })
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "one"})
	h.s.Focus(ch)
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Fatalf("unread after focus = %d", unread)
	}
	h.s.Blur(h.n)
	h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "two"})
	h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "three"})
	if unread, _ := h.s.Unread(ch); unread != 2 {
		t.Errorf("unread = %d, want 2 despite the identical timestamps", unread)
	}
}

// TestChangeOrderIsDeterministic pins that no change ever reaches the stream in
// Go map order: a QUIT across many buffers reports them in buffer creation
// order, every time.
func TestChangeOrderIsDeterministic(t *testing.T) {
	want := []string{}
	for run := 0; run < 8; run++ {
		h := newHarness(t)
		for _, ch := range []string{"#a", "#b", "#c", "#d", "#e", "#f"} {
			h.joinChannel(ch, "alice")
		}
		out := h.apply(model.Quit{EventMeta: h.meta(), Sender: user("alice"), Reason: "bye"})
		got := make([]string, 0, len(out))
		for _, c := range out {
			got = append(got, c.Meta().Buffer.Folded+":"+c.ChangeName())
		}
		if run == 0 {
			want = got
			continue
		}
		if !equalStrings(got, want) {
			t.Fatalf("run %d produced %v, want %v", run, got, want)
		}
	}
	if len(want) != 12 {
		t.Errorf("a quit across six channels produced %d changes: %v", len(want), want)
	}
}

// TestEchoIntoAnUnknownBufferIsANoOp pins that the outgoing path cannot invent
// a buffer behind the caller's back.
func TestEchoIntoAnUnknownBufferIsANoOp(t *testing.T) {
	h := newHarness(t)
	id, out := h.s.Echo(h.id("#missing"), Outgoing{Label: "x", Text: "hi"})
	if id != 0 || out != nil {
		t.Errorf("Echo into an unknown buffer = %d, %v", id, changeNames(out))
	}
	if _, ok := h.s.BufferInfo(h.id("#missing")); ok {
		t.Error("Echo created a buffer")
	}
}

// TestResolveOutsideTheEventStream pins the labeled-response ACK path: a server
// that answers with an empty labeled batch still resolves the pending row.
func TestResolveOutsideTheEventStream(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	id, _ := h.s.Echo(ch, Outgoing{Label: "l1", Text: "hello"})

	at := baseTime.Add(30 * time.Second)
	out := h.s.Resolve(h.n, "l1", "srv-1", at)
	resolved := mustFind[model.MessageResolved](t, out)
	if resolved.ID != id || resolved.Message.MsgID != "srv-1" || resolved.Message.Pending != model.Delivered {
		t.Errorf("MessageResolved = %+v", resolved)
	}
	if out := h.s.Resolve(h.n, "l1", "srv-1", at); out != nil {
		t.Errorf("resolving twice emitted %v", changeNames(out))
	}
}
