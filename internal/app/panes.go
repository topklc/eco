package app

import (
	"strings"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The three regions and the two splits between them (blueprint §4.1). v1 sized
// its side panes with invisible 170px rectangles, which meant they were the
// size v1 said they were and no other; these are container.Splits, so they are
// the size the user drags them to — and that size is remembered.
//
// Everything here is rendering. Which buffer is shown, what the rail's rows
// are, whether the People pane belongs open for THIS buffer and which networks
// are folded are all the viewmodel's, and are tested there.

// Preference keys for the two offsets and the folded networks. The offsets are
// fractions (0..1), which is what a Split understands, so a window resized
// between runs keeps the proportion rather than a stale pixel count.
const (
	prefRailSplit   = "split.rail"
	prefPeopleSplit = "split.people"
	prefRailFolds   = "rail.folded"
	prefRailPins    = "rail.pins"
)

// Default proportions at the default 1000px window: a 220px rail (§4.1) and a
// People pane a little narrower than that.
const (
	defaultRailOffset   = 0.22
	defaultPeopleOffset = 0.80
)

// loadSplits restores the divider positions, and the folded networks with them.
func (u *UI) loadSplits() {
	p := u.app.Preferences()
	u.railSplit.SetOffset(clampOffset(p.FloatWithFallback(prefRailSplit, defaultRailOffset), defaultRailOffset))
	u.peopleSplit.SetOffset(clampOffset(p.FloatWithFallback(prefPeopleSplit, defaultPeopleOffset), defaultPeopleOffset))
	if s := p.String(prefRailFolds); s != "" {
		u.vm.SetCollapsedProfiles(strings.Split(s, "\x00"))
	}
	if s := p.String(prefRailPins); s != "" {
		u.vm.SetPinKeys(strings.Split(s, "\n"))
	}
}

// savePins writes the Alt+1..9 pins down. Each entry is profile+"\x00"+name and
// the entries are newline-separated, because a buffer name can contain neither
// — the pins are PROFILE-scoped so they survive a reconnect, which is the same
// reason drafts and counts are (viewmodel/hotlist.go).
func (u *UI) savePins() {
	u.app.Preferences().SetString(prefRailPins, strings.Join(u.vm.PinKeys(), "\n"))
}

// saveSplits writes the divider positions back.
//
// Fyne 2.8's Split has no "the user dragged me" callback, so this is called at
// the moments the answer is known to be settled — a pane toggled, a buffer
// shown, the window closed — and writes only when something actually moved. A
// drag followed by a crash is the one case that loses the position, and it
// costs a drag.
func (u *UI) saveSplits() {
	if u.railSplit == nil || u.peopleSplit == nil {
		return
	}
	p := u.app.Preferences()
	if got := u.railSplit.Offset; got != p.FloatWithFallback(prefRailSplit, defaultRailOffset) {
		p.SetFloat(prefRailSplit, got)
	}
	if got := u.peopleSplit.Offset; got != p.FloatWithFallback(prefPeopleSplit, defaultPeopleOffset) {
		p.SetFloat(prefPeopleSplit, got)
	}
}

// saveFolds remembers which networks are collapsed. It is the rail's callback,
// so folding one is durable the moment it happens.
func (u *UI) saveFolds() {
	u.app.Preferences().SetString(prefRailFolds, strings.Join(u.vm.CollapsedProfiles(), "\x00"))
}

// clampOffset keeps a stored offset inside a Split's range; anything outside it
// is a preference file somebody edited, and a rail pinned to 0 with no way back
// is not a state to restore into.
func clampOffset(v, def float64) float64 {
	if v < 0.05 || v > 0.95 {
		return def
	}
	return v
}

// toggleRail is Ctrl+B.
func (u *UI) toggleRail() {
	u.setRailVisible(!u.sidePane.Visible())
	u.saveSplits()
}

// setRailVisible shows or hides the rail. A Split gives all its space to
// whichever child is visible, so that is nearly the whole of it — the other
// half is the gear.
//
// The app menu belongs to the identity chip at the bottom of the rail (§4.1),
// and the toolbar keeps the same menu for the window that has no rail on it:
// an idle one, or one where Ctrl+B put the rail away. Exactly one of the two
// is on screen at a time, because two gears in one window is not a second way
// in, it is clutter.
func (u *UI) setRailVisible(v bool) {
	if v {
		u.sidePane.Show()
	} else {
		u.sidePane.Hide()
	}
	if u.settingsBtn != nil {
		if v {
			u.settingsBtn.Hide()
		} else {
			u.settingsBtn.Show()
		}
	}
	if u.railSplit != nil {
		u.railSplit.Refresh()
	}
}

// togglePeople is Ctrl+U. The choice is recorded against the buffer on screen
// — §4.1's per-buffer remembered visibility — and the pane follows it.
func (u *UI) togglePeople() {
	u.vm.TogglePeople()
	u.applyPeople()
	u.saveSplits()
}

// applyPeople opens or closes the People pane for the buffer on screen. It is
// the only place either happens, so the pane and the remembered choice cannot
// disagree.
func (u *UI) applyPeople() {
	want := u.vm.PeopleVisible() && u.session() != nil
	if want == u.people.Visible() {
		return
	}
	if want {
		u.people.Show()
	} else {
		u.people.Hide()
	}
	u.peopleSplit.Refresh()
}

// setRoster hands the People pane the buffer's members and the network's
// PREFIX table — the ranks come from what the SERVER advertised, never from a
// string in this program (blueprint §4.1).
func (u *UI) setRoster(s *Session) {
	if s == nil {
		u.vm.SetMembers(nil)
		return
	}
	modes, prefixes := s.Prefixes()
	u.vm.SetPrefixes(modes, prefixes)
	u.vm.SetMembers(s.Roster(u.current()))
	u.people.Refresh()
}

// identityOf is what the chip says about a session: the store's three facts,
// asked for rather than remembered. A copy kept in this layer would be the one
// that goes stale on a buffer switch — which is exactly how the logged-in
// badge would disappear the first time you opened a private conversation.
func (u *UI) identityOf(s *Session) widgets.Identity {
	if s == nil {
		return widgets.Identity{}
	}
	nick, account, away := s.Self()
	return widgets.Identity{Nick: nick, Account: account, Away: away}
}

// setIdentity draws the chip at the bottom of the rail.
func (u *UI) setIdentity(id widgets.Identity) { u.rail.SetIdentity(id) }

// connectProfile dials a network by profile name: the rail's [Connect] button
// and the menu item of the same name. Secrets are resolved only now, and only
// for the one profile chosen.
func (u *UI) connectProfile(profile string) {
	for _, n := range config.Load(u.app.Preferences()) {
		if n.Name == profile {
			u.connectTo(n)
			return
		}
	}
}
