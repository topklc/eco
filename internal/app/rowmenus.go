package app

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// rowMenuItems builds one rail row's actions: network management on a network
// header (and on its log, which is the same network), channel tools on
// channels, user tools on private conversations.
//
// It is ONE list for two gestures. The rail's visible ⋯ overflow and its
// right-click menu both ask for it (blueprint §4.1: right-click mirrors the
// menu, never replaces it), so there is no second list to fall behind.
func (u *UI) rowMenuItems(row viewmodel.Row) []*fyne.MenuItem {
	p := u.app.Preferences()
	sess := u.sessionFor(row.Ref.Session)
	var items []*fyne.MenuItem
	switch {
	case row.IsNetwork() || row.IsServerLog():
		// The network's own log offers the network's actions, because it IS
		// the network as far as anything you can do to it goes — which is what
		// makes the header optional when there is only one (R-6).
		return u.networkMenuItems(row.Profile, sess, !row.IsServerLog())
	case strings.HasPrefix(row.Ref.Name, "#"):
		ch := row.Ref.Name
		ajLabel := "Add to autojoin"
		if sess != nil && config.AutojoinHas(p, sess.Name, ch) {
			ajLabel = "Remove from autojoin"
		}
		jpLabel := "Hide joins/parts here"
		if sess != nil && !config.Current().HideJoinParts && joinPartsHiddenFor(sess.Name, ch) {
			jpLabel = "Show joins/parts here"
		}
		items = append(items,
			u.notifyMenuItems(sess, ch),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Channel settings…", func() { u.showChannelSettings(sess, ch) }),
			fyne.NewMenuItem("Invite someone…", func() { u.showInviteTo(sess, ch) }),
			fyne.NewMenuItem("Rejoin (cycle)", func() { sess.Cycle(ch) }),
			fyne.NewMenuItemSeparator())
		items = append(items, u.historyMenuItems(sess, ch)...)
		items = append(items,
			fyne.NewMenuItem(ajLabel, func() {
				if config.AutojoinToggle(p, sess.Name, ch) {
					sess.status(serverBuffer, ch+" added to autojoin for "+sess.Name+".")
				} else {
					sess.status(serverBuffer, ch+" removed from autojoin for "+sess.Name+".")
				}
			}),
			fyne.NewMenuItem(jpLabel, func() {
				if config.Current().HideJoinParts {
					sess.status(serverBuffer, "Joins/parts are hidden everywhere by the global preference.")
					return
				}
				toggleJPHidden(p, sess.Name, ch)
			}),
			fyne.NewMenuItem("Clear history", func() {
				sess.ClearBuffer(ch)
				if sess == u.session() && strings.EqualFold(ch, u.current()) {
					u.renderBuffer()
				}
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Leave channel", func() { sess.Part(ch) }),
		)
	default:
		nick := row.Ref.Name
		igLabel := "Ignore user"
		if isIgnored(nick) {
			igLabel = "Unignore user"
		}
		items = append(items,
			u.notifyMenuItems(sess, nick),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Whois", func() { sess.Whois(nick, nick) }),
			fyne.NewMenuItemSeparator())
		items = append(items, u.historyMenuItems(sess, nick)...)
		items = append(items,
			fyne.NewMenuItem(igLabel, func() {
				if toggleIgnore(p, nick) {
					sess.status(serverBuffer, "Ignoring "+nick+". Their messages will not be shown.")
				} else {
					sess.status(serverBuffer, "No longer ignoring "+nick+".")
				}
				u.people.Refresh()
			}),
			fyne.NewMenuItem("Close conversation", func() { sess.CloseBuffer(nick) }),
		)
	}
	// Every buffer row can be dismissed from the rail without opening it —
	// which is what the pill is asking about — and pinned to a digit, which is
	// where Alt+1..9 gets its meaning (§4.3, viewmodel/hotlist.go).
	if row.Kind == viewmodel.RowBuffer {
		items = append(items, fyne.NewMenuItemSeparator(),
			u.pinBufferMenuItem(row),
			fyne.NewMenuItem("Mark as read", func() {
				u.vm.MarkRead(row.Ref)
				u.rail.Refresh()
			}))
	}
	return items
}

// showBufferMenu drops the chat header's ⋮ menu. §4.1 asks for a kebab
// "carrying every action that also lives on right-click (Parity Rule)", so this
// does not build a list: it asks rowMenuItems for the SAME one the rail row
// offers, for the buffer on screen. Two lists that had to be kept in step is
// exactly what the rule exists to forbid.
func (u *UI) showBufferMenu(anchor fyne.CanvasObject) {
	r := u.vm.Current()
	if r.IsZero() || u.sessionFor(r.Session) == nil {
		return
	}
	row := viewmodel.Row{Kind: viewmodel.RowBuffer, Ref: r, Live: true,
		Profile: u.vm.Profile(r.Session), Pin: u.vm.PinnedAt(r)}
	items := u.rowMenuItems(row)
	if len(items) == 0 {
		return
	}
	widget.ShowPopUpMenuAtRelativePosition(fyne.NewMenu("", items...), u.win.Canvas(),
		fyne.NewPos(0, anchor.Size().Height), anchor)
}

// networkMenuItems is §4.1's overflow menu: Connect / Disconnect / Edit / Find
// channels / Show network log, plus the pin, and it is what right-click on a
// network offers too. showLog is false when the menu was opened from the log
// itself, where "go there" means nothing.
func (u *UI) networkMenuItems(profile string, sess *Session, showLog bool) []*fyne.MenuItem {
	if sess == nil {
		// offline: the only interesting verb is the one that fixes that
		return []*fyne.MenuItem{
			fyne.NewMenuItem("Connect", func() { u.connectProfile(profile) }),
			fyne.NewMenuItem("Edit network…", func() { u.showNetworkManagerFor(profile) }),
			u.networkNotifyMenuItems(profile, nil),
			u.pinMenuItem(profile),
		}
	}
	items := []*fyne.MenuItem{
		u.networkNotifyMenuItems(profile, sess),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Find channels", func() {
			// One surface for finding a room (§4.1's Switcher, which absorbed
			// browser.go): the menu item opens it rather than a second dialog
			// that only knows about channels you are not in.
			u.showSwitcher()
			if len(sess.ChannelList()) == 0 {
				sess.RequestList()
			}
		}),
	}
	if showLog {
		items = append(items,
			fyne.NewMenuItem("Show network log", func() { u.selectRow(sess, serverBuffer) }))
	}
	return append(items,
		fyne.NewMenuItem("Edit network…", func() { u.showNetworkManagerFor(profile) }),
		u.pinMenuItem(profile),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Reconnect", func() { u.connectProfile(profile) }),
		fyne.NewMenuItem("Disconnect", func() { u.closeSession(sess) }),
	)
}

// historyMenuItems are the per-buffer half of the privacy contract (blueprint
// §5.4): logging is on by default, and this is where a person turns it off for
// one channel or one conversation, and where they erase what is already saved.
//
// The setting lives in the archive, not in preferences, because it is a fact
// about that buffer's history and has to survive a restart with it — and
// because the Recorder enforces it there, so a message from a buffer with
// logging off never reaches a statement, let alone the file.
func (u *UI) historyMenuItems(s *Session, buffer string) []*fyne.MenuItem {
	if s == nil || !s.Archiving() {
		off := fyne.NewMenuItem("History is not being saved", func() {})
		off.Disabled = true
		return []*fyne.MenuItem{off}
	}
	on := s.Logging(buffer)
	label := "Stop saving history here"
	if !on {
		label = "Save history here"
	}
	toggle := fyne.NewMenuItem(label, func() {
		s.SetLogging(buffer, !on)
		if on {
			s.status(buffer, "New messages in "+buffer+" will not be saved. What is already saved stays until you erase it.")
		} else {
			s.status(buffer, "Saving history for "+buffer+" again.")
		}
	})
	erase := fyne.NewMenuItem("Erase saved history…", func() {
		dialog.ShowConfirm("Erase saved history",
			"Delete everything eco has saved for "+buffer+" on this computer?\n\n"+
				"This cannot be undone. Messages still on screen stay until you close the buffer.",
			func(ok bool) {
				if !ok {
					return
				}
				s.ForgetBuffer(buffer)
				s.status(buffer, "Erased the saved history for "+buffer+".")
			}, u.win)
	})
	return []*fyne.MenuItem{toggle, erase}
}

// pinBufferMenuItem is the Alt+digit pin: what it is now, and how to change it.
// The label says the DIGIT, because the digit is the whole point — a menu item
// reading "Pin" would leave the user to count the pins themselves.
func (u *UI) pinBufferMenuItem(row viewmodel.Row) *fyne.MenuItem {
	if at := u.vm.PinnedAt(row.Ref); at > 0 {
		return fyne.NewMenuItem("Unpin from Alt+"+strconv.Itoa(at), func() {
			u.vm.Unpin(row.Ref)
			u.savePins()
			u.rail.Refresh()
		})
	}
	next := len(u.vm.PinKeys()) + 1
	if next > viewmodel.MaxPins {
		it := fyne.NewMenuItem("All nine pins are taken", func() {})
		it.Disabled = true
		return it
	}
	return fyne.NewMenuItem("Pin to Alt+"+strconv.Itoa(next), func() {
		u.vm.Pin(row.Ref)
		u.savePins()
		u.rail.Refresh()
	})
}

// pinMenuItem toggles a profile's sidebar pin.
func (u *UI) pinMenuItem(profile string) *fyne.MenuItem {
	p := u.app.Preferences()
	pinned := false
	for _, n := range config.LoadMeta(p) {
		if n.Name == profile {
			pinned = n.Pinned
		}
	}
	label := "Pin to sidebar"
	if pinned {
		label = "Unpin from sidebar"
	}
	return fyne.NewMenuItem(label, func() {
		config.SetPinned(p, profile, !pinned)
		u.refreshBuffers()
	})
}

// channelOptionsForm is the one topic/key/+i/+m/+s form, shared between
// channel creation (empty baseline) and channel settings (tracked state),
// so the two dialogs cannot drift apart.
type channelOptionsForm struct {
	topic                      *widget.Entry
	key                        *widget.Entry
	invite, moderated, secret  *widget.Check
	curModes, curKey, curTopic string
}

func newChannelOptionsForm(curModes, curKey, curTopic string) *channelOptionsForm {
	f := &channelOptionsForm{curModes: curModes, curKey: curKey, curTopic: curTopic}
	f.topic = widget.NewEntry()
	f.topic.SetText(curTopic)
	f.topic.SetPlaceHolder("optional")
	f.key = widget.NewPasswordEntry()
	f.key.SetText(curKey)
	f.key.SetPlaceHolder("no key")
	f.invite = widget.NewCheck("Invite only: users must be invited (+i)", nil)
	f.invite.SetChecked(strings.Contains(curModes, "i"))
	f.moderated = widget.NewCheck("Moderated: only voiced users may speak (+m)", nil)
	f.moderated.SetChecked(strings.Contains(curModes, "m"))
	f.secret = widget.NewCheck("Secret: hidden from the channel list (+s)", nil)
	f.secret.SetChecked(strings.Contains(curModes, "s"))
	return f
}

func (f *channelOptionsForm) items() []*widget.FormItem {
	return []*widget.FormItem{
		widget.NewFormItem("Topic", f.topic),
		widget.NewFormItem("Key", f.key),
		widget.NewFormItem("", f.invite),
		widget.NewFormItem("", f.moderated),
		widget.NewFormItem("", f.secret),
	}
}

// commands computes the MODE/TOPIC commands that turn the baseline state
// into the chosen one; only diffs are sent.
func (f *channelOptionsForm) commands(ch string) [][]string {
	var plus, minus string
	var params []string
	flag := func(m byte, want bool) {
		has := strings.Contains(f.curModes, string(m))
		if want && !has {
			plus += string(m)
		} else if !want && has {
			minus += string(m)
		}
	}
	flag('i', f.invite.Checked)
	flag('m', f.moderated.Checked)
	flag('s', f.secret.Checked)
	newKey := strings.TrimSpace(f.key.Text)
	if newKey != "" && newKey != f.curKey {
		plus += "k"
		params = append(params, newKey)
	} else if newKey == "" && f.curKey != "" {
		minus += "k"
		params = append(params, f.curKey) // -k requires the key on some servers
	}
	modestr := ""
	if plus != "" {
		modestr += "+" + plus
	}
	if minus != "" {
		modestr += "-" + minus
	}
	var cmds [][]string
	if modestr != "" {
		cmds = append(cmds, append([]string{"MODE", ch, modestr}, params...))
	}
	if t := strings.TrimSpace(f.topic.Text); t != f.curTopic {
		cmds = append(cmds, []string{"TOPIC", ch, t})
	}
	return cmds
}

// showChannelSettings edits topic/key/flag modes; it sends only the diffs,
// so it is safe to open without ops (the server will refuse what it must).
func (u *UI) showChannelSettings(s *Session, ch string) {
	b := s.Buffer(ch)
	if b == nil || s.irc == nil {
		return
	}
	s.mu.Lock()
	f := newChannelOptionsForm(b.Modes, b.Key, b.Topic)
	s.mu.Unlock()
	dialog.ShowForm("Settings for "+ch, "Apply", "Cancel", f.items(), func(ok bool) {
		if !ok || s.irc == nil {
			return
		}
		for _, c := range f.commands(ch) {
			s.irc.Send(c[0], c[1:]...)
		}
	}, u.win)
}

// showInviteTo prompts for a nick to invite into ch.
func (u *UI) showInviteTo(s *Session, ch string) {
	nick := widget.NewEntry()
	nick.SetPlaceHolder("nickname")
	items := []*widget.FormItem{widget.NewFormItem("Nick", nick)}
	dialog.ShowForm("Invite to "+ch, "Invite", "Cancel", items, func(ok bool) {
		n := strings.TrimSpace(nick.Text)
		if ok && n != "" && s.irc != nil {
			s.irc.Send("INVITE", n, ch)
			s.status(ch, "Invited "+n+".")
		}
	}, u.win)
}

// memberMenuItems is one member's actions: the conversation, whois, the
// channel-operator verbs when we are looking at a channel, and the ignore
// toggle. The People pane's hover kebab and its right-click menu both ask for
// it, so the two cannot offer different things (the Parity Rule, §4.1).
func (u *UI) memberMenuItems(nick string) []*fyne.MenuItem {
	sess := u.session()
	if nick == "" || sess == nil {
		return nil
	}
	send := func(params ...string) func() {
		return func() {
			if sess.irc != nil {
				sess.irc.Send(params[0], params[1:]...)
			}
		}
	}
	self := sess.irc != nil && strings.EqualFold(nick, sess.irc.CurrentNick())
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Message", func() {
			sess.getBuffer(nick)
			u.selectBuffer(nick)
		}),
		fyne.NewMenuItem("Whois", func() { sess.Whois(u.current(), nick) }),
	}
	if ch := u.current(); strings.HasPrefix(ch, "#") && !self {
		items = append(items, fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Give op (+o)", send("MODE", ch, "+o", nick)),
			fyne.NewMenuItem("Take op (-o)", send("MODE", ch, "-o", nick)),
			fyne.NewMenuItem("Give voice (+v)", send("MODE", ch, "+v", nick)),
			fyne.NewMenuItem("Take voice (-v)", send("MODE", ch, "-v", nick)),
			fyne.NewMenuItem("Kick", send("KICK", ch, nick)),
			fyne.NewMenuItem("Ban (+b)", send("MODE", ch, "+b", nick+"!*@*")),
			fyne.NewMenuItem("Kickban", func() {
				if sess.irc != nil {
					sess.irc.Send("MODE", ch, "+b", nick+"!*@*")
					sess.irc.Send("KICK", ch, nick)
				}
			}),
		)
		// invite into any other joined channel
		var chItems []*fyne.MenuItem
		for _, b := range sess.Buffers() {
			name := b.Name
			if strings.HasPrefix(name, "#") && !strings.EqualFold(name, ch) {
				chItems = append(chItems, fyne.NewMenuItem(name, send("INVITE", nick, name)))
			}
		}
		if len(chItems) > 0 {
			inv := fyne.NewMenuItem("Invite to", nil)
			inv.ChildMenu = fyne.NewMenu("", chItems...)
			items = append(items, inv)
		}
	}
	if !self {
		igLabel := "Ignore user"
		if isIgnored(nick) {
			igLabel = "Unignore user"
		}
		items = append(items, fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem(igLabel, func() {
				if toggleIgnore(u.app.Preferences(), nick) {
					sess.status(serverBuffer, "Ignoring "+nick+". Their messages will not be shown.")
				} else {
					sess.status(serverBuffer, "No longer ignoring "+nick+".")
				}
				u.people.Refresh()
			}))
	}
	return items
}
