package irc

import (
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

func TestChathistoryBatchArrivesAsOneChunk(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	handle, err := h.c.History(HistoryRequest{
		Target: "#eco", Direction: model.HistoryBefore, MsgID: "m9", Limit: 3,
	})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	line := h.srv.WaitFor("*CHATHISTORY*")
	if untagged(line) != "CHATHISTORY BEFORE #eco msgid=m9 3" {
		t.Fatalf("chathistory line = %q", untagged(line))
	}
	if tagValue(line, "label") != handle.Label() {
		t.Fatalf("label = %q, want %q", tagValue(line, "label"), handle.Label())
	}

	h.srv.Send("@label=%s BATCH +h chathistory #eco", handle.Label())
	h.srv.Send("@batch=h;msgid=h1;time=2024-01-02T03:04:05.000Z :alice!a@host PRIVMSG #eco :older one")
	h.srv.Send("@batch=h;msgid=h2 :bob!b@host PRIVMSG #eco :older two")
	h.srv.Send("BATCH -h")

	open, ok := h.waitFor("batchopen").(model.BatchOpen)
	if !ok || open.Ref != "h" || open.Type != "chathistory" || open.Parent != "" {
		t.Fatalf("batch open = %+v", open)
	}

	// The messages do not arrive one by one: a closed chathistory batch is
	// delivered whole, so the store prepends a block instead of interleaving
	// replayed history with live traffic.
	chunk, ok := h.next().(model.HistoryChunk)
	if !ok {
		t.Fatalf("after the batch opened, want a historychunk")
	}
	if chunk.Target != "#eco" {
		t.Errorf("chunk target = %q", chunk.Target)
	}
	if chunk.Direction != model.HistoryBefore {
		t.Errorf("chunk direction = %v, want before (from the request's label)", chunk.Direction)
	}
	if !chunk.Complete {
		t.Errorf("2 of 3 asked for is a complete chunk")
	}
	if chunk.Label != handle.Label() {
		t.Errorf("chunk label = %q, want %q", chunk.Label, handle.Label())
	}
	if len(chunk.Events) != 2 {
		t.Fatalf("chunk holds %d events, want 2", len(chunk.Events))
	}
	for i, ev := range chunk.Events {
		pm, ok := ev.(model.PrivMsg)
		if !ok {
			t.Fatalf("chunk event %d is %s", i, ev.EventName())
		}
		if !pm.Playback {
			t.Errorf("chunk event %d is not marked Playback", i)
		}
		if pm.Batch != "h" {
			t.Errorf("chunk event %d batch = %q", i, pm.Batch)
		}
	}
	if got := chunk.Events[0].(model.PrivMsg).Text; got != "older one" {
		t.Errorf("first replayed message = %q", got)
	}

	if cl, ok := h.next().(model.BatchClose); !ok || cl.Ref != "h" || cl.Type != "chathistory" {
		t.Fatalf("batch close = %+v", cl)
	}
}

func TestChathistoryChunkReportsServerTruncation(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	handle, err := h.c.History(HistoryRequest{Target: "#eco", Direction: model.HistoryLatest, Limit: 2})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	h.srv.WaitFor("*CHATHISTORY*")
	h.srv.Send("@label=%s BATCH +h chathistory #eco", handle.Label())
	h.srv.Send("@batch=h;msgid=h1 :alice!a@host PRIVMSG #eco :one")
	h.srv.Send("@batch=h;msgid=h2 :alice!a@host PRIVMSG #eco :two")
	h.srv.Send("BATCH -h")

	chunk, ok := h.waitFor("historychunk").(model.HistoryChunk)
	if !ok {
		t.Fatal("want a historychunk")
	}
	// The server stopped exactly at the limit, which is the only signal that
	// there is more before this.
	if chunk.Complete {
		t.Error("a chunk that filled the limit must not claim to be complete")
	}
}

func TestLiveMultilineMergesAtBatchClose(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	h.srv.Send("@msgid=ml1;time=2024-01-02T03:04:05.000Z BATCH +m draft/multiline #eco")
	h.srv.Send("@batch=m :alice!a@host PRIVMSG #eco :first line")
	h.srv.Send("@batch=m :alice!a@host PRIVMSG #eco :second line")
	h.srv.Send("@batch=m;draft/multiline-concat :alice!a@host PRIVMSG #eco : continued")
	h.srv.Send("BATCH -m")

	if open, ok := h.waitFor("batchopen").(model.BatchOpen); !ok || open.Type != "draft/multiline" {
		t.Fatalf("batch open = %+v", open)
	}
	pm, ok := h.next().(model.PrivMsg)
	if !ok {
		t.Fatal("want one merged privmsg at batch close")
	}
	if want := "first line\nsecond line continued"; pm.Text != want {
		t.Errorf("merged text = %q, want %q", pm.Text, want)
	}
	// The parts carry no msgid of their own; identity comes from the BATCH
	// command, which is where the server puts it.
	if pm.MsgID != "ml1" {
		t.Errorf("merged msgid = %q, want ml1", pm.MsgID)
	}
	if !pm.ServerTime {
		t.Error("merged message lost the batch's server-time")
	}
	// The headline fix: a live multiline message is live. v1 called anything
	// with a batch tag playback and then tried to buy it back with a
	// two-minute wall-clock window.
	if pm.Playback {
		t.Error("a live multiline message must not be playback")
	}
	if pm.Batch != "" {
		t.Errorf("merged batch = %q, want the enclosing batch (none here)", pm.Batch)
	}
	if pm.Sender.Nick != "alice" {
		t.Errorf("merged sender = %+v", pm.Sender)
	}
	if cl, ok := h.next().(model.BatchClose); !ok || cl.Ref != "m" {
		t.Fatalf("batch close = %+v", cl)
	}
}

func TestMultilineNestedInChathistoryIsPlaybackAndLandsInTheChunk(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	handle, err := h.c.History(HistoryRequest{Target: "#eco", Direction: model.HistoryLatest, Limit: 50})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	h.srv.WaitFor("*CHATHISTORY*")

	// The nesting the whole batch registry exists for: a multiline message
	// replayed inside a chathistory batch. The inner BATCH lines carry the
	// outer batch's tag, per the spec.
	h.srv.Send("@label=%s BATCH +h chathistory #eco", handle.Label())
	h.srv.Send("@batch=h;msgid=p1 :alice!a@host PRIVMSG #eco :a plain one")
	h.srv.Send("@batch=h;msgid=ml9 BATCH +m draft/multiline #eco")
	h.srv.Send("@batch=m :bob!b@host PRIVMSG #eco :part one")
	h.srv.Send("@batch=m :bob!b@host PRIVMSG #eco :part two")
	h.srv.Send("@batch=h BATCH -m")
	h.srv.Send("BATCH -h")

	h.waitFor("batchopen") // the chathistory batch
	inner, ok := h.waitFor("batchopen").(model.BatchOpen)
	if !ok || inner.Parent != "h" {
		t.Fatalf("inner batch open = %+v, want parent h", inner)
	}
	if !inner.Playback {
		t.Error("a batch opened inside a chathistory batch is playback")
	}
	if cl, ok := h.next().(model.BatchClose); !ok || cl.Ref != "m" {
		t.Fatalf("inner batch close = %+v", cl)
	}

	chunk, ok := h.next().(model.HistoryChunk)
	if !ok {
		t.Fatal("want a historychunk after the outer batch closed")
	}
	if len(chunk.Events) != 2 {
		t.Fatalf("chunk holds %d events, want the plain message and the merged one", len(chunk.Events))
	}
	merged, ok := chunk.Events[1].(model.PrivMsg)
	if !ok {
		t.Fatalf("second chunk event is %s", chunk.Events[1].EventName())
	}
	if merged.Text != "part one\npart two" {
		t.Errorf("merged text = %q", merged.Text)
	}
	if merged.MsgID != "ml9" {
		t.Errorf("merged msgid = %q, want ml9 from the inner BATCH", merged.MsgID)
	}
	if !merged.Playback {
		t.Error("multiline nested in chathistory is playback")
	}
	if merged.Batch != "h" {
		t.Errorf("merged batch = %q, want the enclosing chathistory batch", merged.Batch)
	}
}

func TestOversizedBatchStreamsRatherThanGrowingForever(t *testing.T) {
	// A batch is server-controlled and unbounded on the wire; holding one
	// whole is only safe because the hold is bounded.
	old := maxBatchEvents
	maxBatchEvents = 2
	t.Cleanup(func() { maxBatchEvents = old })

	h := newHarness(t, nil)
	h.register()

	handle, err := h.c.History(HistoryRequest{Target: "#eco", Direction: model.HistoryLatest, Limit: 100})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	h.srv.WaitFor("*CHATHISTORY*")
	h.srv.Send("@label=%s BATCH +h chathistory #eco", handle.Label())
	for _, id := range []string{"a", "b", "c", "d"} {
		h.srv.Send("@batch=h;msgid=%s :alice!a@host PRIVMSG #eco :%s", id, id)
	}
	h.srv.Send("BATCH -h")

	h.waitFor("batchopen")
	// The third and fourth messages did not fit, so they arrive live rather
	// than being held.
	for _, want := range []string{"c", "d"} {
		pm, ok := h.next().(model.PrivMsg)
		if !ok || pm.Text != want {
			t.Fatalf("overflow event = %+v, want the live message %q", pm, want)
		}
		if !pm.Playback {
			t.Errorf("an overflowed playback message is still playback")
		}
	}
	chunk, ok := h.next().(model.HistoryChunk)
	if !ok {
		t.Fatal("want a historychunk")
	}
	if len(chunk.Events) != 2 {
		t.Errorf("chunk holds %d events, want the bound", len(chunk.Events))
	}
	if chunk.Complete {
		t.Error("a truncated chunk must not claim to be complete")
	}
}

func TestUnknownAndBulkBatchesPassThroughLive(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	h.srv.Send("BATCH +n netjoin irc.a irc.b")
	h.srv.Send("@batch=n :carol!c@host JOIN #eco")
	h.srv.Send("BATCH -n")

	if open, ok := h.waitFor("batchopen").(model.BatchOpen); !ok || open.Type != "netjoin" {
		t.Fatalf("batch open = %+v", open)
	}
	jn, ok := h.next().(model.Join)
	if !ok {
		t.Fatal("a netjoin batch streams its contents live")
	}
	if jn.Batch != "n" {
		t.Errorf("join batch ref = %q, want n", jn.Batch)
	}
	// The v1 rule was "has a batch tag, therefore history". It is not.
	if jn.Playback {
		t.Error("a netjoin is live traffic, not playback")
	}
	if cl, ok := h.next().(model.BatchClose); !ok || cl.Ref != "n" {
		t.Fatalf("batch close = %+v", cl)
	}

	// An unregistered batch type is transparent live traffic, which is what
	// the spec requires of a type the client does not know.
	h.srv.Send("BATCH +z example.com/whatever")
	h.srv.Send("@batch=z :dave!d@host PRIVMSG #eco :still live")
	h.srv.Send("BATCH -z")
	h.waitFor("batchopen")
	pm, ok := h.next().(model.PrivMsg)
	if !ok || pm.Text != "still live" || pm.Playback || pm.Batch != "z" {
		t.Fatalf("unknown batch content = %+v", pm)
	}
}

func TestOurOwnMultilineEchoComesBackAsOneMessage(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	handle, err := h.c.SendMessage("#eco", "line a\nline b")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	open := h.srv.WaitFor("*BATCH +*")
	ref := strings.TrimPrefix(strings.Fields(untagged(open))[1], "+")
	h.srv.WaitFor("BATCH -" + ref)

	// echo-message replays the whole batch back with our label on it.
	h.srv.Send("@label=%s;msgid=srv9 BATCH +e draft/multiline #eco", handle.Label())
	h.srv.Send("@batch=e :eco!eco@test PRIVMSG #eco :line a")
	h.srv.Send("@batch=e :eco!eco@test PRIVMSG #eco :line b")
	h.srv.Send("BATCH -e")

	h.waitFor("batchopen")
	pm, ok := h.next().(model.PrivMsg)
	if !ok {
		t.Fatal("want one merged echo")
	}
	if pm.Label != handle.Label() {
		t.Errorf("echo label = %q, want %q", pm.Label, handle.Label())
	}
	if pm.MsgID != "srv9" {
		t.Errorf("echo msgid = %q, want srv9 — this is what makes our own message redactable", pm.MsgID)
	}
	if !pm.Sender.Self {
		t.Error("our own echo is not marked Self")
	}
	if pm.Text != "line a\nline b" {
		t.Errorf("echo text = %q", pm.Text)
	}
}
