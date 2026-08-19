package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestMessageRouting pins where every kind of message lands. It is the port of
// v1's handleMessage routing, including the two rules a beginner client always
// gets wrong: our own PM echoed from another device belongs in the *peer's*
// buffer, and a notice from the server itself belongs in the network log rather
// than in a buffer named after the server's hostname.
func TestMessageRouting(t *testing.T) {
	tests := []struct {
		name       string
		ev         model.Event
		wantBuffer string // "" means the server buffer
		wantSelf   bool
	}{
		{
			name:       "channel message",
			ev:         model.PrivMsg{Target: "#eco", Sender: user("alice"), Text: "hi"},
			wantBuffer: "#eco",
		},
		{
			name:       "statusmsg target routes to the channel",
			ev:         model.PrivMsg{Target: "@#eco", Sender: user("alice"), Text: "ops only"},
			wantBuffer: "#eco",
		},
		{
			name:       "query lands in the sender's buffer",
			ev:         model.PrivMsg{Target: testNick, Sender: user("bob"), Text: "got a sec?"},
			wantBuffer: "bob",
		},
		{
			name:       "our own query echo lands in the peer's buffer",
			ev:         model.PrivMsg{Target: "bob", Sender: selfUser(), Text: "sent from my phone"},
			wantBuffer: "bob",
			wantSelf:   true,
		},
		{
			name:       "server notice lands in the network log",
			ev:         model.Notice{Target: testNick, Sender: model.Sender{Nick: "irc.test"}, Text: "*** Checking ident"},
			wantBuffer: "",
		},
		{
			name:       "notice with no source lands in the network log",
			ev:         model.Notice{Target: testNick, Text: "AUTH :*** Looking up your hostname"},
			wantBuffer: "",
		},
		{
			name:       "service notice opens the service's buffer",
			ev:         model.Notice{Target: testNick, Sender: user("NickServ"), Text: "not registered"},
			wantBuffer: "NickServ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.apply(withNetwork(tc.ev, h.n))
			want := h.server()
			if tc.wantBuffer != "" {
				want = h.id(tc.wantBuffer)
			}
			added := mustFind[model.MessageAdded](t, h.changes)
			if added.Meta().Buffer != want {
				t.Fatalf("message landed in %v, want %v", added.Meta().Buffer, want)
			}
			if added.Message.Sender.Self != tc.wantSelf {
				t.Errorf("Sender.Self = %v, want %v", added.Message.Sender.Self, tc.wantSelf)
			}
			if h.s.buffer(h.id(testNick)) != nil {
				t.Error("a buffer was opened under our own nick")
			}
			if h.s.buffer(h.id("irc.test")) != nil {
				t.Error("a buffer was opened for the server's hostname")
			}
		})
	}
}

// withNetwork stamps a network id onto an event built without one.
func withNetwork(ev model.Event, n model.NetworkID) model.Event {
	switch e := ev.(type) {
	case model.PrivMsg:
		e.EventMeta.Network = n
		return e
	case model.Notice:
		e.EventMeta.Network = n
		return e
	}
	return ev
}

// TestMessageAttentionContract pins the tuple the whole attention model rests
// on: which messages count as unread, which raise a notification, and which do
// neither. It is v1's flag contract (regression_buffers_test.go) restated on
// Message + Change, and every classification slip in the store shows up here
// instead of as a missing badge or a notification storm.
func TestMessageAttentionContract(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	type want struct {
		kind      model.MessageKind
		highlight bool
		notify    bool
		unread    int
	}
	steps := []struct {
		name string
		ev   model.Event
		buf  model.BufferID
		want want
	}{
		{
			name: "plain chat is unread and quiet",
			ev:   model.PrivMsg{EventMeta: h.msg("f1"), Target: "#eco", Sender: user("alice"), Text: "the build is green"},
			buf:  ch,
			want: want{kind: model.MsgPrivmsg, unread: 1},
		},
		{
			name: "a mention highlights and notifies",
			ev:   model.PrivMsg{EventMeta: h.msg("f2"), Target: "#eco", Sender: user("alice"), Text: testNick + ": ship it"},
			buf:  ch,
			want: want{kind: model.MsgPrivmsg, highlight: true, notify: true, unread: 2},
		},
		{
			name: "a substring of our nick does not",
			ev:   model.PrivMsg{EventMeta: h.msg("f3"), Target: "#eco", Sender: user("alice"), Text: "the economy is fine"},
			buf:  ch,
			want: want{kind: model.MsgPrivmsg, unread: 3},
		},
		{
			name: "an action is a message like any other",
			ev:   model.PrivMsg{EventMeta: h.msg("f4"), Target: "#eco", Sender: user("alice"), Text: "waves", Action: true},
			buf:  ch,
			want: want{kind: model.MsgAction, unread: 4},
		},
		{
			name: "playback never badges, even when it mentions us",
			ev:   model.PrivMsg{EventMeta: withMsgID(h.playback(), "f5"), Target: "#eco", Sender: user("alice"), Text: testNick + ": this is old"},
			buf:  ch,
			want: want{kind: model.MsgPrivmsg, unread: 4},
		},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			out := h.apply(st.ev)
			added := mustFind[model.MessageAdded](t, out)
			if added.Message.Kind != st.want.kind {
				t.Errorf("kind = %v, want %v", added.Message.Kind, st.want.kind)
			}
			if added.Message.Highlight != st.want.highlight {
				t.Errorf("Highlight = %v, want %v", added.Message.Highlight, st.want.highlight)
			}
			if added.Notify != st.want.notify {
				t.Errorf("Notify = %v, want %v", added.Notify, st.want.notify)
			}
			if unread, _ := h.s.Unread(st.buf); unread != st.want.unread {
				t.Errorf("unread = %d, want %d", unread, st.want.unread)
			}
		})
	}

	// a private message is a highlight without naming us; a notice is not, or
	// every routine NickServ line raises a desktop popup
	out := h.apply(model.PrivMsg{EventMeta: h.msg("p1"), Target: testNick, Sender: user("bob"), Text: "hey"})
	pm := mustFind[model.MessageAdded](t, out)
	if !pm.Message.Highlight || !pm.Notify {
		t.Errorf("a private message must highlight and notify: %+v", pm)
	}
	out = h.apply(model.Notice{EventMeta: h.msg("p2"), Target: testNick, Sender: user("NickServ"), Text: "Your account is not registered."})
	svc := mustFind[model.MessageAdded](t, out)
	if svc.Message.Highlight || svc.Notify {
		t.Errorf("a service notice must not notify: %+v", svc)
	}

	// status lines never count and never notify
	out = h.apply(model.Join{EventMeta: h.meta(), Channel: "#eco", Sender: user("carol")})
	status := mustFind[model.MessageAdded](t, out)
	if status.Message.Kind != model.MsgStatus || status.Notify {
		t.Errorf("join line = %+v, want a quiet status message", status)
	}
	if _, ok := findChange[model.UnreadChanged](out); ok {
		t.Error("a join line moved the unread count")
	}
	if _, hl := h.s.Unread(ch); hl != 1 {
		t.Errorf("highlight count = %d, want the single mention", hl)
	}
}

func withMsgID(m model.EventMeta, id string) model.EventMeta {
	m.MsgID = id
	return m
}

// TestNotifyLevels pins the per-buffer notification levels and the focused
// buffer suppression (blueprint §4.4: level, then highlight, then focus).
func TestNotifyLevels(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	plain := model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "hello"}
	mention := model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: testNick + " hi"}

	notifies := func(ev model.Event) bool {
		added := mustFind[model.MessageAdded](t, h.apply(ev))
		return added.Notify
	}

	if notifies(plain) {
		t.Error("plain chat notified at the default level")
	}
	if !notifies(mention) {
		t.Error("a mention did not notify at the default level")
	}

	h.s.SetNotifyLevel(ch, NotifyAll)
	if !notifies(plain) {
		t.Error("plain chat did not notify at NotifyAll")
	}

	h.s.SetNotifyLevel(ch, NotifyMute)
	if notifies(mention) {
		t.Error("a mention notified in a muted buffer")
	}

	h.s.SetNotifyLevel(ch, NotifyDefault)
	if got := h.s.NotifyLevelFor(ch); got != NotifyMentions {
		t.Errorf("resolved level = %v, want the network default", got)
	}
	h.s.SetNetworkNotifyLevel(h.n, NotifyMute)
	if notifies(mention) {
		t.Error("a mention notified with the network muted")
	}
	h.s.SetNetworkNotifyLevel(h.n, NotifyMentions)

	// the buffer the user is looking at never notifies
	h.s.Focus(ch)
	if notifies(mention) {
		t.Error("the focused buffer raised a notification")
	}
	h.s.Blur(h.n)
	if !notifies(mention) {
		t.Error("blurring did not restore notifications")
	}
}

// TestFocusMovesTheReadMarker pins that unread derives from the marker: opening
// a buffer clears it, and messages after the marker start counting again.
func TestFocusMovesTheReadMarker(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	for _, text := range []string{"one", "two", "three"} {
		h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: text})
	}
	if unread, _ := h.s.Unread(ch); unread != 3 {
		t.Fatalf("unread = %d, want 3", unread)
	}
	out := h.s.Focus(ch)
	moved := mustFind[model.ReadMarkerMoved](t, out)
	if moved.Remote {
		t.Error("a local focus was reported as a remote marker move")
	}
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Errorf("unread after focus = %d, want 0", unread)
	}
	h.s.Blur(h.n)
	h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "four"})
	if unread, _ := h.s.Unread(ch); unread != 1 {
		t.Errorf("unread after a new message = %d, want 1", unread)
	}
}

// TestRemoteMarkerWins pins ADR-3's conflict rule: markers only move forward,
// so a MARKREAD from a device that was behind cannot un-read a buffer.
func TestRemoteMarkerWins(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	for i := 0; i < 3; i++ {
		h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: "x"})
	}
	msgs := h.s.Messages(ch)
	newest := msgs[len(msgs)-1].Time

	out := h.apply(model.MarkRead{EventMeta: h.meta(), Target: "#eco", Timestamp: newest})
	moved := mustFind[model.ReadMarkerMoved](t, out)
	if !moved.Remote {
		t.Error("a MARKREAD was not reported as remote")
	}
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Errorf("unread = %d, want 0 after the remote marker", unread)
	}
	// a stale marker from a device that was behind changes nothing
	out = h.apply(model.MarkRead{EventMeta: h.meta(), Target: "#eco", Timestamp: newest.Add(-time.Hour)})
	if _, ok := findChange[model.ReadMarkerMoved](out); ok {
		t.Errorf("a stale marker moved the read position: %v", changeNames(out))
	}
}

// TestEchoResolvesByLabel is MUST 3. v1 matched the server's echo against the
// text it had rendered, so two identical messages in a row were
// indistinguishable and our own lines never carried a msgid. Here the label
// resolves the row in place: same MessageID, real msgid, server time.
func TestEchoResolvesByLabel(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")

	id1, out := h.s.Echo(ch, Outgoing{Label: "l1", Text: "on it"})
	added := mustFind[model.MessageAdded](t, out)
	if added.Message.Pending != model.PendingEcho {
		t.Fatalf("echo pending state = %v, want PendingEcho", added.Message.Pending)
	}
	if added.Notify {
		t.Error("our own message notified us")
	}
	// the same text again: v1 would have eaten one of the two echoes
	id2, _ := h.s.Echo(ch, Outgoing{Label: "l2", Text: "on it"})
	if id1 == id2 {
		t.Fatal("two sends share one message id")
	}

	serverTime := baseTime.Add(time.Minute)
	out = h.apply(model.PrivMsg{
		EventMeta: model.EventMeta{Network: h.n, MsgID: "srv1", Label: "l1", Time: serverTime, ServerTime: true},
		Target:    "#eco", Sender: selfUser(), Text: "on it",
	})
	if countChanges[model.MessageAdded](out) != 0 {
		t.Fatalf("the echo added a second line: %v", changeNames(out))
	}
	resolved := mustFind[model.MessageResolved](t, out)
	if resolved.ID != id1 {
		t.Errorf("resolved id = %d, want the first pending message %d", resolved.ID, id1)
	}
	if resolved.Message.MsgID != "srv1" {
		t.Errorf("resolved msgid = %q, want srv1 - own messages must become redactable", resolved.Message.MsgID)
	}
	if !resolved.Message.Time.Equal(serverTime) || !resolved.Message.ServerTime {
		t.Errorf("resolved time = %v (server=%v), want the server's", resolved.Message.Time, resolved.Message.ServerTime)
	}
	if resolved.Message.Pending != model.Delivered {
		t.Errorf("resolved pending = %v, want Delivered", resolved.Message.Pending)
	}
	if got := len(h.s.Messages(ch)); got != 4 { // "You joined", "alice joined", two echoes
		t.Errorf("buffer holds %d messages, want the two join lines plus the two sends", got)
	}

	// the second one resolves independently
	out = h.apply(model.PrivMsg{
		EventMeta: model.EventMeta{Network: h.n, MsgID: "srv2", Label: "l2", Time: serverTime, ServerTime: true},
		Target:    "#eco", Sender: selfUser(), Text: "on it",
	})
	if got := mustFind[model.MessageResolved](t, out); got.ID != id2 {
		t.Errorf("second resolve hit %d, want %d", got.ID, id2)
	}
}

// TestUnlabeledSelfEchoStillArrives pins the fallback: a network without
// labeled-response echoes our message with no label, and it must appear rather
// than vanish. Deduplication then rests on msgid alone.
func TestUnlabeledSelfEchoStillArrives(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	out := h.apply(model.PrivMsg{EventMeta: h.msg("e1"), Target: "#eco", Sender: selfUser(), Text: "hello"})
	added := mustFind[model.MessageAdded](t, out)
	if !added.Message.Sender.Self {
		t.Error("an unlabeled self echo lost its Self flag")
	}
	if added.Notify {
		t.Error("our own message notified us")
	}
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Errorf("our own message counted as unread (%d)", unread)
	}
}

// TestPendingFails pins the FAIL half of the labeled ledger: the row stays,
// marked failed, so the text is not lost.
func TestPendingFails(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	id, _ := h.s.Echo(ch, Outgoing{Label: "l9", Text: "does not send"})

	out := h.s.Fail(h.n, "l9", "MSG_TOO_LONG", "line too long")
	failed := mustFind[model.MessageFailed](t, out)
	if failed.ID != id || failed.Code != "MSG_TOO_LONG" {
		t.Errorf("MessageFailed = %+v, want the pending message and its code", failed)
	}
	m, ok := h.s.Message(ch, id)
	if !ok || m.Pending != model.PendingFailed {
		t.Errorf("message pending state = %v, want PendingFailed", m.Pending)
	}
	if out := h.s.Fail(h.n, "l9", "X", "y"); out != nil {
		t.Errorf("failing an unknown label emitted %v", changeNames(out))
	}
}

// TestDisconnectFailsPendingMessages pins that a message waiting for an echo
// that will never come stops spinning.
func TestDisconnectFailsPendingMessages(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	id, _ := h.s.Echo(ch, Outgoing{Label: "l1", Text: "in flight"})
	out := h.apply(model.ConnState{EventMeta: h.meta(), Status: model.ConnDisconnected, Err: "connection reset"})
	failed := mustFind[model.MessageFailed](t, out)
	if failed.ID != id || failed.Code != "DISCONNECTED" {
		t.Errorf("MessageFailed = %+v, want the in-flight message", failed)
	}
}

// TestMsgIDDedupePerBuffer pins exact dedupe: the same msgid twice in one
// buffer is one message, and the same msgid in two buffers is two messages
// (servers reuse ids across targets in relayed setups).
func TestMsgIDDedupePerBuffer(t *testing.T) {
	h := newHarness(t)
	h.joinChannel("#eco")
	h.joinChannel("#other")

	first := h.apply(model.PrivMsg{EventMeta: h.msg("dup"), Target: "#eco", Sender: user("alice"), Text: "once"})
	if countChanges[model.MessageAdded](first) != 1 {
		t.Fatalf("first delivery = %v", changeNames(first))
	}
	again := h.apply(model.PrivMsg{EventMeta: h.msg("dup"), Target: "#eco", Sender: user("alice"), Text: "once"})
	if len(again) != 0 {
		t.Errorf("the same msgid was displayed twice: %v", changeNames(again))
	}
	other := h.apply(model.PrivMsg{EventMeta: h.msg("dup"), Target: "#other", Sender: user("alice"), Text: "once"})
	if countChanges[model.MessageAdded](other) != 1 {
		t.Errorf("dedupe leaked across buffers: %v", changeNames(other))
	}
}

// TestReopenedBufferRefetches inverts a v1 characterization test: v1 kept
// seenIDs and historyRequested for a buffer the user had closed, so reopening
// it swallowed the replay. Closing a buffer here drops all of it.
func TestReopenedBufferRefetches(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco")
	h.apply(model.PrivMsg{EventMeta: h.msg("old"), Target: "#eco", Sender: user("alice"), Text: "history"})
	h.s.MarkHistoryRequested(ch)
	if h.s.NeedsHistory(ch) {
		t.Fatal("history was still wanted right after it was requested")
	}
	h.s.CloseBuffer(ch)

	reopened, _ := h.s.OpenBuffer(h.n, "#eco")
	if !h.s.NeedsHistory(reopened) {
		t.Error("a reopened buffer inherited the old fetch guard")
	}
	out := h.apply(model.PrivMsg{EventMeta: h.msg("old"), Target: "#eco", Sender: user("alice"), Text: "history"})
	if countChanges[model.MessageAdded](out) != 1 {
		t.Errorf("a reopened buffer swallowed the replay: %v", changeNames(out))
	}
}

// TestIgnoreSuppressesMessages pins per-network hostmask ignores: an ignored
// sender's messages never reach a buffer, our own never match, and un-ignoring
// takes effect on the next message.
func TestIgnoreSuppressesMessages(t *testing.T) {
	h := newHarness(t)
	h.joinChannel("#eco")
	h.s.AddIgnore(h.n, "spammer!*@*")

	if out := h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("spammer"), Text: "buy"}); len(out) != 0 {
		t.Errorf("an ignored message reached a buffer: %v", changeNames(out))
	}
	if out := h.apply(model.PrivMsg{EventMeta: h.meta(), Target: testNick, Sender: user("spammer"), Text: "buy"}); len(out) != 0 {
		t.Errorf("an ignored PM opened a buffer: %v", changeNames(out))
	}
	if h.s.buffer(h.id("spammer")) != nil {
		t.Error("an ignored sender got a query buffer")
	}
	if !h.s.RemoveIgnore(h.n, "spammer!*@*") {
		t.Fatal("RemoveIgnore did not remove the mask it was given")
	}
	out := h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("spammer"), Text: "hello again"})
	if countChanges[model.MessageAdded](out) != 1 {
		t.Errorf("un-ignoring did not take effect: %v", changeNames(out))
	}
}

// TestRedactLeavesATombstone pins draft/message-redaction: the message becomes
// a tombstone in place, and a redacted mention stops counting.
func TestRedactLeavesATombstone(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("r1"), Target: "#eco", Sender: user("alice"), Text: testNick + " look"})
	if unread, hl := h.s.Unread(ch); unread != 1 || hl != 1 {
		t.Fatalf("counts before redaction = %d/%d, want 1/1", unread, hl)
	}
	out := h.apply(model.Redact{
		EventMeta: h.meta(), Target: "#eco", Sender: user("alice"),
		TargetMsgID: "r1", Reason: "typo",
	})
	red := mustFind[model.MessageRedacted](t, out)
	if red.MsgID != "r1" || red.ByNick != "alice" || red.Reason != "typo" {
		t.Errorf("MessageRedacted = %+v", red)
	}
	m, _ := h.s.Message(ch, red.ID)
	if m.Kind != model.MsgTombstone || !m.Redacted || m.Text != "" {
		t.Errorf("redacted message = %+v, want an empty tombstone", m)
	}
	if unread, hl := h.s.Unread(ch); unread != 0 || hl != 0 {
		t.Errorf("counts after redaction = %d/%d, want 0/0", unread, hl)
	}
	if out := h.apply(model.Redact{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), TargetMsgID: "r1"}); len(out) != 0 {
		t.Errorf("redacting twice emitted %v", changeNames(out))
	}
}

// TestHistoryChunkArrivesAsOneBlock pins the chathistory path: the enclosed
// events run through the ordinary reducers, but their messages come out as one
// HistoryLoaded rather than a hundred MessageAdded, and none of them badge.
func TestHistoryChunkArrivesAsOneBlock(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	pb := h.playback()
	out := h.apply(model.HistoryChunk{
		EventMeta: pb,
		Target:    "#eco",
		Direction: model.HistoryBefore,
		Complete:  false,
		Events: []model.Event{
			model.PrivMsg{EventMeta: withMsgID(pb, "h1"), Target: "#eco", Sender: user("alice"), Text: "older"},
			model.PrivMsg{EventMeta: withMsgID(pb, "h2"), Target: "#eco", Sender: user("alice"), Text: testNick + " mentioned you"},
		},
	})
	if countChanges[model.MessageAdded](out) != 0 {
		t.Fatalf("history leaked as live messages: %v", changeNames(out))
	}
	loaded := mustFind[model.HistoryLoaded](t, out)
	if len(loaded.Messages) != 2 {
		t.Fatalf("HistoryLoaded carries %d messages, want 2", len(loaded.Messages))
	}
	// The block is what the UI draws, in the order it draws it. Counting the
	// messages says nothing about that, and a chunk assembled backwards is a
	// scrollback that reads backwards - so pin the order, not the count.
	// (Found at M7 verification by a mutation the whole suite let through.)
	if got := []string{loaded.Messages[0].Text, loaded.Messages[1].Text}; got[0] != "older" || got[1] != testNick+" mentioned you" {
		t.Errorf("HistoryLoaded messages = %q, want them in the order the chunk carried them", got)
	}
	if loaded.Direction != model.HistoryBefore || loaded.Complete {
		t.Errorf("HistoryLoaded = %+v, want a partial BEFORE block", loaded)
	}
	for _, m := range loaded.Messages {
		if !m.Playback || m.Highlight {
			t.Errorf("history message %+v is not marked playback / was highlighted", m)
		}
	}
	if unread, hl := h.s.Unread(ch); unread != 0 || hl != 0 {
		t.Errorf("history badged the buffer: %d/%d", unread, hl)
	}
	// replaying the same chunk changes nothing: msgid dedupe is exact
	out = h.apply(model.HistoryChunk{
		EventMeta: pb, Target: "#eco", Direction: model.HistoryBefore,
		Events: []model.Event{
			model.PrivMsg{EventMeta: withMsgID(pb, "h1"), Target: "#eco", Sender: user("alice"), Text: "older"},
		},
	})
	loaded = mustFind[model.HistoryLoaded](t, out)
	if len(loaded.Messages) != 0 {
		t.Errorf("a repeated chunk duplicated %d messages", len(loaded.Messages))
	}
}

// TestTypingExpiresOnTheInjectedClock pins that typing indicators die without a
// timer goroutine: the store owns none, so expiry runs off the clock the test
// handed it.
func TestTypingExpiresOnTheInjectedClock(t *testing.T) {
	now := baseTime
	h := newHarness(t, func(o *Options) {
		o.Clock = func() time.Time { return now }
		o.TypingTimeout = 6 * time.Second
	})
	ch := h.joinChannel("#eco", "alice")

	out := h.apply(model.Typing{EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), State: model.TypingActive})
	typing := mustFind[model.TypingChanged](t, out)
	if typing.Nick != "alice" || typing.State != model.TypingActive {
		t.Fatalf("TypingChanged = %+v", typing)
	}
	if got := h.s.Typing(ch); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("Typing = %v, want [alice]", got)
	}
	// our own typing is not shown back to us
	if out := h.apply(model.Typing{EventMeta: h.meta(), Target: "#eco", Sender: selfUser(), State: model.TypingActive}); len(out) != 0 {
		t.Errorf("our own typing was reported: %v", changeNames(out))
	}

	now = now.Add(7 * time.Second)
	out = h.apply(model.PrivMsg{EventMeta: h.meta(), Target: "#eco", Sender: user("bob"), Text: "hi"})
	done, ok := findChange[model.TypingChanged](out)
	if !ok || done.State != model.TypingDone {
		t.Fatalf("stale typing was not expired: %v", changeNames(out))
	}
	if got := h.s.Typing(ch); len(got) != 0 {
		t.Errorf("Typing = %v, want empty after expiry", got)
	}
}
