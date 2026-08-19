package app

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The M11(b) flag. ECO_NEWCHAT=1 swaps the chat pane's single RichText — one
// widget holding every segment of the buffer, capped at 250 lines, rebuilt
// whole on every switch — for widgets.MessageList, the virtualized Timeline
// of §4.2.
//
// Both paths are live and both are tested. The old one is still the default
// because the mono gate (R-10, M11(c)) has not been run yet: until the
// CJK/emoji/RTL corpus says whether the body is monospace or proportional,
// the Timeline's typography is a decision that has not been made. The flag
// is how it gets used in anger before that day.
//
// Everything below is one of two shapes: a constructor for the Timeline, or a
// fork in a path the funnel already had. There is no third copy of any rule —
// stick-to-bottom, the pager and the read marker are all read from the same
// places the old path reads them.
func newChatEnabled() bool {
	v := os.Getenv("ECO_NEWCHAT")
	if v == "" {
		return false
	}
	on, err := strconv.ParseBool(v)
	return err == nil && on
}

// newTimeline builds the Timeline and wires it to this window. Every callback
// here is a seam that already existed: the pager is /history's, the clipboard
// and the redaction are the row menu's, and the composer is the composer.
func (u *UI) newTimeline() *widgets.MessageList {
	return widgets.NewMessageList(widgets.Options{
		Render: func() render.Options {
			mono := strings.EqualFold(u.current(), serverBuffer)
			opts := renderOptions(mono, u.mentionResolver())
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
	u.win.Canvas().Focus(u.input)
}

// logNote writes a line into the network log of the session on screen. It is
// the only way this file says anything to the user.
func (u *UI) logNote(text string) {
	if s := u.session(); s != nil {
		s.status(serverBuffer, text)
	}
}

// timelineShowing reports whether the Timeline is the chat pane right now.
// Every fork below asks this rather than the environment, so a test can build
// either path.
func (u *UI) timelineShowing() bool { return u.timeline != nil }

// showTimelineBuffer is renderBuffer's new-path half: hand the Timeline the
// buffer and the marker its NEW divider hangs on.
func (u *UI) showTimelineBuffer(sess *Session, name string) {
	if sess == nil {
		u.timeline.SetBuffer(nil, time.Time{})
		return
	}
	var lines []Line
	if b := sess.Buffer(name); b != nil {
		sess.mu.Lock()
		lines = append([]Line(nil), b.Lines...)
		sess.mu.Unlock()
	}
	u.timeline.SetBuffer(lines, sess.ReadMarker(name))
}
