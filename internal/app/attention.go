package app

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/state"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// Attention as a person sets it — blueprint §4.4's per-buffer notification
// levels, their per-network default, and the OS badge.
//
// The RULE is not here. §4.4 is evaluated in exactly one place (internal/state:
// level, then highlight, then focus suppression) and this file is the two ends
// of it: where the levels are stored so they survive a restart, and where the
// window says which buffer is being read so the suppression step has something
// to suppress against.

// notifyChoices are the levels a menu offers, in the order they are offered:
// loudest first, because that is the order of the sentence people say to
// themselves ("tell me everything / only when I am named / nothing").
var notifyChoices = []struct {
	Label string
	Level state.NotifyLevel
}{
	{"All messages", state.NotifyAll},
	{"Only mentions", state.NotifyMentions},
	{"Mute", state.NotifyMute},
}

// notifyLevelName is a level as a menu writes it.
func notifyLevelName(l state.NotifyLevel) string {
	for _, c := range notifyChoices {
		if c.Level == l {
			return c.Label
		}
	}
	return "Use the network's setting"
}

// --- the buffer's level ----------------------------------------------------------

// SetNotifyLevel sets one buffer's notification level and writes it down.
//
// The setting lives in the archive's buffers row, beside the read marker and
// the logging flag, because it is a fact ABOUT THAT BUFFER and has to come back
// with it after a restart — the same reasoning the per-buffer logging opt-out
// was built on, and the column the schema has been carrying since M9
// (buffers.notify_level).
func (s *Session) SetNotifyLevel(name string, l state.NotifyLevel) {
	s.mu.Lock()
	id := s.idLocked(name)
	s.store.SetNotifyLevel(id, l)
	s.mu.Unlock()

	a := s.arc
	if a == nil {
		return
	}
	folded := archive.FoldedName(id)
	a.do(func() {
		if err := a.db.SetNotifyLevel(a.ctx, folded, uint8(l)); err != nil {
			s.archiveFailed("saving a notification setting", err)
		}
	})
}

// NotifyLevel returns the level SET on a buffer — NotifyDefault when it is
// inheriting the network's, which is what a menu needs to know to put the check
// mark in the right place.
func (s *Session) NotifyLevel(name string) state.NotifyLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.NotifyLevelSet(s.idLocked(name))
}

// EffectiveNotifyLevel returns the level in force for a buffer, with
// NotifyDefault already resolved against the network.
func (s *Session) EffectiveNotifyLevel(name string) state.NotifyLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.NotifyLevelFor(s.idLocked(name))
}

// DefaultNotifyLevel is the level this network's buffers inherit — what the menu
// names beside "Use the network's setting". It is NOT EffectiveNotifyLevel: a
// muted buffer's effective level is mute, and offering "use the network's
// setting (mute)" would tell the user the network is muted when it is not.
func (s *Session) DefaultNotifyLevel() state.NotifyLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.NotifyLevelDefault(s.netID)
}

// restoreNotifyLevel applies a level read back from the archive. It is called
// on the hydration path, where a buffer's stored settings arrive, and it never
// writes: the value came FROM the file.
func (s *Session) restoreNotifyLevelLocked(id model.BufferID, level uint8) {
	if level == 0 {
		return
	}
	s.store.SetNotifyLevel(id, state.NotifyLevel(level))
}

// --- the network's default --------------------------------------------------------

// profileNotify returns the network default the user set for this profile. It
// comes from the settings snapshot, like every other preference a session
// reads: the profile is named in one map (config.Settings.NetworkNotify) rather
// than the session holding a Preferences of its own.
func (s *Session) profileNotify() (state.NotifyLevel, bool) {
	return profileNotifyLevel(config.Current(), s.Name)
}

// profileNotifyLevel reads one profile's default level out of a snapshot.
func profileNotifyLevel(set *config.Settings, profile string) (state.NotifyLevel, bool) {
	if set == nil || profile == "" {
		return 0, false
	}
	switch set.NetworkNotify[profile] {
	case "all":
		return state.NotifyAll, true
	case "mentions":
		return state.NotifyMentions, true
	case "mute":
		return state.NotifyMute, true
	}
	return 0, false
}

// ApplyAttentionSettings re-reads the settings snapshot into the store: the
// network's default level, the highlight words and the smart filter. The
// per-event syncs would get there on their own, but only once something
// happened on the wire — and a preference that takes effect on the next
// message is a preference that looks broken.
func (s *Session) ApplyAttentionSettings() {
	s.mu.Lock()
	s.syncNotifyLocked()
	s.syncAttentionLocked()
	s.mu.Unlock()
}

// --- the menus --------------------------------------------------------------------

// notifyMenuItems is the Notifications submenu for one buffer: the three levels
// plus the way back to the network's. It is built once and used by both the
// rail's row menu and the chat header's kebab, which is the Parity Rule (§4.1)
// made structural rather than promised.
func (u *UI) notifyMenuItems(s *Session, buffer string) *fyne.MenuItem {
	parent := fyne.NewMenuItem("Notifications", nil)
	if s == nil || buffer == "" {
		parent.Disabled = true
		return parent
	}
	set := s.NotifyLevel(buffer)
	inherited := s.DefaultNotifyLevel()

	items := make([]*fyne.MenuItem, 0, len(notifyChoices)+2)
	for _, c := range notifyChoices {
		c := c
		it := fyne.NewMenuItem(c.Label, func() {
			s.SetNotifyLevel(buffer, c.Level)
			u.refreshBuffers()
		})
		it.Checked = set == c.Level
		items = append(items, it)
	}
	def := fyne.NewMenuItem("Use the network's setting ("+
		strings.ToLower(notifyLevelName(inherited))+")", func() {
		s.SetNotifyLevel(buffer, state.NotifyDefault)
		u.refreshBuffers()
	})
	def.Checked = set == state.NotifyDefault
	items = append(items, fyne.NewMenuItemSeparator(), def)

	parent.ChildMenu = fyne.NewMenu("", items...)
	return parent
}

// networkNotifyMenuItems is the same thing for a whole network: the default its
// buffers inherit.
func (u *UI) networkNotifyMenuItems(profile string, s *Session) *fyne.MenuItem {
	parent := fyne.NewMenuItem("Notifications for this network", nil)
	if profile == "" {
		parent.Disabled = true
		return parent
	}
	cur, isSet := profileNotifyLevel(config.Current(), profile)

	items := make([]*fyne.MenuItem, 0, len(notifyChoices)+2)
	for _, c := range notifyChoices {
		c := c
		it := fyne.NewMenuItem(c.Label, func() {
			u.setNetworkNotify(profile, s, c.Level)
		})
		it.Checked = isSet && cur == c.Level
		items = append(items, it)
	}
	def := fyne.NewMenuItem("Use the global setting", func() {
		u.setNetworkNotify(profile, s, state.NotifyDefault)
	})
	def.Checked = !isSet
	items = append(items, fyne.NewMenuItemSeparator(), def)

	parent.ChildMenu = fyne.NewMenu("", items...)
	return parent
}

// setNetworkNotify writes a profile's default and applies it. The preference is
// written whether or not the network is connected, so one configured while
// offline is configured when it connects.
func (u *UI) setNetworkNotify(profile string, s *Session, l state.NotifyLevel) {
	level := ""
	if l != state.NotifyDefault {
		level = l.String()
	}
	config.SetNetworkNotify(u.app.Preferences(), profile, level)
	applySettings(u.app)
	if s != nil {
		s.ApplyAttentionSettings()
	}
	u.refreshBuffers()
}

// --- focus, and the badge ----------------------------------------------------------

// focusBuffer tells the sessions which buffer is being read, which is §4.4's
// suppression step: the store raises no notification for the buffer on screen.
//
// Every OTHER session is blurred, because only one buffer can be on screen and
// a session that still believed its buffer was would go quiet for a
// conversation nobody is looking at. This is the wire M9 built the rule for and
// nothing was ever plugged into — before this slice the store's focus was
// always the zero buffer, so the suppression step could not fire.
func (u *UI) focusBuffer(r viewmodel.Ref) {
	for _, s := range u.sessions {
		if s.ID() == r.Session {
			s.Focus(r.Name)
			continue
		}
		s.Blur()
	}
}

// Focus records the buffer the user is reading on this session.
func (s *Session) Focus(name string) {
	s.mu.Lock()
	items := s.applyChanges(s.store.Focus(s.idLocked(name)))
	s.mu.Unlock()
	s.emit(items)
}

// Blur records that no buffer of this session is being read.
func (s *Session) Blur() {
	s.mu.Lock()
	s.store.Blur(s.netID)
	s.mu.Unlock()
}

// updateBadge puts the mention count in the window title — §4.4's "OS badge
// (mention count only)", as honestly as Fyne 2.8 allows.
//
// There is no badge API in the toolkit: no dock badge, no taskbar overlay, no
// tray count. What every desktop DOES show for a window is its title, so that
// is where the number goes, in front of the name — "(3) eco: #eco" — and it is
// the mention count and nothing else. Unread is deliberately not in it: a
// number in the taskbar that counted every message would read "47" for a busy
// channel nobody has said your name in, which is the opposite of what a badge
// is for.
func (u *UI) updateBadge() {
	if u.win == nil {
		return
	}
	title := u.baseTitle
	if title == "" {
		title = "eco"
	}
	if n := u.vm.TotalMentions(); n > 0 {
		title = "(" + strconv.Itoa(n) + ") " + title
	}
	if title == u.shownTitle {
		return
	}
	u.shownTitle = title
	u.win.SetTitle(title)
}

// setBaseTitle records the window's title without the badge on it, and repaints
// the badge onto it.
func (u *UI) setBaseTitle(t string) {
	u.baseTitle = t
	u.updateBadge()
}
