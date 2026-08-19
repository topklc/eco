package app

import (
	"net/url"
	"strings"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The chat pane, and everything the window has to hand it.
//
// Since M11(c) there is one of these. The flag that used to choose between
// this and v1's single RichText (`ECO_NEWCHAT`) is gone with the widget it
// chose: an escape hatch back to a deleted pane is not an escape hatch, and
// keeping the old one alive would have meant keeping its 250-line cap, its
// second copy of every display rule, and a plaintext fork of the buffer. The
// fallback that does still exist is the one blueprint risk #1 names —
// fixed-height rows — and it is a change to one function in
// internal/ui/widgets, not a switch.

// monoBody reports whether a buffer's message bodies are set in the
// monospace face. This is the M11(c) gate's answer, and it lives here so
// there is exactly one of it.
//
// The gate (R-10) measured the CJK/emoji/RTL corpus in both candidate
// typographies and chose: gutter, nick column and art monospace, conversation
// bodies proportional. The short version of why is that the columns are the
// row's, not the font's — timeline rows lay the gutter and the nick out in
// pixels, so a proportional body costs no alignment — while the alignment
// monospace would buy inside the body is a fiction for every script that is
// not Latin: a CJK or emoji glyph is 1.66 monospace cells wide because it
// comes from a fallback face, in either typography, identically.
//
// The network log is the exception, and it is not a conversation: it is the
// MOTD, LIST and WHOIS as the server formatted them, in columns that only
// line up in a fixed-width face. Reader mode (F6) is monospace for the same
// reason.
func monoBody(buffer string) bool { return strings.EqualFold(buffer, serverBuffer) }

// newTimeline builds the chat pane and wires it to this window. Every
// callback here is a seam that already existed: the pager is /history's, the
// clipboard and the redaction are the row menu's, and the composer is the
// composer.
func (u *UI) newTimeline() *widgets.MessageList {
	return widgets.NewMessageList(widgets.Options{
		Render: func() render.Options {
			opts := renderOptions(monoBody(u.current()), u.mentionResolver())
			opts.URL = u.urlResolver()
			return opts
		},
		Older: func() {
			if s := u.session(); s != nil {
				s.LoadOlder(u.current())
			}
		},
		Copy:  func(text string) { u.win.Clipboard().SetContent(text) },
		Quote: u.quoteIntoComposer,
		Delete: func(l model.Line) {
			if l.MsgID != "" {
				u.handleInput("/redact " + l.MsgID)
			}
		},
	})
}

// urlResolver turns a link in a message into an action: open it in the
// browser. It is the v1 gap §4.2 names — v1 rendered a URL as text and left
// the user to select it, which v1 could not do either.
//
// The scheme filter is render's (http and https only); this side re-checks
// the parse, because the thing being handed to the desktop is a URL a
// stranger on IRC wrote.
func (u *UI) urlResolver() func(string) func() {
	return func(raw string) func() {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil
		}
		return func() {
			if err := u.app.OpenURL(parsed); err != nil {
				u.logNote("Could not open " + parsed.Host + ": " + err.Error())
			}
		}
	}
}

// quoteIntoComposer puts a message in front of the reply to it: the quoted
// line, then the cursor on a line of its own. Shift+Enter multiline is v1's
// and is already what the composer speaks.
func (u *UI) quoteIntoComposer(l model.Line) {
	text := render.PlainText(l)
	if l.Nick != "" && !l.Status {
		text = "<" + l.Nick + "> " + text
	}
	u.input.SetText("> " + text + "\n")
	u.input.cursorToEnd()
	u.focusComposer()
}

// logNote writes a line into the network log of the session on screen. It is
// the only way this file says anything to the user.
func (u *UI) logNote(text string) {
	if s := u.session(); s != nil {
		s.status(serverBuffer, text)
	}
}

// showBuffer hands the Timeline a buffer and the marker its NEW divider hangs
// on. The marker is read once, here, because a landmark that follows the
// reader down the screen is not one.
//
// This is a buffer SWITCH, and it is the only path allowed to move the reader:
// it jumps to the newest message. Anything that changes a buffer the reader is
// already in goes through repaintBuffer instead.
func (u *UI) showBuffer(sess *Session, name string) {
	if sess == nil {
		u.timeline.SetBuffer(nil, time.Time{})
		return
	}
	u.timeline.SetBuffer(u.bufferLines(sess, name), sess.ReadMarker(name))
}

// bufferLines copies the buffer's lines out from under the session's lock.
func (u *UI) bufferLines(sess *Session, name string) []Line {
	b := sess.Buffer(name)
	if b == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return append([]Line(nil), b.Lines...)
}

// repaintBuffer redraws the buffer on screen from the session, in place: the
// same buffer, changed underneath. A withdrawn message and an amended row both
// need it, and neither is a reason to yank someone reading history back to the
// bottom — which is what renderBuffer, being a buffer switch, does.
//
// It re-reads the lines rather than redrawing the ones the Timeline holds,
// because the change it is answering is IN those lines: a redaction replaces
// the message with a tombstone in the session's slice, and a pane redrawn from
// its own copy would keep the withdrawn text on screen.
func (u *UI) repaintBuffer() {
	sess := u.session()
	if sess == nil {
		return
	}
	// the whole buffer is about to be handed over, and the queue is already
	// inside it — the same reason renderBuffer drops it
	u.pending = u.pending[:0]
	u.segsDirty = false
	u.timeline.SetLines(u.bufferLines(sess, u.current()))
	u.refreshReader()
}

// repaintPane redraws the pane after something changed how it LOOKS rather
// than what it holds — a settings change. The lines are the ones the Timeline
// already has; only the display options moved, and the reader stays put.
func (u *UI) repaintPane() {
	u.timeline.Redraw()
	u.refreshReader()
}
