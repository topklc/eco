package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

func TestTopicRepliesCarryTheSetterWithoutClearingTheTopic(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":irc.test 332 eco #eco :the topic")
	tp := h.waitFor("topic").(model.Topic)
	if tp.Channel != "#eco" || tp.Topic != "the topic" || tp.Cleared {
		t.Fatalf("RPL_TOPIC = %+v", tp)
	}

	// RPL_TOPICWHOTIME carries the setter but no topic text. Emitting it as a
	// bare Topic would read as "the topic was cleared", so the text is carried
	// over from the reply that just arrived.
	h.srv.Send(":irc.test 333 eco #eco alice!a@host 1704164645")
	tp = h.waitFor("topic").(model.Topic)
	if tp.Topic != "the topic" {
		t.Errorf("topic text lost on RPL_TOPICWHOTIME: %+v", tp)
	}
	if tp.SetBy != "alice!a@host" {
		t.Errorf("setter = %q", tp.SetBy)
	}
	if want := time.Unix(1704164645, 0); !tp.SetAt.Equal(want) {
		t.Errorf("set at = %v, want %v", tp.SetAt, want)
	}
	if tp.Cleared {
		t.Error("a topic with a setter is not a cleared topic")
	}

	h.srv.Send(":irc.test 331 eco #quiet :No topic is set")
	tp = h.waitFor("topic").(model.Topic)
	if !tp.Cleared || tp.Channel != "#quiet" {
		t.Errorf("RPL_NOTOPIC = %+v", tp)
	}

	// A 333 with no topic behind it is not a Topic event at all.
	h.srv.Send(":irc.test 333 eco #unknown bob!b@host 1704164645")
	num := h.waitFor("numeric").(model.Numeric)
	if num.Code != "333" {
		t.Errorf("orphan RPL_TOPICWHOTIME = %+v", num)
	}
}

func TestNamesKeepsPrefixesForTheStoreToSplit(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":irc.test 353 eco = #eco :@+alice bob ~carol")
	nm := h.waitFor("names").(model.Names)
	if nm.Channel != "#eco" || nm.End {
		t.Fatalf("353 = %+v", nm)
	}
	// Raw entries, prefixes intact: splitting them needs PREFIX, which the
	// store owns.
	if strings.Join(nm.Names, " ") != "@+alice bob ~carol" {
		t.Errorf("names = %v", nm.Names)
	}
	h.srv.Send(":irc.test 366 eco #eco :End of /NAMES list.")
	nm = h.waitFor("names").(model.Names)
	if !nm.End || nm.Channel != "#eco" {
		t.Errorf("366 = %+v", nm)
	}
}

func TestModeDeltasArriveUnparsed(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":alice!a@host MODE #eco +ok-m bob")
	md := h.waitFor("mode").(model.Mode)
	if md.Target != "#eco" || md.ModeString != "+ok-m" {
		t.Fatalf("mode = %+v", md)
	}
	if len(md.Args) != 1 || md.Args[0] != "bob" {
		t.Errorf("mode args = %v", md.Args)
	}
	if md.Full {
		t.Error("a MODE command is a delta, not the full state")
	}
	if md.Sender.Nick != "alice" {
		t.Errorf("mode sender = %+v", md.Sender)
	}

	// 324 is the authoritative full state, which replaces rather than amends.
	h.srv.Send(":irc.test 324 eco #eco +ntk secret")
	md = h.waitFor("mode").(model.Mode)
	if !md.Full || md.ModeString != "+ntk" || len(md.Args) != 1 || md.Args[0] != "secret" {
		t.Fatalf("324 = %+v", md)
	}
}

func TestJoinRefusalsAreTypedAndAmbiguousOnesAreNot(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":irc.test 475 eco #locked :Cannot join channel (+k)")
	jr := h.waitFor("joinrefused").(model.JoinRefused)
	if jr.Channel != "#locked" || jr.Code != "475" || !jr.NeedKey {
		t.Fatalf("475 = %+v", jr)
	}
	if jr.Reason == "" {
		t.Error("the server's own reason was dropped")
	}

	h.srv.Send(":irc.test 473 eco #invite :Cannot join channel (+i)")
	jr = h.waitFor("joinrefused").(model.JoinRefused)
	if jr.NeedKey || jr.Code != "473" {
		t.Fatalf("473 = %+v", jr)
	}

	// 403 also answers PART, TOPIC and MODE. v1 guessed from a pending map and
	// closed live buffers when it guessed wrong; the label makes it exact.
	part, err := h.c.Part("#gone", "")
	if err != nil {
		t.Fatalf("part: %v", err)
	}
	h.srv.WaitFor("*PART #gone")
	h.srv.Send("@label=%s :irc.test 403 eco #gone :No such channel", part.Label())
	num := h.waitFor("numeric").(model.Numeric)
	if num.Code != "403" || num.Label != part.Label() {
		t.Fatalf("403 answering a PART = %+v, want a plain numeric carrying the label", num)
	}

	join, err := h.c.Join("#nope", "")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	h.srv.WaitFor("*JOIN #nope")
	h.srv.Send("@label=%s :irc.test 403 eco #nope :No such channel", join.Label())
	jr = h.waitFor("joinrefused").(model.JoinRefused)
	if jr.Channel != "#nope" || jr.Code != "403" {
		t.Fatalf("403 answering a JOIN = %+v, want a join refusal", jr)
	}
}

func TestListStreamsItemsAndEnds(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	if _, err := h.c.List(); err != nil {
		t.Fatalf("list: %v", err)
	}
	h.srv.WaitFor("*LIST")
	h.srv.Send(":irc.test 321 eco Channel :Users  Name")
	h.srv.Send(":irc.test 322 eco #eco 42 :a channel")
	h.srv.Send(":irc.test 322 eco #other 7x :another")
	h.srv.Send(":irc.test 323 eco :End of /LIST")

	li := h.waitFor("listitem").(model.ListItem)
	if li.Channel != "#eco" || li.Users != 42 || li.Topic != "a channel" {
		t.Fatalf("list item = %+v", li)
	}
	li = h.waitFor("listitem").(model.ListItem)
	if li.Users != 7 {
		t.Errorf("a user count with trailing junk = %d, want 7", li.Users)
	}
	h.waitFor("listend")
}

func TestMembershipAndPresenceEvents(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":alice!a@host JOIN #eco alicia :Alice Liddell")
	jn := h.waitFor("join").(model.Join)
	if jn.Channel != "#eco" || jn.Account != "alicia" || jn.RealName != "Alice Liddell" {
		t.Fatalf("extended-join = %+v", jn)
	}
	// "*" means "not logged in" and must not become an account name.
	h.srv.Send(":bob!b@host JOIN #eco * :Bob")
	jn = h.waitFor("join").(model.Join)
	if jn.Account != "" {
		t.Errorf("account = %q, want empty for *", jn.Account)
	}

	h.srv.Send(":alice!a@host PART #eco :bye")
	if pt := h.waitFor("part").(model.Part); pt.Reason != "bye" {
		t.Errorf("part = %+v", pt)
	}
	h.srv.Send(":bob!b@host QUIT :Ping timeout")
	if q := h.waitFor("quit").(model.Quit); q.Reason != "Ping timeout" || q.Sender.Nick != "bob" {
		t.Errorf("quit = %+v", q)
	}
	h.srv.Send(":op!o@host KICK #eco carol :rude")
	if k := h.waitFor("kick").(model.Kick); k.Target != "carol" || k.Reason != "rude" {
		t.Errorf("kick = %+v", k)
	}
	h.srv.Send(":carol!c@host NICK caroline")
	if n := h.waitFor("nick").(model.Nick); n.NewNick != "caroline" || n.Sender.Nick != "carol" {
		t.Errorf("nick = %+v", n)
	}
	h.srv.Send(":alice!a@host AWAY :lunch")
	if a := h.waitFor("awaychange").(model.AwayChange); !a.Away || a.Message != "lunch" {
		t.Errorf("away = %+v", a)
	}
	h.srv.Send(":alice!a@host AWAY")
	if a := h.waitFor("awaychange").(model.AwayChange); a.Away {
		t.Errorf("back from away = %+v", a)
	}
	h.srv.Send(":alice!a@host ACCOUNT alicia")
	if a := h.waitFor("accountchange").(model.AccountChange); a.Account != "alicia" {
		t.Errorf("account = %+v", a)
	}
	h.srv.Send(":alice!a@host ACCOUNT *")
	if a := h.waitFor("accountchange").(model.AccountChange); a.Account != "" {
		t.Errorf("logout = %+v", a)
	}
	h.srv.Send(":alice!a@host CHGHOST newuser new.host")
	if c := h.waitFor("chghost").(model.ChgHost); c.NewUser != "newuser" || c.NewHost != "new.host" {
		t.Errorf("chghost = %+v", c)
	}
	h.srv.Send(":alice!a@host SETNAME :Alice B.")
	if s := h.waitFor("setname").(model.SetName); s.RealName != "Alice B." {
		t.Errorf("setname = %+v", s)
	}
	h.srv.Send(":op!o@host INVITE eco #secret")
	if i := h.waitFor("invite").(model.Invite); i.Target != "eco" || i.Channel != "#secret" {
		t.Errorf("invite = %+v", i)
	}
	h.srv.Send(":irc.test RENAME #eco #eco-new :spring cleaning")
	if r := h.waitFor("channelrenamed").(model.ChannelRenamed); r.From != "#eco" || r.To != "#eco-new" {
		t.Errorf("rename = %+v", r)
	}
}

func TestRedactAndMarkReadDecodeAndSend(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	h.srv.Send(":alice!a@host REDACT #eco m1 :spam")
	rd := h.waitFor("redact").(model.Redact)
	if rd.Target != "#eco" || rd.TargetMsgID != "m1" || rd.Reason != "spam" {
		t.Fatalf("redact = %+v", rd)
	}

	h.srv.Send(":irc.test MARKREAD #eco timestamp=2024-01-02T03:04:05.000Z")
	mr := h.waitFor("markread").(model.MarkRead)
	if mr.Unset || !mr.Timestamp.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("markread = %+v", mr)
	}
	h.srv.Send(":irc.test MARKREAD #eco *")
	if mr = h.waitFor("markread").(model.MarkRead); !mr.Unset {
		t.Fatalf("markread * = %+v, want Unset", mr)
	}

	if err := h.c.Redact("#eco", "m2", "mistake"); err != nil {
		t.Fatalf("send redact: %v", err)
	}
	if got := untagged(h.srv.WaitFor("*REDACT*")); got != "REDACT #eco m2 :mistake" {
		t.Errorf("redact line = %q", got)
	}
	at := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := h.c.MarkRead("#eco", at); err != nil {
		t.Fatalf("send markread: %v", err)
	}
	if got := untagged(h.srv.WaitFor("MARKREAD*")); got != "MARKREAD #eco timestamp=2024-05-06T07:08:09.000Z" {
		t.Errorf("markread line = %q", got)
	}
	if err := h.c.MarkRead("#eco", time.Time{}); err != nil {
		t.Fatalf("clear markread: %v", err)
	}
	if got := untagged(h.srv.WaitFor("MARKREAD #eco *")); got != "MARKREAD #eco *" {
		t.Errorf("markread clear line = %q", got)
	}

	if err := h.c.SendTyping("#eco", model.TypingActive); err != nil {
		t.Fatalf("typing: %v", err)
	}
	line := h.srv.WaitFor("*TAGMSG #eco")
	if tagValue(line, "+typing") != "active" {
		t.Errorf("typing line = %q", line)
	}
}

func TestUnmodelledLinesStillReachTheConsumer(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	// standard-replies are not numerics but answer labels, so they travel as
	// Numeric with the command as the code — nothing is silently dropped.
	h.srv.Send(":irc.test FAIL JOIN CHANNEL_LIMIT #eco :Too many channels")
	num := h.waitFor("numeric").(model.Numeric)
	if num.Code != "FAIL" || num.Text != "Too many channels" {
		t.Fatalf("FAIL = %+v", num)
	}

	h.srv.Send(":irc.test 381 eco :You are now an IRC operator")
	num = h.waitFor("numeric").(model.Numeric)
	if num.Code != "381" || num.Text != "You are now an IRC operator" {
		t.Fatalf("381 = %+v", num)
	}
	// Params drop the leading nick every numeric repeats back at us.
	if len(num.Params) != 1 {
		t.Errorf("params = %v, want the nick stripped", num.Params)
	}

	h.srv.Send("ERROR :Closing link: throttled")
	se := h.waitFor("servererror").(model.ServerError)
	if se.Message != "Closing link: throttled" {
		t.Fatalf("ERROR = %+v", se)
	}
}

func TestCTCPQueriesAreAnsweredOnTheWire(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	h.srv.Send(":alice!a@host PRIVMSG eco :\x01VERSION\x01")
	pm := h.waitFor("privmsg").(model.PrivMsg)
	if pm.CTCP != "VERSION" {
		t.Fatalf("ctcp = %+v", pm)
	}
	reply := h.srv.WaitFor("NOTICE alice*")
	if !strings.Contains(reply, "\x01VERSION eco-test\x01") {
		t.Errorf("version reply = %q", reply)
	}

	h.srv.Send(":alice!a@host PRIVMSG eco :\x01PING 12345\x01")
	h.waitFor("privmsg")
	if reply := h.srv.WaitFor("NOTICE alice*"); !strings.Contains(reply, "\x01PING 12345\x01") {
		t.Errorf("ping reply = %q, want the payload echoed untouched", reply)
	}

	// Replayed history must not fire a reply per replayed query.
	h.srv.Send("BATCH +p chathistory eco")
	h.srv.Send("@batch=p;msgid=old :alice!a@host PRIVMSG eco :\x01VERSION\x01")
	h.srv.Send("BATCH -p")
	h.waitFor("historychunk")
	h.srv.Send(":alice!a@host PRIVMSG eco :done")
	h.srv.Sync()
	for _, line := range h.srv.Pending() {
		if strings.HasPrefix(line, "NOTICE") {
			t.Errorf("replayed CTCP was answered: %q", line)
		}
	}
}

func TestEmitRawMirrorsInboundLines(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		cfg.EmitRaw = true
	})
	h.register()

	h.srv.Send(":alice!a@host PRIVMSG #eco :hello")
	raw := h.waitFor("raw").(model.Raw)
	if raw.Outgoing {
		t.Error("only inbound lines are mirrored")
	}
	if raw.Line == "" {
		t.Error("raw line is empty")
	}
	// The mirror is a copy of the wire, not a re-serialisation of the parse.
	for {
		if strings.Contains(raw.Line, "PRIVMSG #eco :hello") {
			break
		}
		raw = h.waitFor("raw").(model.Raw)
	}
}
