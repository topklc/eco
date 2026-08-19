package app

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/state"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// M12's third slice where it meets the app: the notification levels and their
// storage, the smart join/part filter's wiring, the highlight words, the
// hotlist keys and the badge. What each of them DECIDES is tested where it is
// decided — the §4.4 pipeline and the filter rule in internal/state, the
// hotlist's order and the pins in internal/ui/viewmodel. This is the wiring.

// --- notification levels --------------------------------------------------------

// The level a person picks in a menu has to reach the store, because the store
// is where §4.4 is evaluated — and the setting has to be readable back, or the
// menu cannot show which one is chosen.
func TestNotifyLevelReachesTheStoreAndComesBack(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	joinChannel(t, s, srv, "#eco", "@alice")

	if got := s.NotifyLevel("#eco"); got != state.NotifyDefault {
		t.Fatalf("a fresh buffer's level = %v, want the inherited default", got)
	}
	s.SetNotifyLevel("#eco", state.NotifyMute)
	if got := s.NotifyLevel("#eco"); got != state.NotifyMute {
		t.Errorf("level = %v, want mute", got)
	}
	if got := s.EffectiveNotifyLevel("#eco"); got != state.NotifyMute {
		t.Errorf("effective level = %v, want mute", got)
	}

	srv.Send("@msgid=m1 :alice!alice@test PRIVMSG #eco :%s: are you there?", testNick)
	srv.Sync()
	if ui.Notified(model.Line{Nick: "alice", Text: testNick + ": are you there?"}) {
		t.Error("a muted channel raised a notification for a mention")
	}

	// and back to inheriting: the menu's fourth item
	s.SetNotifyLevel("#eco", state.NotifyDefault)
	if got := s.NotifyLevel("#eco"); got != state.NotifyDefault {
		t.Errorf("level after clearing = %v, want the default", got)
	}
	srv.Send("@msgid=m2 :alice!alice@test PRIVMSG #eco :%s: still there?", testNick)
	srv.Sync()
	if !ui.Notified(model.Line{Nick: "alice", Text: testNick + ": still there?"}) {
		t.Error("clearing the level did not restore notifications")
	}
}

// The level is a fact about the buffer and lives in the archive's row for it,
// beside the read marker — so it is still there after a restart. This is the
// column the schema has carried since M9, finally written to.
func TestNotifyLevelSurvivesARestart(t *testing.T) {
	first, srv, _, path := archiveSession(t)
	joinChannel(t, first, srv, "#eco", "@alice")
	first.SetNotifyLevel("#eco", state.NotifyMute)
	settleUI(first)
	first.Quit()
	if err := first.closeArchive(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}

	// A NEW process, over the same file.
	second, srv2, _, _ := restartOver(t, path)
	joinChannel(t, second, srv2, "#eco", "@alice")
	settleUI(second)

	if got := second.NotifyLevel("#eco"); got != state.NotifyMute {
		t.Fatalf("the level after a restart = %v, want mute — it did not come back from the archive", got)
	}
	// and it is in force, not just remembered
	if got := second.EffectiveNotifyLevel("#eco"); got != state.NotifyMute {
		t.Error("the restored level is not the one being applied")
	}
}

// §4.4 asks for a per-network default. It beats the global preference for that
// network and nothing else, and it applies without waiting for the wire.
func TestPerNetworkNotifyDefault(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")

	u.setNetworkNotify("libera", s, state.NotifyMute)
	if got := s.EffectiveNotifyLevel("#eco"); got != state.NotifyMute {
		t.Errorf("effective level = %v, want the network default", got)
	}
	if got, ok := profileNotifyLevel(config.Current(), "libera"); !ok || got != state.NotifyMute {
		t.Errorf("stored profile level = %v (%v), want mute", got, ok)
	}
	// another profile is untouched
	if _, ok := profileNotifyLevel(config.Current(), "ergo"); ok {
		t.Error("setting one network's default set another's")
	}

	// a buffer's own level still beats it, which is §4.4's order
	s.SetNotifyLevel("#eco", state.NotifyAll)
	if got := s.EffectiveNotifyLevel("#eco"); got != state.NotifyAll {
		t.Errorf("effective level = %v, want the buffer's own", got)
	}

	u.setNetworkNotify("libera", s, state.NotifyDefault)
	if _, ok := profileNotifyLevel(config.Current(), "libera"); ok {
		t.Error("choosing the global setting left the per-network one behind")
	}
}

// The menus are one list for two gestures (§4.1's Parity Rule): the rail row's
// right-click and the chat header's kebab both build from rowMenuItems, and the
// notification submenu is in it with the current level checked.
func TestTheNotificationMenuIsInBothPlaces(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, "#eco")
	s.SetNotifyLevel("#eco", state.NotifyMute)

	row := viewmodel.Row{Kind: viewmodel.RowBuffer, Ref: viewmodel.Ref{Session: s.ID(), Name: "#eco"},
		Live: true, Profile: "libera"}
	items := u.rowMenuItems(row)
	notify := findMenuItem(items, "Notifications")
	if notify == nil {
		t.Fatal("the row menu has no Notifications submenu")
	}
	if notify.ChildMenu == nil {
		t.Fatal("Notifications has no items")
	}
	var checked []string
	for _, it := range notify.ChildMenu.Items {
		if it.Checked {
			checked = append(checked, it.Label)
		}
	}
	if len(checked) != 1 || checked[0] != "Mute" {
		t.Errorf("checked items = %v, want just Mute", checked)
	}

	// choosing a level from the menu moves it
	for _, it := range notify.ChildMenu.Items {
		if it.Label == "All messages" {
			it.Action()
		}
	}
	if got := s.NotifyLevel("#eco"); got != state.NotifyAll {
		t.Errorf("after choosing All the level is %v", got)
	}
}

// The network header's menu carries the network's default, in both its states.
func TestTheNetworkMenuCarriesItsDefault(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")

	online := u.networkMenuItems("libera", s, true)
	if findMenuItem(online, "Notifications for this network") == nil {
		t.Error("a connected network's menu has no notification default")
	}
	offline := u.networkMenuItems("ergo", nil, true)
	if findMenuItem(offline, "Notifications for this network") == nil {
		t.Error("an offline network's menu has no notification default")
	}
}

// --- focus suppression -------------------------------------------------------------

// §4.4's third step: the buffer you are reading does not raise a toast. The
// store has had the rule since M9 and NOTHING was ever wired into it — the
// focused buffer was always the zero one — so this is the wire, and a mutation
// that removes focusBuffer turns it red.
func TestTheBufferOnScreenDoesNotNotify(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("#other")
	u.refreshBuffers()

	u.showRef(viewmodel.Ref{Session: s.ID(), Name: "#eco"})
	if got := s.store.Focused(s.netID); got.Folded != "#eco" {
		t.Fatalf("the store's focused buffer is %q, want #eco", got.Folded)
	}
	u.showRef(viewmodel.Ref{Session: s.ID(), Name: "#other"})
	if got := s.store.Focused(s.netID); got.Folded != "#other" {
		t.Errorf("the focus did not follow the switch: %q", got.Folded)
	}
}

// Only one buffer can be on screen, so every other session is blurred. Without
// that, a background network keeps believing its buffer is being read and goes
// quiet for a conversation nobody is looking at.
func TestSwitchingNetworksBlursTheOther(t *testing.T) {
	u := newTestUI(t)
	a := attachSession(t, u, "libera")
	b := attachSession(t, u, "ergo")
	a.getBuffer("#eco")
	b.getBuffer("#go")
	u.refreshBuffers()

	u.showRef(viewmodel.Ref{Session: a.ID(), Name: "#eco"})
	u.showRef(viewmodel.Ref{Session: b.ID(), Name: "#go"})

	if got := a.store.Focused(a.netID); !got.IsZero() {
		t.Errorf("the network left behind still has %q focused", got.Folded)
	}
	if got := b.store.Focused(b.netID); got.Folded != "#go" {
		t.Errorf("the network switched to has %q focused, want #go", got.Folded)
	}
}

// --- the badge ------------------------------------------------------------------------

// §4.4: the OS badge is the mention count, and only that. Fyne 2.8 has no badge
// API, so the honest maximum is the window title — which is what a taskbar
// shows — and unread deliberately does not appear in it.
func TestTheBadgeCountsMentionsOnly(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("#busy")
	u.refreshBuffers()
	u.setBaseTitle("eco: libera")

	u.UnreadChanged(s.ID(), dispatch.Buffer{Name: "#busy"}, 47, 0)
	if got := u.win.Title(); got != "eco: libera" {
		t.Errorf("title = %q; unread must not badge", got)
	}

	u.UnreadChanged(s.ID(), dispatch.Buffer{Name: "#eco"}, 3, 2)
	if got := u.win.Title(); got != "(2) eco: libera" {
		t.Errorf("title = %q, want the mention count", got)
	}

	u.UnreadChanged(s.ID(), dispatch.Buffer{Name: "#eco"}, 0, 0)
	if got := u.win.Title(); got != "eco: libera" {
		t.Errorf("title = %q, want the badge gone", got)
	}
}

// --- the smart join/part filter --------------------------------------------------------

// The filter ships ON and is wired end to end: a stranger's part leaves no line,
// and the same event from someone who has been talking does. The rule itself is
// tested in internal/state; this is the preference reaching the store.
func TestSmartFilterIsWiredAndOnByDefault(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	// The M5 harness pins the filter OFF (see pinTestSettings); this test is
	// about the shipped default, so it puts it back and lets the session pick
	// the change up the way the running app does.
	withSetting(t, func(c *config.Settings) { c.SmartJoinParts = true })
	joinChannel(t, s, srv, "#eco", "@alice", "quiet")

	srv.Send(":quiet!quiet@test PART #eco")
	srv.Sync()
	if got := statusLines(ui.Lines("#eco"), "quiet"); len(got) != 0 {
		t.Errorf("a silent stranger's part was shown: %v", got)
	}

	srv.Send("@msgid=s1 :alice!alice@test PRIVMSG #eco :morning")
	srv.Sync()
	srv.Send(":alice!alice@test PART #eco")
	srv.Sync()
	if got := statusLines(ui.Lines("#eco"), "alice"); len(got) != 1 {
		t.Errorf("a recent speaker's part = %v, want one line", got)
	}
}

// Turning the filter off in Preferences applies at once, not at the next
// message: applyDisplaySettings pushes it into every live session.
func TestTurningTheSmartFilterOffApplies(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	if !s.store.SmartJoinParts(s.netID) {
		t.Fatal("a fresh session did not start with the filter on")
	}

	u.app.Preferences().SetBool("smartjoinparts", false)
	u.applyDisplaySettings()
	if s.store.SmartJoinParts(s.netID) {
		t.Error("turning the filter off in Preferences did not reach the store")
	}

	u.app.Preferences().SetBool("smartjoinparts", true)
	u.applyDisplaySettings()
	if !s.store.SmartJoinParts(s.netID) {
		t.Error("turning it back on did not reach the store")
	}
}

// --- highlight words -------------------------------------------------------------------

// §4.4's user word list: typed in Preferences, evaluated in the store, and
// applied to a live session without waiting for anything.
func TestHighlightWordsReachTheStore(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")

	u.app.Preferences().SetString("highlightwords", "deploy, release train")
	u.applyDisplaySettings()

	if got := s.store.HighlightWords(s.netID); len(got) != 2 || got[0] != "deploy" {
		t.Errorf("highlight words in the store = %v", got)
	}
	if got := config.Current().HighlightWords; len(got) != 2 || got[1] != "release train" {
		t.Errorf("parsed words = %v", got)
	}

	u.app.Preferences().SetString("highlightwords", "")
	u.applyDisplaySettings()
	if got := s.store.HighlightWords(s.netID); len(got) != 0 {
		t.Errorf("clearing the field left %v behind", got)
	}
}

// A highlight word fires a real notification, whole-word, through the whole
// pipeline. This is the end-to-end proof that the field is not decoration.
func TestAHighlightWordNotifies(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	withSetting(t, func(c *config.Settings) { c.HighlightWords = []string{"deploy"} })
	joinChannel(t, s, srv, "#eco", "@alice")

	srv.Send("@msgid=h1 :alice!alice@test PRIVMSG #eco :the deploy is done")
	srv.Sync()
	if !ui.Notified(model.Line{Nick: "alice", Text: "the deploy is done"}) {
		t.Error("a highlight word did not notify")
	}
	srv.Send("@msgid=h2 :alice!alice@test PRIVMSG #eco :redeploying now")
	srv.Sync()
	if ui.Notified(model.Line{Nick: "alice", Text: "redeploying now"}) {
		t.Error("a highlight word matched inside another word")
	}
}

// --- the hotlist and the pins ------------------------------------------------------------

// Alt+A is the hotlist: mentions first. The ordering is the viewmodel's and is
// tested there; this is the key reaching it and the rail following.
func TestAltAJumpsToTheMentionFirst(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#busy")
	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	u.vm.SetUnread(s.ID(), "#busy", viewmodel.Counts{Unread: 20})
	u.vm.SetUnread(s.ID(), "#eco", viewmodel.Counts{Unread: 1, Highlights: 1})

	if !u.handleShortcut(bound(t, u, "unread")) {
		t.Fatal("Alt+A was not handled")
	}
	if got := u.current(); got != "#eco" {
		t.Errorf("Alt+A landed on %q, want the mention", got)
	}
	if !u.handleShortcut(bound(t, u, "unread")) {
		t.Fatal("the second Alt+A was not handled")
	}
	if got := u.current(); got != "#busy" {
		t.Errorf("the second Alt+A landed on %q, want the unread one", got)
	}
}

// Alt+1..9 are the pins: set from the row menu, persisted, and the digit jumps
// to the conversation rather than to a row number.
func TestAltDigitsJumpToPinnedBuffers(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("#go")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	row := viewmodel.Row{Kind: viewmodel.RowBuffer, Live: true, Profile: "libera",
		Ref: viewmodel.Ref{Session: s.ID(), Name: "#go"}}
	pin := u.pinBufferMenuItem(row)
	if pin.Label != "Pin to Alt+1" {
		t.Fatalf("menu item = %q, want the digit it would take", pin.Label)
	}
	pin.Action()

	if got := u.app.Preferences().String(prefRailPins); !strings.Contains(got, "#go") {
		t.Errorf("the pin was not written down: %q", got)
	}
	if !u.handleShortcut(altDigit(1)) {
		t.Fatal("Alt+1 was not handled")
	}
	if got := u.current(); got != "#go" {
		t.Errorf("Alt+1 opened %q, want the pinned #go", got)
	}
	// a digit nobody pinned does nothing, quietly
	before := u.current()
	u.handleShortcut(altDigit(5))
	if u.current() != before {
		t.Error("an unpinned digit moved the viewer")
	}
	// and the menu now offers to take the pin off, naming the digit
	row.Pin = u.vm.PinnedAt(row.Ref)
	if got := u.pinBufferMenuItem(row).Label; got != "Unpin from Alt+1" {
		t.Errorf("menu item = %q, want the unpin", got)
	}
}

// The submenu has to say which level is SET, not which is in force. They differ
// exactly when the buffer inherits — which is the common case — and the check
// mark is the only thing on screen that says whether a choice was made here or
// upstream.
//
// And the way back out has to name the level it would fall back TO. It named the
// level in force instead: mute a buffer on an all-messages network and the item
// read "Use the network's setting (mute)", which is the network's setting only
// by accident of the buffer's.
func TestTheNotifySubmenuChecksTheSetLevelAndNamesTheFallback(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, "#eco")

	// the network is loud, the buffer says nothing of its own
	u.setNetworkNotify("libera", s, state.NotifyAll)
	checked := checkedLabels(u.notifyMenuItems(s, "#eco"))
	if !eqStrings(checked, []string{"Use the network's setting (all messages)"}) {
		t.Errorf("with nothing set the menu checks %v, want only the inherit row", checked)
	}

	// now the buffer is muted: the check moves, and the inherit row still names
	// what it would go back to
	s.SetNotifyLevel("#eco", state.NotifyMute)
	parent := u.notifyMenuItems(s, "#eco")
	if checked := checkedLabels(parent); !eqStrings(checked, []string{"Mute"}) {
		t.Errorf("with the buffer muted the menu checks %v, want only Mute", checked)
	}
	if got := findMenuItem(parent.ChildMenu.Items, "Use the network's setting (all messages)"); got == nil {
		t.Errorf("the inherit row reads %v, want it to name the NETWORK's level (all messages)",
			menuLabels(parent.ChildMenu.Items))
	}
	if s.EffectiveNotifyLevel("#eco") != state.NotifyMute {
		t.Error("the muted buffer is not muted; this test is asserting the wrong thing")
	}
}

// checkedLabels is which of a Notifications submenu's rows wear the check.
func checkedLabels(parent *fyne.MenuItem) []string {
	var out []string
	if parent == nil || parent.ChildMenu == nil {
		return out
	}
	for _, it := range parent.ChildMenu.Items {
		if it.Checked {
			out = append(out, it.Label)
		}
	}
	return out
}

// Alt+1..9 are PINS. The safety rule is not just "an unpinned digit does
// nothing" but "it does not do something else": a fallback to the n-th row
// would be the absolute-row-number behaviour §4.3 exists to refuse, and it is
// invisible unless a digit is pressed that HAS an n-th row behind it.
func TestAnUnpinnedDigitNeverFallsBackToARowNumber(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("#go")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	// one pin, on the third selectable row; digits 2 and 3 are unpinned and
	// both have a row behind them
	u.vm.Pin(viewmodel.Ref{Session: s.ID(), Name: "#go"})
	if _, ok := u.vm.Nth(1); !ok {
		t.Fatal("the rail has no second selectable row; this test needs one to fall back TO")
	}
	before := u.current()
	for _, digit := range []int{2, 3, 9} {
		u.handleShortcut(altDigit(digit))
		if got := u.current(); got != before {
			t.Errorf("Alt+%d opened %q; nothing is pinned to it and rows are not pins",
				digit, got)
		}
	}
	// and the one that IS pinned still works
	u.handleShortcut(altDigit(1))
	if got := u.current(); got != "#go" {
		t.Errorf("Alt+1 opened %q, want the pinned #go", got)
	}
}

// --- helpers -----------------------------------------------------------------------------

// altDigit is the shortcut Alt+n as the canvas delivers it.
func altDigit(n int) *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: fyne.KeyName(rune('0' + n)), Modifier: fyne.KeyModifierAlt}
}

// withSetting publishes a tweaked settings snapshot for the test's lifetime.
func withSetting(t *testing.T, tweak func(*config.Settings)) {
	t.Helper()
	prev := config.Current()
	next := *prev
	tweak(&next)
	config.Publish(&next)
	t.Cleanup(func() { config.Publish(prev) })
}

// findMenuItem returns the item with this label, or nil.
func findMenuItem(items []*fyne.MenuItem, label string) *fyne.MenuItem {
	for _, it := range items {
		if it.Label == label {
			return it
		}
	}
	return nil
}

// statusLines returns the status lines mentioning a nick.
func statusLines(lines []Line, nick string) []string {
	var out []string
	for _, l := range lines {
		if l.Status && strings.Contains(l.Text, nick) {
			out = append(out, l.Text)
		}
	}
	return out
}

// --- the NEW divider, which everything else derives from -----------------------------

// §4.4 says every attention surface derives from the read marker, and §4.6 says
// each buffer "opens at its divider". It did not: arriving in a buffer marks it
// read — that is what makes the badge zero — and the divider was seeded from
// the marker AFTERWARDS, so it landed on the newest message and separated
// nothing. Every NEW divider in the program was invisible, which is why nothing
// had noticed.
//
// The seed is now sampled BEFORE the switch. This test is the difference:
// remove the sample in showRef and the divider lands after the unread messages
// again.
func TestTheDividerIsSeededBeforeTheSwitchMarksItRead(t *testing.T) {
	u, s := wiredUI(t)
	s.status("#other", "opening the buffer")
	u.refreshBuffers()
	u.showRef(viewmodel.Ref{Session: s.ID(), Name: "#eco"})

	// One message read an hour ago, one that arrived since, in a buffer the
	// viewer is NOT in.
	old := time.Now().Add(-time.Hour)
	s.mu.Lock()
	id := s.idLocked("#other")
	items := s.applyChanges(s.store.Apply(model.PrivMsg{
		EventMeta: model.EventMeta{Network: s.netID, MsgID: "d1", Time: old},
		Target:    "#other", Sender: model.Sender{Nick: "alice"}, Text: "read this",
	}))
	items = append(items, s.applyChanges(s.store.MarkRead(id, "d1", old))...)
	items = append(items, s.applyChanges(s.store.Apply(model.PrivMsg{
		EventMeta: model.EventMeta{Network: s.netID, MsgID: "d2", Time: time.Now()},
		Target:    "#other", Sender: model.Sender{Nick: "alice"}, Text: "unread!",
	}))...)
	s.mu.Unlock()
	s.emit(items)
	u.dispatch.Flush()
	u.BatchDone()

	marker := s.ReadMarker("#other")
	if !marker.Equal(old) {
		t.Fatalf("the fixture's marker is %v, want the older message at %v", marker, old)
	}

	// The switch below lays widgets out on THIS goroutine, and under the test
	// driver the funnel runs its batches INLINE on its own — and switching
	// buffers emits (the focus moves the marker). Two goroutines measuring text
	// is the hazard attachSession's comment names; what this test asserts is the
	// seed, not a delivery, so the funnel is stopped first.
	u.dispatch.Stop()
	u.showRef(viewmodel.Ref{Session: s.ID(), Name: "#other"})

	if got := u.timeline.Since(); !got.Equal(marker) {
		t.Errorf("the divider was seeded with %v, want the marker as it stood before the switch (%v)",
			got, marker)
	}
	// and the divider is really in the rows, above the unread message
	rows := u.timeline.Rows()
	newAt, unreadAt := -1, -1
	for i, r := range rows {
		switch {
		case r.Kind == widgets.RowNew:
			newAt = i
		case r.Line.Text == "unread!":
			unreadAt = i
		}
	}
	if newAt < 0 {
		t.Fatal("no NEW divider was drawn for a buffer with unread messages")
	}
	if unreadAt < 0 || newAt > unreadAt {
		t.Errorf("the divider is at row %d and the unread message at %d; it must be above it", newAt, unreadAt)
	}
}
