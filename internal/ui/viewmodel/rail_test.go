package viewmodel

import (
	"testing"
)

// M12's rail, tested where it is decided: R-6's disclosure-by-invitation, the
// collapse that filters rows, the rollup, and the offline row that must never
// dial a server by being clicked. None of it needs a canvas.

// names renders the rows compactly so a failure prints the rail rather than a
// wall of structs: "net:libera" for a header, "#go" for a buffer.
func names(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.IsNetwork() {
			out = append(out, "net:"+r.Profile)
			continue
		}
		out = append(out, r.Ref.Name)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// One network gets no network chrome at all: no header row, no chevron, no
// overflow menu — its buffers ARE the rail, and Caption names it once. The
// second network is what invites the headers (blueprint §4.1, R-6).
func TestDisclosureByInvitation(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go", "alice"})

	if got, want := names(h.Rows()), []string{ServerBuffer, "#go", "alice"}; !eq(got, want) {
		t.Errorf("one network: rows %v, want %v — a header is chrome nothing asked for", got, want)
	}
	if got := h.Caption(); got != "libera" {
		t.Errorf("Caption = %q, want the one network's name", got)
	}
	for i, r := range h.Rows() {
		if r.Depth != 0 {
			t.Errorf("row %d is indented under a header that does not exist", i)
		}
	}

	h.AddSession(sB, "ergo")
	h.SetBuffers(sB, []string{ServerBuffer, "#eco"})

	want := []string{"net:libera", ServerBuffer, "#go", "alice", "net:ergo", ServerBuffer, "#eco"}
	if got := names(h.Rows()); !eq(got, want) {
		t.Errorf("two networks: rows %v, want %v", got, want)
	}
	if got := h.Caption(); got != "" {
		t.Errorf("Caption = %q with headers showing; the headers already say it", got)
	}
	for _, r := range h.Rows() {
		if r.IsNetwork() && r.Depth != 0 {
			t.Errorf("header %q is indented", r.Profile)
		}
		if !r.IsNetwork() && r.Depth != 1 {
			t.Errorf("buffer %q sits at depth %d under a header, want 1", r.Ref.Name, r.Depth)
		}
	}
}

// A second network that is not connected invites the headers just as much: the
// rail has to say which network each row belongs to either way.
func TestAnOfflineNetworkCountsTowardsDisclosure(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})
	h.SetPinned([]string{"oftc"})

	want := []string{"net:libera", ServerBuffer, "#go", "net:oftc"}
	if got := names(h.Rows()); !eq(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if got := h.Caption(); got != "" {
		t.Errorf("Caption = %q with two networks", got)
	}
}

// The one network that IS offline still gets its row: it has no buffers, so
// this is the only row it has and the only place [Connect] can live.
func TestASingleOfflineNetworkKeepsItsRow(t *testing.T) {
	h := newHarness()
	h.SetPinned([]string{"oftc"})

	rows := h.Rows()
	if got, want := names(rows), []string{"net:oftc"}; !eq(got, want) {
		t.Fatalf("rows %v, want %v — an offline network with no row cannot be connected", got, want)
	}
	if rows[0].Live {
		t.Error("the offline network claims a session")
	}
	if rows[0].Selectable() {
		t.Error("the offline network is selectable: clicking the row would dial a server (§4.1)")
	}
}

// Collapsing a network filters ITS rows out and nothing else's, and the header
// keeps the rollup of what it is hiding — including the mark for the buffer on
// screen, which is now the only place left to say where the viewer is.
func TestCollapseFiltersRowsAndRollsUpTheCounts(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetUnread(sA, ServerBuffer, Counts{Unread: 2})
	h.SetUnread(sB, "#eco", Counts{Unread: 5, Highlights: 1})

	// expanded: the header carries no pill, the buffers carry their own
	rows := h.Rows()
	if got := rows[0]; got.Counts != (Counts{}) {
		t.Errorf("expanded header counts %+v, want none: the pills are on the rows below it", got.Counts)
	}

	h.SetCollapsed("libera", true)
	rows = h.Rows()
	if got, want := names(rows), []string{"net:libera", "net:ergo", ServerBuffer, "#eco"}; !eq(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if got, want := rows[0].Counts, (Counts{Unread: 2}); got != want {
		t.Errorf("rollup %+v, want %+v — the counts of everything folded away", got, want)
	}
	if !rows[0].Collapsed {
		t.Error("the collapsed header does not say it is collapsed")
	}
	if !rows[0].Current {
		t.Error("the header hiding the buffer on screen does not carry the current mark")
	}
	if rows[1].Counts != (Counts{}) {
		t.Error("collapsing one network moved the other's counts")
	}

	// and the chevron puts them back: ToggleCollapsed reports the NEW state
	if folded := h.ToggleCollapsed("libera"); folded {
		t.Error("ToggleCollapsed on a folded network reported it still folded")
	}
	want := []string{"net:libera", ServerBuffer, "#go", "net:ergo", ServerBuffer, "#eco"}
	if got := names(h.Rows()); !eq(got, want) {
		t.Errorf("unfolded rows %v, want %v", got, want)
	}
}

// The buffer on screen is read by definition, so it never contributes to the
// rollup either — §4.4's suppression applies once, in Badge, and the rollup is
// built from Badge for exactly that reason.
func TestTheRollupSuppressesTheBufferOnScreen(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetUnread(sA, "#go", Counts{Unread: 9, Highlights: 9})
	h.SetUnread(sA, ServerBuffer, Counts{Unread: 1})
	h.SetCollapsed("libera", true)

	if got, want := h.Rows()[0].Counts, (Counts{Unread: 1}); got != want {
		t.Errorf("rollup %+v, want %+v: the buffer being read is not unread", got, want)
	}
}

// A single network has no chevron, so it must not be foldable: a rail that can
// be collapsed into nothing, with nothing to click to bring it back, is a
// window with no buffers in it.
func TestASingleNetworkCannotBeFoldedAway(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})
	h.SetCollapsed("libera", true)

	if got, want := names(h.Rows()), []string{ServerBuffer, "#go"}; !eq(got, want) {
		t.Errorf("rows %v, want %v — there is no chevron to undo this with", got, want)
	}
}

// Stepping with the keyboard walks buffers. A network header switches nothing,
// so Alt+Up/Down passes over it, and a folded network is passed over with it —
// that is what folding it asked for.
func TestStepPassesOverHeadersAndFolds(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))

	i, ok := h.Step(1)
	if !ok {
		t.Fatal("nowhere to step")
	}
	if got := h.Rows()[i]; got.IsNetwork() || !got.Ref.same(ref(sB, ServerBuffer)) {
		t.Errorf("Step(1) from libera's last buffer = %+v, want ergo's log", got)
	}

	h.SetCollapsed("ergo", true)
	i, ok = h.Step(1)
	if !ok {
		t.Fatal("nowhere to step with the other network folded")
	}
	if got := h.Rows()[i]; !got.Ref.same(ref(sA, ServerBuffer)) {
		t.Errorf("Step(1) = %+v, want it to wrap inside libera past the folded network", got)
	}
}

// Folding a network hides its rows, not its attention: Alt+A reaches into the
// fold and opens it, because a hotlist that cannot reach half the buffers is
// worse than one that unfolds what it has to.
func TestNextUnreadReachesIntoAFold(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetCollapsed("ergo", true)
	h.SetUnread(sB, "#eco", Counts{Unread: 3})

	i, ok := h.NextUnread()
	if !ok {
		t.Fatal("NextUnread found nothing: the only unread buffer is inside a fold")
	}
	if got := h.Rows()[i]; !got.Ref.same(ref(sB, "#eco")) {
		t.Errorf("NextUnread = %+v, want the folded network's channel", got)
	}
	if h.Collapsed("ergo") {
		t.Error("the network is still folded, so the row it returned is not on screen")
	}
}

// Alt+1..9 counts what it can land on. Headers are not rows a jump can select,
// so counting them would make the same digit mean a different buffer depending
// on how many networks happen to be connected.
func TestNthCountsSelectableRowsOnly(t *testing.T) {
	h := twoNetworks(t)
	h.SetPinned([]string{"oftc"})

	want := []string{ServerBuffer, "#go", ServerBuffer, "#eco"}
	for n, wantName := range want {
		i, ok := h.Nth(n)
		if !ok {
			t.Fatalf("Nth(%d) found nothing", n)
		}
		got := h.Rows()[i]
		if got.IsNetwork() || got.Ref.Name != wantName {
			t.Errorf("Nth(%d) = %+v, want %q", n, got, wantName)
		}
	}
	if _, ok := h.Nth(len(want)); ok {
		t.Error("Nth past the last buffer found a row")
	}
}

// The fold survives a restart, which means it has to be listable and
// restorable — and an offline network's fold counts too.
func TestCollapsedProfilesRoundTrip(t *testing.T) {
	h := twoNetworks(t)
	h.SetPinned([]string{"oftc"})
	h.SetCollapsed("ergo", true)
	h.SetCollapsed("oftc", true)

	got := h.CollapsedProfiles()
	if len(got) != 2 || got[0] != "ergo" || got[1] != "oftc" {
		t.Fatalf("CollapsedProfiles = %v, want [ergo oftc] in rail order", got)
	}

	fresh := twoNetworks(t)
	fresh.SetPinned([]string{"oftc"})
	fresh.SetCollapsedProfiles(got)
	if !fresh.Collapsed("ergo") || !fresh.Collapsed("oftc") || fresh.Collapsed("libera") {
		t.Errorf("restored folds: ergo=%v oftc=%v libera=%v",
			fresh.Collapsed("ergo"), fresh.Collapsed("oftc"), fresh.Collapsed("libera"))
	}
	// libera's header and its two buffers, ergo's header with its rows folded
	// away, and the offline network
	want := []string{"net:libera", ServerBuffer, "#go", "net:ergo", "net:oftc"}
	if got := names(fresh.Rows()); !eq(got, want) {
		t.Errorf("restored rail %v, want %v", got, want)
	}
}

// The network's own log is a buffer row like any other — it is only ever
// singled out because the menus that act on a network also act from it.
func TestTheServerLogIsARow(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#go"})

	rows := h.Rows()
	if !rows[0].IsServerLog() {
		t.Errorf("row 0 = %+v, want the network log", rows[0])
	}
	if rows[1].IsServerLog() {
		t.Errorf("row 1 = %+v, want a plain channel", rows[1])
	}
	if !rows[0].Selectable() {
		t.Error("the network log cannot be shown")
	}
}
