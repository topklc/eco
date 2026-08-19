package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// M12's second slice where it meets the app: the Switcher (Ctrl+K), the
// in-buffer search bar (Ctrl+F), the Finder (Ctrl+Shift+F), and the Escape
// stack that puts all three away. What each of them DECIDES is tested where it
// is decided — the ranking in internal/ui/viewmodel, the stepping and the
// highlight in internal/ui/widgets, the query grammar in internal/archive.
// This is the wiring between them, plus the two things only this layer can say:
// that the browser dialog is gone rather than hidden, and that a Finder hit
// opens the message where it was actually said.

// --- the Escape stack ------------------------------------------------------------

// The priority rule, as a table. Every combination has exactly one answer, and
// the answer is the topmost thing on screen — which is the whole of §4.3's
// stack and the whole of what v1's chain of ifs got wrong.
func TestEscapeStackOrder(t *testing.T) {
	all := escState{Dialogs: 2, Completing: true, Overlay: true, Reader: true, ScrolledUp: true}
	for _, c := range []struct {
		name  string
		state escState
		want  escLayer
	}{
		{"nothing open", escState{}, escNone},
		{"everything open", all, escDialog},
		{"a dialog over a palette", escState{Dialogs: 1, Overlay: true}, escDialog},
		{"a tab cycle under a dialog", escState{Dialogs: 1, Completing: true}, escDialog},
		{"a tab cycle over a palette", escState{Completing: true, Overlay: true}, escCompletion},
		{"a palette over the reader", escState{Overlay: true, Reader: true}, escOverlay},
		{"the search bar and the scrollback", escState{Overlay: true, ScrolledUp: true}, escOverlay},
		{"the reader and the scrollback", escState{Reader: true, ScrolledUp: true}, escReader},
		{"only the scrollback", escState{ScrolledUp: true}, escBottom},
		{"reading the newest message", escState{}, escNone},
	} {
		if got := escapeTarget(c.state); got != c.want {
			t.Errorf("%s: escapeTarget = %d, want %d", c.name, got, c.want)
		}
	}
}

// Escape closes the TOPMOST dialog, not the first one ever opened. v1 kept one
// slot here, so a form opened from a dialog left the dialog unclosable and lost
// itself out of the slot on the way back.
func TestEscapeClosesTheTopmostDialog(t *testing.T) {
	u := newTestUI(t)
	first, second := &fakeDialog{}, &fakeDialog{}
	u.trackDialog(first)
	u.trackDialog(second)

	if !u.handleEscape() {
		t.Fatal("Escape did nothing with two dialogs open")
	}
	if second.hidden != 1 || first.hidden != 0 {
		t.Fatalf("Escape hid first=%d second=%d, want the topmost only", first.hidden, second.hidden)
	}
	second.close() // the dialog tells us it went, as a real one does

	if !u.handleEscape() {
		t.Fatal("Escape did nothing with one dialog left")
	}
	if first.hidden != 1 {
		t.Errorf("the remaining dialog was hidden %d times, want 1", first.hidden)
	}
	first.close()
	if len(u.dialogs) != 0 {
		t.Errorf("%d dialogs still tracked after both closed", len(u.dialogs))
	}
}

// A dialog closed by its own button — out of order — comes off the stack too.
func TestADialogClosedOutOfOrderLeavesTheStack(t *testing.T) {
	u := newTestUI(t)
	bottom, top := &fakeDialog{}, &fakeDialog{}
	u.trackDialog(bottom)
	u.trackDialog(top)
	bottom.close()
	if len(u.dialogs) != 1 || u.dialogs[0] != dialogHandle(top) {
		t.Fatalf("stack = %v, want only the top dialog left", u.dialogs)
	}
	u.handleEscape()
	if top.hidden != 1 {
		t.Errorf("Escape hid the top dialog %d times, want 1", top.hidden)
	}
}

// fakeDialog is a dialog as far as the stack is concerned.
type fakeDialog struct {
	hidden   int
	onClosed func()
}

func (d *fakeDialog) Hide()                { d.hidden++ }
func (d *fakeDialog) SetOnClosed(f func()) { d.onClosed = f }
func (d *fakeDialog) close()               { d.onClosed() }

// --- the Switcher -----------------------------------------------------------------

// Ctrl+K opens the one surface, and the browser dialog is not behind it: the
// toolbar button that used to open browser.go opens the Switcher, and the
// Switcher is where a channel from the LIST cache is offered.
func TestCtrlKOpensTheSwitcher(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	if u.overlayOpen() {
		t.Fatal("an overlay is open on a fresh window")
	}
	if !u.handleShortcut(bound(t, u, "switcher")) {
		t.Fatal("Ctrl+K is not bound to anything")
	}
	if !u.switcherBox.Visible() || !u.overlayOpen() {
		t.Fatal("Ctrl+K did not open the Switcher")
	}
	if u.win.Canvas().Focused() != fyne.Focusable(u.switcher.Entry()) {
		t.Error("the Switcher opened without the keyboard")
	}

	// the same button the browser used to hang off
	u.closeOverlays()
	u.channelsBtn.OnTapped()
	if !u.switcherBox.Visible() {
		t.Error("the toolbar's find button does not open the Switcher")
	}
}

// The Switcher lists buffers and channels from the LIST cache in one ranked
// list — the absorption, asserted where it is visible.
func TestTheSwitcherOffersBuffersAndUnjoinedChannels(t *testing.T) {
	u := newTestUI(t)
	s := attachArchived(t, u, "libera")
	s.getBuffer("#eco")
	u.refreshBuffers()
	seedListCache(t, s, "#eco-dev", "#ecosystem")

	u.showSwitcher()
	u.switcher.SetQuery("eco")

	kinds := map[viewmodel.SwitchKind]int{}
	for _, c := range u.switchRows {
		kinds[c.Kind]++
	}
	if kinds[viewmodel.SwitchBuffer] == 0 {
		t.Errorf("no open buffer in the list: %v", switchNames(u.switchRows))
	}
	if kinds[viewmodel.SwitchChannel] == 0 {
		t.Errorf("no LIST channel in the list: %v", switchNames(u.switchRows))
	}
	if got := u.switchRows[0].Name; got != "#eco" {
		t.Errorf("top match is %q, want the buffer named exactly what was typed", got)
	}
	// and the rows say something: the panel is not a column of blank lines
	rows := u.switcherRows()
	if len(rows) != len(u.switchRows) || rows[0].Title != "#eco" {
		t.Errorf("rows = %v, want one per candidate with the name as its title", rows)
	}
}

// Enter on a buffer row shows it, and closes the overlay on the way.
func TestChoosingABufferShowsIt(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	u.showSwitcher()
	u.switcher.SetQuery("#eco")
	i := indexOfKind(u.switchRows, viewmodel.SwitchBuffer)
	if i < 0 {
		t.Fatalf("no buffer row to choose: %v", switchNames(u.switchRows))
	}
	u.switcherChose(i)

	if got := u.current(); !strings.EqualFold(got, "#eco") {
		t.Errorf("after Enter the buffer on screen is %q, want #eco", got)
	}
	if u.overlayOpen() {
		t.Error("the Switcher is still open after choosing")
	}
	if u.win.Canvas().Focused() != fyne.Focusable(u.input) {
		t.Error("the keyboard did not go back to the composer")
	}
}

// Enter takes the row that is HIGHLIGHTED, which is not always the first one:
// the arrows move the selection and the panel hands its index back. Choosing
// only ever the top match is a test that cannot tell the difference, and it is
// the difference between the Switcher and a lucky guess.
func TestChoosingTheSecondRowOpensTheSecondRow(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("#eco-dev")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	u.showSwitcher()
	u.switcher.SetQuery("eco")
	if len(u.switchRows) < 2 {
		t.Fatalf("only %v matched; this test needs two", switchNames(u.switchRows))
	}
	// down one, then Enter, exactly as a finger would
	u.switcher.Step(1)
	want := u.switchRows[u.switcher.Selected()]
	if want.Kind != viewmodel.SwitchBuffer {
		t.Fatalf("the second row is a %v, not a buffer; the fixture moved", want.Kind)
	}
	u.switcher.Choose()

	if got := u.current(); !strings.EqualFold(got, want.Name) {
		t.Errorf("Enter on row 2 (%q) opened %q", want.Name, got)
	}
}

// --- Ctrl+F ------------------------------------------------------------------------

// The bar opens over the conversation, colours what matches, steps through the
// matches, and Escape puts it away and gives the keyboard back.
func TestCtrlFFindsStepsAndCloses(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)
	u.timeline.SetBuffer([]Line{
		{Time: time.Now(), Nick: "alice", Text: "the deploy went out"},
		{Time: time.Now(), Nick: "bob", Text: "nice"},
		{Time: time.Now(), Nick: "carol", Text: "second DEPLOY queued"},
	}, time.Time{})

	if !u.handleShortcut(bound(t, u, "find")) {
		t.Fatal("Ctrl+F is not bound to anything")
	}
	if !u.searchBar.Visible() || !u.overlayOpen() {
		t.Fatal("Ctrl+F did not open the search bar")
	}
	u.searchBar.SetQuery("deploy")
	if got := u.timeline.SearchMatches(); got != 2 {
		t.Fatalf("%d matches, want 2 — the search is case-insensitive", got)
	}
	if got := u.searchBar.Count(); got != "2 matches" {
		t.Errorf("the bar says %q before stepping, want the count", got)
	}
	u.findStep(1)
	if got := u.timeline.SearchLine(); got != 0 {
		t.Errorf("the first step landed on line %d, want 0", got)
	}
	if got := u.searchBar.Count(); got != "1 of 2" {
		t.Errorf("the bar says %q after one step, want %q", got, "1 of 2")
	}

	if !u.handleEscape() {
		t.Fatal("Escape did nothing with the search bar open")
	}
	if u.searchBar.Visible() {
		t.Error("Escape did not close the search bar")
	}
	if got := u.timeline.SearchMatches(); got != 0 {
		t.Errorf("%d matches still highlighted after closing", got)
	}
	if u.win.Canvas().Focused() != fyne.Focusable(u.input) {
		t.Error("closing the bar did not restore composer focus")
	}
	if u.lastFind != "deploy" {
		t.Errorf("the bar forgot what was looked for: %q", u.lastFind)
	}
}

// A buffer switch ends the question the bar was asking.
func TestSwitchingBuffersClosesTheSearchBar(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)
	u.showSearch()
	if !u.searchBar.Visible() {
		t.Fatal("the bar did not open")
	}
	u.selectRow(s, "#eco")
	if u.searchBar.Visible() {
		t.Error("the search bar survived a buffer switch")
	}
}

// --- the Finder ----------------------------------------------------------------------

// The Finder's query goes to archive.Search as typed, so its grammar is the
// archive's: from: narrows by sender, in: by buffer, and the words narrow by
// words. Nothing in the UI parses a query.
func TestFinderQueryReachesArchiveSearch(t *testing.T) {
	u, s := finderUI(t)

	for _, c := range []struct {
		query string
		want  []string
	}{
		{"deploy", []string{"the deploy went out", "second deploy queued", "deploy notes"}},
		{"from:alice deploy", []string{"the deploy went out"}},
		{"in:#eco deploy", []string{"the deploy went out", "second deploy queued"}},
		{"from:bob in:#ops", []string{"deploy notes"}},
		{"kubernetes", nil},
	} {
		u.finder.SetQuery(c.query)
		got := hitBodies(u.finderRows)
		if !sameSet(got, c.want) {
			t.Errorf("Finder %q found %v, want %v", c.query, got, c.want)
		}
	}
	if s.arc == nil {
		t.Fatal("the fixture has no archive; the table above proved nothing")
	}
}

// The rows say what §4.3 asks them to: the buffer, the nick, a snippet and a
// timestamp — and the snippet is drawable text, not the archive's control-byte
// match markers.
func TestFinderRowsSayWhereAndWhen(t *testing.T) {
	u, _ := finderUI(t)
	u.finder.SetQuery("deploy")
	rows := u.finder.Rows()
	if len(rows) == 0 {
		t.Fatal("no rows for a word that is in the archive three times")
	}
	for i, r := range rows {
		if r.Title == "" || r.Meta == "" || r.Detail == "" {
			t.Fatalf("row %d is missing a part: %+v", i, r)
		}
		if strings.ContainsAny(r.Detail, archive.MarkStart+archive.MarkEnd) {
			t.Errorf("row %d still carries the archive's match markers: %q", i, r.Detail)
		}
		if !strings.Contains(r.Detail, ":") {
			t.Errorf("row %d does not name who said it: %q", i, r.Detail)
		}
	}
	if got := rows[0].Title; got != "#eco" && got != "#ops" {
		t.Errorf("row title is %q, want the buffer the message was said in", got)
	}
}

// Enter opens the message where it was said: the buffer is shown, the
// conversation around the hit is pulled out of the archive, and the line itself
// is in the window.
func TestChoosingAFinderHitOpensTheMessageInItsBuffer(t *testing.T) {
	u, s := finderUI(t)
	u.finder.SetQuery("from:bob in:#ops")
	if len(u.finderRows) != 1 {
		t.Fatalf("expected one hit, got %v", hitBodies(u.finderRows))
	}
	want := u.finderRows[0]

	u.finderChose(0)

	if got := u.current(); !strings.EqualFold(got, "#ops") {
		t.Fatalf("the buffer on screen is %q, want #ops", got)
	}
	if u.overlayOpen() {
		t.Error("the Finder is still open after choosing")
	}
	if i := s.LineIndex("#ops", model.MessageID(want.rowID)); i < 0 {
		t.Fatal("the chosen message is not in the buffer's window")
	}
	found := false
	for _, l := range u.timeline.Lines() {
		if strings.Contains(l.Text, "deploy notes") {
			found = true
		}
	}
	if !found {
		t.Error("the pane does not show the message that was chosen")
	}
}

// A snippet carries two kinds of control byte — the archive's match markers and
// whatever formatting the message itself had — and the row has to lose both
// without either eating the other. It could not, because the markers WERE
// formatting bytes: taking \x02/\x03 out of "\x0304RED ALERT\x03 …" removed the
// colour introducer and left "04RED ALERT" on the row. The markers are outside
// the formatting set now, and this is the table that says so.
func TestTheSnippetLosesBothKindsOfControlByte(t *testing.T) {
	for _, c := range []struct{ name, snippet, want string }{
		{"a plain match", "the " + mark("deploy") + " went out", "the deploy went out"},
		{"a digit straight after the end marker", mark("deploy") + "12 files", "deploy12 files"},
		{"the message's own colour survives being stripped",
			"\x0304RED ALERT\x03 " + mark("deploy"), "RED ALERT deploy"},
		{"and its own bold", "\x02" + mark("deploy") + "\x02 now", "deploy now"},
	} {
		if got := snippetText(archive.Hit{Snippet: c.snippet}); got != c.want {
			t.Errorf("%s: snippetText(%q) = %q, want %q", c.name, c.snippet, got, c.want)
		}
	}
	// and with no snippet at all it falls back to the body
	if got := snippetText(archive.Hit{Message: archive.Message{Body: "plain words"}}); got != "plain words" {
		t.Errorf("snippetText fell back to %q", got)
	}
	// the constraint itself, so the next person to pick a marker byte is told
	for _, formatting := range []string{"\x02", "\x03", "\x04", "\x0F", "\x11", "\x16", "\x1D", "\x1E", "\x1F"} {
		if archive.MarkStart == formatting || archive.MarkEnd == formatting {
			t.Errorf("a snippet marker is %q, which is an IRC formatting byte", formatting)
		}
	}
}

func mark(s string) string { return archive.MarkStart + s + archive.MarkEnd }

// Ctrl+F and the two palettes are ONE rung of the Escape stack, so only one of
// them is ever the thing Escape means: opening the bar takes an open palette
// off the screen.
func TestCtrlFPutsAnOpenPaletteAway(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	u.showSwitcher()
	if !u.switcherBox.Visible() {
		t.Fatal("the Switcher did not open")
	}
	u.showSearch()
	if u.switcherBox.Visible() {
		t.Error("Ctrl+F left the Switcher on screen; two overlays, one Escape")
	}
	if !u.searchBar.Visible() {
		t.Error("Ctrl+F did not open the bar")
	}
}

// The same claim for the Finder, where the rows are messages: Enter opens the
// HIGHLIGHTED hit. A Finder that always opened the best-ranked one would be a
// list you cannot walk.
func TestChoosingTheSecondFinderRowOpensThatMessage(t *testing.T) {
	u, s := finderUI(t)
	u.finder.SetQuery("deploy")
	if len(u.finderRows) < 2 {
		t.Fatalf("only %v matched; this test needs two", hitBodies(u.finderRows))
	}
	u.finder.Step(1)
	want := u.finderRows[u.finder.Selected()]
	u.finder.Choose()

	if got := u.current(); !strings.EqualFold(got, want.buffer) {
		t.Errorf("Enter on row 2 opened %q, want %q", got, want.buffer)
	}
	if i := s.LineIndex(want.buffer, model.MessageID(want.rowID)); i < 0 {
		t.Error("the message the second row named is not in the window that opened")
	}
}

// A hit older than the window a cold open restores is the case the jump exists
// for: the buffer has to be re-opened AROUND that message, and the reader has to
// be left looking at it rather than at the newest page of the same buffer.
func TestAFinderHitOlderThanTheWindowOpensWhereItSits(t *testing.T) {
	u := newTestUI(t)
	u.win.Resize(fyne.NewSize(900, 600))
	s := attachArchived(t, u, "libera")
	seedLongBuffer(t, s, "#eco", hydrateLimit+60)
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)
	u.timeline.Resize(fyne.NewSize(600, 300))

	u.showFinder()
	u.finder.SetQuery("needle")
	if len(u.finderRows) != 1 {
		t.Fatalf("the Finder found %v, want the one needle", hitBodies(u.finderRows))
	}
	want := u.finderRows[0]
	u.finderChose(0)

	if got := u.current(); !strings.EqualFold(got, "#eco") {
		t.Fatalf("the buffer on screen is %q, want #eco", got)
	}
	i := s.LineIndex("#eco", model.MessageID(want.rowID))
	if i < 0 {
		t.Fatal("the chosen message is not in the window; the jump did not pull it in")
	}
	if !strings.Contains(u.timeline.Lines()[i].Text, "needle") {
		t.Fatalf("line %d is %q, want the needle", i, u.timeline.Lines()[i].Text)
	}
	// and the reader is standing on it: the pane scrolled somewhere, and it is
	// not the top it would have gone to if the line index had been ignored
	if got := u.timeline.ScrollOffset(); got <= 0 {
		t.Errorf("the pane is at offset %v — it opened at the top rather than at the message", got)
	}
}

// --- fixtures ---------------------------------------------------------------------

// attachArchived is attachSession with a history file behind it: enough for
// the two surfaces that read one. No socket — everything the Finder and the
// LIST cache do is reads of a file plus the store.
func attachArchived(t *testing.T, u *UI, profile string) *Session {
	t.Helper()
	cfg := Config{Server: "example.invalid", Port: "6667", Nick: "toprak",
		Archive: filepath.Join(t.TempDir(), profile+".db")}
	s := NewSession(cfg, u.dispatch)
	if s.arcErr != nil {
		t.Fatalf("opening the archive: %v", s.arcErr)
	}
	s.Name = profile
	u.dispatch.Close(s.ID())
	u.sessions = append(u.sessions, s)
	u.vm.AddSession(s.ID(), profile)
	t.Cleanup(func() { s.Quit(); _ = s.closeArchive() })
	return s
}

// finderUI is a window with one session over an archive that already holds a
// small conversation in two buffers.
func finderUI(t *testing.T) (*UI, *Session) {
	t.Helper()
	u := newTestUI(t)
	s := attachArchived(t, u, "libera")
	seedFinderArchive(t, s)

	u.refreshBuffers()
	u.selectRow(s, serverBuffer)
	u.showFinder()
	return u, s
}

// seedFinderArchive writes the conversation the Finder tests search, straight
// into the session's own file: the Recorder is the archive's write path, and
// the session's DB handle is the one the search will read back through.
func seedFinderArchive(t *testing.T, s *Session) {
	t.Helper()
	rec, err := archive.NewRecorder(context.Background(), s.arc.db, archive.RecorderOptions{})
	if err != nil {
		t.Fatalf("starting a recorder: %v", err)
	}
	base := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	msgs := []struct{ buffer, sender, body string }{
		{"#eco", "alice", "the deploy went out"},
		{"#eco", "bob", "nice"},
		{"#eco", "carol", "second deploy queued"},
		{"#ops", "bob", "deploy notes"},
		{"#ops", "alice", "unrelated chatter"},
	}
	for i, m := range msgs {
		if !rec.Record(archive.Message{
			MsgID:  "f" + itoa(i),
			Buffer: m.buffer,
			Time:   base.Add(time.Duration(i) * time.Minute),
			Sender: m.sender,
			Kind:   model.MsgPrivmsg,
			Body:   m.body,
		}) {
			t.Fatalf("the recorder refused message %d", i)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("closing the recorder: %v", err)
	}
}

// seedListCache puts channels into the profile's list_cache the way a LIST scan
// does, so the Switcher has something to offer that is not a buffer.
func seedListCache(t *testing.T, s *Session, names ...string) {
	t.Helper()
	chans := make([]model.ChannelInfo, 0, len(names))
	for i, n := range names {
		chans = append(chans, model.ChannelInfo{Name: n, Users: 100 - i, Topic: "about " + n})
	}
	if err := s.arc.db.PutChannels(s.arc.ctx, chans, time.Now()); err != nil {
		t.Fatalf("seeding the list cache: %v", err)
	}
}

func switchNames(cs []viewmodel.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func indexOfKind(cs []viewmodel.Candidate, k viewmodel.SwitchKind) int {
	for i, c := range cs {
		if c.Kind == k {
			return i
		}
	}
	return -1
}

func hitBodies(hits []finderHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.hit.Body)
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	for _, s := range want {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// Ctrl+F looks at what is loaded first and falls back to the archive for older
// matches, opening one where it sits (§4.3). The buffer here is longer than a
// cold open restores, and the word being looked for is only in a message from
// before that — so a search that stopped at the window would find nothing at
// all, and the one that works has to reach the disk and pull the conversation
// around the hit back in.
func TestCtrlFFallsBackToTheArchiveForOlderMatches(t *testing.T) {
	u := newTestUI(t)
	s := attachArchived(t, u, "libera")
	seedLongBuffer(t, s, "#eco", hydrateLimit+60)

	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, "#eco")
	// The archive's worker is the only thing to wait for: this session is
	// retired from the funnel, so nothing is delivered and there is no tick to
	// race. repaintBuffer is what puts the restored rows on screen.
	s.arc.sync()
	u.repaintBuffer()

	if got := len(u.timeline.Lines()); got == 0 || got > hydrateLimit+1 {
		t.Fatalf("the cold open restored %d lines, want a window shorter than the buffer", got)
	}

	u.showSearch()
	u.searchBar.SetQuery("needle")
	if got := u.timeline.SearchMatches(); got != 0 {
		t.Fatalf("%d loaded matches; the needle was supposed to be older than the window", got)
	}

	u.findStep(1)

	if got := u.timeline.SearchMatches(); got == 0 {
		t.Fatal("stepping past the loaded matches did not reach the archive")
	}
	line := u.timeline.SearchLine()
	if line < 0 {
		t.Fatal("the fallback found a match but did not step to it")
	}
	if got := u.timeline.Lines()[line].Text; !strings.Contains(got, "needle") {
		t.Errorf("stepped to %q, want the line the archive matched", got)
	}
	if got := u.searchBar.Count(); got != "1 of 1" {
		t.Errorf("the bar says %q after the fallback, want %q", got, "1 of 1")
	}
}

// The case above has NO loaded match, which is the easy half. The hard half is
// the one the milestone claims — "the bar searches what is loaded first and
// when stepping runs out it asks the archive" — because StepMatch wraps on
// purpose: a backward step from the oldest loaded match comes round to the
// newest rather than refusing. So "runs out" has to be asked at the boundary,
// and it was not: with a single loaded match, Shift+Enter cycled that one match
// for ever and the archive was never reached at all.
func TestCtrlFReachesTheArchiveFromTheOldestLoadedMatch(t *testing.T) {
	u := newTestUI(t)
	s := attachArchived(t, u, "libera")
	seedNeedles(t, s, "#eco", hydrateLimit+60)

	s.getBuffer("#eco")
	u.refreshBuffers()
	u.selectRow(s, "#eco")
	s.arc.sync()
	u.repaintBuffer()

	u.showSearch()
	u.searchBar.SetQuery("needle")
	if got := u.timeline.SearchMatches(); got != 1 {
		t.Fatalf("%d loaded matches, want exactly the recent one", got)
	}

	// Shift+Enter: onto the one loaded match.
	u.findStep(-1)
	first := u.timeline.SearchLine()
	if first < 0 || !strings.Contains(u.timeline.Lines()[first].Text, "recent needle") {
		t.Fatalf("the first step landed on line %d (%q), want the loaded needle",
			first, lineText(u, first))
	}

	// Shift+Enter again: the window has run out backwards, so the disk answers.
	u.findStep(-1)

	if got := u.timeline.SearchMatches(); got < 2 {
		t.Fatalf("%d matches after stepping past the oldest loaded one; "+
			"the archive was never asked", got)
	}
	at := u.timeline.SearchLine()
	if at < 0 || !strings.Contains(u.timeline.Lines()[at].Text, "old needle") {
		t.Errorf("the second step landed on %q, want the older needle the archive found",
			lineText(u, at))
	}
	if got := u.searchBar.Count(); got != "1 of 2" {
		t.Errorf("the bar says %q, want the older match counted as the first of two", got)
	}
}

func lineText(u *UI, i int) string {
	lines := u.timeline.Lines()
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i].Text
}

// seedNeedles writes a buffer longer than a cold open restores with the word
// "needle" twice: once far enough back that only the archive knows about it, and
// once in the last page, so the loaded window has a match to run out of.
func seedNeedles(t *testing.T, s *Session, buffer string, n int) {
	t.Helper()
	rec, err := archive.NewRecorder(context.Background(), s.arc.db, archive.RecorderOptions{})
	if err != nil {
		t.Fatalf("starting a recorder: %v", err)
	}
	base := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		body := "chatter " + itoa(i)
		switch i {
		case 2:
			body = "the old needle nobody has scrolled back to"
		case n - 1:
			body = "the recent needle, on the page that opens"
		}
		if !rec.Record(archive.Message{
			MsgID:  "N" + itoa(i),
			Buffer: buffer,
			Time:   base.Add(time.Duration(i) * time.Second),
			Sender: "alice",
			Kind:   model.MsgPrivmsg,
			Body:   body,
		}) {
			t.Fatalf("the recorder refused message %d", i)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("closing the recorder: %v", err)
	}
}

// seedLongBuffer writes a buffer longer than a cold open restores, with the
// word "needle" in its second message and nowhere else.
func seedLongBuffer(t *testing.T, s *Session, buffer string, n int) {
	t.Helper()
	rec, err := archive.NewRecorder(context.Background(), s.arc.db, archive.RecorderOptions{})
	if err != nil {
		t.Fatalf("starting a recorder: %v", err)
	}
	base := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		body := "chatter " + itoa(i)
		if i == 2 {
			body = "the needle nobody has scrolled back to"
		}
		if !rec.Record(archive.Message{
			MsgID:  "L" + itoa(i),
			Buffer: buffer,
			Time:   base.Add(time.Duration(i) * time.Second),
			Sender: "alice",
			Kind:   model.MsgPrivmsg,
			Body:   body,
		}) {
			t.Fatalf("the recorder refused message %d", i)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("closing the recorder: %v", err)
	}
}
