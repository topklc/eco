package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// rowMenuItems builds the right-click menu for a sidebar row: network
// management on roots, channel tools on channels, user tools on PMs.
func (u *UI) rowMenuItems(ref rowRef) []*fyne.MenuItem {
	p := u.app.Preferences()
	var items []*fyne.MenuItem
	switch {
	case ref.root && ref.sess != nil:
		sess, profile := ref.sess, ref.profile
		items = append(items,
			fyne.NewMenuItem("Browse channels", func() {
				u.selectRow(sess, serverBuffer)
				if len(sess.ChannelList()) > 0 {
					u.showChannelBrowser()
				} else {
					u.handleInput("/list")
				}
			}),
			fyne.NewMenuItem("Edit network…", func() { u.showNetworkManagerFor(profile) }),
			u.pinMenuItem(profile),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Reconnect", func() {
				for _, n := range loadNetworks(p) {
					if n.Name == profile {
						u.connectTo(n)
						return
					}
				}
			}),
			fyne.NewMenuItem("Disconnect network", func() { u.closeSession(sess) }),
		)
	case ref.root && ref.sess == nil:
		profile := ref.profile
		items = append(items,
			fyne.NewMenuItem("Connect", func() {
				for _, n := range loadNetworks(p) {
					if n.Name == profile {
						u.connectTo(n)
						return
					}
				}
			}),
			fyne.NewMenuItem("Edit network…", func() { u.showNetworkManagerFor(profile) }),
			u.pinMenuItem(profile),
		)
	case strings.HasPrefix(ref.name, "#"):
		sess, ch := ref.sess, ref.name
		ajLabel := "Add to autojoin"
		if sess != nil && autojoinHas(p, sess.Name, ch) {
			ajLabel = "Remove from autojoin"
		}
		jpLabel := "Hide joins/parts here"
		if sess != nil && !hideJoinParts.Load() && joinPartsHiddenFor(sess.Name, ch) {
			jpLabel = "Show joins/parts here"
		}
		items = append(items,
			fyne.NewMenuItem("Channel settings…", func() { u.showChannelSettings(sess, ch) }),
			fyne.NewMenuItem("Invite someone…", func() { u.showInviteTo(sess, ch) }),
			fyne.NewMenuItem("Rejoin (cycle)", func() { sess.Cycle(ch) }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem(ajLabel, func() {
				if autojoinToggle(p, sess.Name, ch) {
					sess.status(serverBuffer, ch+" added to autojoin for "+sess.Name+".")
				} else {
					sess.status(serverBuffer, ch+" removed from autojoin for "+sess.Name+".")
				}
			}),
			fyne.NewMenuItem(jpLabel, func() {
				if hideJoinParts.Load() {
					sess.status(serverBuffer, "Joins/parts are hidden everywhere by the global preference.")
					return
				}
				toggleJPHidden(p, sess.Name, ch)
			}),
			fyne.NewMenuItem("Clear history", func() {
				sess.ClearBuffer(ch)
				if sess == u.sess && strings.EqualFold(ch, u.current) {
					u.showBuffer(u.current)
				}
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Leave channel", func() { sess.Part(ch) }),
		)
	default:
		sess, nick := ref.sess, ref.name
		igLabel := "Ignore user"
		if isIgnored(nick) {
			igLabel = "Unignore user"
		}
		items = append(items,
			fyne.NewMenuItem("Whois", func() {
				if sess.irc != nil {
					sess.mu.Lock()
					sess.whoisTarget = nick
					sess.mu.Unlock()
					sess.irc.Send("WHOIS", nick)
				}
			}),
			fyne.NewMenuItem(igLabel, func() {
				if toggleIgnore(p, nick) {
					sess.status(serverBuffer, "Ignoring "+nick+". Their messages will not be shown.")
				} else {
					sess.status(serverBuffer, "No longer ignoring "+nick+".")
				}
				u.memberList.Refresh()
			}),
			fyne.NewMenuItem("Close conversation", func() { sess.CloseBuffer(nick) }),
		)
	}
	return items
}

// pinMenuItem toggles a profile's sidebar pin.
func (u *UI) pinMenuItem(profile string) *fyne.MenuItem {
	p := u.app.Preferences()
	pinned := false
	for _, n := range loadNetworkMeta(p) {
		if n.Name == profile {
			pinned = n.Pinned
		}
	}
	label := "Pin to sidebar"
	if pinned {
		label = "Unpin from sidebar"
	}
	return fyne.NewMenuItem(label, func() {
		setPinned(p, profile, !pinned)
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

// memberRow is a nick in the member pane: left click opens a PM,
// right click offers user and channel-operator actions.
type memberRow struct {
	widget.Label
	u *UI
}

func newMemberRow(u *UI) *memberRow {
	r := &memberRow{u: u}
	r.ExtendBaseWidget(r)
	return r
}

func (r *memberRow) nick() string { return strings.TrimLeft(r.Text, "~&@%+") }

func (r *memberRow) Tapped(_ *fyne.PointEvent) {
	nick := r.nick()
	if nick == "" || r.u.sess == nil {
		return
	}
	r.u.sess.getBuffer(nick)
	r.u.selectBuffer(nick)
}

func (r *memberRow) TappedSecondary(e *fyne.PointEvent) {
	r.u.showMemberMenu(r.nick(), e)
}

func (u *UI) showMemberMenu(nick string, e *fyne.PointEvent) {
	sess := u.sess
	if nick == "" || sess == nil {
		return
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
		fyne.NewMenuItem("Whois", func() {
			if sess.irc != nil {
				sess.mu.Lock()
				sess.whoisTarget = u.current
				sess.mu.Unlock()
				sess.irc.Send("WHOIS", nick)
			}
		}),
	}
	if ch := u.current; strings.HasPrefix(ch, "#") && !self {
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
				u.memberList.Refresh()
			}))
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), u.win.Canvas(), e.AbsolutePosition)
}

// msgView wraps the message RichText to add a right-click menu with quick
// actions on the most recent messages.
type msgView struct {
	widget.RichText
	u *UI
}

func newMsgView(u *UI) *msgView {
	v := &msgView{u: u}
	// the zero value of Scroll is ScrollBoth, which nests a scroller
	// inside our own scroll container and collapses the text to nothing
	v.Scroll = fyne.ScrollNone
	v.ExtendBaseWidget(v)
	return v
}

func (v *msgView) TappedSecondary(e *fyne.PointEvent) {
	v.u.showMessageMenu(e)
}

func (u *UI) showMessageMenu(e *fyne.PointEvent) {
	sess := u.sess
	if sess == nil {
		return
	}
	b := sess.Buffer(u.current)
	if b == nil {
		return
	}
	// last visible message, and our own most recent redactable one
	var last *Line
	lastSelfID := ""
	sess.mu.Lock()
	for i := len(b.Lines) - 1; i >= 0; i-- {
		l := b.Lines[i]
		if l.Status {
			continue
		}
		if last == nil {
			cp := l
			last = &cp
		}
		if lastSelfID == "" && l.Self && l.MsgID != "" {
			lastSelfID = l.MsgID
		}
		if last != nil && lastSelfID != "" {
			break
		}
	}
	sess.mu.Unlock()

	var items []*fyne.MenuItem
	if last != nil {
		text, nick := last.Text, last.Nick
		items = append(items, fyne.NewMenuItem("Copy last message", func() {
			u.win.Clipboard().SetContent(text)
		}))
		if nick != "" {
			items = append(items, fyne.NewMenuItem("Copy nick: "+nick, func() {
				u.win.Clipboard().SetContent(nick)
			}))
		}
	}
	if lastSelfID != "" {
		id := lastSelfID
		items = append(items, fyne.NewMenuItem("Delete your last message", func() {
			u.handleInput("/redact " + id)
		}))
	}
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	items = append(items, fyne.NewMenuItem("Load older messages", func() {
		u.handleInput("/history")
	}))
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), u.win.Canvas(), e.AbsolutePosition)
}
