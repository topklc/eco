package app

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/render"
)

type UI struct {
	app  fyne.App
	win  fyne.Window
	sess *Session

	current    string // buffer currently shown
	currentKey string // skey of the shown buffer; owns the draft slot

	bufferList *widget.List
	sessions   []*Session
	rows       []rowRef // sidebar rows: session roots and their buffers
	unread     map[string]int

	messages    *msgView
	msgScroll   *container.Scroll
	memberList  *widget.List
	memberPane  *fyne.Container
	bgImage     *canvas.Image
	members     []string
	topicLabel  *widget.Label
	bufferTitle *widget.Label
	input       *chatEntry
	status      *statusDot
	buffersBtn  *ttwidget.Button
	peopleBtn   *ttwidget.Button
	channelsBtn *ttwidget.Button
	boundKeys   []*desktop.CustomShortcut // rebindable shortcuts currently registered
	sidePane    *fyne.Container
	centerBox   *fyne.Container
	root        *fyne.Container

	flushPending bool // a coalesced message-pane refresh is scheduled
	unreadDirty  bool // buffer list needs a repaint on next flush
	segsDirty    bool // message pane content changed since the last flush

	activeDialog interface{ Hide() } // open dialog, closable with Escape

	selectEntry *widget.Entry // selectable plain text view of the buffer
	selectOn    bool
	selectKey   string // buffer identity of the rendered plaintext
	selectCount int    // lines already rendered into selectEntry

	tipButtons []tipButton // toolbar buttons with toggleable tooltips

	welcome *fyne.Container // idle screen: centered icon, shown when no server

	networkName string // display name of the connected network profile
	composeHint *widget.Label
	scrollAnim  *fyne.Animation // in-flight smooth scroll, if any
	stickBottom bool            // view was near the bottom when the batch began

	drafts    map[string]string // unsent input, per buffer
	hlBuffers map[string]bool   // buffers with an unseen mention
}

// smoothScrollToBottom eases the message pane down to the newest message
// instead of jumping.
func (u *UI) smoothScrollToBottom() {
	target := u.messages.MinSize().Height - u.msgScroll.Size().Height
	if target < 0 {
		target = 0
	}
	start := u.msgScroll.Offset.Y
	if target == start {
		return
	}
	if u.scrollAnim != nil {
		u.scrollAnim.Stop()
	}
	u.scrollAnim = fyne.NewAnimation(180*time.Millisecond, func(f float32) {
		u.msgScroll.Offset.Y = start + (target-start)*f
		u.msgScroll.Refresh()
	})
	u.scrollAnim.Curve = fyne.AnimationEaseOut
	u.scrollAnim.Start()
}

// updateComposeHint shows "n/max lines" while composing a multiline draft.
func (u *UI) updateComposeHint(lines int) {
	if lines <= 1 {
		if u.composeHint.Visible() {
			u.composeHint.Hide()
		}
		return
	}
	max := 24
	if u.sess != nil {
		max = u.sess.multilineMaxLines()
	}
	u.composeHint.SetText(fmt.Sprintf("%d/%d lines", lines, max))
	if !u.composeHint.Visible() {
		u.composeHint.Show()
	}
}

// bufferRow is a sidebar entry: right-click offers leave/close/mark-read.
type bufferRow struct {
	widget.Label
	u   *UI
	ref rowRef
}

func newBufferRow(u *UI) *bufferRow {
	r := &bufferRow{u: u}
	r.ExtendBaseWidget(r)
	return r
}

// Tapped forwards left clicks to list selection: implementing
// TappedSecondary makes this widget capture ALL clicks, so without this
// the list's own selection handling would never fire.
func (r *bufferRow) Tapped(_ *fyne.PointEvent) {
	for i, ref := range r.u.rows {
		// profile must match too: offline pinned roots all share
		// (nil, "server", true), and without it clicking the second
		// pinned network selected (and connected) the first
		if ref.sess == r.ref.sess && strings.EqualFold(ref.name, r.ref.name) &&
			ref.root == r.ref.root && ref.profile == r.ref.profile {
			r.u.bufferList.Select(i)
			return
		}
	}
}

func (r *bufferRow) TappedSecondary(e *fyne.PointEvent) {
	ref := r.ref
	if !ref.root && ref.sess == nil {
		return
	}
	items := r.u.rowMenuItems(ref)
	if !ref.root {
		items = append(items, fyne.NewMenuItem("Mark as read", func() {
			delete(r.u.unread, skey(ref.sess, ref.name))
			delete(r.u.hlBuffers, skey(ref.sess, ref.name))
			r.u.bufferList.Refresh()
		}))
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), r.u.win.Canvas(), e.AbsolutePosition)
}

// rowRef is one sidebar row: a network root (connected session, or a
// pinned profile waiting to connect) or a buffer beneath a session.
type rowRef struct {
	sess    *Session // nil for a pinned, disconnected profile
	name    string   // buffer name; serverBuffer for roots
	root    bool
	profile string // network profile name (for roots)
	key     string // precomputed skey for the unread/highlight maps
}

// skey scopes a buffer name to its session for the unread/highlight maps.
func skey(s *Session, name string) string {
	who := ""
	if s != nil {
		who = s.Name
	}
	return who + "\x00" + strings.ToLower(name)
}

func (u *UI) hasSession(s *Session) bool {
	for _, x := range u.sessions {
		if x == s {
			return true
		}
	}
	return false
}

// closeSession quits and removes one session; pinned profiles keep their
// sidebar row and can be reconnected with a click.
func (u *UI) closeSession(s *Session) {
	s.Quit()
	for i, x := range u.sessions {
		if x == s {
			u.sessions = append(u.sessions[:i], u.sessions[i+1:]...)
			break
		}
	}
	// purge this network's unread and mention state: keys are name-scoped,
	// so a later reconnect to the same network would inherit stale badges
	// for messages that are no longer in any buffer. Drafts survive on
	// purpose (typed text is worth keeping across a reconnect).
	prefix := s.Name + "\x00"
	for k := range u.unread {
		if strings.HasPrefix(k, prefix) {
			delete(u.unread, k)
		}
	}
	for k := range u.hlBuffers {
		if strings.HasPrefix(k, prefix) {
			delete(u.hlBuffers, k)
		}
	}
	if u.sess == s {
		u.sess = nil
		if len(u.sessions) > 0 {
			u.sess = u.sessions[len(u.sessions)-1]
			u.networkName = u.sess.Name
			u.showBuffer(serverBuffer)
		} else {
			u.status.set(dotRed, "Disconnected — use the gear menu to connect")
			u.bufferTitle.SetText("")
			u.topicLabel.SetText("")
			u.messages.Segments = nil
			u.messages.Refresh()
			u.welcome.Show()
			u.sidePane.Hide()
			u.memberPane.Hide()
			u.buffersBtn.Disable()
			u.peopleBtn.Disable()
			u.channelsBtn.Disable()
			u.centerBox.Refresh()
			u.root.Refresh()
		}
	}
	u.refreshBuffers()
}

type tipButton struct {
	btn *ttwidget.Button
	tip string
}

// statusDot is the little connection indicator: green connected, orange
// connecting, red disconnected. Hovering shows the detail as a tooltip.
type statusDot struct {
	ttwidget.ToolTipWidget
	circle *canvas.Circle
}

func newStatusDot() *statusDot {
	d := &statusDot{circle: canvas.NewCircle(dotRed)}
	d.ExtendBaseWidget(d)
	return d
}

func (d *statusDot) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewCenter(
		container.NewGridWrap(fyne.NewSize(12, 12), d.circle)))
}

func (d *statusDot) set(c color.Color, tip string) {
	d.circle.FillColor = c
	d.circle.Refresh()
	d.SetToolTip(tip)
}

var (
	dotGreen  = color.NRGBA{0x2e, 0xb8, 0x2e, 0xff}
	dotOrange = color.NRGBA{0xe8, 0x93, 0x1a, 0xff}
	dotRed    = color.NRGBA{0xd0, 0x33, 0x33, 0xff}
)

// applyTips enables or disables all toolbar tooltips per the preference.
func (u *UI) applyTips() {
	on := u.app.Preferences().BoolWithFallback("tooltips", true)
	for _, t := range u.tipButtons {
		if on {
			t.btn.SetToolTip(t.tip)
		} else {
			t.btn.SetToolTip("")
		}
	}
}

// trackDialog registers a dialog so the Escape key can close it.
func (u *UI) trackDialog(d interface {
	Hide()
	SetOnClosed(func())
}) {
	u.activeDialog = d
	d.SetOnClosed(func() { u.activeDialog = nil })
}

// Rendering caps: RichText refresh cost is O(shown segments), so bound it.
const (
	maxShownLines    = 250
	maxShownSegments = maxShownLines * 3
)

func NewUI(a fyne.App, w fyne.Window) *UI {
	u := &UI{app: a, win: w, current: serverBuffer, unread: map[string]int{},
		drafts: map[string]string{}, hlBuffers: map[string]bool{}}

	u.messages = newMsgView(u)
	u.messages.Wrapping = fyne.TextWrapWord
	u.msgScroll = container.NewScroll(u.messages)

	u.selectEntry = widget.NewMultiLineEntry()
	u.selectEntry.Wrapping = fyne.TextWrapWord
	u.selectEntry.TextStyle = fyne.TextStyle{Monospace: true}
	u.selectEntry.Hide()

	u.topicLabel = widget.NewLabel("")
	u.topicLabel.Truncation = fyne.TextTruncateEllipsis
	u.topicLabel.TextStyle = fyne.TextStyle{Italic: true}

	u.bufferTitle = widget.NewLabel("")
	u.bufferTitle.TextStyle = fyne.TextStyle{Bold: true}

	u.input = newChatEntry(u)
	u.input.SetPlaceHolder("Type a message… (Shift+Enter for a new line, /help for commands)")

	u.bufferList = widget.NewList(
		func() int { return len(u.rows) },
		func() fyne.CanvasObject { return newBufferRow(u) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(u.rows) {
				return
			}
			ref := u.rows[i]
			row := o.(*bufferRow)
			row.ref = ref
			text := ""
			if ref.root {
				text = ref.profile
				if ref.sess == nil {
					text += "  (offline)"
				}
			} else {
				text = "    " + ref.name
			}
			if n := u.unread[ref.key]; n > 0 && ref.sess != nil {
				text = fmt.Sprintf("%s  (%d)", text, n)
				row.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				row.TextStyle = fyne.TextStyle{Bold: ref.root}
			}
			if ref.sess != nil && u.hlBuffers[ref.key] {
				row.Importance = widget.WarningImportance
			} else {
				row.Importance = widget.MediumImportance
			}
			row.SetText(text)
		},
	)
	u.bufferList.HideSeparators = true
	u.bufferList.OnSelected = func(i widget.ListItemID) {
		if i >= len(u.rows) {
			return
		}
		ref := u.rows[i]
		if ref.sess == nil && ref.root {
			// pinned but offline: clicking connects (secrets resolved only
			// now, for the one chosen profile)
			u.bufferList.UnselectAll()
			for _, n := range config.Load(u.app.Preferences()) {
				if n.Name == ref.profile {
					u.connectTo(n)
					return
				}
			}
			return
		}
		u.sess = ref.sess
		u.networkName = ref.sess.Name
		u.showBuffer(ref.name)
	}

	u.memberList = widget.NewList(
		func() int { return len(u.members) },
		func() fyne.CanvasObject { return newMemberRow(u) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(u.members) {
				r := o.(*memberRow)
				r.SetText(u.members[i])
				// ignored users stay listed but grayed, so they can
				// still be right-clicked and unignored
				if isIgnored(r.nick()) {
					r.Importance = widget.LowImportance
				} else {
					r.Importance = widget.MediumImportance
				}
				r.Refresh()
			}
		},
	)

	var settingsBtn *ttwidget.Button
	settingsBtn = ttwidget.NewButtonWithIcon("", theme.SettingsIcon(), func() { u.showMainMenu(settingsBtn) })
	settingsBtn.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{settingsBtn, "Menu"})

	// fixed-width, collapsible side panes
	memberWidth := canvas.NewRectangle(color.Transparent)
	memberWidth.SetMinSize(fyne.NewSize(170, 0))
	u.memberPane = container.NewStack(memberWidth, u.memberList)

	sideWidth := canvas.NewRectangle(color.Transparent)
	sideWidth.SetMinSize(fyne.NewSize(170, 0))
	u.sidePane = container.NewStack(sideWidth, u.bufferList)

	toggle := func(pane *fyne.Container) func() {
		return func() {
			if pane.Visible() {
				pane.Hide()
			} else {
				pane.Show()
			}
			// re-run the surrounding layouts so the chat area reclaims the space
			u.centerBox.Refresh()
			u.root.Refresh()
		}
	}

	buffersBtn := ttwidget.NewButtonWithIcon("", theme.ListIcon(), toggle(u.sidePane))
	u.buffersBtn = buffersBtn
	buffersBtn.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{buffersBtn, "Show or hide the channel list"})

	people := ttwidget.NewButtonWithIcon("", theme.AccountIcon(), nil)
	u.peopleBtn = people
	people.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{people, "Show or hide the member list"})

	channelsBtn := ttwidget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		if u.sess == nil {
			return
		}
		if len(u.sess.ChannelList()) > 0 {
			u.showChannelBrowser()
		} else {
			u.handleInput("/list")
		}
	})
	u.channelsBtn = channelsBtn
	channelsBtn.Importance = widget.LowImportance
	channelsBtn.Disable()
	u.tipButtons = append(u.tipButtons, tipButton{channelsBtn, "Browse channels"})

	// plaintext mode: swaps the colored view for selectable plain text
	selectBtn := ttwidget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		u.selectOn = !u.selectOn
		if u.selectOn {
			u.updateSelectView()
			u.msgScroll.Hide()
			u.selectEntry.Show()
		} else {
			u.selectEntry.Hide()
			u.msgScroll.Show()
		}
		u.centerBox.Refresh()
	})
	selectBtn.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{selectBtn, "Plaintext mode"})

	u.status = newStatusDot()
	u.status.set(dotRed, "Disconnected — use the gear menu to connect")

	topBar := container.NewBorder(nil, nil,
		container.NewHBox(buffersBtn, channelsBtn, settingsBtn, u.bufferTitle),
		container.NewHBox(container.NewPadded(u.status), selectBtn, people), u.topicLabel)

	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(160, 160))
	welcomeText := widget.NewLabelWithStyle("Welcome to eco",
		fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	connectBtn := widget.NewButtonWithIcon("Connect to a server", theme.ComputerIcon(),
		func() { u.ShowNetworkManager() })
	connectBtn.Importance = widget.HighImportance
	u.welcome = container.NewCenter(container.NewVBox(
		container.NewCenter(logo), welcomeText, container.NewCenter(connectBtn)))
	u.welcome.Hide()

	// compose bar: padded input with a send button, anchored by a separator
	send := ttwidget.NewButtonWithIcon("", theme.MailSendIcon(), func() {
		u.input.submit()
		u.win.Canvas().Focus(u.input)
	})
	send.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{send, "Send"})

	u.composeHint = widget.NewLabel("")
	u.composeHint.TextStyle = fyne.TextStyle{Italic: true}
	u.composeHint.Hide()

	composer := container.NewBorder(nil, nil, nil,
		container.NewHBox(u.composeHint, send), u.input)
	bottom := container.NewVBox(widget.NewSeparator(), container.NewPadded(composer))

	u.centerBox = container.NewBorder(topBar, bottom, nil, u.memberPane,
		container.NewStack(u.msgScroll, u.selectEntry, u.welcome))
	people.OnTapped = toggle(u.memberPane)

	u.root = container.NewBorder(nil, nil, u.sidePane, nil, u.centerBox)

	// idle startup: both side panes stay closed and their toggles disabled
	// until a server is joined
	u.sidePane.Hide()
	u.memberPane.Hide()
	buffersBtn.Disable()
	people.Disable()

	u.bgImage = &canvas.Image{FillMode: canvas.ImageFillStretch}
	u.bgImage.Hide()
	u.applyBackground()

	w.SetContent(fynetooltip.AddWindowToolTipLayer(container.NewStack(u.bgImage, u.root), w.Canvas()))
	w.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape && u.activeDialog != nil {
			u.activeDialog.Hide()
		}
	})
	u.applyTips()
	u.registerShortcuts()
	w.Canvas().Focus(u.input)
	return u
}

// handleShortcut services the app-wide keyboard shortcuts; returns whether
// the combo was ours. Called from both the canvas and the focused entry.
func (u *UI) handleShortcut(cs *desktop.CustomShortcut) bool {
	switch {
	case u.matchesAction(cs, "close"):
		u.handleInput("/close")
	case u.matchesAction(cs, "prev"):
		u.stepBuffer(-1)
	case u.matchesAction(cs, "next"):
		u.stepBuffer(1)
	case u.matchesAction(cs, "unread"):
		u.jumpToUnread()
	case u.matchesAction(cs, "quit"):
		u.handleInput("/quit") // one quit path: /quit delays so QUIT flushes
	case cs.Modifier == fyne.KeyModifierAlt && len(cs.KeyName) == 1 &&
		cs.KeyName[0] >= '1' && cs.KeyName[0] <= '9':
		i := int(cs.KeyName[0] - '1')
		if i < len(u.rows) {
			u.bufferList.Select(i)
		}
	default:
		return false
	}
	return true
}

func (u *UI) stepBuffer(dir int) {
	if len(u.rows) == 0 {
		return
	}
	cur := 0
	for i, ref := range u.rows {
		if ref.sess == u.sess && strings.EqualFold(ref.name, u.current) {
			cur = i
		}
	}
	// step only across live rows: cycling into an offline pinned root
	// would trigger a connection, which keyboard browsing must never do
	next := cur
	for range u.rows {
		next = (next + dir + len(u.rows)) % len(u.rows)
		if u.rows[next].sess != nil {
			u.bufferList.Select(next)
			return
		}
	}
}

func (u *UI) jumpToUnread() {
	for i, ref := range u.rows {
		if ref.sess != nil && u.unread[ref.key] > 0 {
			u.bufferList.Select(i)
			return
		}
	}
}

func (u *UI) registerShortcuts() {
	cv := u.win.Canvas()
	// fixed row jumps; the rest are rebindable via Preferences > Shortcuts
	for _, c := range []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4,
		fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9} {
		cs := &desktop.CustomShortcut{KeyName: c, Modifier: fyne.KeyModifierAlt}
		cv.AddShortcut(cs, func(fyne.Shortcut) { u.handleShortcut(cs) })
	}
	u.applyKeybinds()
}

// callbacksFor wires the session-facing callbacks; all UI mutation goes
// through fyne.Do. Each callback checks that its session is still the
// active one, so a torn-down connection cannot flip UI state afterwards.
func (u *UI) callbacksFor(getSess func() *Session) UICallbacks {
	// stale means the session was closed; a live background session still
	// gets its data recorded, it just does not drive the visible pane
	stale := func() bool { return !u.hasSession(getSess()) }
	active := func() bool { s := getSess(); return s != nil && s == u.sess }
	return UICallbacks{
		JoinFailed: func(channel, reason string, needKey bool) {
			fyne.Do(func() {
				if stale() {
					return
				}
				if !needKey {
					dialog.ShowInformation("Cannot join "+channel, reason, u.win)
					return
				}
				key := widget.NewPasswordEntry()
				key.SetPlaceHolder("channel key")
				items := []*widget.FormItem{widget.NewFormItem("Key", key)}
				dialog.ShowForm("Join "+channel, "Join", "Cancel", items, func(ok bool) {
					if ok && key.Text != "" {
						getSess().JoinWithKey(channel, key.Text)
					}
				}, u.win)
			})
		},
		StatusChange: func(connected bool, status string) {
			fyne.Do(func() {
				if stale() {
					return
				}
				u.refreshBuffers() // root row labels track connection state
				if !active() {
					return
				}
				switch {
				case connected:
					u.status.set(dotGreen, "Connected to "+u.networkName+" as "+u.sess.CurrentNick())
				case strings.Contains(status, "Reconnecting"):
					u.status.set(dotOrange, "Connection lost, reconnecting")
				default:
					u.status.set(dotRed, status+", right-click the network to reconnect")
				}
			})
		},
		ChannelListDone: func() {
			fyne.Do(func() {
				if stale() || !active() {
					return
				}
				u.showChannelBrowser()
			})
		},
		AddLine: func(buffer string, l Line) {
			if l.Highlight || (config.Current().NotifyAll && !l.Self && !l.Status && !l.Playback) {
				u.notify(l.Nick+" ("+buffer+")", l.Text)
			}
			fyne.Do(func() {
				if stale() {
					return
				}
				if active() && strings.EqualFold(buffer, u.current) {
					u.appendLine(l)
				} else {
					// only live chat messages count as unread; history
					// playback, joins, parts, and status noise do not
					if !l.Status && !l.Playback && !l.Self {
						u.unread[skey(getSess(), buffer)]++
						u.unreadDirty = true
						if l.Highlight {
							u.hlBuffers[skey(getSess(), buffer)] = true
						}
					}
					u.scheduleFlush()
				}
			})
		},
		RenameBuffer: func(oldName, newName string) {
			fyne.Do(func() {
				if stale() {
					return
				}
				s := getSess()
				oldKey, newKey := skey(s, oldName), skey(s, newName)
				// migrate per-buffer UI state so a rename keeps drafts,
				// unread counts, and mention flags
				if v, ok := u.drafts[oldKey]; ok {
					u.drafts[newKey] = v
					delete(u.drafts, oldKey)
				}
				if v, ok := u.unread[oldKey]; ok {
					u.unread[newKey] = v
					delete(u.unread, oldKey)
				}
				if u.hlBuffers[oldKey] {
					u.hlBuffers[newKey] = true
					delete(u.hlBuffers, oldKey)
				}
				// the viewer follows the rename instead of bouncing to the
				// server log
				if s == u.sess && strings.EqualFold(u.current, oldName) {
					u.current = newName
					u.currentKey = newKey
					u.bufferTitle.SetText(newName)
				}
			})
		},
		HighlightNote: func(buffer, nick, text string) {
			u.notify(nick+" ("+buffer+")", text)
			fyne.Do(func() {
				if stale() {
					return
				}
				s := getSess()
				if s == u.sess && strings.EqualFold(buffer, u.current) {
					return // visible; no badge needed
				}
				u.hlBuffers[skey(s, buffer)] = true
				u.bufferList.Refresh()
			})
		},
		AmendBuffer: func(buffer string) {
			fyne.Do(func() {
				if stale() || !active() {
					return
				}
				if !strings.EqualFold(buffer, u.current) {
					return
				}
				gap := (u.messages.MinSize().Height - u.msgScroll.Size().Height) - u.msgScroll.Offset.Y
				if gap > 80 {
					return // reading history; the redraw would yank the view
				}
				u.showBuffer(u.current)
			})
		},
		BufferList: func() {
			fyne.Do(func() {
				if stale() {
					return
				}
				u.refreshBuffers()
			})
		},
		STS: func(port string, durationSecs int64) {
			fyne.Do(func() {
				if stale() {
					return
				}
				s := getSess()
				if transport.IsWebSocketURL(s.Config.Server) {
					return
				}
				host := s.Config.Server
				// per the STS spec, persist policies ONLY from a secure
				// connection: on plaintext the advertisement is attacker
				// controlled and may only trigger the upgrade attempt
				if s.Config.TLS {
					config.STSRecord(u.app.Preferences(), host, s.Config.Port, durationSecs)
					return
				}

				// plaintext connection to an STS host: upgrade immediately
				if port != "" && durationSecs > 0 {
					s.status(serverBuffer, "This server enforces TLS (STS policy). Reconnecting securely on port "+port+".")
					cfg := s.Config
					cfg.TLS = true
					cfg.Port = port
					u.startSession(cfg, s.Name)
				}
			})
		},
		MembersChange: func(buffer string) {
			fyne.Do(func() {
				if stale() || !active() {
					return
				}
				if strings.EqualFold(buffer, u.current) {
					u.members = u.sess.SortedMembers(u.current)
					u.memberList.Refresh()
				}
			})
		},
		TopicChange: func(buffer, topic string) {
			fyne.Do(func() {
				if stale() || !active() {
					return
				}
				if strings.EqualFold(buffer, u.current) {
					u.topicLabel.SetText(topic)
				}
			})
		},
		// The core owns redaction since the v2 cutover: the store replaces the
		// withdrawn message with a tombstone and the session's projection
		// already carries it, so all this has to do is repaint a buffer the user
		// is looking at. v1 kept the text in the buffer and asked this callback
		// to overwrite it, which left the original in memory — and in the
		// plaintext view — for as long as the buffer lived.
		RedactMsg: func(buffer, msgid string) {
			fyne.Do(func() {
				if stale() || !active() {
					return
				}
				if strings.EqualFold(buffer, u.current) {
					u.showBuffer(u.current)
				}
			})
		},
	}
}

func (u *UI) refreshBuffers() {
	u.rows = u.rows[:0]
	connected := map[string]bool{}
	for _, s := range u.sessions {
		connected[s.Name] = true
		u.rows = append(u.rows, rowRef{sess: s, name: serverBuffer, root: true, profile: s.Name, key: skey(s, serverBuffer)})
		for _, b := range s.Buffers() {
			if b.Name == serverBuffer {
				continue
			}
			u.rows = append(u.rows, rowRef{sess: s, name: b.Name, key: skey(s, b.Name)})
		}
	}
	// pinned but offline profiles keep a clickable root row
	for _, n := range config.LoadMeta(u.app.Preferences()) {
		if n.Pinned && !connected[n.Name] {
			u.rows = append(u.rows, rowRef{sess: nil, name: serverBuffer, root: true, profile: n.Name})
		}
	}
	u.bufferList.Refresh()

	if u.sess == nil {
		return
	}
	// if the current buffer vanished, fall back to its session's server log
	found := false
	for _, ref := range u.rows {
		if ref.sess == u.sess && strings.EqualFold(ref.name, u.current) {
			found = true
		}
	}
	if !found {
		u.showBuffer(serverBuffer)
	}
}

// selectRow highlights and shows a specific session buffer.
func (u *UI) selectRow(s *Session, name string) {
	u.refreshBuffers()
	for i, ref := range u.rows {
		if ref.sess == s && strings.EqualFold(ref.name, name) {
			u.bufferList.Select(i)
			return
		}
	}
}

func (u *UI) selectBuffer(name string) {
	u.selectRow(u.sess, name)
}

func (u *UI) showBuffer(name string) {
	if u.scrollAnim != nil {
		u.scrollAnim.Stop()
		u.scrollAnim = nil
	}
	u.stickBottom = true
	// drafts are keyed by the buffer we were ACTUALLY showing (currentKey):
	// on cross-session switches u.sess has already changed, so recomputing
	// the outgoing key here would file the draft under the wrong network
	newKey := skey(u.sess, name)
	if newKey != u.currentKey && u.currentKey != "" {
		// first show (empty currentKey) adopts the key without touching
		// the input, preserving text typed before connecting
		u.drafts[u.currentKey] = u.input.Text
		u.input.SetText(u.drafts[newKey])
		u.input.cursorToEnd()
	}
	u.current = name
	u.currentKey = newKey
	delete(u.hlBuffers, newKey)
	delete(u.unread, newKey)
	u.bufferTitle.SetText(name)
	if u.sess == nil {
		return // idle startup: nothing to render yet
	}
	u.bufferList.Refresh()

	b := u.sess.Buffer(name)
	u.messages.Segments = u.messages.Segments[:0]
	if b != nil {
		u.sess.mu.Lock()
		src := b.Lines
		if len(src) > maxShownLines {
			src = src[len(src)-maxShownLines:]
		}
		lines := append([]Line(nil), src...)
		topic := b.Topic
		u.sess.mu.Unlock()
		mono := strings.EqualFold(name, serverBuffer)
		mention := u.mentionResolver()
		for _, l := range lines {
			u.messages.Segments = append(u.messages.Segments, render.Segments(l, renderOptions(mono, mention))...)
		}
		u.topicLabel.SetText(topic)
	} else {
		u.topicLabel.SetText("")
	}
	u.messages.Refresh()
	u.segsDirty = false // this refresh covered any pending flush's work
	u.msgScroll.ScrollToBottom()

	u.members = u.sess.SortedMembers(name)
	u.memberList.Refresh()
	if u.selectOn {
		u.selectKey = "" // content may have changed shape; full rebuild
		u.updateSelectView()
	}
	u.win.Canvas().Focus(u.input)
}

func (u *UI) appendLine(l Line) {
	// remember whether the reader was near the bottom BEFORE this batch:
	// only then does the view follow new messages, so scrolling up to
	// read history is never yanked away
	if !u.flushPending {
		gap := (u.messages.MinSize().Height - u.msgScroll.Size().Height) - u.msgScroll.Offset.Y
		u.stickBottom = gap < 80
	}
	mono := strings.EqualFold(u.current, serverBuffer)
	u.messages.Segments = append(u.messages.Segments, render.Segments(l, renderOptions(mono, u.mentionResolver()))...)
	u.segsDirty = true
	u.scheduleFlush()
}

// scheduleFlush coalesces refreshes: bursts of incoming lines (e.g. a
// chathistory playback of 100 messages) cause one re-layout, not one each.
func (u *UI) scheduleFlush() {
	if u.flushPending {
		return
	}
	u.flushPending = true
	time.AfterFunc(80*time.Millisecond, func() {
		fyne.Do(func() {
			u.flushPending = false
			// a background-buffer burst only touches counters; skip the
			// expensive message-pane relayout unless segments changed
			if u.segsDirty {
				u.segsDirty = false
				if n := len(u.messages.Segments); n > maxShownSegments && u.stickBottom {
					u.messages.Segments = append([]widget.RichTextSegment(nil), u.messages.Segments[n-maxShownSegments:]...)
				}
				u.messages.Refresh()
				if u.stickBottom {
					u.smoothScrollToBottom()
				}
				if u.selectOn {
					u.updateSelectView()
				}
			}
			if u.unreadDirty {
				u.unreadDirty = false
				u.bufferList.Refresh()
			}
		})
	})
}

// showWelcome fills the empty startup window with the app icon and a pointer.
func (u *UI) showWelcome() {
	u.bufferTitle.SetText("welcome")
	u.welcome.Show()
	u.centerBox.Refresh()
}

// dialogSize scales a baseline dialog size (designed at 16pt) with the
// user's font size so larger text never cuts off dialog content.
func (u *UI) dialogSize(w, h float32) fyne.Size {
	scale := float32(u.app.Preferences().FloatWithFallback("fontpts", 16)) / 16
	if scale < 1 {
		scale = 1
	}
	return fyne.NewSize(w*scale, h*scale)
}

// newRect is a filled rectangle used by the preference swatches.
func newRect(c color.Color) *canvas.Rectangle {
	return canvas.NewRectangle(c)
}

// applyBackground picks up the "bgimage" preference (a path, or empty).
func (u *UI) applyBackground() {
	path := u.app.Preferences().String("bgimage")
	op := u.app.Preferences().FloatWithFallback("bgopacity", 18)
	u.bgImage.Translucency = 1 - op/100
	if path == "" {
		u.bgImage.Hide()
	} else {
		u.bgImage.File = path
		u.bgImage.Show()
	}
	u.bgImage.Refresh()
}

// updateSelectView rebuilds the selectable plain text from the current buffer.
func (u *UI) updateSelectView() {
	if u.sess == nil {
		u.selectEntry.SetText("")
		u.selectKey, u.selectCount = "", 0
		return
	}
	b := u.sess.Buffer(u.current)
	if b == nil {
		u.selectEntry.SetText("")
		u.selectKey, u.selectCount = "", 0
		return
	}
	u.sess.mu.Lock()
	lines := append([]Line(nil), b.Lines...)
	u.sess.mu.Unlock()

	render := func(ls []Line) string {
		var sb strings.Builder
		for _, l := range ls {
			sb.WriteString(l.Time.Format(config.Current().TimeLayout))
			sb.WriteString("  ")
			if l.Nick != "" {
				sb.WriteString("<" + l.Nick + "> ")
			}
			sb.WriteString(ircfmt.Strip(l.Text))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	key := skey(u.sess, u.current)
	// append-only path: rebuilding the whole log per flush was O(history)
	// every tick with plaintext mode open. It is only valid while the
	// buffer has never been trimmed: once len reaches the scrollback cap,
	// old lines fall off the front while len stays constant, so a pure
	// length comparison would render nothing and freeze the view. At the
	// cap we fall back to full rebuilds, which that regime bounds anyway.
	sbCap := int(config.Current().ScrollbackCap)
	untrimmed := sbCap == 0 || len(lines) < sbCap
	if key == u.selectKey && u.selectCount > 0 && u.selectCount <= len(lines) && untrimmed {
		if u.selectCount < len(lines) {
			u.selectEntry.Append(render(lines[u.selectCount:]))
			u.selectCount = len(lines)
		}
		return
	}
	u.selectEntry.SetText(render(lines))
	u.selectKey = key
	u.selectCount = len(lines)
	if len(lines) > 0 {
		u.selectEntry.CursorRow = len(lines) - 1
	}
}

// mentionResolver recognizes "@nick" for members of the current buffer
// (or an open PM); the returned action opens the DM, hyperlink-style.
// The member list is snapshotted once so per-token lookups do not take
// the session lock on the hot render path.
func (u *UI) mentionResolver() func(string) func() {
	sess := u.sess
	if sess == nil {
		return nil
	}
	members := map[string]string{}
	if b := sess.Buffer(u.current); b != nil {
		sess.mu.Lock()
		for m := range b.Members {
			members[strings.ToLower(m)] = m
		}
		sess.mu.Unlock()
	}
	return func(nick string) func() {
		target := members[strings.ToLower(nick)]
		if target == "" && !strings.HasPrefix(nick, "#") && sess.Buffer(nick) != nil {
			target = nick
		}
		if target == "" {
			return nil
		}
		return func() {
			sess.getBuffer(target)
			u.selectBuffer(target)
		}
	}
}
