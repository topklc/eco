package state

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The store's half of M9: what happens when messages arrive from a FILE rather
// than from a wire. Every test here is about one of the three things that makes
// that different — the messages are older than what is already held, their ids
// were minted by somebody else, and the read marker they belong to may have
// been restored before there was anything for it to point at.

// archived builds a row as the archive hands one back: an id that IS its rowid,
// so ordering survives the trip to disk.
func archived(id uint64, msgid, sender, text string, at time.Time) model.Message {
	return model.Message{
		ID:     model.MessageID(id),
		MsgID:  msgid,
		Time:   at,
		Sender: model.Sender{Nick: sender},
		Kind:   model.MsgPrivmsg,
		Text:   text,
	}
}

// texts is a buffer's window as a reader sees it, in order.
func texts(ms []model.Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Text)
	}
	return out
}

// TestRestoreMessagesPrependsInOrder pins the shape of a restore: the block
// goes ABOVE the window, in its own order, and the change that reports it is
// the same HistoryLoaded a CHATHISTORY BEFORE reply produces — so the UI has
// one way to learn about older rows and not two.
func TestRestoreMessagesPrependsInOrder(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("live"), Target: "#eco", Sender: user("alice"), Text: "today"})

	cs := h.s.RestoreMessages(ch, []model.Message{
		archived(10, "a1", "alice", "yesterday", baseTime.Add(-2*time.Hour)),
		archived(11, "a2", "bob", "later yesterday", baseTime.Add(-time.Hour)),
	}, false)

	loaded := mustFind[model.HistoryLoaded](t, cs)
	if loaded.Direction != model.HistoryBefore {
		t.Errorf("direction = %v, want HistoryBefore: the UI decides prepend-or-append from it", loaded.Direction)
	}
	if loaded.Complete {
		t.Error("Complete was true for a block the caller said there was more before")
	}
	got := texts(h.s.Messages(ch))
	if len(got) < 3 || got[0] != "yesterday" || got[1] != "later yesterday" {
		t.Errorf("window = %q, want the restored block first, in order", got)
	}
	if got[len(got)-1] != "today" {
		t.Errorf("window = %q, want the live message still last", got)
	}
	// The block is addressable afterwards: a redaction or a resolve names a
	// message by id, and a restore that did not index them would lose them.
	if m, ok := h.s.Message(ch, model.MessageID(10)); !ok || m.Text != "yesterday" {
		t.Errorf("restored message 10 = %+v ok=%v, want it indexed by its id", m, ok)
	}
}

// TestRestoreMessagesDedupes pins the merge rule (ADR-3): the same row restored
// twice — by id or by msgid — is one message. Paging up and then reconnecting
// asks for overlapping ranges as a matter of course.
func TestRestoreMessagesDedupes(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")

	rows := []model.Message{archived(10, "a1", "alice", "once", baseTime.Add(-time.Hour))}
	h.s.RestoreMessages(ch, rows, false)
	if cs := h.s.RestoreMessages(ch, rows, false); len(cs) != 0 {
		t.Errorf("restoring the same rows again produced %v", changeNames(cs))
	}
	// A different id carrying the same msgid is the same message: the archive
	// was rebuilt, or the row came back from the server instead.
	h.s.RestoreMessages(ch, []model.Message{
		archived(99, "a1", "alice", "once", baseTime.Add(-time.Hour)),
	}, false)

	n := 0
	for _, m := range h.s.Messages(ch) {
		if m.Text == "once" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the restored message is in the window %d times, want once", n)
	}

	// And by id alone. A row with no msgid — a join notice a previous session
	// wrote — cannot be recognised by the merge key, so the local id is the
	// only thing standing between it and a second copy. Without this case the
	// msgid check answers for both and the id check can be deleted unnoticed.
	bare := []model.Message{archived(12, "", "alice", "no msgid here", baseTime.Add(-30*time.Minute))}
	h.s.RestoreMessages(ch, bare, false)
	if cs := h.s.RestoreMessages(ch, bare, false); len(cs) != 0 {
		t.Errorf("restoring a msgid-less row again produced %v", changeNames(cs))
	}
	n = 0
	for _, m := range h.s.Messages(ch) {
		if m.Text == "no msgid here" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the msgid-less row is in the window %d times, want once", n)
	}
}

// TestRestoreMessagesCountAgainstTheRestoredMarker is the milestone's unread
// promise, in the one place it is decided. The marker is restored first, before
// there is a single message for it to point at — which is exactly the order a
// cold start produces — and the count still has to come out right when the
// messages land.
func TestRestoreMessagesCountAgainstTheRestoredMarker(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")

	// The marker arrives with nothing to resolve against.
	h.s.RestoreMarker(ch, "a2", baseTime.Add(-90*time.Minute))
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Fatalf("unread = %d with an empty window", unread)
	}

	h.s.RestoreMessages(ch, []model.Message{
		archived(10, "a1", "alice", "read", baseTime.Add(-2*time.Hour)),
		archived(11, "a2", "alice", "also read", baseTime.Add(-90*time.Minute)),
		archived(12, "a3", "alice", "unread", baseTime.Add(-time.Hour)),
		archived(13, "a4", "bob", "also unread", baseTime.Add(-30*time.Minute)),
	}, true)

	unread, _ := h.s.Unread(ch)
	if unread != 2 {
		t.Errorf("unread after restoring = %d, want 2 (the messages after the marker)", unread)
	}
	// Our own messages never count, whichever side of the disk they came from.
	h.s.RestoreMessages(ch, []model.Message{
		func() model.Message {
			m := archived(9, "a0", testNick, "mine", baseTime.Add(-3*time.Hour))
			m.Sender.Self = true
			return m
		}(),
	}, true)
	if again, _ := h.s.Unread(ch); again != 2 {
		t.Errorf("unread = %d after restoring one of our own messages, want 2", again)
	}
}

// TestRestoredMessagesAreNotPlayback pins the flag that decides whether a
// restored message counts at all. Playback means "the server is saying this
// again" and is what stops a replay from badging a buffer; a message read back
// off our own disk is the buffer's contents, not a replay of them. Marking
// these Playback silently zeroes every restored badge — the exact failure this
// milestone exists to fix.
func TestRestoredMessagesAreNotPlayback(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")
	h.s.RestoreMessages(ch, []model.Message{
		archived(10, "a1", "alice", "from disk", baseTime.Add(-time.Hour)),
	}, true)

	m, ok := h.s.Message(ch, model.MessageID(10))
	if !ok {
		t.Fatal("the restored message is not in the window")
	}
	if m.Playback {
		t.Error("a restored message is flagged Playback, so it can never count as unread")
	}
	if unread, _ := h.s.Unread(ch); unread != 1 {
		t.Errorf("unread = %d, want 1", unread)
	}
}

// TestRestoreMarkerNeverGoesBackwards pins ADR-3 on the path a restart takes: a
// stored marker is older than one this session has already moved forward, and
// reading it must not un-read the buffer.
func TestRestoreMarkerNeverGoesBackwards(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("m1"), Target: "#eco", Sender: user("alice"), Text: "one"})
	h.apply(model.PrivMsg{EventMeta: h.msg("m2"), Target: "#eco", Sender: user("alice"), Text: "two"})
	h.s.MarkReadLatest(ch)
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Fatalf("unread = %d after reading everything", unread)
	}

	if cs := h.s.RestoreMarker(ch, "m1", baseTime.Add(-time.Hour)); len(cs) != 0 {
		t.Errorf("an older stored marker moved the read position: %v", changeNames(cs))
	}
	if unread, _ := h.s.Unread(ch); unread != 0 {
		t.Errorf("unread = %d after restoring an older marker, want 0", unread)
	}
}

// TestMarkerMsgIDBeatsItsTimestamp pins the resolution rule. A marker carries
// both a msgid (exact) and a timestamp (portable, and only accurate to the
// millisecond). Three messages can share one millisecond, so taking the later
// of the two answers marks messages read that the user has not seen — which is
// how an unread count comes back as zero after a restart.
func TestMarkerMsgIDBeatsItsTimestamp(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice")
	same := baseTime.Add(-time.Hour)
	h.s.RestoreMessages(ch, []model.Message{
		archived(10, "a1", "alice", "one", same),
		archived(11, "a2", "alice", "two", same),
		archived(12, "a3", "alice", "three", same),
	}, true)

	h.s.RestoreMarker(ch, "a1", same)
	if unread, _ := h.s.Unread(ch); unread != 2 {
		t.Errorf("unread = %d, want 2: the marker names a1 exactly, and the two after it are unread", unread)
	}

	// The same thing in the order a cold start actually produces: the marker is
	// read out of the buffers table before the messages are read out of the
	// messages table, so it resolves against an EMPTY window and has to be
	// re-resolved when the window arrives. Without that, everything falls back
	// to the timestamp — and every message in that millisecond is read.
	cold := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	other := cold.joinChannel("#eco", "alice")
	cold.s.RestoreMarker(other, "a1", same)
	cold.s.RestoreMessages(other, []model.Message{
		archived(10, "a1", "alice", "one", same),
		archived(11, "a2", "alice", "two", same),
		archived(12, "a3", "alice", "three", same),
	}, true)
	if unread, _ := cold.s.Unread(other); unread != 2 {
		t.Errorf("unread on a cold start = %d, want 2: the marker was not re-resolved against the restored window", unread)
	}
}

// TestRestoreStaysWithinItsHeadroom pins RestoreHeadroom. The scrollback cap
// governs the live path; a reader paging backwards has asked for more than
// that, and refusing because the window is already full makes scroll-up stop
// working at exactly the point it becomes useful. It is still bounded, though —
// an afternoon of paging is not an excuse to hold the whole file.
func TestRestoreStaysWithinItsHeadroom(t *testing.T) {
	const cap = 4
	h := newHarness(t, func(o *Options) {
		o.IDs = model.NewIDSeq(1000)
		o.Scrollback = cap
	})
	ch := h.joinChannel("#eco", "alice")
	for i := range 6 {
		h.apply(model.PrivMsg{EventMeta: h.msg("live" + string(rune('a'+i))),
			Target: "#eco", Sender: user("alice"), Text: "live"})
	}
	if got := len(h.s.Messages(ch)); got != cap {
		t.Fatalf("the live path kept %d messages with a cap of %d", got, cap)
	}

	// A full window still takes a page: this is the whole point.
	rows := make([]model.Message, 0, 10)
	for i := range 10 {
		rows = append(rows, archived(uint64(100+i), "old"+string(rune('a'+i)), "bob",
			"older", baseTime.Add(-time.Duration(10-i)*time.Minute)))
	}
	if cs := h.s.RestoreMessages(ch, rows, false); len(cs) == 0 {
		t.Fatal("a page was refused because the window was already at its cap")
	}
	if got := len(h.s.Messages(ch)); got != cap+len(rows) {
		t.Errorf("window = %d messages, want %d: the page landed whole", got, cap+len(rows))
	}
	// And the ceiling is real: past cap+RestoreHeadroom nothing more is taken.
	huge := make([]model.Message, 0, RestoreHeadroom+10)
	for i := range RestoreHeadroom + 10 {
		huge = append(huge, archived(uint64(10_000+i), "h"+string(rune(i)), "bob", "ancient",
			baseTime.Add(-time.Duration(RestoreHeadroom+20-i)*time.Hour)))
	}
	h.s.RestoreMessages(ch, huge, false)
	if got := len(h.s.Messages(ch)); got > cap+RestoreHeadroom {
		t.Errorf("window grew to %d, past the %d ceiling", got, cap+RestoreHeadroom)
	}
}

// TestOldestAnchoredPicksARowTheArchiveKnows pins the anchor rule for
// scroll-up. An id above base was minted by THIS store and means nothing on
// disk; asking the archive for "everything before row 4182" with one of those
// returns somebody else's conversation.
func TestOldestAnchoredPicksARowTheArchiveKnows(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice") // leaves a status line with no msgid
	h.apply(model.PrivMsg{EventMeta: h.msg("m1"), Target: "#eco", Sender: user("alice"), Text: "live"})

	got, ok := h.s.OldestAnchored(ch, 100)
	if !ok {
		t.Fatal("no anchor in a window that holds a message with a msgid")
	}
	if got.MsgID != "m1" {
		t.Errorf("anchor = %+v, want the oldest message carrying a msgid", got)
	}
	// Once a restored row is in the window it is older, and its id is a row
	// identity, so it becomes the anchor.
	h.s.RestoreMessages(ch, []model.Message{
		archived(10, "", "alice", "from disk", baseTime.Add(-time.Hour)),
	}, true)
	got, ok = h.s.OldestAnchored(ch, 100)
	if !ok || got.ID != 10 {
		t.Errorf("anchor = %+v ok=%v, want the restored row 10", got, ok)
	}
	// A window with nothing anchorable says so rather than guessing.
	empty := h.id("#nowhere")
	h.s.OpenBuffer(h.n, "#nowhere")
	if _, ok := h.s.OldestAnchored(empty, 100); ok {
		t.Error("an empty buffer offered an anchor")
	}
}

// TestOldestNamedPicksARowTheServerKnows pins the other anchor rule — the one
// the WIRE uses. A buffer that was just joined holds eco's own status line as
// its oldest row, and that line has no msgid and carries eco's local clock, so
// anchoring `CHATHISTORY BEFORE` on it turns an exact question into a
// timestamp one asked in a timebase the two ends do not share.
func TestOldestNamedPicksARowTheServerKnows(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IDs = model.NewIDSeq(100) })
	ch := h.joinChannel("#eco", "alice") // leaves a status line with no msgid
	h.apply(model.PrivMsg{EventMeta: h.msg("m1"), Target: "#eco", Sender: user("alice"), Text: "live"})

	// Oldest is that status line; OldestNamed is not.
	if first, ok := h.s.Oldest(ch); !ok || first.MsgID != "" {
		t.Fatalf("the fixture's oldest row is %+v, want eco's own status line", first)
	}
	got, ok := h.s.OldestNamed(ch)
	if !ok {
		t.Fatal("no named anchor in a window that holds a message with a msgid")
	}
	if got.MsgID != "m1" {
		t.Errorf("wire anchor = %+v, want the oldest message carrying a msgid", got)
	}
	// A restored row with a msgid is older still, so it takes over.
	h.s.RestoreMessages(ch, []model.Message{
		archived(10, "a0", "alice", "from disk", baseTime.Add(-time.Hour)),
	}, true)
	if got, ok := h.s.OldestNamed(ch); !ok || got.MsgID != "a0" {
		t.Errorf("wire anchor = %+v ok=%v, want the restored row's msgid", got, ok)
	}
	// A buffer holding nothing the server named says so, so the caller can
	// fall back rather than send an empty selector.
	h.s.OpenBuffer(h.n, "#nowhere")
	if _, ok := h.s.OldestNamed(h.id("#nowhere")); ok {
		t.Error("an empty buffer offered a wire anchor")
	}
}

// TestHistoryBeforeChunkLandsAboveTheWindow pins the wire half of scroll-up.
// The reducers append, because appending is what every other message does; a
// BEFORE chunk is the page ABOVE the reader and has to end up there.
func TestHistoryBeforeChunkLandsAboveTheWindow(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.PrivMsg{EventMeta: h.msg("m2"), Target: "#eco", Sender: user("alice"), Text: "newer"})

	h.apply(model.HistoryChunk{
		EventMeta: h.meta(), Target: "#eco", Direction: model.HistoryBefore, Complete: true,
		Events: []model.Event{
			model.PrivMsg{EventMeta: h.playback(), Target: "#eco", Sender: user("bob"), Text: "older"},
		},
	})

	got := texts(h.s.Messages(ch))
	if len(got) == 0 || got[0] != "older" {
		t.Errorf("window = %q, want the fetched page first", got)
	}
	if got[len(got)-1] != "newer" {
		t.Errorf("window = %q, want the live message still last", got)
	}
}

// TestHistoryLatestChunkStillAppends is the other side of that rule: the reply
// to the LATEST request a join issues is not a page above anything, and moving
// it would put a channel's own scrollback in front of its join notice.
func TestHistoryLatestChunkStillAppends(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.apply(model.HistoryChunk{
		EventMeta: h.meta(), Target: "#eco", Direction: model.HistoryLatest, Complete: true,
		Events: []model.Event{
			model.PrivMsg{EventMeta: h.playback(), Target: "#eco", Sender: user("bob"), Text: "scrollback"},
		},
	})
	got := texts(h.s.Messages(ch))
	if len(got) == 0 || got[len(got)-1] != "scrollback" {
		t.Errorf("window = %q, want the LATEST reply appended", got)
	}
}

// TestHistoryTargetOpensQueriesOnly pins the TARGETS rule: a private message
// that arrived while we were offline has nobody to announce it, so its buffer
// is opened here — and a channel is NOT, because a channel announces itself by
// being rejoined and reopening every channel the user ever left, on every
// reconnect, is the failure that rule exists to avoid.
func TestHistoryTargetOpensQueriesOnly(t *testing.T) {
	h := newHarness(t)

	cs := h.apply(model.HistoryTarget{EventMeta: h.meta(), Target: "carol", At: baseTime})
	created := mustFind[model.BufferCreated](t, cs)
	if created.Info.Name != "carol" {
		t.Errorf("opened %q, want carol", created.Info.Name)
	}
	if !created.Restored {
		t.Error("the buffer was not marked Restored, so the UI would steal focus for it")
	}

	if cs := h.apply(model.HistoryTarget{EventMeta: h.meta(), Target: "#left", At: baseTime}); len(cs) != 0 {
		t.Errorf("a channel named by TARGETS opened a buffer: %v", changeNames(cs))
	}
	if _, ok := h.s.BufferInfo(h.id("#left")); ok {
		t.Error("a channel named by TARGETS exists as a buffer")
	}
	// Our own nick is not a conversation with ourselves.
	if cs := h.apply(model.HistoryTarget{EventMeta: h.meta(), Target: testNick, At: baseTime}); len(cs) != 0 {
		t.Errorf("TARGETS naming our own nick opened a buffer: %v", changeNames(cs))
	}
}

// TestForgetListDropsTheResidentCopy pins §5.4's "LIST results are never
// resident": with an archive streaming the scan to disk, the store's copy is
// the second one, and a fifty-thousand-row second copy is what the rule is
// about.
func TestForgetListDropsTheResidentCopy(t *testing.T) {
	h := newHarness(t)
	h.apply(model.ListItem{EventMeta: h.meta(), Channel: "#alpha", Users: 42})
	h.apply(model.ListEnd{EventMeta: h.meta()})
	if n := len(h.s.ChannelList(h.n)); n != 1 {
		t.Fatalf("the store holds %d channels after a scan, want 1", n)
	}
	h.s.ForgetList(h.n)
	if n := len(h.s.ChannelList(h.n)); n != 0 {
		t.Errorf("the store still holds %d channels after ForgetList", n)
	}
	// A later scan still works: forgetting is not disabling.
	h.apply(model.ListItem{EventMeta: h.meta(), Channel: "#beta", Users: 7})
	h.apply(model.ListEnd{EventMeta: h.meta()})
	if n := len(h.s.ChannelList(h.n)); n != 1 {
		t.Errorf("the store holds %d channels after a second scan, want 1", n)
	}
}
