package viewmodel

import "testing"

// M12(c)'s two keyboard answers, tested where they are decided: the hotlist's
// order (Alt+A) and the pins (Alt+1..9). No canvas, no widgets — the rail draws
// what these say and decides none of it.

// refNames renders a []Ref for a failure message.
func refNames(rs []Ref) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

// hot builds a two-network rail with counts on four buffers, so an ordering
// failure prints something a person can argue with.
func hotRail(t *testing.T) *harness {
	t.Helper()
	h := newHarness()
	h.AddSession(sA, "libera")
	h.AddSession(sB, "ergo")
	h.SetBuffers(sA, []string{ServerBuffer, "#go", "#quiet", "alice"})
	h.SetBuffers(sB, []string{ServerBuffer, "#eco", "bob"})
	return h
}

// Mentions first, then unread — WeeChat's semantics and §4.3's. Within a band
// the order is the rail's, which is the order the user arranged.
func TestHotlistPutsMentionsFirst(t *testing.T) {
	h := hotRail(t)
	h.SetUnread(sA, "#go", Counts{Unread: 4})
	h.SetUnread(sA, "alice", Counts{Unread: 1, Highlights: 1})
	h.SetUnread(sB, "#eco", Counts{Unread: 9})
	h.SetUnread(sB, "bob", Counts{Unread: 2, Highlights: 2})

	want := []string{"alice", "bob", "#go", "#eco"}
	if got := refNames(h.Hotlist()); !eq(got, want) {
		t.Errorf("Hotlist = %v, want %v", got, want)
	}
}

// A buffer with nothing outstanding is not in the list, and neither is the one
// on screen — which is the same suppression the pill uses, applied once
// (Badge), not twice.
func TestHotlistSkipsQuietAndCurrent(t *testing.T) {
	h := hotRail(t)
	h.SetUnread(sA, "#go", Counts{Unread: 4})
	h.SetUnread(sB, "#eco", Counts{Unread: 1})
	h.Select(ref(sA, "#go"))

	if got, want := refNames(h.Hotlist()), []string{"#eco"}; !eq(got, want) {
		t.Errorf("Hotlist = %v, want %v — the buffer being read is not waiting for anyone", got, want)
	}
	if got := refNames(h.Hotlist()); len(got) == 1 && got[0] == "#quiet" {
		t.Error("a buffer with no counts entered the hotlist")
	}
}

// Alt+A walks the list: each jump marks the buffer read, which takes it out and
// leaves the next one at the front. Nothing keeps a cursor — a cursor would be a
// second opinion about what is unread.
func TestHotlistCyclesAsBuffersAreRead(t *testing.T) {
	h := hotRail(t)
	h.SetUnread(sA, "#go", Counts{Unread: 4})
	h.SetUnread(sA, "alice", Counts{Unread: 1, Highlights: 1})
	h.SetUnread(sB, "#eco", Counts{Unread: 2})

	var visited []string
	for range 3 {
		i, ok := h.NextUnread()
		if !ok {
			t.Fatalf("hotlist ran out after %v", visited)
		}
		row, _ := h.Row(i)
		visited = append(visited, row.Ref.Name)
		h.Select(row.Ref)
	}
	if want := []string{"alice", "#go", "#eco"}; !eq(visited, want) {
		t.Errorf("Alt+A visited %v, want %v", visited, want)
	}
	if _, ok := h.NextUnread(); ok {
		t.Error("the hotlist still had something after everything was read")
	}
}

// A mention inside a folded network is still a mention: Alt+A opens the fold to
// get there. This is M12(a)'s rule, and the mentions-first ordering must not
// have quietly lost it.
func TestHotlistReachesIntoAFold(t *testing.T) {
	h := hotRail(t)
	h.SetCollapsed("ergo", true)
	h.SetUnread(sA, "#go", Counts{Unread: 4})
	h.SetUnread(sB, "bob", Counts{Unread: 1, Highlights: 1})

	if got, want := refNames(h.Hotlist()), []string{"bob", "#go"}; !eq(got, want) {
		t.Errorf("Hotlist = %v, want %v — a fold hides rows, not attention", got, want)
	}
	i, ok := h.NextUnread()
	if !ok {
		t.Fatal("Alt+A found nothing with a mention inside a fold")
	}
	if h.Collapsed("ergo") {
		t.Error("Alt+A landed on a row inside a network it did not unfold")
	}
	row, _ := h.Row(i)
	if row.Ref.Name != "bob" {
		t.Errorf("Alt+A landed on %q, want the mention", row.Ref.Name)
	}
}

// The OS badge is the mention count and nothing else (§4.4). Unread messages
// are the rail's business; a taskbar number that counted them would say "47"
// for a busy channel nobody has been named in.
func TestTotalMentionsIsMentionsOnly(t *testing.T) {
	h := hotRail(t)
	h.SetUnread(sA, "#go", Counts{Unread: 40})
	h.SetUnread(sA, "alice", Counts{Unread: 3, Highlights: 2})
	h.SetUnread(sB, "bob", Counts{Unread: 1, Highlights: 1})

	if got := h.TotalMentions(); got != 3 {
		t.Errorf("TotalMentions = %d, want 3", got)
	}
	h.Select(ref(sA, "alice"))
	if got := h.TotalMentions(); got != 1 {
		t.Errorf("TotalMentions after reading alice = %d, want 1", got)
	}
}

// --- the pins ------------------------------------------------------------------

// Alt+1..9 are pins, not row numbers: the digit follows the buffer, whatever
// happens to the rail above it. This is the whole reason §4.3 refuses absolute
// rows — with two networks connected, row 2 was a different conversation every
// time one of them dropped.
func TestPinsFollowTheBufferNotTheRow(t *testing.T) {
	h := hotRail(t)
	if got := h.Pin(ref(sB, "#eco")); got != 1 {
		t.Fatalf("first pin = %d, want 1", got)
	}
	if got := h.Pin(ref(sA, "#go")); got != 2 {
		t.Fatalf("second pin = %d, want 2", got)
	}
	// a whole network's worth of rows appears above them
	h.SetBuffers(sA, []string{ServerBuffer, "#new", "#newer", "#go", "#quiet", "alice"})

	got, ok := h.PinRef(1)
	if !ok || got.Name != "#eco" {
		t.Errorf("Alt+1 = %v (%v), want #eco", got, ok)
	}
	got, ok = h.PinRef(2)
	if !ok || got.Name != "#go" {
		t.Errorf("Alt+2 = %v (%v), want #go", got, ok)
	}
	if _, ok := h.PinRef(3); ok {
		t.Error("Alt+3 resolved with only two pins")
	}
	if _, ok := h.PinRef(0); ok {
		t.Error("Alt+0 is not a pin")
	}
}

// The digit is on the row, so the rail can draw it and a menu can offer to take
// it off without a second lookup.
func TestPinnedRowsCarryTheirDigit(t *testing.T) {
	h := hotRail(t)
	h.Pin(ref(sA, "#go"))
	for _, row := range h.Rows() {
		want := 0
		if row.Ref.Name == "#go" && row.Ref.Session == sA {
			want = 1
		}
		if row.Pin != want {
			t.Errorf("row %q Pin = %d, want %d", row.Ref.Name, row.Pin, want)
		}
	}
}

// Unpinning closes the gap. An Alt+3 that jumps to nothing because the third pin
// was removed is a shortcut that teaches people not to trust the row of them.
func TestUnpinningClosesTheGap(t *testing.T) {
	h := hotRail(t)
	h.Pin(ref(sA, "#go"))
	h.Pin(ref(sB, "#eco"))
	h.Pin(ref(sA, "alice"))

	if !h.Unpin(ref(sB, "#eco")) {
		t.Fatal("Unpin reported nothing removed")
	}
	if got := h.PinnedAt(ref(sA, "alice")); got != 2 {
		t.Errorf("alice moved to digit %d, want 2", got)
	}
	if got, ok := h.PinRef(2); !ok || got.Name != "alice" {
		t.Errorf("Alt+2 = %v (%v), want alice", got, ok)
	}
	if h.Unpin(ref(sB, "#eco")) {
		t.Error("Unpin removed something twice")
	}
}

// Toggle is what the menu calls, and it says what happened.
func TestTogglePin(t *testing.T) {
	h := hotRail(t)
	if got := h.TogglePin(ref(sA, "#go")); got != 1 {
		t.Errorf("TogglePin on = %d, want 1", got)
	}
	if got := h.TogglePin(ref(sA, "#go")); got != 0 {
		t.Errorf("TogglePin off = %d, want 0", got)
	}
	if got := h.PinnedAt(ref(sA, "#go")); got != 0 {
		t.Errorf("PinnedAt after unpinning = %d, want 0", got)
	}
}

// Nine is the cap, because nine is how many digits Alt has.
func TestPinsStopAtNine(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	names := []string{ServerBuffer}
	for i := range MaxPins + 3 {
		names = append(names, "#c"+string(rune('a'+i)))
	}
	h.SetBuffers(sA, names)
	for _, n := range names[1:] {
		h.Pin(ref(sA, n))
	}
	if got := len(h.PinKeys()); got != MaxPins {
		t.Errorf("pins = %d, want the %d Alt has digits for", got, MaxPins)
	}
	if got := h.Pin(ref(sA, names[len(names)-1])); got != 0 {
		t.Errorf("pinning past the cap returned %d, want 0", got)
	}
}

// Pins are profile-scoped, so they survive a reconnect — which hands the same
// network a NEW session id — and they round-trip through preferences.
func TestPinsSurviveAReconnectAndARestart(t *testing.T) {
	h := hotRail(t)
	h.Pin(ref(sA, "#go"))
	h.Pin(ref(sB, "#eco"))
	keys := h.PinKeys()

	// the reconnect: libera comes back under a different session id
	h.RemoveSession(sA)
	const sC = "s-3"
	h.AddSession(sC, "libera")
	h.SetBuffers(sC, []string{ServerBuffer, "#go"})
	if got, ok := h.PinRef(1); !ok || got.Session != sC || got.Name != "#go" {
		t.Errorf("after a reconnect Alt+1 = %v (%v), want #go on the new session", got, ok)
	}

	// the restart: a fresh model, the same preference string
	h2 := hotRail(t)
	h2.SetPinKeys(keys)
	if got, ok := h2.PinRef(2); !ok || got.Name != "#eco" {
		t.Errorf("after a restart Alt+2 = %v (%v), want #eco", got, ok)
	}
	if got := h2.PinKeys(); !eq(got, keys) {
		t.Errorf("PinKeys round trip = %v, want %v", got, keys)
	}
}

// A pin whose network is not connected keeps its digit and says so, rather than
// jumping somewhere else. Silently renumbering would move every pin below it
// every time a network dropped.
func TestAnOfflinePinKeepsItsDigit(t *testing.T) {
	h := hotRail(t)
	h.Pin(ref(sA, "#go"))
	h.Pin(ref(sB, "#eco"))
	h.RemoveSession(sA)

	if _, ok := h.PinRef(1); ok {
		t.Error("Alt+1 resolved to a buffer on a network that is not connected")
	}
	if got, ok := h.PinRef(2); !ok || got.Name != "#eco" {
		t.Errorf("Alt+2 = %v (%v), want #eco — the other pin did not move", got, ok)
	}
	if got := len(h.PinKeys()); got != 2 {
		t.Errorf("pins = %d, want both kept", got)
	}
}

// Malformed or duplicated preference entries are dropped rather than shifting
// the digits of everything after them.
func TestSetPinKeysIgnoresRubbish(t *testing.T) {
	h := hotRail(t)
	h.SetPinKeys([]string{"libera\x00#go", "nonsense", "", "libera\x00#go", "ergo\x00#eco"})
	if got, want := h.PinKeys(), []string{"libera\x00#go", "ergo\x00#eco"}; !eq(got, want) {
		t.Errorf("PinKeys = %q, want %q", got, want)
	}
}
