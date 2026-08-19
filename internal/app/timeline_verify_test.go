package app

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The seams the M11 mutation sweep found nothing holding: what the window does
// when a buffer changes UNDER the reader (a withdrawal, an amended row, a
// settings change), and the parts of Reader mode whose numbers were only ever
// asserted by hand.
//
// The distinction every test here turns on: showBuffer is a buffer SWITCH and
// jumps to the newest message; repaintBuffer is the same buffer changing
// underneath and must not move the reader at all. Before this file the sweep
// could delete the redaction repaint outright and the suite stayed green.

// wiredUI is a full widget tree with a real Session on screen, so the sink's
// callbacks reach the pane the way they do in the running app.
func wiredUI(t *testing.T) (*UI, *Session) {
	t.Helper()
	u := newTestUI(t)
	// NewUI's own funnel runs its batches through fyne.Do, which never returns
	// without an event loop under it. dispatch has exactly one seam for that
	// (Options.Do) and this is what it is for.
	u.dispatch.Stop()
	d := dispatch.New(dispatch.Options{Sink: u, Do: func(f func()) { f() }})
	t.Cleanup(d.Stop)
	u.dispatch = d

	s := NewSession(Config{Server: "irc.test", Port: "6667", Nick: testNick}, d)
	s.Name = "libera"
	d.Open(s.ID())
	u.sessions = append(u.sessions, s)
	u.vm.AddSession(s.ID(), s.Name)
	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: "#eco"})
	return u, s
}

// put writes lines straight into the session's buffer, the way the store's
// projection does, without needing a server.
func put(t *testing.T, u *UI, s *Session, name string, lines ...Line) {
	t.Helper()
	s.status(name, "opening the buffer") // the only public way to mint one
	b := s.Buffer(name)                  // and Buffer takes s.mu, so never call it under s.mu
	if b == nil {
		t.Fatalf("no buffer %q", name)
	}
	s.mu.Lock()
	b.Lines = append(b.Lines[:0], lines...)
	s.mu.Unlock()
	// the status line above is in flight through the funnel; land it and draw
	// it now, so no test has a stray message arriving mid-assertion
	u.dispatch.Flush()
	u.BatchDone()
}

// TestAWithdrawnMessageLeavesThePane is the privacy claim in §4.2 and the
// M11(c) log, wired end to end. It used to be proved by a test that called
// refreshReader by hand - which proves the function, not that anything calls
// it. Deleting the whole body of LineRedacted left the old suite green.
func TestAWithdrawnMessageLeavesThePane(t *testing.T) {
	u, s := wiredUI(t)
	put(t, u, s, "#eco",
		Line{Time: time.Now(), Nick: "alice", Text: "ordinary chatter"},
		Line{Time: time.Now(), Nick: "bob", Text: "the secret", MsgID: "abc"})
	u.renderBuffer()
	u.toggleReader()
	if !strings.Contains(u.reader.Text, "the secret") {
		t.Fatalf("the reader never had it: %q", u.reader.Text)
	}

	// the store tombstones the row in place; the window is only told to repaint
	b := s.Buffer("#eco") // Buffer takes the lock itself: never inside one
	s.mu.Lock()
	for i, l := range b.Lines {
		if l.MsgID == "abc" {
			b.Lines[i] = Line{Time: l.Time, Text: redactedText, Status: true, MsgID: l.MsgID}
		}
	}
	s.mu.Unlock()
	u.LineRedacted(s.ID(), dispatch.Buffer{Name: "#eco"}, "abc")

	for _, l := range u.timeline.Lines() {
		if strings.Contains(l.Text, "the secret") {
			t.Error("the withdrawn text is still in the Timeline")
		}
	}
	if strings.Contains(u.reader.Text, "the secret") {
		t.Errorf("the withdrawn text survived in Reader mode, where it can be copied: %q", u.reader.Text)
	}
	if !strings.Contains(u.reader.Text, redactedText) {
		t.Errorf("the reader was not rebuilt from the tombstoned buffer: %q", u.reader.Text)
	}
}

// TestAnAmendedRowIsRepaintedEvenInTheScrollback. The old path refused to
// repaint at all while the reader was up in the scrollback, because repainting
// meant a buffer switch and a buffer switch jumps to the bottom. That left the
// amended row showing its old text until something else redrew the pane.
func TestAnAmendedRowIsRepaintedEvenInTheScrollback(t *testing.T) {
	u, s := wiredUI(t)
	var lines []Line
	for i := 0; i < 80; i++ {
		lines = append(lines, Line{Time: time.Now().Add(time.Duration(i) * time.Second),
			Nick: "alice", Text: "filler"})
	}
	lines[0].Text, lines[0].MsgID = "before the edit", "abc"
	put(t, u, s, "#eco", lines...)
	u.renderBuffer()

	u.timeline.ScrollToTop()
	if u.nearBottom() {
		t.Skip("the pane is not tall enough to scroll in this headless window")
	}
	before := len(u.timeline.Lines())

	b := s.Buffer("#eco") // outside the lock: Buffer takes it
	s.mu.Lock()
	b.Lines[0].Text = "after the edit"
	s.mu.Unlock()
	u.LineAmended(s.ID(), dispatch.Buffer{Name: "#eco"})

	if got := u.timeline.Lines(); len(got) != before || got[0].Text != "after the edit" {
		t.Errorf("the pane still reads %q while the reader is in the scrollback", got[0].Text)
	}
	if u.nearBottom() {
		t.Error("repainting an amended row threw the reader back to the newest message")
	}
}

// TestASettingsChangeRepaintsThePaneWithoutMovingIt. Turning timestamps off is
// not a reason to lose your place, and it IS a reason for the pane to redraw:
// the segments are cached per line and would otherwise keep the old options.
func TestASettingsChangeRepaintsThePaneWithoutMovingIt(t *testing.T) {
	u, s := wiredUI(t)
	// long enough to wrap: the gutter is worth about forty pixels, and forty
	// pixels only shows up in the pane's height if there is a wrap for it to
	// move. One-line filler would make this test pass against a pane that
	// never re-read anything.
	long := strings.Repeat("a reasonably long message that has to wrap more than once ", 3)
	var lines []Line
	for i := 0; i < 80; i++ {
		lines = append(lines, Line{Time: time.Now().Add(time.Duration(i) * time.Second),
			Nick: "alice", Text: long})
	}
	put(t, u, s, "#eco", lines...)
	u.renderBuffer()
	u.timeline.ScrollToTop()
	if u.nearBottom() {
		t.Skip("the pane is not tall enough to scroll in this headless window")
	}

	height, held := u.timeline.ContentHeight(), len(u.timeline.Lines())

	// turn the timestamp gutter off the way the checkbox does - through the
	// preference, because applySettings republishes the config snapshot FROM
	// the preferences and would overwrite anything set directly
	u.app.Preferences().SetBool("showtimestamps", false)

	u.applyDisplaySettings() // exactly what a control in the Preferences dialog calls

	if got := len(u.timeline.Lines()); got != held {
		t.Errorf("the pane holds %d lines after a settings repaint, want the %d it had", got, held)
	}
	if got := u.timeline.ContentHeight(); got == height {
		t.Errorf("content height is still %v with the timestamp gutter turned off: "+
			"the pane never re-read the display options", got)
	}
	if u.nearBottom() {
		t.Error("a settings change threw the reader back to the newest message")
	}
}

// TestABufferSwitchIsTheOneThingThatMoves: the other side of the same rule.
// Opening a buffer lands on the newest message, and clears the state the last
// one left behind.
func TestABufferSwitchIsTheOneThingThatMoves(t *testing.T) {
	u, s := wiredUI(t)
	var lines []Line
	for i := 0; i < 80; i++ {
		lines = append(lines, Line{Time: time.Now().Add(time.Duration(i) * time.Second),
			Nick: "alice", Text: "filler"})
	}
	put(t, u, s, "#eco", lines...)
	put(t, u, s, "#other", Line{Time: time.Now(), Nick: "bob", Text: "over here"})
	u.renderBuffer()
	u.setLoadingOlder(true)
	u.timeline.ScrollToTop()
	u.toggleReader() // and leave it open across the switch

	u.vm.Select(viewmodel.Ref{Session: s.ID(), Name: "#other"})
	u.renderBuffer()

	// an open Reader is a copy of the buffer; the switch has to rebuild it, or
	// the last buffer's conversation sits there under the new one's name
	if !strings.Contains(u.reader.Text, "over here") || strings.Contains(u.reader.Text, "filler") {
		t.Errorf("Reader mode still shows the buffer we left: %q", first80(u.reader.Text))
	}
	u.closeReader()

	if !u.nearBottom() {
		t.Error("a buffer switch did not land on the newest message")
	}
	for _, r := range u.timeline.Rows() {
		if r.Kind == widgets.RowLoading {
			t.Error("the new buffer inherited the last one's \"Loading older…\" row")
		}
	}
}

// --- Reader mode -------------------------------------------------------------

// TestTheReaderKeepsRunUpAroundWhatWasOnScreen. §4.2: the window is what you
// were looking at PLUS context, so scrolling up inside the reader reaches the
// messages that led to it. Without the run-up it opens on a couple of lines
// with nothing above them.
func TestTheReaderKeepsRunUpAroundWhatWasOnScreen(t *testing.T) {
	for _, c := range []struct {
		name             string
		n, first, last   int
		wantLo, wantHi   int
		wantNoteMentions bool
	}{
		{"a window in the middle gets context both sides", 1000, 500, 510, 380, 630, true},
		{"clamped at the start of the buffer", 1000, 0, 10, 0, 130, true},
		{"clamped at the end", 1000, 990, 1000, 870, 1000, true},
		{"a short buffer is shown whole", 40, 10, 20, 0, 40, false},
		{"nothing on screen falls back to the newest", 1000, 0, 0, 879, 1000, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			lo, hi := readerWindow(c.n, c.first, c.last)
			if lo != c.wantLo || hi != c.wantHi {
				t.Errorf("readerWindow(%d,%d,%d) = %d,%d want %d,%d",
					c.n, c.first, c.last, lo, hi, c.wantLo, c.wantHi)
			}
			if hi-lo > readerMax {
				t.Errorf("window of %d exceeds the %d cap", hi-lo, readerMax)
			}
			if got := hi-lo != c.n; got != c.wantNoteMentions {
				t.Errorf("truncated = %v, want %v (this is what puts the note on screen)", got, c.wantNoteMentions)
			}
		})
	}
}

// TestTheReaderSaysWhenItIsNotShowingEverything. A bounded view that does not
// say it is bounded is a view that quietly loses the top of the conversation.
func TestTheReaderSaysWhenItIsNotShowingEverything(t *testing.T) {
	u, s := wiredUI(t)
	var lines []Line
	for i := 0; i < readerMax*3; i++ {
		lines = append(lines, Line{Time: time.Now().Add(time.Duration(i) * time.Second),
			Nick: "alice", Text: "filler"})
	}
	put(t, u, s, "#eco", lines...)
	u.renderBuffer()
	u.toggleReader()

	if !u.readerNote.Visible() {
		t.Fatalf("no note over a %d-line buffer capped at %d", len(lines), readerMax)
	}
	note := u.readerNote.Text
	if !strings.Contains(note, "of 1500 messages") {
		t.Errorf("the note reads %q; it must say how much of the buffer this is", note)
	}
	shown := strings.Count(u.reader.Text, "\n")
	if shown > readerMax {
		t.Errorf("the reader laid out %d lines, cap is %d", shown, readerMax)
	}
	if !strings.Contains(note, "Esc") && !strings.Contains(note, "F6") {
		t.Errorf("the note does not say how to close it: %q", note)
	}

	u.closeReader()
	u.toggleReader()
	u.timeline.SetBuffer(nil, time.Time{})
	u.refreshReader()
	if u.readerNote.Visible() {
		t.Errorf("the note is still up over an empty buffer: %q", u.readerNote.Text)
	}
}

// TestClosingTheReaderDropsTheConversation. It is a copy of the buffer in a
// text box; leaving it there after it is closed keeps a second copy of every
// message alive for the rest of the session - including ones later withdrawn.
func TestClosingTheReaderDropsTheConversation(t *testing.T) {
	u, s := wiredUI(t)
	put(t, u, s, "#eco", Line{Time: time.Now(), Nick: "alice", Text: "something private"})
	u.renderBuffer()
	u.toggleReader()
	if !strings.Contains(u.reader.Text, "something private") {
		t.Fatal("the reader never had it")
	}
	u.closeReader()
	if u.reader.Text != "" {
		t.Errorf("a closed reader still holds the conversation: %q", u.reader.Text)
	}
}

// TestTheNewDividerIsSeededFromTheMarkerAtTheSwitch. §4.2: the divider is
// placed once, from the read marker as it stood when the buffer was opened -
// never the live marker, which would walk the landmark down the screen as you
// read past it. The seed is asserted rather than the divider because a buffer
// with no marker gets no divider AND a wrongly-seeded one often gets no
// divider either, so counting rules cannot tell the two apart.
func TestTheNewDividerIsSeededFromTheMarkerAtTheSwitch(t *testing.T) {
	u, s := wiredUI(t)
	put(t, u, s, "#eco", Line{Time: time.Now().Add(-time.Hour), Nick: "alice", Text: "hello"})
	u.renderBuffer()

	if got, want := u.timeline.Since(), s.ReadMarker("#eco"); !got.Equal(want) {
		t.Errorf("the NEW divider was seeded with %v, the buffer's read marker is %v", got, want)
	}
	// and it holds still: reading on does not re-seed it
	seed := u.timeline.Since()
	u.appendLine(Line{Time: time.Now(), Nick: "bob", Text: "and more"})
	u.BatchDone()
	u.repaintBuffer()
	if got := u.timeline.Since(); !got.Equal(seed) {
		t.Errorf("the seed moved to %v while the reader was reading; it was %v", got, seed)
	}
}

// first80 keeps a failure message readable when the text under test is a whole
// buffer.
func first80(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
