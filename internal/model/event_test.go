package model

import (
	"reflect"
	"testing"
	"time"
)

// allEvents is every member of the Event union. Adding a variant means adding
// it here; the tests below then check it for the mistakes that are easy to make
// when copying a neighbouring struct - a forgotten EventName, a duplicated one,
// or a missing EventMeta.
func allEvents() []Event {
	m := EventMeta{Network: "n1"}
	return []Event{
		PrivMsg{EventMeta: m},
		Notice{EventMeta: m},
		Typing{EventMeta: m},
		Redact{EventMeta: m},
		MarkRead{EventMeta: m},
		Join{EventMeta: m},
		Part{EventMeta: m},
		Quit{EventMeta: m},
		Kick{EventMeta: m},
		Nick{EventMeta: m},
		Invite{EventMeta: m},
		Names{EventMeta: m},
		AwayChange{EventMeta: m},
		AccountChange{EventMeta: m},
		ChgHost{EventMeta: m},
		SetName{EventMeta: m},
		Topic{EventMeta: m},
		Mode{EventMeta: m},
		ChannelRenamed{EventMeta: m},
		BatchOpen{EventMeta: m},
		BatchClose{EventMeta: m},
		CapChange{EventMeta: m},
		ISupport{EventMeta: m},
		ConnState{EventMeta: m},
		AuthResult{EventMeta: m},
		JoinRefused{EventMeta: m},
		ListItem{EventMeta: m},
		ListEnd{EventMeta: m},
		HistoryChunk{EventMeta: m},
		Numeric{EventMeta: m},
		ServerError{EventMeta: m},
		Raw{EventMeta: m},
	}
}

func TestEventNamesAreUniqueAndCanonical(t *testing.T) {
	seen := map[string]string{}
	for _, ev := range allEvents() {
		typeName := reflect.TypeOf(ev).Name()
		name := ev.EventName()
		if name == "" {
			t.Errorf("%s has an empty EventName", typeName)
			continue
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("%s and %s share the EventName %q", prev, typeName, name)
			continue
		}
		seen[name] = typeName
		// The convention is the lowercased type name, which keeps a log line
		// greppable back to the struct.
		if want := lower(typeName); name != want {
			t.Errorf("%s.EventName() = %q, want %q", typeName, name, want)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// TestEveryEventCarriesItsNetwork: routing in a multi-network client depends on
// it, so no variant may lose the envelope.
func TestEveryEventCarriesItsNetwork(t *testing.T) {
	for _, ev := range allEvents() {
		if got := ev.Meta().Network; got != "n1" {
			t.Errorf("%T.Meta().Network = %q, want n1", ev, got)
		}
	}
}

// TestEventUnionIsClosedOverTheKnownTypes: a type switch over the union must
// reach a named case for every variant. The default branch here stands in for
// the "unhandled event" log line real consumers write.
func TestEventUnionIsClosedOverTheKnownTypes(t *testing.T) {
	for _, ev := range allEvents() {
		switch ev.(type) {
		case PrivMsg, Notice, Typing, Redact, MarkRead:
		case Join, Part, Quit, Kick, Nick, Invite, Names:
		case AwayChange, AccountChange, ChgHost, SetName:
		case Topic, Mode, ChannelRenamed:
		case BatchOpen, BatchClose, CapChange, ISupport:
		case ConnState, AuthResult, JoinRefused:
		case ListItem, ListEnd, HistoryChunk:
		case Numeric, ServerError, Raw:
		default:
			t.Errorf("%T (%s) is not handled by the reference type switch", ev, ev.EventName())
		}
	}
}

// TestEventMetaIsNotAnEvent: EventMeta supplies Meta but not EventName, so a
// bare envelope cannot be mistaken for an event.
func TestEventMetaIsNotAnEvent(t *testing.T) {
	var v any = EventMeta{}
	if _, ok := v.(Event); ok {
		t.Error("EventMeta satisfies Event; the union is not sealed by EventName")
	}
}

func TestEventMetaFields(t *testing.T) {
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	ev := PrivMsg{
		EventMeta: EventMeta{
			Network:    "n1",
			Time:       at,
			ServerTime: true,
			MsgID:      "abc123",
			Label:      "L7",
			Tags:       map[string]string{"+draft/reply": "xyz", "account": "alice"},
			Batch:      "b1",
			Playback:   true,
		},
		Target: "#go",
		Sender: Sender{Nick: "alice", Account: "alice"},
		Text:   "hello",
	}

	m := ev.Meta()
	if m.Network != "n1" || !m.Time.Equal(at) || !m.ServerTime || m.MsgID != "abc123" || m.Label != "L7" || m.Batch != "b1" || !m.Playback {
		t.Errorf("Meta lost fields: %+v", m)
	}
	if v, ok := m.Tag("account"); !ok || v != "alice" {
		t.Errorf("Tag(account) = (%q, %v)", v, ok)
	}
	if _, ok := m.Tag("absent"); ok {
		t.Error("Tag reported a tag that is not there")
	}
	// A valueless tag is present with an empty value.
	m.Tags["+typing"] = ""
	if v, ok := m.Tag("+typing"); !ok || v != "" {
		t.Errorf("valueless tag = (%q, %v)", v, ok)
	}
}

func TestTagOnNilTags(t *testing.T) {
	var m EventMeta
	if _, ok := m.Tag("anything"); ok {
		t.Error("Tag on a nil map reported a tag")
	}
}

// TestHistoryChunkNestsEvents: a chathistory batch is delivered whole, carrying
// the same event values the live path uses.
func TestHistoryChunkNestsEvents(t *testing.T) {
	chunk := HistoryChunk{
		EventMeta: EventMeta{Network: "n1"},
		Target:    "#go",
		Direction: HistoryBefore,
		Events: []Event{
			PrivMsg{EventMeta: EventMeta{Network: "n1", MsgID: "m1", Playback: true}, Target: "#go", Text: "old"},
			Join{EventMeta: EventMeta{Network: "n1", Playback: true}, Channel: "#go"},
		},
	}
	if len(chunk.Events) != 2 {
		t.Fatalf("Events = %d", len(chunk.Events))
	}
	pm, ok := chunk.Events[0].(PrivMsg)
	if !ok {
		t.Fatalf("first nested event is %T", chunk.Events[0])
	}
	if !pm.Meta().Playback || pm.Text != "old" {
		t.Errorf("nested event = %+v", pm)
	}
	if chunk.Direction.String() != "before" {
		t.Errorf("Direction = %s", chunk.Direction)
	}
}

func TestCapOpString(t *testing.T) {
	for _, tc := range []struct {
		op   CapOp
		want string
	}{
		{CapLS, "LS"}, {CapNEW, "NEW"}, {CapDEL, "DEL"},
		{CapACK, "ACK"}, {CapNAK, "NAK"}, {CapOp(99), "?"},
	} {
		if got := tc.op.String(); got != tc.want {
			t.Errorf("CapOp(%d) = %q, want %q", tc.op, got, tc.want)
		}
	}
}

func TestEnumStrings(t *testing.T) {
	checks := []struct {
		got, want string
	}{
		{MsgPrivmsg.String(), "privmsg"},
		{MsgNotice.String(), "notice"},
		{MsgAction.String(), "action"},
		{MsgStatus.String(), "status"},
		{MsgTombstone.String(), "tombstone"},
		{MessageKind(99).String(), "unknown"},

		{Delivered.String(), "delivered"},
		{PendingEcho.String(), "pending"},
		{PendingFailed.String(), "failed"},
		{PendingState(99).String(), "unknown"},

		{BufferServer.String(), "server"},
		{BufferChannel.String(), "channel"},
		{BufferQuery.String(), "query"},
		{BufferKind(99).String(), "unknown"},

		{TypingActive.String(), "active"},
		{TypingPaused.String(), "paused"},
		{TypingDone.String(), "done"},
		{TypingState(99).String(), "unknown"},

		{ConnDisconnected.String(), "disconnected"},
		{ConnConnecting.String(), "connecting"},
		{ConnRegistering.String(), "registering"},
		{ConnAuthenticating.String(), "authenticating"},
		{ConnReady.String(), "ready"},
		{ConnFailed.String(), "failed"},
		{ConnStatus(99).String(), "unknown"},

		{HistoryLatest.String(), "latest"},
		{HistoryBefore.String(), "before"},
		{HistoryAfter.String(), "after"},
		{HistoryAround.String(), "around"},
		{HistoryDirection(99).String(), "unknown"},
	}
	for i, c := range checks {
		if c.got != c.want {
			t.Errorf("check %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

// TestArchiveEnumValuesAreStable pins the numeric values that reach SQLite.
// Renumbering them would silently reinterpret every stored row.
func TestArchiveEnumValuesAreStable(t *testing.T) {
	if MsgPrivmsg != 0 || MsgNotice != 1 || MsgAction != 2 || MsgStatus != 3 || MsgTombstone != 4 {
		t.Error("MessageKind values changed; the archive's kind column depends on them")
	}
	if BufferServer != 0 || BufferChannel != 1 || BufferQuery != 2 {
		t.Error("BufferKind values changed; the archive's buffers.kind column depends on them")
	}
	if Delivered != 0 {
		t.Error("the zero PendingState must mean Delivered, so inbound messages need no initialisation")
	}
}

func TestSenderIsZero(t *testing.T) {
	if !(Sender{}).IsZero() {
		t.Error("a zero Sender means the server spoke")
	}
	if (Sender{Nick: "alice"}).IsZero() {
		t.Error("a sender with a nick is not zero")
	}
	// Account and Self alone do not make a sender; a user always has a nick.
	if !(Sender{Account: "x", Self: true}).IsZero() {
		t.Error("account/self without a nick is still the zero sender")
	}
}
