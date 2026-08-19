package app

import (
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The UI is the funnel's Sink: this file is every way a Change can reach the
// screen, and there is no other one.
//
// Threading: every method here runs on the Fyne thread, inside the single
// fyne.Do of one batch — so no method opens its own, and none may block. The
// batch ends with BatchDone (in ui.go), which pays for the repaint once.
//
// Liveness is not decided here. The funnel drops a torn-down session's changes
// in one place (dispatch.Dispatcher.Close, called from retireSession), which is
// what replaced twelve copies of that question. The u.sessionFor(id) == nil
// checks below are not that rule a second time: they resolve the session these
// methods need, and returning early on a miss is what makes them total.
// What the methods below do ask is "is this on screen?" — a rendering question,
// answered by u.showing, per session and not per name.

// LinesAdded appends rows to a buffer: the visible one is drawn, the rest are
// left to the counters.
//
// What this no longer does is DECIDE anything. Unread is derived once, in the
// store, from the read marker (state.countsUnread) and arrives as
// UnreadChanged. Since M9 the notification is the same: the store evaluates
// §4.4 (the buffer's level, the highlight match, the playback rule) and says
// so on the change, and the copy of that rule that used to live in this file is
// gone. The window's job is to raise the toast, not to have an opinion about
// whether one is due.
func (u *UI) LinesAdded(id dispatch.SessionID, b dispatch.Buffer, lines []model.Line, notify bool) {
	if u.sessionFor(id) == nil {
		return
	}
	visible := u.showing(id, b)
	for _, l := range lines {
		if notify {
			u.notify(l.Nick+" ("+b.Name+")", l.Text)
		}
		if visible {
			u.appendLine(l)
		}
	}
}

// LinesPrepended puts older rows above what is on screen and keeps the reader
// where they were. Scrolling up to read history and being thrown back to the
// bottom for the trouble is the failure this exists to avoid, so the view's
// offset is moved by exactly as much as the content grew — arithmetic the
// Timeline does on its own rows.
func (u *UI) LinesPrepended(id dispatch.SessionID, b dispatch.Buffer, lines []model.Line) {
	if !u.showing(id, b) || len(lines) == 0 {
		return
	}
	// the Timeline does the arithmetic on its own rows: the view moves down by
	// exactly what arrived above it, which is standing still
	u.timeline.Prepend(lines)
}

// HistoryLoading shows or hides the "Loading older…" state for the buffer on
// screen. It is a label in the topic bar's row rather than a spinner, which is
// the v1 face's vocabulary; the state itself is the funnel's.
func (u *UI) HistoryLoading(id dispatch.SessionID, b dispatch.Buffer, loading bool) {
	if !u.showing(id, b) {
		return
	}
	u.setLoadingOlder(loading)
}

// AskDMLogging is the first-DM privacy prompt: the moment a private
// conversation is about to be written to disk, said out loud, once per profile.
// Both answers are remembered, so this is the only time it is asked.
func (u *UI) AskDMLogging(id dispatch.SessionID, b dispatch.Buffer) {
	if u.sessionFor(id) == nil {
		return
	}
	p := u.app.Preferences()
	if p.BoolWithFallback("dmloganswered", false) {
		return
	}
	dialog.ShowCustomConfirm("Keep a history of private messages?",
		"Keep history", "Do not save DMs",
		widget.NewRichTextFromMarkdown(
			"eco saves your conversations on **this computer** so they are still here "+
				"after you restart, and so you can search them.\n\n"+
				"The file is **not encrypted**: anyone who can read your files can read it.\n\n"+
				"You can change this any time in Preferences → History, and turn logging off "+
				"for a single channel or person from its right-click menu."),
		func(keep bool) {
			p.SetBool("dmloganswered", true)
			p.SetBool("logexcludedms", !keep)
			applySettings(u.app)
			for _, s := range u.sessions {
				s.ApplyDMLogging(!keep)
			}
		}, u.win)
}

// UnreadChanged files the counts the store derived for a buffer. The viewmodel
// keeps the number and suppresses it for the buffer on screen; nothing in this
// layer computes one.
func (u *UI) UnreadChanged(id dispatch.SessionID, b dispatch.Buffer, unread, highlights int) {
	if u.sessionFor(id) == nil {
		return
	}
	u.vm.SetUnread(id, b.Name, viewmodel.Counts{Unread: unread, Highlights: highlights})
	u.unreadDirty = true
	u.scheduleFlush()
}

// LineAmended redraws a buffer whose row changed in place — but never while the
// reader is up in the scrollback, where the redraw would yank the view.
func (u *UI) LineAmended(id dispatch.SessionID, b dispatch.Buffer) {
	if !u.showing(id, b) {
		return
	}
	if !u.nearBottom() {
		return // reading history; the redraw would yank the view
	}
	u.renderBuffer()
}

// LineRedacted repaints a buffer someone withdrew a message from.
//
// The core has owned redaction since the v2 cutover: the store replaces the
// withdrawn message with a tombstone and the session's projection already
// carries it, so all this has to do is repaint a buffer the user is looking at.
// v1 kept the text in the buffer and asked a callback to overwrite it, which
// left the original in memory — and in v1's plaintext view — for as long as
// the buffer lived. The repaint here goes through renderBuffer, so an open
// Reader mode is rebuilt from the tombstoned buffer with it.
func (u *UI) LineRedacted(id dispatch.SessionID, b dispatch.Buffer, msgID string) {
	if !u.showing(id, b) {
		return
	}
	u.renderBuffer()
}

// BufferListChanged repaints the sidebar.
func (u *UI) BufferListChanged(id dispatch.SessionID) {
	if u.sessionFor(id) == nil {
		return
	}
	u.refreshBuffers()
}

// BufferRenamed migrates everything filed per buffer — draft, counts, sidebar
// row, and the viewer when it is looking at it — instead of orphaning it under
// a name nothing will ask for again. The migration is the viewmodel's, and is
// tested there; what is left here is the title bar.
func (u *UI) BufferRenamed(id dispatch.SessionID, from, to dispatch.Buffer) {
	if u.sessionFor(id) == nil {
		return
	}
	if u.vm.BufferRenamed(id, from.Name, to.Name) {
		u.bufferTitle.SetText(to.Name)
	}
}

// MembersChanged refreshes the roster pane when it is the roster on screen.
func (u *UI) MembersChanged(id dispatch.SessionID, b dispatch.Buffer) {
	s := u.sessionFor(id)
	if s == nil || !u.showing(id, b) {
		return
	}
	u.vm.SetMembers(s.SortedMembers(u.current()))
	u.memberList.Refresh()
}

// TopicChanged updates the topic bar.
func (u *UI) TopicChanged(id dispatch.SessionID, b dispatch.Buffer, topic string) {
	if !u.showing(id, b) {
		return
	}
	u.topicLabel.SetText(topic)
}

// ConnStateChanged drives the status dot. The wording lives here because it is
// wording: the session reports the state and whether the client intends to dial
// again, and this turns that into the sentence a person reads.
func (u *UI) ConnStateChanged(id dispatch.SessionID, c dispatch.ConnState) {
	s := u.sessionFor(id)
	if s == nil {
		return
	}
	u.refreshBuffers() // root row labels track connection state
	if s != u.session() {
		return
	}
	// The order is by status first, deliberately: a refused connection reads as
	// a refusal even if the client happens to have queued a retry behind it,
	// which is what the old "does the sentence contain 'Reconnecting'?" test
	// happened to give us and what a person needs to be told.
	switch {
	case c.Status == model.ConnReady:
		u.status.set(dotOnline, "Connected to "+u.vm.CurrentProfile()+" as "+s.CurrentNick())
	case c.Status == model.ConnFailed:
		u.status.set(dotOffline, "Connection failed: "+
			firstNonEmpty(c.Err, "the server rejected the connection")+
			", right-click the network to reconnect")
	case c.Retrying:
		u.status.set(dotConnecting, "Connection lost, reconnecting")
	default:
		u.status.set(dotOffline, "Disconnected, right-click the network to reconnect")
	}
}

// JoinRefused explains a refused JOIN, and asks for the key when that is all
// that is missing.
func (u *UI) JoinRefused(id dispatch.SessionID, channel, reason string, needKey bool) {
	s := u.sessionFor(id)
	if s == nil {
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
			s.JoinWithKey(channel, key.Text)
		}
	}, u.win)
}

// STSOffered applies an advertised STS policy. The rule about when a policy may
// be *stored* lives here because it is about the connection it arrived on,
// which is this layer's to know.
func (u *UI) STSOffered(id dispatch.SessionID, port string, seconds int64) {
	s := u.sessionFor(id)
	if s == nil || transport.IsWebSocketURL(s.Config.Server) {
		return
	}
	host := s.Config.Server
	// per the STS spec, persist policies ONLY from a secure connection: on
	// plaintext the advertisement is attacker controlled and may only trigger
	// the upgrade attempt
	if s.Config.TLS {
		config.STSRecord(u.app.Preferences(), host, s.Config.Port, seconds)
		return
	}

	// plaintext connection to an STS host: upgrade immediately
	if port != "" && seconds > 0 {
		s.status(serverBuffer, "This server enforces TLS (STS policy). Reconnecting securely on port "+port+".")
		cfg := s.Config
		cfg.TLS = true
		cfg.Port = port
		u.startSession(cfg, s.Name)
	}
}

// ChannelListDone opens the browser on a scan somebody asked for. A background
// rescan after an auto-reconnect refreshes the data and says nothing.
func (u *UI) ChannelListDone(id dispatch.SessionID, requested bool) {
	s := u.sessionFor(id)
	if !requested || s == nil || s != u.session() {
		return
	}
	u.showChannelBrowser()
}
