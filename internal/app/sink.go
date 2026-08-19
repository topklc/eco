package app

import (
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
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

// LineAmended redraws a buffer whose row changed in place.
//
// It used to refuse to repaint at all while the reader was up in the
// scrollback, because the repaint went through renderBuffer — a buffer switch,
// which jumps to the newest message. That traded one bug for another: the
// amended row simply stayed wrong until something else redrew the pane.
// repaintBuffer keeps the reader exactly where they are, so there is nothing
// left to trade (M11 verification).
func (u *UI) LineAmended(id dispatch.SessionID, b dispatch.Buffer) {
	if !u.showing(id, b) {
		return
	}
	u.repaintBuffer()
}

// LineRedacted repaints a buffer someone withdrew a message from.
//
// The core has owned redaction since the v2 cutover: the store replaces the
// withdrawn message with a tombstone and the session's projection already
// carries it, so all this has to do is repaint a buffer the user is looking at.
// v1 kept the text in the buffer and asked a callback to overwrite it, which
// left the original in memory — and in v1's plaintext view — for as long as
// the buffer lived. The repaint here re-reads the buffer, so the tombstone
// replaces the text in the pane and in an open Reader mode both; and it keeps
// the reader's position, because someone else deleting a message is not a
// reason to lose the place you were reading.
func (u *UI) LineRedacted(id dispatch.SessionID, b dispatch.Buffer, msgID string) {
	if !u.showing(id, b) {
		return
	}
	u.repaintBuffer()
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
	u.setRoster(s)
	// The pane's default visibility is "does this buffer have a roster?", and
	// for a channel just joined the answer arrives HERE, with the names — so
	// this is where it opens, not only on the next buffer switch.
	u.applyPeople()
}

// SelfChanged redraws the identity chip: the nick the server has us under, the
// account we are logged in as, and our own away state. Only the network on
// screen owns the chip — the others keep changing in the background, and a
// chip that reported whichever network moved last would be a lie about the one
// you are looking at.
func (u *UI) SelfChanged(id dispatch.SessionID, nick, account string, away bool) {
	if id != u.vm.Current().Session {
		return
	}
	u.setIdentity(widgets.Identity{Nick: nick, Account: account, Away: away})
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

// ChannelListDone repaints the Switcher when a scan somebody asked for lands.
// A background rescan after an auto-reconnect refreshes the cache and says
// nothing.
//
// It repaints rather than opening: since M12 the LIST has no window of its own
// to appear in, so a scan finishing while the Switcher is open fills it in
// under the query already typed, and one finishing while it is closed is simply
// a better answer waiting for the next Ctrl+K.
func (u *UI) ChannelListDone(id dispatch.SessionID, requested bool) {
	s := u.sessionFor(id)
	if !requested || s == nil {
		return
	}
	if u.switcherBox != nil && u.switcherBox.Visible() {
		u.switcher.Reload()
	}
}
