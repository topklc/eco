package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The rail's widget half. What rows exist is the viewmodel's and is tested
// there; what is left here is what the pixels decide — which pill, which
// gesture does what, and that the two menus are one menu.

// TestPillSelection is §4.1's "grey = unread count, accent-filled = mentions",
// as a table. It is the whole visual difference between "there is traffic" and
// "this is addressed to you", and it is a pure function so it can be checked
// without drawing anything.
func TestPillSelection(t *testing.T) {
	cases := []struct {
		name   string
		counts viewmodel.Counts
		text   string
		fill   fyne.ThemeColorName
		show   bool
	}{
		{name: "nothing waiting", counts: viewmodel.Counts{}},
		{name: "unread is the muted pill", counts: viewmodel.Counts{Unread: 4},
			text: "4", fill: ecotheme.UnreadPill, show: true},
		{name: "a mention takes the accent", counts: viewmodel.Counts{Unread: 40, Highlights: 2},
			text: "2", fill: ecotheme.MentionPill, show: true},
		{name: "a mention with no unread still counts", counts: viewmodel.Counts{Highlights: 1},
			text: "1", fill: ecotheme.MentionPill, show: true},
		{name: "past ninety-nine the number stops being information",
			counts: viewmodel.Counts{Unread: 1200}, text: "99+", fill: ecotheme.UnreadPill, show: true},
		{name: "and mentions cap the same way",
			counts: viewmodel.Counts{Highlights: 100}, text: "99+", fill: ecotheme.MentionPill, show: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, fill, ink, show := pillFor(c.counts)
			if show != c.show {
				t.Fatalf("show = %v, want %v", show, c.show)
			}
			if !show {
				return
			}
			if text != c.text {
				t.Errorf("pill reads %q, want %q", text, c.text)
			}
			if fill != c.fill {
				t.Errorf("pill fill %q, want %q", fill, c.fill)
			}
			if c.fill == ecotheme.MentionPill && ink != ecotheme.OnAccent {
				t.Errorf("mention pill ink %q, want the ink meant for an accent fill", ink)
			}
		})
	}
}

// railHarness is a rail over a model, with every intent recorded.
type railHarness struct {
	*Rail
	vm        *viewmodel.Model
	shown     []viewmodel.Ref
	connected []string
	menus     []viewmodel.Row
}

func newRailHarness(t testing.TB) *railHarness {
	t.Helper()
	testApp(t)
	h := &railHarness{vm: viewmodel.New(viewmodel.Hooks{})}
	h.Rail = NewRail(RailOptions{
		Model:   h.vm,
		Show:    func(r viewmodel.Ref) { h.shown = append(h.shown, r) },
		Connect: func(p string) { h.connected = append(h.connected, p) },
		Menu: func(row viewmodel.Row) []*fyne.MenuItem {
			h.menus = append(h.menus, row)
			return []*fyne.MenuItem{fyne.NewMenuItem("Connect", func() {})}
		},
	})
	return h
}

// twoNetworkRail: two connected profiles and one pinned-but-offline, which is
// the shape that has every row kind in it.
func twoNetworkRail(t testing.TB) *railHarness {
	h := newRailHarness(t)
	h.vm.AddSession("s-1", "libera")
	h.vm.SetBuffers("s-1", []string{viewmodel.ServerBuffer, "#go"})
	h.vm.AddSession("s-2", "ergo")
	h.vm.SetBuffers("s-2", []string{viewmodel.ServerBuffer, "#eco"})
	h.vm.SetPinned([]string{"oftc"})
	return h
}

// row is the rail's row at an index, poured through a row widget the way the
// List pours it.
func (h *railHarness) row(t testing.TB, i int) *railRow {
	t.Helper()
	r, ok := h.vm.Row(i)
	if !ok {
		t.Fatalf("no row %d", i)
	}
	w := newRailRow(h.Rail)
	w.set(i, r)
	return w
}

// Clicking an offline network must never dial. It is the difference between a
// rail of pinned networks you can browse and one where a stray click opens
// five connections — so the row's own tap does nothing, and [Connect] is the
// only thing that connects.
func TestClickingAnOfflineNetworkNeverConnects(t *testing.T) {
	h := twoNetworkRail(t)
	var offline viewmodel.Row
	for _, r := range h.vm.Rows() {
		if r.IsNetwork() && !r.Live {
			offline = r
		}
	}
	if offline.Profile != "oftc" {
		t.Fatal("no offline network in the rail")
	}

	h.tap(offline)
	if len(h.connected) != 0 {
		t.Errorf("clicking the row dialled %v", h.connected)
	}
	if len(h.shown) != 0 {
		t.Errorf("clicking the row showed %v", h.shown)
	}

	// the button, and only the button
	w := h.row(t, h.vm.Index(offline))
	if !w.connect.Visible() {
		t.Fatal("the offline network has no [Connect] button")
	}
	w.connect.Tapped(&fyne.PointEvent{})
	if len(h.connected) != 1 || h.connected[0] != "oftc" {
		t.Errorf("[Connect] dialled %v, want [oftc]", h.connected)
	}
}

// Clicking a live network header folds it; clicking a buffer shows it.
func TestTappingRowsMeansTheRightThing(t *testing.T) {
	h := twoNetworkRail(t)
	rows := h.vm.Rows()

	h.tap(rows[0]) // the libera header
	if !h.vm.Collapsed("libera") {
		t.Error("tapping a live network header did not fold it")
	}
	h.tap(h.vm.Rows()[0])
	if h.vm.Collapsed("libera") {
		t.Error("tapping it again did not unfold it")
	}
	if len(h.shown) != 0 {
		t.Errorf("folding a network showed a buffer: %v", h.shown)
	}

	h.tap(h.vm.Rows()[2]) // #go
	if len(h.shown) != 1 || h.shown[0].Name != "#go" {
		t.Errorf("shown %v, want #go", h.shown)
	}
}

// The Parity Rule, structurally: the ⋯ overflow and the right-click menu are
// one list, built once. Both paths reach the same function with the same row,
// so they cannot come apart.
func TestOverflowAndRightClickAreOneMenu(t *testing.T) {
	h := twoNetworkRail(t)
	w := h.row(t, 0) // the libera header

	if !w.more.Visible() {
		t.Fatal("a network header has no visible ⋯ overflow menu (§4.1)")
	}
	w.more.tap(fyne.NewPos(0, 0))
	w.TappedSecondary(&fyne.PointEvent{})

	if len(h.menus) != 2 {
		t.Fatalf("the two paths asked for %d menus, want 2", len(h.menus))
	}
	if h.menus[0].Profile != h.menus[1].Profile || h.menus[0].Kind != h.menus[1].Kind {
		t.Errorf("⋯ asked for %+v, right-click for %+v", h.menus[0], h.menus[1])
	}
}

// The chevron folds without selecting anything: it is inside a row whose tap
// means something else.
func TestTheChevronFoldsOnItsOwn(t *testing.T) {
	h := twoNetworkRail(t)
	w := h.row(t, 0)
	if !w.chevron.Visible() {
		t.Fatal("a live network header has no chevron")
	}
	w.chevron.tap(fyne.NewPos(0, 0))
	if !h.vm.Collapsed("libera") {
		t.Error("the chevron did not fold the network")
	}
	if len(h.shown) != 0 {
		t.Errorf("the chevron showed %v", h.shown)
	}
}

// A single network has no chrome to click: no header row, so no chevron and no
// ⋯ — and the caption is what names it instead.
func TestOneNetworkDrawsNoNetworkChrome(t *testing.T) {
	h := newRailHarness(t)
	h.vm.AddSession("s-1", "libera")
	h.vm.SetBuffers("s-1", []string{viewmodel.ServerBuffer, "#go"})
	h.Refresh() // through the call the app makes, so Refresh has to re-read the caption

	for i := range h.vm.Rows() {
		w := h.row(t, i)
		if w.chevron.Visible() || w.more.Visible() || w.connect.Visible() {
			t.Errorf("row %d drew network chrome on a single-network rail", i)
		}
		if w.row.Depth != 0 {
			t.Errorf("row %d is indented under a header that does not exist", i)
		}
	}
	if h.caption.Text != "libera" {
		t.Errorf("caption %q, want the network's name", h.caption.Text)
	}
	if !h.capBox.Visible() {
		t.Error("the caption is hidden")
	}

	h.vm.AddSession("s-2", "ergo")
	h.vm.SetBuffers("s-2", []string{viewmodel.ServerBuffer})
	h.Refresh()
	if h.caption.Text != "" || h.capBox.Visible() {
		t.Errorf("caption %q still showing with two networks", h.caption.Text)
	}
}

// Two networks invite the chrome, and each header wears exactly its own: a live
// one gets the chevron and the ⋯ and NOT [Connect], an offline one gets
// [Connect] and no chevron to fold rows it does not have. The two are one
// switch in railRow.set and one extra Show() in the wrong branch is a rail
// offering to dial a server it is already talking to.
func TestEachNetworkHeaderWearsItsOwnChrome(t *testing.T) {
	h := twoNetworkRail(t)
	h.Refresh()

	live, offline := 0, 0
	for i, row := range h.vm.Rows() {
		if !row.IsNetwork() {
			continue
		}
		w := h.row(t, i)
		switch {
		case row.Live:
			live++
			if !w.chevron.Visible() || !w.more.Visible() {
				t.Errorf("live header %q has no chevron/⋯", row.Profile)
			}
			if w.connect.Visible() {
				t.Errorf("live header %q offers [Connect] for a network it is connected to", row.Profile)
			}
		default:
			offline++
			if !w.connect.Visible() || !w.more.Visible() {
				t.Errorf("offline header %q has no [Connect]/⋯", row.Profile)
			}
			if w.chevron.Visible() {
				t.Errorf("offline header %q has a chevron; it has no rows to fold", row.Profile)
			}
		}
	}
	if live != 2 || offline != 1 {
		t.Fatalf("the fixture drew %d live and %d offline headers, want 2 and 1", live, offline)
	}

	// and a buffer row wears none of it
	for i, row := range h.vm.Rows() {
		if row.IsNetwork() {
			continue
		}
		if w := h.row(t, i); w.chevron.Visible() || w.more.Visible() || w.connect.Visible() {
			t.Errorf("buffer row %q wears network chrome", row.Ref.Name)
		}
	}
}

// The identity chip: the nick, the badge that says the server knows who you
// are, and the away toggle that reports what it will DO rather than what it is.
func TestIdentityChip(t *testing.T) {
	h := newRailHarness(t)
	var away []bool
	h.opts.Away = func(v bool) { away = append(away, v) }

	h.SetIdentity(Identity{Nick: "toprak"})
	if h.chip.badge.Visible() {
		t.Error("the logged-in badge shows for a nick with no account")
	}
	h.chip.away.Tapped(&fyne.PointEvent{})
	if len(away) != 1 || !away[0] {
		t.Errorf("away toggle sent %v, want [true]", away)
	}

	h.SetIdentity(Identity{Nick: "toprak", Account: "toprak", Away: true})
	if !h.chip.badge.Visible() {
		t.Error("no logged-in badge for an authenticated nick")
	}
	if !h.chip.away.on {
		t.Error("the away pill is not lit while away")
	}
	h.chip.away.Tapped(&fyne.PointEvent{})
	if len(away) != 2 || away[1] {
		t.Errorf("away toggle sent %v, want a request to come back", away)
	}
}

// The rail is a real widget: it lays out, and the rows it builds survive being
// drawn at a sensible width. This is the smoke test the headless tables cannot
// be — everything above pours rows through widgets without a canvas under them.
func TestRailRenders(t *testing.T) {
	h := twoNetworkRail(t)
	h.SetIdentity(Identity{Nick: "toprak", Account: "toprak"})
	h.vm.SetUnread("s-2", "#eco", viewmodel.Counts{Unread: 3, Highlights: 1})

	w := test.NewWindow(h.Rail)
	defer w.Close()
	w.Resize(fyne.NewSize(220, 600))
	h.Refresh()

	if got := h.MinSize().Width; got > 220 {
		t.Errorf("the rail's minimum width is %v: it cannot be dragged narrow", got)
	}
	if h.list.Length() != len(h.vm.Rows()) {
		t.Errorf("the list holds %d rows, the model has %d", h.list.Length(), len(h.vm.Rows()))
	}
}

// A name too wide for its column is cut and marked, not drawn over whatever is
// to its right — canvas.Text has no truncation of its own.
func TestElide(t *testing.T) {
	testApp(t)
	m := newMeasurer(14)
	style := fyne.TextStyle{}
	full := "#a-very-long-channel-name-indeed"
	wide := m.width(full, style)

	if got := m.elide(full, style, wide+10); got != full {
		t.Errorf("elide of a name that fits = %q, want it whole", got)
	}
	got := m.elide(full, style, wide/2)
	if got == full {
		t.Fatal("a name twice its column's width was not cut")
	}
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Errorf("elided name %q does not say it was cut", got)
	}
	if w := m.width(got, style); w > wide/2 {
		t.Errorf("elided name measures %v, wider than the %v it was given", w, wide/2)
	}
	if got := m.elide("anything", style, 0); got != "" {
		t.Errorf("elide into no room = %q, want nothing", got)
	}
}

// A pinned row wears its digit, faintly, and an unpinned one wears nothing.
// The alternative is a person keeping the Alt+1..9 mapping in their head, or a
// legend elsewhere that is right until they pin something.
func TestAPinnedRowShowsItsDigit(t *testing.T) {
	h := twoNetworkRail(t)
	if got := h.vm.Pin(viewmodel.Ref{Session: "s-2", Name: "#eco"}); got != 1 {
		t.Fatalf("pin = %d, want 1", got)
	}
	var pinned, plain *railRow
	for i := range h.vm.RowCount() {
		w := h.row(t, i)
		switch {
		case w.row.Ref.Name == "#eco":
			pinned = w
		case w.row.Ref.Name == "#go":
			plain = w
		}
	}
	if pinned == nil || plain == nil {
		t.Fatal("the fixture lost a row")
	}
	if !pinned.digit.Visible() {
		t.Error("a pinned row draws no digit")
	}
	if got := pinned.digit.Text; got != "1" {
		t.Errorf("digit = %q, want 1", got)
	}
	if plain.digit.Visible() {
		t.Error("an unpinned row drew a digit")
	}
	// unpinning takes it off again
	h.vm.Unpin(viewmodel.Ref{Session: "s-2", Name: "#eco"})
	for i := range h.vm.RowCount() {
		if w := h.row(t, i); w.row.Ref.Name == "#eco" && w.digit.Visible() {
			t.Error("the digit outlived the pin")
		}
	}
}
