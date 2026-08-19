package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

func TestRegistrationReachesReadyWithServerProperties(t *testing.T) {
	h := newHarness(t, nil)

	// The connection state machine announces itself before anything is dialed
	// and again as soon as the server speaks.
	if cs, ok := h.next().(model.ConnState); !ok || cs.Status != model.ConnConnecting || cs.Attempt != 1 {
		t.Fatalf("first event = %#v, want ConnState{connecting, attempt 1}", cs)
	}
	h.srv.Register("eco")
	h.waitConn(model.ConnRegistering)
	ready := h.waitConn(model.ConnReady)
	if ready.Nick != "eco" {
		t.Fatalf("ready nick = %q, want eco", ready.Nick)
	}
	if got := h.c.Status(); got != model.ConnReady {
		t.Fatalf("Status() = %v, want ready", got)
	}
	if got := h.c.CurrentNick(); got != "eco" {
		t.Fatalf("CurrentNick() = %q, want eco", got)
	}

	// ISUPPORT became server properties, not a swallowed numeric: this is the
	// whole reason folding and channel detection can be correct at all.
	p := h.c.Props()
	if p.Network != "TestNet" {
		t.Errorf("NETWORK = %q, want TestNet", p.Network)
	}
	if p.CaseMapping.String() != "ascii" {
		t.Errorf("CASEMAPPING = %v, want ascii", p.CaseMapping)
	}
	if p.ChanTypes != "#" {
		t.Errorf("CHANTYPES = %q, want #", p.ChanTypes)
	}
	if p.Prefix.Modes != "qaohv" || p.Prefix.Prefixes != "~&@%+" {
		t.Errorf("PREFIX = %v, want (qaohv)~&@%%+", p.Prefix)
	}
	if p.NickLen != 32 {
		t.Errorf("NICKLEN = %d, want 32", p.NickLen)
	}
	if !p.IsChannel("#eco") || p.IsChannel("&eco") {
		t.Errorf("IsChannel disagrees with CHANTYPES=#")
	}

	for _, name := range []string{"batch", "labeled-response", "message-tags", "echo-message", "server-time"} {
		if !h.c.Enabled(name) {
			t.Errorf("cap %q not enabled after registration", name)
		}
	}
	if lim, ok := h.c.MultilineLimits(); !ok || lim.MaxLines != 24 || lim.MaxBytes != 4096 {
		t.Errorf("multiline limits = %+v ok=%v, want max-lines 24 / max-bytes 4096", lim, ok)
	}
}

func TestRegistrationEmitsISupportOnceWithReset(t *testing.T) {
	h := newHarness(t, nil)
	h.srv.Register("eco")

	var isupports []model.ISupport
	deadline := time.After(eventTimeout)
collect:
	for {
		select {
		case ev := <-h.events:
			if is, ok := ev.(model.ISupport); ok {
				isupports = append(isupports, is)
			}
			if cs, ok := ev.(model.ConnState); ok && cs.Status == model.ConnReady {
				break collect
			}
		case <-deadline:
			t.Fatal("never reached ready")
		}
	}
	if len(isupports) == 0 {
		t.Fatal("no ISupport events")
	}
	if !isupports[0].Reset {
		t.Error("first ISupport must carry Reset: a reconnect may land on another server")
	}
	for i, is := range isupports[1:] {
		if is.Reset {
			t.Errorf("ISupport %d also carries Reset", i+1)
		}
	}
	tokens := strings.Join(isupports[0].Tokens, " ")
	if strings.Contains(tokens, "are supported") || strings.Contains(tokens, " eco ") {
		t.Errorf("ISupport tokens still carry the nick or the trailing text: %q", tokens)
	}
}

func TestIncomingMessagesDecodeToTypedEvents(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	h.srv.Send("@msgid=m1;time=2024-01-02T03:04:05.000Z :alice!a@host PRIVMSG #eco :hello there")
	pm, ok := h.waitFor("privmsg").(model.PrivMsg)
	if !ok {
		t.Fatal("want a privmsg")
	}
	if pm.Target != "#eco" || pm.Text != "hello there" {
		t.Errorf("privmsg = %q to %q", pm.Text, pm.Target)
	}
	if pm.Sender.Nick != "alice" || pm.Sender.User != "a" || pm.Sender.Host != "host" {
		t.Errorf("sender = %+v", pm.Sender)
	}
	if pm.Sender.Self {
		t.Error("alice is not us")
	}
	if pm.MsgID != "m1" {
		t.Errorf("msgid = %q", pm.MsgID)
	}
	if !pm.ServerTime {
		t.Error("server-time tag was not honoured")
	}
	if want := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC); !pm.Time.Equal(want) {
		t.Errorf("time = %v, want %v", pm.Time, want)
	}
	if pm.Network != "testnet" {
		t.Errorf("network = %q, want testnet", pm.Network)
	}

	// No server-time tag: the injected clock supplies the timestamp, and the
	// event says so.
	h.srv.Send(":alice!a@host PRIVMSG #eco :untagged")
	pm = h.waitFor("privmsg").(model.PrivMsg)
	if pm.ServerTime || !pm.Time.Equal(testClock) {
		t.Errorf("untagged message time = %v serverTime=%v, want the injected clock", pm.Time, pm.ServerTime)
	}

	h.srv.Send(":alice!a@host PRIVMSG #eco :\x01ACTION waves\x01")
	pm = h.waitFor("privmsg").(model.PrivMsg)
	if !pm.Action || pm.Text != "waves" || pm.CTCP != "" {
		t.Errorf("action = %+v, want Action with the wrapper stripped", pm)
	}

	h.srv.Send(":alice!a@host PRIVMSG eco :\x01VERSION\x01")
	pm = h.waitFor("privmsg").(model.PrivMsg)
	if pm.CTCP != "VERSION" || pm.Action {
		t.Errorf("ctcp = %+v, want CTCP VERSION", pm)
	}

	h.srv.Send("@account=alicia :alice!a@host NOTICE #eco :heads up")
	nt, ok := h.waitFor("notice").(model.Notice)
	if !ok || nt.Text != "heads up" || nt.Sender.Account != "alicia" {
		t.Errorf("notice = %+v", nt)
	}

	// A query keeps its raw target: folding and buffer identity are the
	// store's job, and the event stays close to the wire.
	h.srv.Send(":bob!b@host PRIVMSG eco :psst")
	pm = h.waitFor("privmsg").(model.PrivMsg)
	if pm.Target != "eco" {
		t.Errorf("query target = %q, want the raw target eco", pm.Target)
	}

	h.srv.Send("@+typing=active :alice!a@host TAGMSG #eco")
	ty, ok := h.waitFor("typing").(model.Typing)
	if !ok || ty.State != model.TypingActive || ty.Target != "#eco" {
		t.Errorf("typing = %+v", ty)
	}

	// A TAGMSG eco models nothing for is not an event at all.
	h.srv.Send("@+draft/reply=m1 :alice!a@host TAGMSG #eco")
	h.srv.Send(":alice!a@host PRIVMSG #eco :after the bare tagmsg")
	pm = h.waitFor("privmsg").(model.PrivMsg)
	if pm.Text != "after the bare tagmsg" {
		t.Errorf("a bare TAGMSG produced an event: %+v", pm)
	}
}

func TestLabeledEchoResolvesTwoIdenticalMessages(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	// v1 matched echoes on buffer|text and could not tell these apart.
	first, err := h.c.SendMessage("#eco", "same text")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	second, err := h.c.SendMessage("#eco", "same text")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if first.Label() == "" || second.Label() == "" {
		t.Fatal("labeled-response was negotiated but no label was allocated")
	}
	if first.Label() == second.Label() {
		t.Fatalf("both sends got label %q", first.Label())
	}
	if first.Multiline {
		t.Error("a single-line message must not be batched")
	}

	l1 := h.srv.WaitFor("*PRIVMSG #eco :same text")
	l2 := h.srv.WaitFor("*PRIVMSG #eco :same text")
	if tagValue(l1, "label") != first.Label() || tagValue(l2, "label") != second.Label() {
		t.Fatalf("wire labels %q/%q, want %q/%q",
			tagValue(l1, "label"), tagValue(l2, "label"), first.Label(), second.Label())
	}

	// The server echoes them in the other order, with the bodies rewritten:
	// only the label can still resolve them.
	h.srv.Send("@label=%s;msgid=srv2 :eco!eco@test PRIVMSG #eco :same text [edited]", second.Label())
	h.srv.Send("@label=%s;msgid=srv1 :eco!eco@test PRIVMSG #eco :same text [edited]", first.Label())

	got := map[string]string{}
	for i := 0; i < 2; i++ {
		pm := h.waitFor("privmsg").(model.PrivMsg)
		if !pm.Sender.Self {
			t.Errorf("echo of our own message not marked Self: %+v", pm.Sender)
		}
		got[pm.Label] = pm.MsgID
	}
	if got[second.Label()] != "srv2" || got[first.Label()] != "srv1" {
		t.Fatalf("label→msgid = %v, want %s→srv1 and %s→srv2", got, first.Label(), second.Label())
	}
}

func TestLabeledReplyWithoutAnEchoStillCarriesTheLabel(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		// No echo-message: the server acknowledges instead of echoing.
		srv.Caps = []string{"batch", "message-tags", "labeled-response", "server-time"}
	})
	h.register()

	handle, err := h.c.SendMessage("#eco", "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	h.srv.WaitFor("*PRIVMSG #eco :hi")

	// A bare ACK is the labeled-response spec's "your command produced no
	// reply". It has no typed event of its own, so it travels as a Numeric -
	// and it must still carry the label, or the pending message never resolves.
	h.srv.Send("@label=%s :irc.test ACK", handle.Label())
	num := h.waitFor("numeric").(model.Numeric)
	if num.Code != "ACK" || num.Label != handle.Label() {
		t.Fatalf("ack = %+v, want code ACK carrying %q", num, handle.Label())
	}
}

func TestLabeledResponseBatchHandsItsLabelToItsContents(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	handle, err := h.c.Whois("alice")
	if err != nil {
		t.Fatalf("whois: %v", err)
	}
	h.srv.WaitFor("*WHOIS alice")

	// A multi-line reply is wrapped in a labeled-response batch: only the
	// BATCH carries the label, so the label has to be handed down or every
	// grouped reply arrives uncorrelated.
	h.srv.Send("@label=%s :irc.test BATCH +r labeled-response", handle.Label())
	h.srv.Send("@batch=r :irc.test 311 eco alice a host * :Alice")
	h.srv.Send("@batch=r :irc.test 318 eco alice :End of /WHOIS list.")
	h.srv.Send(":irc.test BATCH -r")

	h.waitFor("batchopen")
	num := h.next().(model.Numeric)
	if num.Code != "311" {
		t.Fatalf("first grouped reply = %+v", num)
	}
	if num.Label != handle.Label() {
		t.Fatalf("grouped reply label = %q, want %q", num.Label, handle.Label())
	}
	if num.Batch != "r" {
		t.Errorf("grouped reply batch = %q", num.Batch)
	}
}

func TestSendMultilineGoesOutAsOneLabeledBatch(t *testing.T) {
	h := newHarness(t, nil)
	h.register()
	h.join("#eco")

	handle, err := h.c.SendMessage("#eco", "one\ntwo\nthree")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !handle.Multiline || len(handle.Labels) != 1 {
		t.Fatalf("handle = %+v, want one label for one batch", handle)
	}
	if len(handle.Lines) != 3 {
		t.Fatalf("lines = %q", handle.Lines)
	}

	open := h.srv.WaitFor("*BATCH +*")
	if tagValue(open, "label") != handle.Label() {
		t.Errorf("BATCH open label = %q, want %q", tagValue(open, "label"), handle.Label())
	}
	fields := strings.Fields(untagged(open))
	ref := strings.TrimPrefix(fields[1], "+")
	if fields[0] != "BATCH" || fields[2] != "draft/multiline" || fields[3] != "#eco" {
		t.Errorf("BATCH open = %q", open)
	}
	for _, want := range []string{"one", "two", "three"} {
		line := h.srv.WaitFor("*PRIVMSG #eco :" + want)
		if tagValue(line, "batch") != ref {
			t.Errorf("part %q batch tag = %q, want %q", want, tagValue(line, "batch"), ref)
		}
		if hasTag(line, "label", "") {
			t.Errorf("part %q carries its own label; the batch owns it", want)
		}
	}
	h.srv.WaitFor("BATCH -" + ref)
}

func TestSendMultilineTruncatesToTheServerLimit(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = []string{"batch", "message-tags", "labeled-response", "server-time",
			"echo-message", "draft/multiline=max-bytes=4096,max-lines=2"}
	})
	h.register()

	handle, err := h.c.SendMessage("#eco", "a\nb\nc\nd")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !handle.Truncated || len(handle.Lines) != 2 {
		t.Fatalf("handle = %+v, want 2 lines and Truncated", handle)
	}
}

func TestSendWithoutMultilineLabelsEveryLine(t *testing.T) {
	h := newHarness(t, func(cfg *Config, srv *testsrv.Server) {
		srv.Caps = []string{"batch", "message-tags", "labeled-response", "server-time", "echo-message"}
	})
	h.register()

	handle, err := h.c.SendMessage("#eco", "one\ntwo")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if handle.Multiline {
		t.Fatal("multiline was not negotiated")
	}
	if len(handle.Labels) != 2 || handle.Labels[0] == handle.Labels[1] {
		t.Fatalf("labels = %v, want one distinct label per line", handle.Labels)
	}
	for i, want := range []string{"one", "two"} {
		line := h.srv.WaitFor("*PRIVMSG #eco :" + want)
		if tagValue(line, "label") != handle.Labels[i] {
			t.Errorf("line %q label = %q, want %q", want, tagValue(line, "label"), handle.Labels[i])
		}
	}
}

func TestSendActionAndEmptyLinePreservation(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	if _, err := h.c.SendAction("#eco", "waves\nhard"); err != nil {
		t.Fatalf("action: %v", err)
	}
	line := h.srv.WaitFor("*PRIVMSG #eco :\x01ACTION*")
	if !strings.Contains(line, "ACTION waves hard\x01") {
		t.Errorf("action line = %q, want the newline folded to a space", line)
	}

	// A blank line in the middle of a message is content; it goes out as a
	// space because an empty parameter is unsendable.
	if _, err := h.c.SendMessage("#eco", "a\n\nb"); err != nil {
		t.Fatalf("send: %v", err)
	}
	h.srv.WaitFor("*PRIVMSG #eco :a")
	h.srv.WaitFor("*PRIVMSG #eco : ")
	h.srv.WaitFor("*PRIVMSG #eco :b")
}

func TestSendBeforeConnectFails(t *testing.T) {
	c := New(Config{Network: "n", Server: "irc.test", Port: "6667", Nick: "eco"})
	if _, err := c.SendMessage("#eco", "hi"); err != ErrNotConnected {
		t.Fatalf("SendMessage before Start = %v, want ErrNotConnected", err)
	}
	if err := c.Send("PING", "x"); err != ErrNotConnected {
		t.Fatalf("Send before Start = %v, want ErrNotConnected", err)
	}
	// Close on a never-started client must return rather than wait forever.
	c.Close()
	c.Close()
	if _, ok := <-c.Events(); ok {
		t.Fatal("event channel should be closed and empty")
	}
}

func TestCloseSendsQuitAndClosesTheStream(t *testing.T) {
	h := newHarness(t, nil)
	h.register()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.c.Close()
	}()
	h.srv.WaitFor("QUIT*")
	select {
	case <-done:
	case <-time.After(eventTimeout):
		t.Fatal("Close did not return after the server dropped the connection")
	}
	// Drain: the stream must be closed, not merely quiet.
	for range h.events {
	}
	if h.c.Status() != model.ConnDisconnected {
		t.Errorf("status after close = %v", h.c.Status())
	}
}
