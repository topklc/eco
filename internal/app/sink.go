package app

import (
	"strings"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// The UI is the funnel's Sink: this file is every way a Change can reach the
// screen, and there is no other one.
//
// Threading: every method here runs on the Fyne thread, inside the single
// fyne.Do of one batch — so no method opens its own, and none may block. The
// batch ends with BatchDone (in ui.go), which pays for the repaint once.
//
// Liveness: nothing here asks "is this session still mine?". The funnel drops a
// torn-down session's changes in one place (dispatch.Dispatcher.Close, called
// from closeSession), which is what replaced twelve copies of that question.
// What the methods below still ask is "is this on screen?" — a rendering
// question, answered by u.showing.

// LinesAdded appends rows to a buffer: the visible one is drawn, the rest are
// counted. Notifications are decided per row, from the row's own flags, exactly
// as the AddLine callback decided them.
func (u *UI) LinesAdded(id dispatch.SessionID, b dispatch.Buffer, lines []model.Line) {
	s := u.sessionFor(id)
	if s == nil {
		return
	}
	visible := u.showing(s, b)
	for _, l := range lines {
		if l.Highlight || (config.Current().NotifyAll && !l.Self && !l.Status && !l.Playback) {
			u.notify(l.Nick+" ("+b.Name+")", l.Text)
		}
		if visible {
			u.appendLine(l)
			continue
		}
		// only live chat messages count as unread; history playback, joins,
		// parts, and status noise do not
		if !l.Status && !l.Playback && !l.Self {
			key := skey(s, b.Name)
			u.unread[key]++
			u.unreadDirty = true
			if l.Highlight {
				u.hlBuffers[key] = true
			}
		}
		u.scheduleFlush()
	}
}

// LineAmended redraws a buffer whose row changed in place — but never while the
// reader is up in the scrollback, where the redraw would yank the view.
func (u *UI) LineAmended(id dispatch.SessionID, b dispatch.Buffer) {
	if !u.showing(u.sessionFor(id), b) {
		return
	}
	gap := (u.messages.MinSize().Height - u.msgScroll.Size().Height) - u.msgScroll.Offset.Y
	if gap > 80 {
		return // reading history; the redraw would yank the view
	}
	u.showBuffer(u.current)
}

// LineRedacted repaints a buffer someone withdrew a message from.
//
// The core has owned redaction since the v2 cutover: the store replaces the
// withdrawn message with a tombstone and the session's projection already
// carries it, so all this has to do is repaint a buffer the user is looking at.
// v1 kept the text in the buffer and asked a callback to overwrite it, which
// left the original in memory — and in the plaintext view — for as long as the
// buffer lived.
func (u *UI) LineRedacted(id dispatch.SessionID, b dispatch.Buffer, msgID string) {
	if !u.showing(u.sessionFor(id), b) {
		return
	}
	u.showBuffer(u.current)
}

// BufferListChanged repaints the sidebar.
func (u *UI) BufferListChanged(id dispatch.SessionID) {
	if u.sessionFor(id) == nil {
		return
	}
	u.refreshBuffers()
}

// BufferRenamed migrates everything this layer files per buffer, so a rename
// keeps drafts, unread counts and mention flags instead of orphaning them under
// a name nothing will ask for again.
func (u *UI) BufferRenamed(id dispatch.SessionID, from, to dispatch.Buffer) {
	s := u.sessionFor(id)
	if s == nil {
		return
	}
	oldKey, newKey := skey(s, from.Name), skey(s, to.Name)
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
	// the viewer follows the rename instead of bouncing to the server log
	if s == u.sess && strings.EqualFold(u.current, from.Name) {
		u.current = to.Name
		u.currentKey = newKey
		u.bufferTitle.SetText(to.Name)
	}
}

// MembersChanged refreshes the roster pane when it is the roster on screen.
func (u *UI) MembersChanged(id dispatch.SessionID, b dispatch.Buffer) {
	if !u.showing(u.sessionFor(id), b) {
		return
	}
	u.members = u.sess.SortedMembers(u.current)
	u.memberList.Refresh()
}

// TopicChanged updates the topic bar.
func (u *UI) TopicChanged(id dispatch.SessionID, b dispatch.Buffer, topic string) {
	if !u.showing(u.sessionFor(id), b) {
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
	if s != u.sess {
		return
	}
	// The order is by status first, deliberately: a refused connection reads as
	// a refusal even if the client happens to have queued a retry behind it,
	// which is what the old "does the sentence contain 'Reconnecting'?" test
	// happened to give us and what a person needs to be told.
	switch {
	case c.Status == model.ConnReady:
		u.status.set(dotGreen, "Connected to "+u.networkName+" as "+u.sess.CurrentNick())
	case c.Status == model.ConnFailed:
		u.status.set(dotRed, "Connection failed: "+
			firstNonEmpty(c.Err, "the server rejected the connection")+
			", right-click the network to reconnect")
	case c.Retrying:
		u.status.set(dotOrange, "Connection lost, reconnecting")
	default:
		u.status.set(dotRed, "Disconnected, right-click the network to reconnect")
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
	if !requested || s == nil || s != u.sess {
		return
	}
	u.showChannelBrowser()
}
