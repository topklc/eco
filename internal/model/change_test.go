package model

import (
	"reflect"
	"testing"
	"time"
)

// allChanges is every member of the Change union, split by scope: buffer
// changes must carry a BufferID, network changes must not.
func allChanges() (buffered, network []Change) {
	b := BufMeta(BufferID{Network: "n1", Folded: "#go"})
	n := NetMeta("n1")
	buffered = []Change{
		MessageAdded{ChangeMeta: b},
		MessageResolved{ChangeMeta: b},
		MessageFailed{ChangeMeta: b},
		MessageRedacted{ChangeMeta: b},
		HistoryLoaded{ChangeMeta: b},
		BufferCreated{ChangeMeta: b},
		BufferClosed{ChangeMeta: b},
		BufferRenamed{ChangeMeta: b},
		MembersChanged{ChangeMeta: b},
		TopicChanged{ChangeMeta: b},
		ModeChanged{ChangeMeta: b},
		UnreadChanged{ChangeMeta: b},
		ReadMarkerMoved{ChangeMeta: b},
		TypingChanged{ChangeMeta: b},
	}
	network = []Change{
		ConnStateChanged{ChangeMeta: n},
		SelfChanged{ChangeMeta: n},
		AuthStateChanged{ChangeMeta: n},
		ListUpdated{ChangeMeta: n},
		JoinFailed{ChangeMeta: n},
	}
	return buffered, network
}

func allChangesFlat() []Change {
	b, n := allChanges()
	return append(append([]Change{}, b...), n...)
}

func TestChangeNamesAreUniqueAndCanonical(t *testing.T) {
	seen := map[string]string{}
	for _, ch := range allChangesFlat() {
		typeName := reflect.TypeOf(ch).Name()
		name := ch.ChangeName()
		if name == "" {
			t.Errorf("%s has an empty ChangeName", typeName)
			continue
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("%s and %s share the ChangeName %q", prev, typeName, name)
			continue
		}
		seen[name] = typeName
		if want := lower(typeName); name != want {
			t.Errorf("%s.ChangeName() = %q, want %q", typeName, name, want)
		}
	}
}

// TestChangeMetaInvariant: Network is always set, and Scoped agrees with
// whether a buffer was named.
func TestChangeMetaInvariant(t *testing.T) {
	buffered, network := allChanges()
	for _, ch := range buffered {
		m := ch.Meta()
		if m.Network != "n1" {
			t.Errorf("%s lost its network", ch.ChangeName())
		}
		if !m.Scoped() {
			t.Errorf("%s should be buffer-scoped", ch.ChangeName())
		}
		if m.Buffer.Network != m.Network {
			t.Errorf("%s: buffer network %q disagrees with meta network %q", ch.ChangeName(), m.Buffer.Network, m.Network)
		}
	}
	for _, ch := range network {
		m := ch.Meta()
		if m.Network != "n1" {
			t.Errorf("%s lost its network", ch.ChangeName())
		}
		if m.Scoped() {
			t.Errorf("%s should be network-scoped, got buffer %s", ch.ChangeName(), m.Buffer)
		}
	}
}

func TestChangeMetaConstructors(t *testing.T) {
	id := BufferID{Network: "n1", Folded: "#go"}
	if got := BufMeta(id); got.Network != "n1" || got.Buffer != id || !got.Scoped() {
		t.Errorf("BufMeta = %+v", got)
	}
	if got := NetMeta("n1"); got.Network != "n1" || !got.Buffer.IsZero() || got.Scoped() {
		t.Errorf("NetMeta = %+v", got)
	}
	// A network status buffer is scoped, even though its folded name is empty.
	if got := BufMeta(ServerBuffer("n1")); !got.Scoped() {
		t.Error("the network status buffer must be buffer-scoped")
	}
}

func TestChangeUnionIsClosedOverTheKnownTypes(t *testing.T) {
	for _, ch := range allChangesFlat() {
		switch ch.(type) {
		case MessageAdded, MessageResolved, MessageFailed, MessageRedacted, HistoryLoaded:
		case BufferCreated, BufferClosed, BufferRenamed:
		case MembersChanged, TopicChanged, ModeChanged:
		case UnreadChanged, ReadMarkerMoved, TypingChanged:
		case ConnStateChanged, SelfChanged, AuthStateChanged, ListUpdated, JoinFailed:
		default:
			t.Errorf("%T (%s) is not handled by the reference type switch", ch, ch.ChangeName())
		}
	}
}

func TestChangeMetaIsNotAChange(t *testing.T) {
	var v any = ChangeMeta{}
	if _, ok := v.(Change); ok {
		t.Error("ChangeMeta satisfies Change; the union is not sealed by ChangeName")
	}
}

// The two unions are disjoint by construction: Event.Meta returns EventMeta and
// Change.Meta returns ChangeMeta, so no type can satisfy both. The compiler
// rejects even the attempt to assert across them ("impossible type assertion"),
// which is a stronger guarantee than a test — a consumer that takes an Event
// can never be handed a Change by mistake. Do not give the two Meta methods
// different names in an attempt to "fix" this: the collision is the feature.
var (
	_ Event  = PrivMsg{}
	_ Change = MessageAdded{}
)

// TestMessageAddedCarriesAStableID: the UI keys rows on Message.ID, so a
// pending message and its resolution must refer to the same row.
func TestMessageAddedCarriesAStableID(t *testing.T) {
	ids := NewIDSeq(0)
	buf := BufferID{Network: "n1", Folded: "#go"}
	sent := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	echoed := sent.Add(120 * time.Millisecond)

	id := ids.Next()
	added := MessageAdded{
		ChangeMeta: BufMeta(buf),
		Message: Message{
			ID:      id,
			Label:   "L1",
			Buffer:  buf,
			Time:    sent,
			Sender:  Sender{Nick: "me", Self: true},
			Kind:    MsgPrivmsg,
			Text:    "hi",
			Pending: PendingEcho,
		},
	}
	resolved := MessageResolved{
		ChangeMeta: BufMeta(buf),
		ID:         id,
		Message: Message{
			ID:         id,
			MsgID:      "server-msgid",
			Label:      "L1",
			Buffer:     buf,
			Time:       echoed,
			ServerTime: true,
			Sender:     Sender{Nick: "me", Self: true},
			Kind:       MsgPrivmsg,
			Text:       "hi",
			Pending:    Delivered,
		},
	}

	if resolved.ID != added.Message.ID {
		t.Fatal("resolution must name the row that already exists")
	}
	if added.Message.MsgID != "" {
		t.Error("a pending message has no server msgid yet")
	}
	if resolved.Message.MsgID == "" || !resolved.Message.ServerTime {
		t.Error("resolution is what gains the msgid and the server timestamp")
	}
	if resolved.Message.Pending != Delivered {
		t.Error("a resolved message is no longer pending")
	}
	// The identity is the id, never the text: two different messages with the
	// same text are two rows.
	other := ids.Next()
	if other == id {
		t.Error("ids must not repeat")
	}
}

func TestMembersChangedResetIsDistinctFromDelta(t *testing.T) {
	buf := BufferID{Network: "n1", Folded: "#go"}
	delta := MembersChanged{
		ChangeMeta: BufMeta(buf),
		Added:      []Member{{Nick: "Alice", Folded: "alice"}},
		Removed:    []Member{{Nick: "Bob", Folded: "bob", Prefixes: "@"}},
	}
	reset := MembersChanged{
		ChangeMeta: BufMeta(buf),
		Reset:      []Member{{Nick: "Alice", Folded: "alice"}, {Nick: "Carol", Folded: "carol"}},
	}

	if delta.Reset != nil {
		t.Error("a delta must not set Reset")
	}
	if len(reset.Added)+len(reset.Removed)+len(reset.Updated) != 0 {
		t.Error("a reset must not also carry a delta")
	}
	// Removed carries the whole Member so the UI can render what left.
	if delta.Removed[0].Prefixes != "@" {
		t.Error("Removed must carry the member's state, not just a name")
	}
}
