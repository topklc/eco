package app

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/render"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

type UI struct {
	app fyne.App
	win fyne.Window

	// vm is the application state this window renders: the sidebar's rows, the
	// buffer on screen, the drafts, the attention counts, the roster. Since M8
	// it is not in this struct — the widgets below read it and fire intents at
	// it, and none of them owns a fact (blueprint M8, internal/ui/viewmodel).
	vm *viewmodel.Model

	// dispatch is the one funnel every session's Changes arrive through, and
	// this UI is its Sink (see sink.go). It is created once and lives as long
	// as the process: sessions come and go through Open/Close, not through it.
	dispatch *dispatch.Dispatcher

	bufferList *widget.List
	sessions   []*Session // live adapters; the viewmodel holds their order

	messages    *msgView
	msgScroll   *container.Scroll
	memberList  *widget.List
	memberPane  *fyne.Container
	bgImage     *canvas.Image
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

	flushPending bool // a repaint is owed at the end of the current batch
	unreadDirty  bool // buffer list needs a repaint on next flush
	segsDirty    bool // message pane content changed since the last flush

	activeDialog interface{ Hide() } // open dialog, closable with Escape

	selectEntry *widget.Entry // selectable plain text view of the buffer
	selectOn    bool
	selectKey   string // buffer identity of the rendered plaintext
	selectCount int    // lines already rendered into selectEntry

	tipButtons []tipButton // toolbar buttons with toggleable tooltips

	welcome *fyne.Container // idle screen: centered icon, shown when no server

	composeHint *widget.Label
	scrollAnim  *fyne.Animation // in-flight smooth scroll, if any
}

// session returns the Session whose buffer is on screen, or nil. It is resolved
// from the viewmodel every time rather than cached beside it: one of the two
// would otherwise be stale, and the stale one is always the pointer.
func (u *UI) session() *Session { return u.sessionFor(u.vm.Current().Session) }

// current returns the display name of the buffer on screen.
func (u *UI) current() string { return u.vm.Current().Name }

// ref names a buffer of the session on screen.
func (u *UI) ref(name string) viewmodel.Ref {
	return viewmodel.Ref{Session: u.vm.Current().Session, Name: name}
}

// nearBottom reports whether the reader is close enough to the newest message
// for the view to follow new ones.
func (u *UI) nearBottom() bool {
	gap := (u.messages.MinSize().Height - u.msgScroll.Size().Height) - u.msgScroll.Offset.Y
	return gap < 80
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
	if u.session() != nil {
		max = u.session().multilineMaxLines()
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
	row viewmodel.Row
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
	if i := r.u.vm.Index(r.row); i >= 0 {
		r.u.bufferList.Select(i)
	}
}

func (r *bufferRow) TappedSecondary(e *fyne.PointEvent) {
	row := r.row
	if !row.Root && !row.Live {
		return
	}
	items := r.u.rowMenuItems(row)
	if !row.Root {
		items = append(items, fyne.NewMenuItem("Mark as read", func() {
			r.u.vm.MarkRead(row.Ref)
			r.u.bufferList.Refresh()
		}))
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), r.u.win.Canvas(), e.AbsolutePosition)
}

// sessionFor resolves the session a delivered Change came from. The funnel has
// already dropped anything from a session the app tore down, so a miss here is
// belt and braces rather than the liveness rule.
func (u *UI) sessionFor(id dispatch.SessionID) *Session {
	if id == "" {
		return nil
	}
	for _, s := range u.sessions {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

// showing reports whether a delivered buffer is the one on screen right now.
// This is a rendering question, not a liveness one: a live background session
// still records everything, it just does not drive the visible pane.
func (u *UI) showing(id dispatch.SessionID, b dispatch.Buffer) bool {
	return u.vm.Showing(viewmodel.Ref{Session: id, Name: b.Name})
}

// retireSession is closeSession's model half: it takes one session out of the
// funnel, the session list and the viewmodel, and reports where the viewer must
// go — the newest remaining network's log, or nowhere, which is the idle
// window. The bool is false when the viewer was not on this session.
//
// It is split out because it is the half that decides anything, and the half a
// test can hold: closeSession's other half is widgets and needs a canvas.
// "The teardown retires the session from the funnel" is exactly the line a
// later refactor drops, and nothing else in the program would notice — the
// funnel would go on delivering a dead session's traffic to a window that no
// longer lists it (TestFunnel_TearingDownASessionRetiresIt).
func (u *UI) retireSession(s *Session) (viewmodel.Ref, bool) {
	// Retire it from the funnel FIRST: everything it has emitted and everything
	// a goroutine that has not noticed yet still emits is dropped there, in one
	// place. This is what the twelve hand-rolled stale() guards used to do.
	u.dispatch.Close(s.ID())
	s.Quit()
	for i, x := range u.sessions {
		if x == s {
			u.sessions = append(u.sessions[:i], u.sessions[i+1:]...)
			break
		}
	}
	// The viewmodel drops the rows and the counts; drafts survive on purpose.
	return u.vm.RemoveSession(s.ID())
}

// closeSession quits and removes one session; pinned profiles keep their
// sidebar row and can be reconnected with a click.
func (u *UI) closeSession(s *Session) {
	// The viewmodel says where the viewer goes: the newest remaining network's
	// log, or nowhere, which is the idle window.
	next, move := u.retireSession(s)
	switch {
	case move && !next.IsZero():
		u.showRef(next)
	case move:
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
	u := &UI{app: a, win: w}
	// The one effect the viewmodel asks for: the read marker moves in the
	// store, which is what makes an unread count zero for real.
	u.vm = viewmodel.New(viewmodel.Hooks{
		MarkRead: func(id dispatch.SessionID, buffer string) {
			if s := u.sessionFor(id); s != nil {
				s.MarkRead(buffer)
			}
		},
	})
	u.dispatch = dispatch.New(dispatch.Options{Sink: u})

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
		func() int { return u.vm.RowCount() },
		func() fyne.CanvasObject { return newBufferRow(u) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row, ok := u.vm.Row(i)
			if !ok {
				return
			}
			w := o.(*bufferRow)
			w.row = row
			text := ""
			if row.Root {
				text = row.Profile
				if !row.Live {
					text += "  (offline)"
				}
			} else {
				text = "    " + row.Ref.Name
			}
			// the counts are the viewmodel's, badge rules already applied —
			// including "the buffer on screen is read"
			if n := row.Counts.Unread; n > 0 && row.Live {
				text = fmt.Sprintf("%s  (%d)", text, n)
				w.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				w.TextStyle = fyne.TextStyle{Bold: row.Root}
			}
			if row.Live && row.Counts.Highlights > 0 {
				w.Importance = widget.WarningImportance
			} else {
				w.Importance = widget.MediumImportance
			}
			w.SetText(text)
		},
	)
	u.bufferList.HideSeparators = true
	u.bufferList.OnSelected = func(i widget.ListItemID) {
		row, ok := u.vm.Row(i)
		if !ok {
			return
		}
		if row.Root && !row.Live {
			// pinned but offline: clicking connects (secrets resolved only
			// now, for the one chosen profile)
			u.bufferList.UnselectAll()
			for _, n := range config.Load(u.app.Preferences()) {
				if n.Name == row.Profile {
					u.connectTo(n)
					return
				}
			}
			return
		}
		if u.vm.Showing(row.Ref) {
			return // already there; the selection was only catching up
		}
		u.showRef(row.Ref)
	}

	u.memberList = widget.NewList(
		func() int { return len(u.vm.Members()) },
		func() fyne.CanvasObject { return newMemberRow(u) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if members := u.vm.Members(); i < len(members) {
				r := o.(*memberRow)
				r.SetText(members[i])
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
		if u.session() == nil {
			return
		}
		if len(u.session().ChannelList()) > 0 {
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
		if i < u.vm.RowCount() {
			u.bufferList.Select(i)
		}
	default:
		return false
	}
	return true
}

// stepBuffer moves to the next or previous buffer. The rule that it steps only
// across LIVE rows — selecting an offline pinned root dials a server, and
// browsing with the keyboard must never do that — is the viewmodel's, and is
// tested there.
func (u *UI) stepBuffer(dir int) {
	if i, ok := u.vm.Step(dir); ok {
		u.bufferList.Select(i)
	}
}

func (u *UI) jumpToUnread() {
	if i, ok := u.vm.NextUnread(); ok {
		u.bufferList.Select(i)
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

// refreshBuffers hands the sidebar's inputs to the viewmodel — each session's
// buffers, and the pinned profiles — and repaints it. The rows themselves, and
// the fallback when the buffer on screen has just closed, are the viewmodel's.
func (u *UI) refreshBuffers() {
	var fallback viewmodel.Ref
	vanished := false
	for _, s := range u.sessions {
		bufs := s.Buffers()
		names := make([]string, 0, len(bufs))
		for _, b := range bufs {
			names = append(names, b.Name)
		}
		if r, gone := u.vm.SetBuffers(s.ID(), names); gone {
			fallback, vanished = r, true
		}
	}
	var pinned []string
	for _, n := range config.LoadMeta(u.app.Preferences()) {
		if n.Pinned {
			pinned = append(pinned, n.Name)
		}
	}
	u.vm.SetPinned(pinned)
	u.bufferList.Refresh()

	if vanished {
		u.showRef(fallback)
	}
}

// selectRow highlights and shows a specific session buffer.
func (u *UI) selectRow(s *Session, name string) {
	if s == nil {
		return
	}
	u.selectRef(viewmodel.Ref{Session: s.ID(), Name: name})
}

func (u *UI) selectBuffer(name string) {
	u.selectRef(u.ref(name))
}

// selectRef shows a buffer and moves the sidebar's selection onto it. Both
// halves are needed: the list's own OnSelected refuses to fire for a row that
// is already selected, so a fresh session landing on the index the old one
// occupied would leave the viewer where it was.
func (u *UI) selectRef(r viewmodel.Ref) {
	u.refreshBuffers()
	u.showRef(r)
	if i := u.vm.IndexOf(r); i >= 0 {
		u.bufferList.Select(i)
	}
}

// showRef is the one path a buffer reaches the screen through: the intent moves
// the viewmodel, and everything after it is rendering.
func (u *UI) showRef(r viewmodel.Ref) {
	if u.vm.Select(r) {
		// The outgoing buffer's text has been parked and this one's is back;
		// a first show says no, so text typed before connecting survives.
		u.input.SetText(u.vm.Draft())
		u.input.cursorToEnd()
	}
	u.renderBuffer()
}

// renderBuffer draws the buffer the viewmodel is showing. It holds no state of
// its own: call it after an intent moved something, or on its own to repaint
// (an amended row, a cleared buffer, a settings change).
func (u *UI) renderBuffer() {
	if u.scrollAnim != nil {
		u.scrollAnim.Stop()
		u.scrollAnim = nil
	}
	u.vm.SetFollow(true)
	name := u.current()
	u.bufferTitle.SetText(name)
	sess := u.session()
	if sess == nil {
		return // idle startup: nothing to render yet
	}
	u.bufferList.Refresh()

	b := sess.Buffer(name)
	u.messages.Segments = u.messages.Segments[:0]
	if b != nil {
		sess.mu.Lock()
		src := b.Lines
		if len(src) > maxShownLines {
			src = src[len(src)-maxShownLines:]
		}
		lines := append([]Line(nil), src...)
		topic := b.Topic
		sess.mu.Unlock()
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

	u.vm.SetMembers(sess.SortedMembers(name))
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
	// read history is never yanked away. The measurement is this layer's (it
	// owns the pixels); the answer is the viewmodel's.
	if !u.flushPending {
		u.vm.SetFollow(u.nearBottom())
	}
	mono := strings.EqualFold(u.current(), serverBuffer)
	u.messages.Segments = append(u.messages.Segments, render.Segments(l, renderOptions(mono, u.mentionResolver()))...)
	u.segsDirty = true
	u.scheduleFlush()
}

// scheduleFlush marks a repaint as owed. Since M8 the timing is not this
// layer's business: the funnel coalesces on its 80ms tick and closes each
// batch with BatchDone, which is where the repaint is actually paid for. All
// that is left here is the flag, and the flag is also what tells appendLine
// whether it is the first line of a batch.
func (u *UI) scheduleFlush() { u.flushPending = true }

// BatchDone implements dispatch.Sink: one coalesced repaint per batch, which is
// the whole reason the funnel exists. Bursts of incoming lines (a chathistory
// playback of 100 messages) cost one re-layout, not one each.
func (u *UI) BatchDone() {
	if !u.flushPending {
		return
	}
	u.flushPending = false
	// a background-buffer burst only touches counters; skip the expensive
	// message-pane relayout unless segments changed
	if u.segsDirty {
		u.segsDirty = false
		follow := u.vm.Follow()
		if n := len(u.messages.Segments); n > maxShownSegments && follow {
			u.messages.Segments = append([]widget.RichTextSegment(nil), u.messages.Segments[n-maxShownSegments:]...)
		}
		u.messages.Refresh()
		if follow {
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
	sess := u.session()
	if sess == nil {
		u.selectEntry.SetText("")
		u.selectKey, u.selectCount = "", 0
		return
	}
	b := sess.Buffer(u.current())
	if b == nil {
		u.selectEntry.SetText("")
		u.selectKey, u.selectCount = "", 0
		return
	}
	sess.mu.Lock()
	lines := append([]Line(nil), b.Lines...)
	sess.mu.Unlock()

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

	key := string(sess.ID()) + "\x00" + strings.ToLower(u.current())
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
	sess := u.session()
	if sess == nil {
		return nil
	}
	members := map[string]string{}
	if b := sess.Buffer(u.current()); b != nil {
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
