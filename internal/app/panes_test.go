package app

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// M12's first slice where it meets the app: the rail and the People pane are
// the widgets, the splits are draggable and remembered, and the two toggles
// are wired. What the rail decides (which rows, which pills, which folds) is
// the viewmodel's and is tested there; what the widgets draw is tested in
// internal/ui/widgets. This is the wiring between them.

// attachSession gives a real window a real Session without a socket: enough
// for the panes, which ask it for the roster, the PREFIX table and who we are.
//
// It is retired from the funnel immediately, and that is not tidiness. This
// session is here to be ASKED things, not to deliver them — and under Fyne's
// test driver fyne.Do runs the batch INLINE on the funnel's own goroutine
// (test.driver.DoFromGoroutine), so a delivery landing while the test body is
// laying widgets out would be two goroutines measuring text at once. In the
// shipped app fyne.Do queues onto the one UI thread and the question does not
// arise; the tests that exercise deliveries use the testUI sink, which holds
// no widgets.
func attachSession(t *testing.T, u *UI, profile string) *Session {
	t.Helper()
	s := NewSession(Config{Server: "example.invalid", Port: "6667", Nick: "toprak"}, u.dispatch)
	s.Name = profile
	u.dispatch.Close(s.ID())
	u.sessions = append(u.sessions, s)
	u.vm.AddSession(s.ID(), profile)
	t.Cleanup(s.Quit)
	return s
}

// bound is the first shortcut currently bound to an action, as the canvas
// would deliver it.
func bound(t *testing.T, u *UI, action string) *desktop.CustomShortcut {
	t.Helper()
	list := combosFor(u.app.Preferences(), action)
	if len(list) == 0 {
		t.Fatalf("no combo bound to %q", action)
	}
	return list[0].shortcut()
}

// The sidebar is the Rail: one widget, reading the viewmodel, on the quieter
// surface — and there is no second buffer list hiding behind it.
func TestTheSidebarIsTheRail(t *testing.T) {
	u := newTestUI(t)
	if u.rail == nil {
		t.Fatal("no rail")
	}
	if u.people == nil {
		t.Fatal("no People pane")
	}
	// the rail sits inside the ThemeOverride, which is what makes it darker
	if u.sidePane == nil {
		t.Fatal("the rail is not on the rail surface")
	}
	// and the panes are in draggable splits, not fixed rectangles
	if u.railSplit == nil || u.peopleSplit == nil {
		t.Fatal("the panes are not in splits")
	}
	if u.railSplit.Leading != fyne.CanvasObject(u.sidePane) {
		t.Error("the rail is not the leading half of its split")
	}
	if u.peopleSplit.Trailing != fyne.CanvasObject(u.people) {
		t.Error("the People pane is not the trailing half of its split")
	}
}

// Ctrl+B hides and shows the rail. A Split gives all its room to whichever
// child is visible, so this is the whole of it.
func TestCtrlBTogglesTheRail(t *testing.T) {
	u := newTestUI(t)
	u.sidePane.Show()

	if !u.handleShortcut(bound(t, u, "rail")) {
		t.Fatal("the rail shortcut was not claimed")
	}
	if u.sidePane.Visible() {
		t.Error("Ctrl+B did not hide the rail")
	}
	u.handleShortcut(bound(t, u, "rail"))
	if !u.sidePane.Visible() {
		t.Error("Ctrl+B did not bring the rail back")
	}
}

// The app menu belongs to the identity chip at the bottom of the rail, and the
// toolbar keeps the same menu for a window with no rail on it. Exactly one of
// the two is ever on screen — and there is never none, which is the trap:
// hiding the rail must not hide the only way into Preferences.
func TestOnlyOneGearIsEverOnScreen(t *testing.T) {
	u := newTestUI(t)

	if u.sidePane.Visible() {
		t.Fatal("the idle window opened with a rail")
	}
	if !u.settingsBtn.Visible() {
		t.Error("an idle window has no menu at all")
	}

	u.setRailVisible(true)
	if u.settingsBtn.Visible() {
		t.Error("two gears on screen: the chip has one and so does the toolbar")
	}
	u.toggleRail()
	if !u.settingsBtn.Visible() {
		t.Error("Ctrl+B took the menu away with the rail")
	}
}

// Ctrl+U toggles the People pane, and the choice is remembered PER BUFFER:
// closing it in a channel must not close it in the next one you open, and
// coming back must find it as you left it.
func TestCtrlUTogglesPeopleAndIsRememberedPerBuffer(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.vm.SetBuffers(s.ID(), []string{serverBuffer, "#go", "#eco"})

	show := func(name string, members ...string) {
		u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: name})
		ms := make([]model.Member, 0, len(members))
		for _, n := range members {
			ms = append(ms, model.Member{Nick: n})
		}
		u.vm.SetMembers(ms)
		u.applyPeople()
	}

	show("#go", "alice", "bob")
	if !u.people.Visible() {
		t.Fatal("a channel with a roster opened with the People pane closed")
	}
	u.handleShortcut(bound(t, u, "people"))
	if u.people.Visible() {
		t.Error("Ctrl+U did not close the pane")
	}

	show("#eco", "carol")
	if !u.people.Visible() {
		t.Error("closing the pane in #go closed it in #eco too")
	}

	show("#go", "alice", "bob")
	if u.people.Visible() {
		t.Error("#go forgot that its pane was closed")
	}
}

// The split positions are the user's, and they survive a restart. Fyne has no
// drag callback, so the toggles and the window's close are where they are
// written down.
func TestSplitOffsetsArePersisted(t *testing.T) {
	u := newTestUI(t)
	u.railSplit.SetOffset(0.31)
	u.peopleSplit.SetOffset(0.62)
	u.saveSplits()

	p := u.app.Preferences()
	if got := p.Float(prefRailSplit); got != 0.31 {
		t.Errorf("rail offset saved as %v, want 0.31", got)
	}
	if got := p.Float(prefPeopleSplit); got != 0.62 {
		t.Errorf("people offset saved as %v, want 0.62", got)
	}

	// a second window on the same preferences comes up where the first left off
	next := NewUI(u.app, u.win)
	if got := next.railSplit.Offset; got != 0.31 {
		t.Errorf("restored rail offset %v, want 0.31", got)
	}
	if got := next.peopleSplit.Offset; got != 0.62 {
		t.Errorf("restored people offset %v, want 0.62", got)
	}

	// and a preferences file somebody edited into a corner is refused
	p.SetFloat(prefRailSplit, 0.0)
	third := NewUI(u.app, u.win)
	if got := third.railSplit.Offset; got != defaultRailOffset {
		t.Errorf("offset restored from a pinned-shut preference = %v, want the default", got)
	}
}

// Folded networks survive a restart too, which is the other half of the same
// promise: the rail comes back the shape you left it.
func TestFoldedNetworksArePersisted(t *testing.T) {
	u := newTestUI(t)
	a := attachSession(t, u, "libera")
	b := attachSession(t, u, "ergo")
	u.vm.SetBuffers(a.ID(), []string{serverBuffer, "#go"})
	u.vm.SetBuffers(b.ID(), []string{serverBuffer, "#eco"})

	u.vm.SetCollapsed("ergo", true)
	u.saveFolds()

	next := NewUI(u.app, u.win)
	next.vm.AddSession("s-x", "ergo")
	if !next.vm.Collapsed("ergo") {
		t.Error("the folded network came back unfolded")
	}
	if next.vm.Collapsed("libera") {
		t.Error("a network that was not folded came back folded")
	}
}

// SelfChanged is the funnel's newest change and the identity chip is why it
// exists: the nick, the account behind the logged-in badge, and away.
func TestSelfChangedFillsTheIdentityChip(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: serverBuffer})

	u.SelfChanged(s.ID(), "toprak", "toprak", true)
	if got := u.rail.Identity(); got != (widgets.Identity{Nick: "toprak", Account: "toprak", Away: true}) {
		t.Errorf("chip = %+v, want the nick, the account and away", got)
	}

	// another network changing its nick must not rewrite the chip of the one
	// on screen
	u.SelfChanged("someone-else", "impostor", "", false)
	if got := u.rail.Identity().Nick; got != "toprak" {
		t.Errorf("chip nick = %q after a background network moved, want toprak", got)
	}
}

// The rail's ⋯ menu is §4.1's list, and it is the same list right-click gets
// (the widget half of that is tested in internal/ui/widgets).
func TestRailMenuContents(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.vm.SetBuffers(s.ID(), []string{serverBuffer, "#go"})

	labels := func(items []*fyne.MenuItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.Label)
		}
		return strings.Join(out, "|")
	}

	offline := viewmodel.Row{Kind: viewmodel.RowNetwork, Profile: "oftc"}
	if got := labels(u.rowMenuItems(offline)); !strings.Contains(got, "Connect") {
		t.Errorf("offline network menu %q, want Connect first", got)
	}

	header := viewmodel.Row{Kind: viewmodel.RowNetwork, Profile: "libera",
		Ref: viewmodel.Ref{Session: s.ID()}, Live: true}
	got := labels(u.rowMenuItems(header))
	for _, want := range []string{"Find channels", "Show network log", "Edit network…", "Disconnect"} {
		if !strings.Contains(got, want) {
			t.Errorf("network menu %q is missing %q (§4.1)", got, want)
		}
	}

	// from the log itself, "go to the log" is not an action
	log := viewmodel.Row{Kind: viewmodel.RowBuffer, Profile: "libera",
		Ref: viewmodel.Ref{Session: s.ID(), Name: serverBuffer}, Live: true}
	if got := labels(u.rowMenuItems(log)); strings.Contains(got, "Show network log") {
		t.Errorf("the network log's own menu offers to show it: %q", got)
	} else if !strings.Contains(got, "Find channels") {
		t.Errorf("the network log does not offer its network's actions: %q", got)
	}

	// a buffer row can be dismissed without being opened
	ch := viewmodel.Row{Kind: viewmodel.RowBuffer, Profile: "libera",
		Ref: viewmodel.Ref{Session: s.ID(), Name: "#go"}, Live: true}
	if got := labels(u.rowMenuItems(ch)); !strings.Contains(got, "Mark as read") {
		t.Errorf("channel menu %q, want Mark as read", got)
	}
}

// A member's menu offers the CHANNEL operator verbs only where there is a
// channel to be an operator of. In a private conversation there is no mode to
// set and no one to kick, and a menu that offered it would be four items that
// send commands the server refuses.
func TestMemberMenuOffersOpVerbsOnlyInAChannel(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#go")
	s.getBuffer("alice")
	u.refreshBuffers()

	opVerbs := []string{"Give op (+o)", "Kick", "Ban (+b)", "Kickban"}

	u.selectRow(s, "#go")
	inChannel := menuLabels(u.memberMenuItems("bob"))
	for _, want := range opVerbs {
		if findMenuItem(u.memberMenuItems("bob"), want) == nil {
			t.Errorf("in a channel the member menu %v is missing %q", inChannel, want)
		}
	}

	u.selectRow(s, "alice")
	inQuery := menuLabels(u.memberMenuItems("alice"))
	for _, unwanted := range opVerbs {
		if findMenuItem(u.memberMenuItems("alice"), unwanted) != nil {
			t.Errorf("in a private conversation the member menu %v offers %q", inQuery, unwanted)
		}
	}
	if findMenuItem(u.memberMenuItems("alice"), "Whois") == nil {
		t.Errorf("the private-conversation member menu %v lost the verbs that DO apply", inQuery)
	}
}

// The People pane ranks people by what the SERVER advertised, not by a string
// in this program: the roster and the PREFIX table travel together.
func TestTheRosterCarriesTheServersPrefixTable(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: "#go"})
	u.setRoster(s)

	modes, prefixes := s.Prefixes()
	if modes == "" || prefixes == "" {
		t.Fatal("the session reports no PREFIX table at all")
	}
	// the store's default is proto's, and it is what reached the pane
	u.vm.SetMembers([]model.Member{
		{Nick: "alice", Prefixes: string(prefixes[0])},
		{Nick: "bob"},
	})
	rows := u.vm.PeopleRows()
	if len(rows) != 4 || rows[0].Kind != viewmodel.PeopleSection {
		t.Fatalf("rows %+v, want a section, alice, a section, bob", rows)
	}
	if rows[0].Title == "" || rows[0].Title == string(prefixes[0]) {
		t.Errorf("section title %q, want the mode's name", rows[0].Title)
	}
}
