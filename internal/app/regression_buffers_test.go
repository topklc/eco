package app

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// M5 regression contract, buffer and state bookkeeping half.
//
// Every test here encodes a bug v1 actually shipped (or a quirk the audit
// signed off on and the rewrite is required to fix). The fixture is the one
// in regression_test.go — newTestSession, testUI, messageLines — and nothing
// in this file changes it; the helpers below are prefixed buf* so this file
// adds nothing ambiguous to the shared namespace.
//
// Rules: assert on Session/Buffer state and callback invocations, never on
// rendered strings; synchronise with srv.Sync(), never with a sleep.

// TestRegression_BufferIdentityIsCaseInsensitive pins buffer identity.
//
// The bug: buffers keyed by the wire casing. IRC channel names are
// case-insensitive (CASEMAPPING=ascii here), so a server that answers
// "JOIN #Chan" with its canonical "#chan", a NAMES reply in a third casing,
// or a user typing "/msg #CHAN" would each open a *second* buffer holding
// half the conversation. It is worse than a cosmetic duplicate: ui.go's
// AddLine keys unread counts and mention flags by the buffer string the
// session hands it, so a split buffer splits the badge too.
func TestRegression_BufferIdentityIsCaseInsensitive(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	s.JoinUser("#Chan")
	srv.WaitFor("JOIN #Chan")
	// the server answers in its own casing, as a real one may, and the rest
	// of the burst deliberately disagrees with itself
	srv.Send(":%s!%s@test JOIN #chan", testNick, testNick)
	srv.Send(":irc.test 332 %s #CHAN :mixed case topic", testNick)
	srv.Send(":irc.test 353 %s = #CHAN :@alice %s", testNick, testNick)
	srv.Send(":irc.test 366 %s #Chan :End of /NAMES list.", testNick)
	srv.Send("@msgid=c1 :alice!alice@test PRIVMSG #chan :lower case target")
	srv.Send("@msgid=c2 :alice!alice@test PRIVMSG #CHAN :upper case target")
	srv.Sync()

	b := s.Buffer("#chan")
	if b == nil {
		t.Fatal("no #chan buffer after joining #Chan")
	}
	for _, name := range []string{"#Chan", "#CHAN", "#chAn"} {
		if got := s.Buffer(name); got != b {
			t.Errorf("Buffer(%q) = %p, want the one #chan buffer %p", name, got, b)
		}
	}
	if n := bufCount(s, "#chan"); n != 1 {
		t.Fatalf("%d buffers named #chan, want 1 (a casing difference forked the buffer)", n)
	}
	bufNoGhosts(t, s)

	// both messages, both member entries and the topic landed in that one buffer
	if got := bufTexts(messageLines(b)); len(got) != 2 {
		t.Errorf("#chan holds %d messages (%q), want both castings' messages", len(got), got)
	}
	if b.Topic != "mixed case topic" {
		t.Errorf("topic = %q, want %q (the 332 for #CHAN missed the buffer)", b.Topic, "mixed case topic")
	}
	wantMembers := map[string]string{"alice": "@", testNick: ""}
	if !maps.Equal(b.Members, wantMembers) {
		t.Errorf("members = %v, want %v (the 353 for #CHAN missed the buffer)", b.Members, wantMembers)
	}

	// the UI is only ever told the canonical name: it keys unread, drafts and
	// the sidebar row by this string, so a second casing would be a second row
	if n := len(ui.Lines("#Chan")) + len(ui.Lines("#CHAN")); n != 0 {
		t.Errorf("%d lines reached the UI under a non-canonical buffer name", n)
	}
	if n := len(ui.Lines("#chan")); n < 3 {
		t.Errorf("the UI saw %d lines for #chan, want the join notice and both messages", n)
	}
	// CHARACTERIZATION, expected to change at M6/M7. TODO(v2/M6):
	// MembersChange/TopicChange still pass the wire casing rather than the
	// buffer's canonical name (session.go passes e.Params through), so a UI
	// keying by that string can miss a refresh. Asserted case-insensitively on
	// purpose — the requirement today is only "the UI is told about this
	// channel"; the rewrite's folded BufferID makes the exact name assertable,
	// and then these two checks tighten to equality.
	if !bufContainsFold(ui.MemberChanges(), "#chan") {
		t.Errorf("MembersChange never fired for #chan: %q", ui.MemberChanges())
	}
	if ui.Topic("#CHAN") != "mixed case topic" && ui.Topic("#chan") != "mixed case topic" {
		t.Error("TopicChange never carried the topic for #chan in any casing")
	}
}

// TestRegression_ScrollbackCapTrimsOldestLines pins the trim.
//
// The bug: the scrollback trim is an off-by-one magnet — it must drop the
// OLDEST lines, keep the newest in order, and never slice past the end at the
// cap boundary (cap of 1 is the degenerate case that panics a wrong bound).
// Audit bug #5 ("plaintext view froze at the scrollback cap") is the UI half
// of the same boundary: it validated by length only, and once trimming began
// the length stopped changing, so updates stopped forever. That is why this
// test also pins that trimming is buffer-side only — the UI is handed every
// line and does its own bookkeeping.
func TestRegression_ScrollbackCapTrimsOldestLines(t *testing.T) {
	for _, capN := range []int64{0, 1, 2, 5} {
		t.Run(fmt.Sprintf("cap=%d", capN), func(t *testing.T) {
			s, srv, ui := newTestSessionUI(t)
			joinChannel(t, s, srv, "#eco", "@alice")
			s.ClearBuffer("#eco") // start from an empty, predictable buffer
			bufPinScrollbackCap(t, capN)
			b := s.Buffer("#eco")

			// exactly cap lines: the boundary where nothing is trimmed yet
			fill := int(capN)
			if capN == 0 { // 0 means unlimited
				fill = 4
			}
			for i := 1; i <= fill; i++ {
				srv.Send("@msgid=sb%d :alice!alice@test PRIVMSG #eco :line %d", i, i)
			}
			srv.Sync()
			if got := len(b.Lines); got != fill {
				t.Fatalf("at the cap boundary the buffer holds %d lines, want %d", got, fill)
			}
			if b.Lines[0].Text != "line 1" {
				t.Errorf("oldest line = %q, want %q: the buffer trimmed before reaching the cap", b.Lines[0].Text, "line 1")
			}

			// three more push past it
			const extra = 3
			for i := fill + 1; i <= fill+extra; i++ {
				srv.Send("@msgid=sb%d :alice!alice@test PRIVMSG #eco :line %d", i, i)
			}
			srv.Sync()

			want := fill
			if capN == 0 {
				want = fill + extra
			}
			got := b.Lines
			if len(got) != want {
				t.Fatalf("buffer holds %d lines after %d messages with cap %d, want %d", len(got), fill+extra, capN, want)
			}
			// the survivors are the NEWEST ones, still in arrival order
			first := fill + extra - want + 1
			for i, l := range got {
				if w := fmt.Sprintf("line %d", first+i); l.Text != w {
					t.Errorf("trimmed buffer line %d = %q, want %q", i, l.Text, w)
				}
			}
			// trimming never reaches the UI: it was handed every message
			if n := len(bufChat(ui.Lines("#eco"))); n != fill+extra {
				t.Errorf("the UI saw %d messages, want all %d (trimming must be buffer-side only)", n, fill+extra)
			}
		})
	}
}

// TestRegression_CloseBufferRemovesTheOrderEntry pins buffer removal.
//
// The bug: CloseBuffer has to delete from BOTH the map and the display order
// slice. Deleting only from the map leaves an order key whose lookup yields
// nil, and Buffers() then hands the UI a nil *Buffer — a sidebar ghost row at
// best, a nil dereference on the next repaint at worst. Closing must also be
// case-insensitive (same identity rule as the buffer map), and reopening the
// channel must produce a genuinely fresh buffer rather than resurrect the old
// topic and member list.
func TestRegression_CloseBufferRemovesTheOrderEntry(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	old := joinChannel(t, s, srv, "#eco", "@alice")
	srv.Send("@msgid=d1 :alice!alice@test PRIVMSG #eco :hello")
	srv.Sync()
	if old.Topic == "" || len(old.Members) == 0 || len(old.Lines) == 0 {
		t.Fatalf("fixture buffer is not populated: %+v", old)
	}

	before := len(s.Buffers())
	calls := ui.BufferListCalls()
	s.CloseBuffer("#ECO") // the user closes the tab, in another casing

	if got := s.Buffer("#eco"); got != nil {
		t.Errorf("Buffer(%q) still resolves after CloseBuffer(%q)", "#eco", "#ECO")
	}
	bufNoGhosts(t, s)
	if got := len(s.Buffers()); got != before-1 {
		t.Errorf("Buffers() holds %d entries after closing one of %d: the order entry outlived the buffer", got, before)
	}
	if bufCount(s, "#eco") != 0 {
		t.Error("a buffer named #eco survives in the display order")
	}
	if ui.BufferListCalls() <= calls {
		t.Error("BufferList never fired: the sidebar keeps showing the closed buffer")
	}

	// reopening builds a fresh buffer, not the old one
	srv.Send("@msgid=d2 :alice!alice@test PRIVMSG #eco :back?")
	srv.Sync()
	fresh := s.Buffer("#eco")
	switch {
	case fresh == nil:
		t.Fatal("no #eco buffer after a message reopened it")
	case fresh == old:
		t.Error("reopening resurrected the closed buffer instead of creating one")
	case fresh.Topic != "":
		t.Errorf("reopened buffer inherited topic %q", fresh.Topic)
	case len(fresh.Members) != 0:
		t.Errorf("reopened buffer inherited %d members", len(fresh.Members))
	case len(fresh.Lines) != 1:
		t.Errorf("reopened buffer holds %d lines, want only the message that reopened it", len(fresh.Lines))
	}
	if got := len(s.Buffers()); got != before {
		t.Errorf("Buffers() holds %d entries after the reopen, want %d", got, before)
	}
	if n := bufCount(s, "#eco"); n != 1 {
		t.Errorf("%d buffers named #eco after close+reopen, want 1 (a duplicate order entry)", n)
	}
	bufNoGhosts(t, s)
}

// TestRegression_ReopenedBufferInheritsDedupeState pins CURRENT behaviour that
// the rewrite is expected to change — deliberately, not by accident.
//
// CloseBuffer drops the buffer but not the two per-channel maps keyed
// alongside it: seenIDs (displayed msgids) and historyRequested (the
// fetch-once guard). So a closed-then-rejoined channel asks for no scrollback
// at all, and any message the server replays with a msgid the old buffer
// already showed is silently dropped into a buffer that is visibly empty.
//
// CHARACTERIZATION, expected to change at M6/M7: this test asserts a v1 bug,
// not a requirement. TODO(v2/M6): the rewrite's buffer lifecycle must clear
// both maps — the archive owns dedupe and CHATHISTORY AFTER <last msgid> owns
// the refill (blueprint §3 MUST 5/7). When that lands, INVERT these
// expectations; do not delete them.
func TestRegression_ReopenedBufferInheritsDedupeState(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco", "@alice") // the join fetched scrollback once
	srv.Send("@msgid=e1 :alice!alice@test PRIVMSG #eco :first")
	srv.Sync()

	s.CloseBuffer("#eco")

	s.JoinUser("#eco")
	srv.WaitFor("JOIN #eco")
	srv.Send(":%s!%s@test JOIN #eco", testNick, testNick)
	srv.Sync()
	// Sync is the barrier: everything the JOIN made the client send is queued
	// before the PONG, so the absence of a CHATHISTORY here is deterministic.
	for _, line := range srv.Pending() {
		if testsrv.Match("CHATHISTORY", line) {
			t.Errorf("the rejoin refetched history (%q) — good, and this pinned expectation is now stale", line)
		}
	}

	srv.Send("@msgid=e1 :alice!alice@test PRIVMSG #eco :first")
	srv.Sync()
	if n := len(messageLines(s.Buffer("#eco"))); n != 0 {
		t.Errorf("the reopened buffer shows %d messages; v1 drops the replay against the stale msgid set", n)
	}
}

// TestRegression_ServerBufferSurvivesChannelOperations pins the one buffer
// that must always exist.
//
// The bug: the server buffer is created once in NewSession and is the only
// buffer the UI can fall back to (it is the initial u.current). Every channel
// teardown path — PART echo, self-KICK, a PART the server refuses with
// 442/403 — closes a buffer by name, and any of them closing or shadowing
// "server" would leave the UI pointing at nothing. It must also stay first in
// the display order and keep accepting status lines after every close.
func TestRegression_ServerBufferSurvivesChannelOperations(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)

	sb := s.Buffer(serverBuffer)
	if sb == nil {
		t.Fatal("no server buffer on a fresh session")
	}
	if first := s.Buffers()[0]; first.Name != serverBuffer {
		t.Errorf("Buffers()[0] = %q, want the server buffer first", first.Name)
	}

	joinChannel(t, s, srv, "#alpha", "@alice")
	joinChannel(t, s, srv, "#beta", "@alice")

	// 1. an ordinary part, closed by the server's echo
	s.Part("#alpha")
	srv.WaitFor("PART #alpha")
	srv.Send(":%s!%s@test PART #alpha", testNick, testNick)
	srv.Sync()

	// 2. we are kicked out of #beta: the buffer closes and the notice lands
	//    in the server log
	kickMark := len(sb.Lines)
	srv.Send(":op!op@test KICK #beta %s :spring cleaning", testNick)
	srv.Sync()
	if len(sb.Lines) <= kickMark {
		t.Error("the self-KICK notice never reached the server buffer")
	}

	// 3. a PART the server refuses (442) on a buffer we were never really in
	srv.Send("@msgid=g1 :alice!alice@test PRIVMSG #ghost :still here?")
	srv.Sync()
	if s.Buffer("#ghost") == nil {
		t.Fatal("no #ghost buffer after a message created it")
	}
	refuseMark := len(sb.Lines)
	s.Part("#ghost")
	srv.WaitFor("PART #ghost")
	srv.Send(":irc.test 442 %s #ghost :You're not on that channel", testNick)
	srv.Sync()
	if len(sb.Lines) <= refuseMark {
		t.Error("the refused PART was never reported in the server buffer")
	}

	for _, ch := range []string{"#alpha", "#beta", "#ghost"} {
		if s.Buffer(ch) != nil {
			t.Errorf("%s buffer survived its close path", ch)
		}
	}
	bufNoGhosts(t, s)
	if got := s.Buffer(serverBuffer); got != sb {
		t.Fatalf("the server buffer was replaced (%p, was %p) by channel operations", got, sb)
	}
	if n := bufCount(s, serverBuffer); n != 1 {
		t.Errorf("%d server buffers, want exactly 1", n)
	}
	if got := s.Buffers(); len(got) != 1 || got[0].Name != serverBuffer {
		t.Errorf("Buffers() = %v, want only the server buffer left", bufNames(got))
	}
	if ui.BufferListCalls() == 0 {
		t.Error("BufferList never fired across three buffer closes")
	}
}

// TestRegression_LineFlagsDriveUnreadAndHighlight pins the flag contract.
//
// The bugs (audit severe #2 and #11, plus the notification-storm class):
// the UI derives EVERYTHING from Line's flags — ui.go's AddLine counts a
// buffer unread only for !Status && !Playback && !Self lines, badges a
// mention only on Highlight, and raises a desktop notification on Highlight.
// So every routing or classification slip in the session shows up as a
// missing badge or a notification storm, never as a compile error. Pin the
// tuple for each kind of line: status noise, plain chat, mentions, our own
// messages, private messages, service notices, and history playback.
func TestRegression_LineFlagsDriveUnreadAndHighlight(t *testing.T) {
	s, srv, ui := newTestSessionUI(t)
	b := joinChannel(t, s, srv, "#eco", "@alice")

	// 1. status noise: never unread, never a notification, no nick, no msgid
	join := bufLast(t, s, "#eco")
	if got, want := bufFlagsOf(join), (bufFlags{Status: true}); got != want {
		t.Errorf("join notice flags = %+v, want %+v", got, want)
	}
	if join.Nick != "" || join.MsgID != "" {
		t.Errorf("status line carries nick %q / msgid %q, want neither", join.Nick, join.MsgID)
	}
	if bufCountsUnread(join) {
		t.Error("a join notice would count as unread")
	}

	// 2. plain chat from someone else: unread, no highlight
	srv.Send("@msgid=f1 :alice!alice@test PRIVMSG #eco :the build is green")
	srv.Sync()
	chat := bufLast(t, s, "#eco")
	if got, want := bufFlagsOf(chat), (bufFlags{}); got != want {
		t.Errorf("plain chat flags = %+v, want %+v", got, want)
	}
	if chat.Nick != "alice" || chat.MsgID != "f1" {
		t.Errorf("plain chat = nick %q msgid %q, want alice/f1", chat.Nick, chat.MsgID)
	}
	if !bufCountsUnread(chat) || bufNotifies(chat) {
		t.Error("plain chat must count as unread and must not notify")
	}

	// 3. a mention highlights; a mere substring does not (containsWord)
	srv.Send("@msgid=f2 :alice!alice@test PRIVMSG #eco :%s: ship it", testNick)
	srv.Send("@msgid=f3 :alice!alice@test PRIVMSG #eco :the economy is fine")
	srv.Sync()
	msgs := messageLines(b)
	if len(msgs) < 3 {
		t.Fatalf("#eco holds %d messages, want at least 3", len(msgs))
	}
	mention, substring := msgs[len(msgs)-2], msgs[len(msgs)-1]
	if got, want := bufFlagsOf(mention), (bufFlags{Highlight: true}); got != want {
		t.Errorf("mention flags = %+v, want %+v", got, want)
	}
	if !bufNotifies(mention) || !bufCountsUnread(mention) {
		t.Error("a mention must both notify and count as unread")
	}
	if got, want := bufFlagsOf(substring), (bufFlags{}); got != want {
		t.Errorf("%q flags = %+v, want %+v (nick matched inside a word)", substring.Text, got, want)
	}

	// 4. our own message: Self, never unread, never a highlight
	s.SendMessage("#eco", "on it")
	srv.WaitFor("PRIVMSG #eco :on it")
	srv.Sync()
	own := bufLast(t, s, "#eco")
	if got, want := bufFlagsOf(own), (bufFlags{Self: true}); got != want {
		t.Errorf("own message flags = %+v, want %+v", got, want)
	}
	if own.Nick != testNick {
		t.Errorf("own message nick = %q, want %q", own.Nick, testNick)
	}
	if bufCountsUnread(own) || bufNotifies(own) {
		t.Error("our own message would badge or notify us")
	}
	// CHARACTERIZATION, expected to change at M6/M7. TODO(v2/M6): own lines
	// carry no MsgID because v1 renders them locally and matches the echo by
	// text (pendingEcho). That is exactly why our own messages cannot be
	// redacted or read-marked; labeled-response resolves the real msgid in
	// place (blueprint §3 MUST 3) and this expectation flips to "own lines
	// carry the server's msgid".
	if own.MsgID != "" {
		t.Errorf("own message msgid = %q; v1 has none until the rewrite lands", own.MsgID)
	}
	// the server's echo must not add a second line or flip the flags
	count := len(b.Lines)
	srv.Send("@msgid=f4 :%s!%s@test PRIVMSG #eco :on it", testNick, testNick)
	srv.Sync()
	if got := len(b.Lines); got != count {
		t.Errorf("the echo added %d line(s): our own message is displayed twice", got-count)
	}
	if got, want := bufFlagsOf(bufLast(t, s, "#eco")), (bufFlags{Self: true}); got != want {
		t.Errorf("after the echo the last line = %+v, want the locally displayed %+v", got, want)
	}

	// 5. a private message highlights even without naming us
	srv.Send("@msgid=f5 :bob!bob@test PRIVMSG %s :got a sec?", testNick)
	srv.Sync()
	pm := bufLast(t, s, "bob")
	if got, want := bufFlagsOf(pm), (bufFlags{Highlight: true}); got != want {
		t.Errorf("PM flags = %+v, want %+v", got, want)
	}
	if pm.Nick != "bob" {
		t.Errorf("PM nick = %q, want bob (the buffer is the other party)", pm.Nick)
	}

	// 6. our own PM sent from another client lands in the peer's buffer,
	//    marked Self — never in a buffer named after ourselves
	srv.Send("@msgid=f6 :%s!%s@test PRIVMSG bob :sent from my phone", testNick, testNick)
	srv.Sync()
	if got, want := bufFlagsOf(bufLast(t, s, "bob")), (bufFlags{Self: true}); got != want {
		t.Errorf("multiclient self-PM flags = %+v, want %+v", got, want)
	}
	if s.Buffer(testNick) != nil {
		t.Error("a buffer was opened under our own nick")
	}

	// 7. a service notice must not notify: PMs highlight, notices do not, or
	//    every routine NickServ line raises a desktop popup
	srv.Send(":NickServ!services@test NOTICE %s :Your account is not registered.", testNick)
	srv.Sync()
	svc := bufLast(t, s, "NickServ")
	if got, want := bufFlagsOf(svc), (bufFlags{Notice: true}); got != want {
		t.Errorf("service notice flags = %+v, want %+v", got, want)
	}
	if bufNotifies(svc) {
		t.Error("a service notice would raise a notification")
	}

	// 8. a server notice belongs in the server log, not in a buffer named
	//    after the server host
	srv.Send(":irc.test NOTICE %s :*** Checking ident", testNick)
	srv.Sync()
	if s.Buffer("irc.test") != nil {
		t.Error("a buffer was opened for the server's own hostname")
	}
	if got, want := bufFlagsOf(bufLast(t, s, serverBuffer)), (bufFlags{Notice: true}); got != want {
		t.Errorf("server notice flags = %+v, want %+v", got, want)
	}

	// 9. history playback: Playback set, never highlighted, never unread —
	//    even when the replayed message mentions us (audit severe #2's
	//    other half: a reconnect must not re-notify the whole scrollback)
	srv.Send(":irc.test BATCH +hp chathistory #eco")
	srv.Send("@batch=hp;msgid=f7 :alice!alice@test PRIVMSG #eco :%s: this is old", testNick)
	srv.Send(":irc.test BATCH -hp")
	srv.Sync()
	old := bufLast(t, s, "#eco")
	if got, want := bufFlagsOf(old), (bufFlags{Playback: true}); got != want {
		t.Errorf("playback flags = %+v, want %+v", got, want)
	}
	if old.MsgID != "f7" {
		t.Errorf("playback msgid = %q, want f7", old.MsgID)
	}
	if bufCountsUnread(old) || bufNotifies(old) {
		t.Error("replayed history would badge or notify")
	}

	// 10. HighlightNote is the merged-multiline path ONLY. Every ordinary
	//     mention above must reach the UI through Line.Highlight, or the user
	//     gets two notifications for one message (audit #11's failure mode).
	if n := len(ui.Highlights()); n != 0 {
		t.Errorf("HighlightNote fired %d time(s) for non-multiline traffic: %+v", n, ui.Highlights())
	}

	// 11. Buffer.Unread is not session state: the session communicates unread
	//     only through AddLine + the flags pinned above, and the UI keeps the
	//     counts (ui.unread, keyed by session+buffer). Nothing here may write
	//     it, or two sources of truth start disagreeing.
	for _, buf := range s.Buffers() {
		if buf.Unread != 0 {
			t.Errorf("buffer %q has Unread=%d: the session started counting unread itself", buf.Name, buf.Unread)
		}
	}
}

// TestRegression_WhoisRepliesRouteToTheAskingBuffer pins whois routing.
//
// WHOIS replies are numerics with no target of their own, so v1 remembers the
// buffer that asked in a single field (commands.go sets s.whoisTarget under
// s.mu). Nothing clears it and nothing correlates a reply to its request. The
// audit reviewed this and kept it as an accepted quirk ("whoisTarget persists
// after a WHOIS completes"), so the assertions below pin the quirk exactly as
// it behaves today, including the misroute.
//
// CHARACTERIZATION, expected to change at M6/M7. TODO(v2/M6): labeled-response
// correlation replaces the single slot (blueprint §3 MUST 3, "deletes the
// single whoisTarget slot"). When that lands, the two TODO-marked expectations
// below must be INVERTED, not deleted: a reply must follow its own request's
// label.
func TestRegression_WhoisRepliesRouteToTheAskingBuffer(t *testing.T) {
	s, srv := newTestSession(t)
	ch := joinChannel(t, s, srv, "#eco", "@alice")
	s.ClearBuffer("#eco")
	sb := s.Buffer(serverBuffer)
	serverMark := len(sb.Lines)

	// with no /whois in flight the replies land in the server log
	srv.Send(":irc.test 311 %s bob bob test.host * :Bob", testNick)
	srv.Send(":irc.test 318 %s bob :End of /WHOIS list.", testNick)
	srv.Sync()
	if n := len(ch.Lines); n != 0 {
		t.Errorf("%d unasked-for whois replies landed in #eco, want 0", n)
	}
	if n := len(sb.Lines) - serverMark; n != 2 {
		t.Errorf("the server log gained %d lines from the unasked whois, want 2", n)
	}
	serverMark = len(sb.Lines)

	// a /whois issued from #eco routes the whole reply set there
	bufWhoisFrom(s, "#eco")
	srv.Send(":irc.test 311 %s bob bob test.host * :Bob", testNick)
	srv.Send(":irc.test 312 %s bob irc.test :Test Network", testNick)
	srv.Send(":irc.test 317 %s bob 42 1700000000 :seconds idle, signon time", testNick)
	srv.Send(":irc.test 318 %s bob :End of /WHOIS list.", testNick)
	srv.Sync()
	if n := len(ch.Lines); n != 4 {
		t.Fatalf("#eco holds %d whois lines, want all 4 of the reply set", n)
	}
	for i, l := range ch.Lines {
		if got, want := bufFlagsOf(l), (bufFlags{Status: true}); got != want {
			t.Errorf("whois line %d flags = %+v, want %+v (whois must never badge a buffer)", i, got, want)
		}
	}
	if n := len(sb.Lines) - serverMark; n != 0 {
		t.Errorf("%d whois replies leaked into the server log", n)
	}

	// TODO(v2/M6) pinned quirk 1: the slot is never cleared, so a later
	// unsolicited reply still lands in the buffer that asked last
	srv.Send(":irc.test 311 %s carol carol test.host * :Carol", testNick)
	srv.Sync()
	if n := len(ch.Lines); n != 5 {
		t.Errorf("#eco holds %d lines, want 5: v1 keeps routing to the stale whois target", n)
	}

	// TODO(v2/M6) pinned quirk 2: last writer wins, so a /whois issued from
	// another buffer steals the replies still in flight for the first one.
	// This is the race the rewrite removes; here it is made deterministic.
	bufWhoisFrom(s, serverBuffer)
	serverMark = len(sb.Lines)
	srv.Send(":irc.test 318 %s carol :End of /WHOIS list.", testNick)
	srv.Sync()
	if n := len(ch.Lines); n != 5 {
		t.Errorf("#eco holds %d lines, want 5: the second /whois should have stolen this reply", n)
	}
	if n := len(sb.Lines) - serverMark; n != 1 {
		t.Errorf("the server log gained %d lines, want the stolen reply", n)
	}
}

// --- helpers (this file only) ---

// bufPinScrollbackCap publishes a settings snapshot carrying a scrollback cap
// for the rest of the test. The shared fixture pins an unlimited cap, so this
// must run after newTestSession; the cleanups unwind in the reverse order.
func bufPinScrollbackCap(t *testing.T, lines int64) {
	t.Helper()
	prev := config.Current()
	next := *prev
	next.ScrollbackCap = lines
	config.Publish(&next)
	t.Cleanup(func() { config.Publish(prev) })
}

// bufWhoisFrom records the buffer a /whois was issued from, exactly the way
// commands.go does it (s.whoisTarget under s.mu). The Session exposes no
// method for it, so the test writes the field directly.
func bufWhoisFrom(s *Session, buffer string) {
	s.mu.Lock()
	s.whoisTarget = buffer
	s.mu.Unlock()
}

// bufFlags is the flag tuple the UI derives all of its behaviour from.
type bufFlags struct {
	Self, Status, Playback, Highlight, Action, Notice bool
}

func bufFlagsOf(l Line) bufFlags {
	return bufFlags{Self: l.Self, Status: l.Status, Playback: l.Playback,
		Highlight: l.Highlight, Action: l.Action, Notice: l.Notice}
}

// bufCountsUnread mirrors ui.go's unread rule, so the assertions can state
// what a flag tuple MEANS rather than only what it is.
func bufCountsUnread(l Line) bool { return !l.Status && !l.Playback && !l.Self }

// bufNotifies mirrors ui.go's notification rule with the NotifyAll preference
// off (the default): a line notifies exactly when it is a highlight.
func bufNotifies(l Line) bool { return l.Highlight }

// bufLast returns a buffer's newest line. Call it only after srv.Sync().
func bufLast(t *testing.T, s *Session, name string) Line {
	t.Helper()
	b := s.Buffer(name)
	if b == nil {
		t.Fatalf("no %s buffer", name)
	}
	if len(b.Lines) == 0 {
		t.Fatalf("%s buffer holds no lines", name)
	}
	return b.Lines[len(b.Lines)-1]
}

// bufCount counts the buffers sharing a name, case-insensitively — the
// duplicate a casing bug produces.
func bufCount(s *Session, name string) int {
	n := 0
	for _, b := range s.Buffers() {
		if b != nil && strings.EqualFold(b.Name, name) {
			n++
		}
	}
	return n
}

// bufNoGhosts fails if Buffers() holds a nil, which is what a stale display
// order entry produces: the map delete without the slice delete.
func bufNoGhosts(t *testing.T, s *Session) {
	t.Helper()
	for i, b := range s.Buffers() {
		if b == nil {
			t.Fatalf("Buffers()[%d] is nil: the display order kept a key whose buffer was deleted", i)
		}
	}
}

func bufNames(bs []*Buffer) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if b == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, b.Name)
	}
	return out
}

// bufChat drops status lines from a recorded line slice (messageLines does the
// same for a live buffer).
func bufChat(lines []Line) []Line {
	var out []Line
	for _, l := range lines {
		if !l.Status {
			out = append(out, l)
		}
	}
	return out
}

func bufTexts(lines []Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Text)
	}
	return out
}

func bufContainsFold(hay []string, needle string) bool {
	for _, s := range hay {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

// --- the 10k-line synthetic corpus ---

// bufSyntheticLines is the size of the synthetic buffer transcript the
// blueprint calls for at M5 ("a 10k-line synthetic buffer transcript (perf
// corpus for M11)"). It is deliberately above v1's 8192-entry seenIDs bound,
// so the corpus doubles as the test for what happens at that boundary.
const bufSyntheticLines = 10000

// TestRegression_SyntheticTenThousandLineBuffer is the M5 perf corpus and the
// scale test for the two per-buffer maps.
//
// It exists for three reasons:
//
//  1. it is the transcript M11's Timeline relayout budget will be measured
//     against (blueprint §8 risk 1), checked in now so the corpus predates the
//     rewrite that has to keep passing it;
//  2. it pins that the buffer stays correct at scale — every line present,
//     still in arrival order, no line reclassified — which an O(n^2) or a
//     wrongly-bounded trim would break long before a human noticed;
//  3. it pins the seenIDs bound, which the QA review singled out as
//     "v1's seenIDs wholesale-reset-at-8192 quirk deserves a test either way"
//     (devdocs/v2/research.md).
//
// It runs in well under a second under -race, which is why the whole corpus is
// driven rather than sampled.
func TestRegression_SyntheticTenThousandLineBuffer(t *testing.T) {
	s, srv := newTestSession(t)
	joinChannel(t, s, srv, "#eco", "@alice")
	s.ClearBuffer("#eco") // start from an empty, exactly countable buffer
	b := s.Buffer("#eco")

	for i := 1; i <= bufSyntheticLines; i++ {
		srv.Send("@msgid=syn%d :alice!alice@test PRIVMSG #eco :synthetic line %d", i, i)
	}
	srv.Sync()

	if got := len(b.Lines); got != bufSyntheticLines {
		t.Fatalf("buffer holds %d lines, want all %d", got, bufSyntheticLines)
	}
	// order and identity survive at scale: spot-check the ends and the middle,
	// then verify no line was silently reclassified along the way
	for _, i := range []int{1, 2, bufSyntheticLines / 2, bufSyntheticLines - 1, bufSyntheticLines} {
		l := b.Lines[i-1]
		if want := fmt.Sprintf("synthetic line %d", i); l.Text != want {
			t.Fatalf("line %d = %q, want %q: the buffer lost its order", i, l.Text, want)
		}
		if want := fmt.Sprintf("syn%d", i); l.MsgID != want {
			t.Errorf("line %d msgid = %q, want %q", i, l.MsgID, want)
		}
	}
	for i, l := range b.Lines {
		if got, want := bufFlagsOf(l), (bufFlags{}); got != want {
			t.Fatalf("line %d flags = %+v, want %+v (plain chat, %d lines in)", i+1, got, want, i+1)
		}
	}

	// dedupe still works for a msgid inside the surviving window: replaying the
	// newest line adds nothing
	srv.Send("@msgid=syn%d :alice!alice@test PRIVMSG #eco :synthetic line %d",
		bufSyntheticLines, bufSyntheticLines)
	srv.Sync()
	if got := len(b.Lines); got != bufSyntheticLines {
		t.Errorf("replaying the newest msgid added %d line(s); dedupe failed inside its own window",
			got-bufSyntheticLines)
	}

	// CHARACTERIZATION, expected to change at M6/M7: v1 does not evict from
	// seenIDs, it EMPTIES the map once it passes 8192 entries (session.go's
	// "if len(s.seenIDs) > 8192 { s.seenIDs = map[string]bool{} }"). So every
	// msgid seen before the last wipe is forgotten wholesale, and a server
	// replaying old history duplicates lines that are still on screen — the
	// exact failure the seen-set exists to prevent, reappearing only on buffers
	// busy enough to cross the bound. Blueprint §3 MUST 5/7 moves dedupe into
	// the archive, keyed by msgid with no arbitrary ceiling; when that lands
	// this expectation INVERTS to "no duplicate", and the assertion below is
	// the one to flip.
	srv.Send("@msgid=syn1 :alice!alice@test PRIVMSG #eco :synthetic line 1")
	srv.Sync()
	if got := len(b.Lines); got != bufSyntheticLines+1 {
		t.Errorf("replaying the OLDEST msgid changed the buffer by %d line(s), want +1: "+
			"v1 forgets everything older than its last seenIDs wipe (and this pinned quirk is now stale)",
			got-bufSyntheticLines)
	}
}
