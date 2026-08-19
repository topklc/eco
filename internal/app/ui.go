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
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

type UI struct {
	app fyne.App
	win fyne.Window

	// vm is the application state this window renders: the sidebar's rows, the
	// buffer on screen, the drafts, the attention counts, the roster. Since M8
	// it is not in this struct - the widgets below read it and fire intents at
	// it, and none of them owns a fact (blueprint M8, internal/ui/viewmodel).
	vm *viewmodel.Model

	// dispatch is the one funnel every session's Changes arrive through, and
	// this UI is its Sink (see sink.go). It is created once and lives as long
	// as the process: sessions come and go through Open/Close, not through it.
	dispatch *dispatch.Dispatcher

	rail     *widgets.Rail // the buffer sidebar (blueprint §4.1)
	sessions []*Session    // live adapters; the viewmodel holds their order

	timeline    *widgets.MessageList // the chat pane (blueprint §4.2)
	people      *widgets.People      // the member pane (blueprint §4.1)
	bgImage     *canvas.Image
	topicLabel  *widget.Label
	bufferTitle *widgets.Title
	input       *chatEntry
	status      *statusDot
	buffersBtn  *ttwidget.Button
	peopleBtn   *ttwidget.Button
	channelsBtn *ttwidget.Button
	settingsBtn *ttwidget.Button          // the gear: the app menu, always on the toolbar
	boundKeys   []*desktop.CustomShortcut // rebindable shortcuts currently registered
	sidePane    *container.ThemeOverride  // the rail on its own quieter surface
	centerBox   *fyne.Container
	root        *fyne.Container

	// The two draggable splits §4.1 asks for, in place of v1's fixed 170px
	// rectangles: the rail against everything else, and the chat against the
	// People pane. Their offsets are the user's and are remembered.
	railSplit   *container.Split
	peopleSplit *container.Split

	// The three finding surfaces (blueprint §4.1, §4.3): Ctrl+K, Ctrl+F and
	// Ctrl+Shift+F. The two palettes live in the overlay layer above the
	// splits; the search bar is a strip inside the chat pane, above the
	// Timeline. See overlays.go, switcher.go, finder.go and find.go.
	switcher    *widgets.Palette
	finder      *widgets.Palette
	searchBar   *widgets.SearchBar
	overlay     *fyne.Container
	switcherBox *fyne.Container
	finderBox   *fyne.Container

	switchRows []viewmodel.Candidate // what the Switcher is offering right now
	finderRows []finderHit           // ...and the Finder
	lastFind   string                // the last in-buffer query, re-offered by Ctrl+F

	pending      []Line // lines this batch owes the Timeline
	flushPending bool   // a repaint is owed at the end of the current batch
	unreadDirty  bool   // buffer list needs a repaint on next flush
	segsDirty    bool   // message pane content changed since the last flush

	// dialogs is the dialog STACK §4.3 asks for, in place of v1's single slot.
	// Escape closes the topmost; see escape.go.
	dialogs []dialogHandle

	reader     *readerView     // F6: the buffer as selectable plain text
	readerNote *widget.Label   // how much of the buffer that view is showing
	readerPane *fyne.Container // the two of them
	readerOn   bool

	tipButtons []tipButton // toolbar buttons with toggleable tooltips

	welcome *fyne.Container // idle screen: centered icon, shown when no server

	composeHint *widget.Label

	// The window title, and the badge on it. baseTitle is the title without a
	// count; shownTitle is what was last handed to the toolkit, so a repaint
	// that changes nothing costs nothing (see attention.go).
	baseTitle  string
	shownTitle string

	// dividerSeed is the read marker as it stood when the buffer on screen was
	// OPENED - sampled before the switch, because arriving is what moves the
	// marker (see showRef).
	dividerSeed time.Time

	// candidates is the completion strip above the composer: the visible half
	// of the Tab cycle (§4.3). The composer owns the cycle; this only draws it.
	candidates *widgets.Candidates

	// notifier, when set, replaces the desktop notification with a call. It is
	// the seam a headless test watches attention through (see notify.go).
	notifier func(title, body string)

	// modifiers, when set, answers "which modifier keys are held" instead of
	// the driver. It is a seam for exactly one reason: no test driver can hold
	// Shift down, and Shift is the difference between Tab and Shift+Tab and
	// between sending a message and adding a line to it.
	modifiers func() fyne.KeyModifier
}

// heldModifiers returns the modifier keys held right now - QUERIED from the
// driver, never remembered. See keycapture.go for the bug that rule exists to
// kill.
func (u *UI) heldModifiers() fyne.KeyModifier {
	if u.modifiers != nil {
		return u.modifiers()
	}
	return currentModifiers(u.app)
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
// for the view to follow new ones. The measurement belongs to the Timeline,
// which owns the pixels; nothing here keeps a second answer.
func (u *UI) nearBottom() bool { return u.timeline.NearBottom() }

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
// go - the newest remaining network's log, or nowhere, which is the idle
// window. The bool is false when the viewer was not on this session.
//
// It is split out because it is the half that decides anything, and the half a
// test can hold: closeSession's other half is widgets and needs a canvas.
// "The teardown retires the session from the funnel" is exactly the line a
// later refactor drops, and nothing else in the program would notice - the
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
		u.status.set(dotOffline, "Disconnected - use the gear menu to connect")
		u.bufferTitle.SetText("")
		u.topicLabel.SetText("")
		u.timeline.SetBuffer(nil, time.Time{})
		u.closeReader()
		u.welcome.Show()
		u.setRailVisible(false)
		u.people.Hide()
		u.buffersBtn.Disable()
		u.peopleBtn.Disable()
		u.channelsBtn.Disable()
		u.bufferTitle.SetEnabled(false)
		u.centerBox.Refresh()
		u.root.Refresh()
	}
	u.refreshBuffers()
}

type tipButton struct {
	btn *ttwidget.Button
	tip string
}

// statusDot is the little connection indicator: the connection state named
// as a semantic token - success connected, warning connecting, danger
// disconnected - and resolved through this widget's own theme, so it
// follows a theme switch and any surrounding ThemeOverride. Hovering shows
// the detail as a tooltip.
type statusDot struct {
	ttwidget.ToolTipWidget
	circle *canvas.Circle
	state  fyne.ThemeColorName
}

func newStatusDot() *statusDot {
	d := &statusDot{circle: canvas.NewCircle(color.Transparent), state: dotOffline}
	d.ExtendBaseWidget(d)
	d.circle.FillColor = d.fill()
	return d
}

func (d *statusDot) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewCenter(
		container.NewGridWrap(fyne.NewSize(12, 12), d.circle)))
}

func (d *statusDot) fill() color.Color {
	th := d.Theme()
	return th.Color(d.state, ecotheme.Variant(th))
}

func (d *statusDot) set(state fyne.ThemeColorName, tip string) {
	d.state = state
	d.circle.FillColor = d.fill()
	d.circle.Refresh()
	d.SetToolTip(tip)
}

// Refresh re-resolves the token: a theme change repaints the dot without
// waiting for the next connection event.
func (d *statusDot) Refresh() {
	d.circle.FillColor = d.fill()
	d.circle.Refresh()
	d.BaseWidget.Refresh()
}

// the three connection states, as tokens
const (
	dotOnline     = ecotheme.Success
	dotConnecting = ecotheme.Warning
	dotOffline    = ecotheme.Danger
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

// setLoadingOlder shows or hides the backfill state: a row at the top of the
// Timeline, where the page being waited for will appear.
//
// There used to be caps here - 250 lines drawn, three segments a line - and a
// label in the title bar to go with them. Both existed because one RichText
// held the whole buffer and its refresh cost was O(everything shown). The
// Timeline draws the dozen rows on screen, so the cap has nothing left to
// protect and the scrollback keeps whatever the archive kept (M11(c)).
func (u *UI) setLoadingOlder(on bool) { u.timeline.SetLoadingOlder(on) }

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

	u.timeline = u.newTimeline()
	u.readerPane = u.newReaderPane()

	u.topicLabel = widget.NewLabel("")
	u.topicLabel.Truncation = fyne.TextTruncateEllipsis
	u.topicLabel.TextStyle = fyne.TextStyle{Italic: true}

	// The conversation's name IS the menu (see widgets.Title): the ⋮ that used
	// to sit in the corner offered the same list the sidebar row's right-click
	// offers, so the door moved onto the thing it acts on.
	u.bufferTitle = widgets.NewTitle(func(anchor fyne.CanvasObject) { u.showBufferMenu(anchor) })

	u.input = newChatEntry(u)
	u.input.SetPlaceHolder("Type a message… (Shift+Enter for a new line, /help for commands)")

	// The rail owns no state: it draws the viewmodel's rows and fires these
	// intents back at it (blueprint §4.1, internal/ui/widgets/rail.go).
	u.rail = widgets.NewRail(widgets.RailOptions{
		Model: u.vm,
		Show: func(r viewmodel.Ref) {
			if u.vm.Showing(r) {
				return // already there; the click was only catching up
			}
			u.showRef(r)
		},
		Connect: u.connectProfile,
		Menu:    u.rowMenuItems,
		Away: func(away bool) {
			if away {
				u.handleInput("/away")
				return
			}
			u.handleInput("/back")
		},
		Folded: u.saveFolds,
	})

	u.people = widgets.NewPeople(widgets.PeopleOptions{
		Model: u.vm,
		Open: func(nick string) {
			if s := u.session(); s != nil {
				s.getBuffer(nick)
				u.selectBuffer(nick)
			}
		},
		Menu:    u.memberMenuItems,
		Ignored: isIgnored,
	})

	// The app menu lives on the identity chip at the bottom of the rail
	// The app menu lives on the toolbar and stays there: one place to look,
	// whether or not the rail is showing.
	var settingsBtn *ttwidget.Button
	settingsBtn = ttwidget.NewButtonWithIcon("", theme.SettingsIcon(), func() { u.showMainMenu(settingsBtn) })
	settingsBtn.Importance = widget.LowImportance
	u.settingsBtn = settingsBtn
	u.tipButtons = append(u.tipButtons, tipButton{settingsBtn, "Menu"})

	// The rail is one step quieter than the chat pane, done with Fyne's
	// per-container theme override rather than a painted rectangle: rows the
	// list builds later inherit the scope too (blueprint §4.1).
	u.sidePane = ecotheme.Rail(u.rail)

	buffersBtn := ttwidget.NewButtonWithIcon("", theme.ListIcon(), u.toggleRail)
	u.buffersBtn = buffersBtn
	buffersBtn.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{buffersBtn, "Show or hide the channel list"})

	peopleToggle := ttwidget.NewButtonWithIcon("", theme.AccountIcon(), nil)
	u.peopleBtn = peopleToggle
	peopleToggle.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{peopleToggle, "Show or hide the member list"})

	// One button for finding a room, and it opens the one surface that does it
	// (§4.1): browsing the LIST and switching to a buffer stopped being two
	// questions the moment the Switcher ranked them in the same list.
	channelsBtn := ttwidget.NewButtonWithIcon("", theme.SearchIcon(), u.showSwitcher)
	u.channelsBtn = channelsBtn
	channelsBtn.Importance = widget.LowImportance
	channelsBtn.Disable()
	u.tipButtons = append(u.tipButtons, tipButton{channelsBtn, "Find a channel or conversation (Ctrl+K)"})

	u.status = newStatusDot()
	u.status.set(dotOffline, "Disconnected - use the gear menu to connect")

	topBar := container.NewBorder(nil, nil,
		container.NewHBox(buffersBtn, channelsBtn, settingsBtn, u.bufferTitle),
		container.NewHBox(container.NewPadded(u.status), peopleToggle),
		u.topicLabel)

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
		u.focusComposer()
	})
	send.Importance = widget.LowImportance
	u.tipButtons = append(u.tipButtons, tipButton{send, "Send"})

	u.composeHint = widget.NewLabel("")
	u.composeHint.TextStyle = fyne.TextStyle{Italic: true}
	u.composeHint.Hide()

	// The completion strip sits between the separator and the box it describes,
	// and is hidden - costing the layout nothing - whenever no cycle is running.
	u.candidates = widgets.NewCandidates()

	composer := container.NewBorder(nil, nil, nil,
		container.NewHBox(u.composeHint, send), u.input)
	bottom := container.NewVBox(widget.NewSeparator(),
		container.NewPadded(u.candidates), container.NewPadded(composer))

	// Ctrl+F's bar is a strip between the header and the Timeline, not a
	// floating box over it: it belongs to this conversation, and a hidden one
	// costs the layout nothing (§4.3).
	u.searchBar = u.newSearchBar()

	// the chat pane, and the two things that ever cover it: Reader mode and
	// the idle welcome screen
	u.centerBox = container.NewBorder(container.NewVBox(topBar, u.searchBar), bottom, nil, nil,
		container.NewStack(u.timeline, u.readerPane, u.welcome))
	peopleToggle.OnTapped = u.togglePeople

	// The three regions, in draggable splits (§4.1). A Split gives all of its
	// room to whichever child is visible, so hiding a pane is all Ctrl+B and
	// Ctrl+U have to do - and the divider disappears with it.
	u.peopleSplit = container.NewHSplit(u.centerBox, u.people)
	u.railSplit = container.NewHSplit(u.sidePane, u.peopleSplit)
	u.loadSplits()
	u.root = container.NewStack(u.railSplit, u.newOverlayLayer())

	// idle startup: both side panes stay closed and their toggles disabled
	// until a server is joined
	u.setRailVisible(false)
	u.people.Hide()
	buffersBtn.Disable()
	peopleToggle.Disable()
	u.bufferTitle.SetEnabled(false)

	u.bgImage = &canvas.Image{FillMode: canvas.ImageFillStretch}
	u.bgImage.Hide()
	u.applyBackground()

	w.SetContent(fynetooltip.AddWindowToolTipLayer(container.NewStack(u.bgImage, u.root), w.Canvas()))
	w.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) { u.handleKey(k) })
	// Fyne has no "the divider was dragged" callback, so this is the backstop:
	// wherever the user left the two splits is written down as the window goes.
	w.SetOnClosed(u.saveSplits)
	u.applyTips()
	u.registerShortcuts()
	w.Canvas().Focus(u.input)
	return u
}

// handleShortcut services the app-wide keyboard shortcuts; returns whether
// the combo was ours. Called from both the canvas and the focused entry.
//
// Every case asks the binding TABLE (keybinds.go) rather than comparing a key
// here, which is what lets the same list be rebindable, conflict-checked and
// documented without a second copy of it.
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
	case u.matchesAction(cs, "switcher"):
		u.showSwitcher()
	case u.matchesAction(cs, "find"):
		u.showSearch()
	case u.matchesAction(cs, "finder"):
		u.showFinder()
	case u.matchesAction(cs, "quit"):
		u.handleInput("/quit") // one quit path: /quit delays so QUIT flushes
	case u.matchesAction(cs, "rail"):
		u.toggleRail()
	case u.matchesAction(cs, "people"):
		u.togglePeople()
	case u.matchesAction(cs, "top"):
		u.timeline.ScrollToTop()
	case u.matchesAction(cs, "bottom"):
		u.jumpToPresent()
	case u.matchesAction(cs, "pins"):
		// A PINNED conversation, not the n-th row (§4.3). A row number is not a
		// name: with two networks connected, Alt+2 was a different conversation
		// every time one of them dropped.
		u.jumpToPin(int(cs.KeyName[0] - '0'))
	default:
		return false
	}
	return true
}

// jumpToPin is Alt+1..9. A digit nobody has pinned does nothing at all -
// quietly, because a person who has pinned three conversations pressing Alt+4
// has made no mistake worth a message.
func (u *UI) jumpToPin(n int) {
	r, ok := u.vm.PinRef(n)
	if !ok {
		return
	}
	u.selectRef(r)
}

// jumpToPresent is Ctrl+End and the End key: back to the newest message, and
// following it again. It is the same call the jump pill makes, so "N new
// messages ↓ (End)" is true by construction.
func (u *UI) jumpToPresent() {
	u.timeline.ScrollToBottom()
	u.focusComposer()
}

// stepBuffer moves to the next or previous buffer. The rule that it steps only
// across selectable rows - a network header switches nothing, and selecting an
// offline network would dial a server - is the viewmodel's, and is tested
// there.
func (u *UI) stepBuffer(dir int) {
	if i, ok := u.vm.Step(dir); ok {
		u.rail.Select(i)
	}
}

// jumpToUnread is Alt+A. The viewmodel may unfold a collapsed network to get
// there, which is why the rail is repainted before the row is selected: the
// index it returned is an index into the rail it just changed.
func (u *UI) jumpToUnread() {
	if i, ok := u.vm.NextUnread(); ok {
		u.rail.Refresh()
		u.rail.Select(i)
	}
}

// registerShortcuts installs the keyboard. There is nothing left to do here
// beyond applying the table: the fixed Alt+digits used to be a hand-rolled loop
// in this function, which is exactly the second list that could not be
// conflict-checked or documented, and they are a row in keyActions now.
func (u *UI) registerShortcuts() { u.applyKeybinds() }

// refreshBuffers hands the sidebar's inputs to the viewmodel - each session's
// buffers, and the pinned profiles - and repaints it. The rows themselves, and
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
	u.rail.Refresh()
	// The badge is a sum over the buffers that exist, so it moves when the list
	// of them does - a network closing takes its mentions with it (§4.4).
	u.updateBadge()

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

// built reports whether this window's widgets exist. A UI can be constructed
// without them - the command-wiring tests run the real table against a real
// session and a real socket with no Fyne window - and in that state the
// viewmodel move is still the fact; there is simply no picture of it to repaint.
func (u *UI) built() bool { return u.rail != nil }

// selectRef shows a buffer and scrolls the rail to it.
func (u *UI) selectRef(r viewmodel.Ref) {
	if !u.built() {
		u.vm.Select(r) // no window: which buffer is on screen is all there is
		return
	}
	u.refreshBuffers()
	u.showRef(r)
	if i := u.vm.IndexOf(r); i >= 0 {
		u.rail.Select(i)
	}
}

// showRef is the one path a buffer reaches the screen through: the intent moves
// the viewmodel, and everything after it is rendering.
func (u *UI) showRef(r viewmodel.Ref) {
	// §4.2's NEW divider hangs on the marker as it stood BEFORE this switch,
	// and this line is the whole of why it is sampled here rather than at the
	// draw. Arriving in a buffer marks it read - the viewmodel asks for the
	// marker move inside Select, which is what makes the badge go to zero - so
	// a divider seeded afterwards is seeded from the newest message and
	// separates nothing. It was: every NEW divider in the program landed at the
	// bottom of the buffer, invisibly, which is why nothing had noticed.
	u.dividerSeed = u.markerFor(r)
	// The finding surfaces are questions about the conversation on screen -
	// which matches, which room - so a switch is the end of them. Closing here
	// rather than in each of the three is what keeps a search bar from
	// outliving the buffer it was searching.
	if !u.vm.Showing(r) {
		u.closeOverlays()
	}
	if u.vm.Select(r) {
		// The outgoing buffer's text has been parked and this one's is back;
		// a first show says no, so text typed before connecting survives.
		u.input.SetText(u.vm.Draft())
		u.input.cursorToEnd()
	}
	// §4.4's suppression step needs to know which buffer is being read, and
	// this is the one place a buffer reaches the screen through.
	u.focusBuffer(r)
	u.renderBuffer()
	u.updateBadge()
}

// markerFor reads a buffer's read marker, or the zero time when there is no
// session behind the ref.
func (u *UI) markerFor(r viewmodel.Ref) time.Time {
	s := u.sessionFor(r.Session)
	if s == nil {
		return time.Time{}
	}
	return s.ReadMarker(r.Name)
}

// renderBuffer draws the buffer the viewmodel is showing. It holds no state of
// its own: call it after an intent moved something, or on its own to repaint
// (an amended row, a cleared buffer, a settings change).
func (u *UI) renderBuffer() {
	name := u.current()
	u.bufferTitle.SetText(name)
	u.setLoadingOlder(false) // whatever the last buffer was waiting for is not this one's
	sess := u.session()
	if sess == nil {
		return // idle startup: nothing to render yet
	}
	u.rail.Refresh()

	u.pending = u.pending[:0] // queued for the buffer we just left
	u.showBuffer(sess, name)
	u.topicLabel.SetText(sess.topicOf(name))
	u.segsDirty = false
	u.setRoster(sess)
	u.applyPeople()
	u.setIdentity(u.identityOf(sess))
	u.refreshReader()
	u.focusComposer()
}

// appendLine queues one arrived line for the pane.
//
// Whether the view follows it is not decided here and is not recorded
// anywhere else either: the Timeline samples the reader's position before it
// takes a batch and answers for itself (v1's stickBottom discipline, now the
// widget's). Until M11(c) the viewmodel kept a copy of that answer which
// nothing read.
func (u *UI) appendLine(l Line) {
	// The Timeline is handed the whole batch at once, in BatchDone. A hundred
	// lines of playback must cost one measure-and-repaint, not a hundred -
	// which is the same reason the funnel exists (M8), and the reason the
	// widget's Append takes a slice.
	u.pending = append(u.pending, l)
	u.segsDirty = true
	u.scheduleFlush()
}

// scheduleFlush marks a repaint as owed. Since M8 the timing is not this
// layer's business: the funnel coalesces on its 80ms tick and closes each
// batch with BatchDone, which is where the repaint is actually paid for. All
// that is left here is the flag.
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
		u.timeline.Append(u.pending...)
		u.pending = u.pending[:0]
	}
	if u.unreadDirty {
		u.unreadDirty = false
		u.rail.Refresh()
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
