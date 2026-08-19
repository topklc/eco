package irc

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// decoder turns one connection's inbound lines into events. It is created per
// connection attempt and is confined to that connection's reader goroutine -
// every field without a synchronisation type below is owned by that goroutine
// alone.
//
// Decoding happens on the raw hook rather than the library's per-command
// callbacks, and that is a deliberate choice: the library buffers whole
// batches and swallows labelled replies it did not itself label, so a client
// that wants to see BATCH framing and its own labels has to read the line
// stream. One hook also means one decode path, which is the only way the
// event order can be promised to be the wire order.
type decoder struct {
	c    *Client
	conn *ircevent.Connection

	props   *proto.ServerProps
	caps    *proto.CapSet
	batches *proto.BatchTracker
	collect map[string]*collector
	// grouped maps a transparent batch's ref to the label it answers. A
	// labeled-response batch exists only to group the reply to one command,
	// and its contents carry the batch tag but no label of their own, so the
	// label is handed down to them.
	grouped map[string]string

	sawLine     bool
	sawISupport bool
	ready       bool

	// authNeeded marks the mechanisms that finish after registration, which
	// is what makes "authenticate, then report ready" expressible at all.
	authNeeded bool
	authDone   bool
	account    string

	// topics remembers the last RPL_TOPIC per channel so RPL_TOPICWHOTIME,
	// which carries the setter but not the text, can be emitted as a complete
	// Topic instead of one that looks like a topic being cleared.
	topics map[string]string

	deadOnce sync.Once
	dead     chan struct{}

	authFail atomic.Bool
	errText  atomic.Pointer[string]
}

func newDecoder(c *Client, conn *ircevent.Connection) *decoder {
	return &decoder{
		c:          c,
		conn:       conn,
		props:      proto.DefaultProps(),
		caps:       proto.NewCapSet(c.cfg.Caps),
		batches:    proto.NewBatchTracker(),
		collect:    map[string]*collector{},
		grouped:    map[string]string{},
		topics:     map[string]string{},
		authNeeded: c.cfg.SASL.scram(),
		dead:       make(chan struct{}),
	}
}

func (d *decoder) markDead() { d.deadOnce.Do(func() { close(d.dead) }) }

// authFatal reports an authentication failure the client must not retry.
func (d *decoder) authFatal() bool { return d.authFail.Load() }

// lastError returns the server's own words for why the connection ended, "" if
// it did not say.
func (d *decoder) lastError() string {
	if p := d.errText.Load(); p != nil {
		return *p
	}
	return ""
}

func (d *decoder) setError(s string) {
	if s != "" {
		d.errText.CompareAndSwap(nil, &s)
	}
}

// line is the raw hook: one call per inbound line, in wire order.
func (d *decoder) line(raw string, msg ircmsg.Message, perr error) {
	if !d.sawLine {
		d.sawLine = true
		d.c.setStatus(model.ConnRegistering)
		d.c.emit(model.ConnState{EventMeta: d.c.meta(), Status: model.ConnRegistering,
			Nick: d.c.CurrentNick()})
	}
	if d.c.cfg.EmitRaw {
		d.c.emit(model.Raw{EventMeta: d.c.meta(), Line: raw})
	}
	if perr != nil {
		return
	}

	m := d.meta(&msg)
	switch strings.ToUpper(msg.Command) {
	case "PRIVMSG":
		d.message(&msg, m, false)
	case "NOTICE":
		d.message(&msg, m, true)
	case "TAGMSG":
		d.tagmsg(&msg, m)
	case "JOIN":
		acct, real := param(&msg, 1), param(&msg, 2)
		if acct == "*" {
			acct = ""
		}
		d.out(model.Join{EventMeta: m, Channel: param(&msg, 0),
			Sender: d.sender(&msg, m), Account: acct, RealName: real})
	case "PART":
		d.out(model.Part{EventMeta: m, Channel: param(&msg, 0),
			Sender: d.sender(&msg, m), Reason: param(&msg, 1)})
	case "QUIT":
		d.out(model.Quit{EventMeta: m, Sender: d.sender(&msg, m), Reason: param(&msg, 0)})
	case "KICK":
		d.out(model.Kick{EventMeta: m, Channel: param(&msg, 0), Sender: d.sender(&msg, m),
			Target: param(&msg, 1), Reason: param(&msg, 2)})
	case "NICK":
		s := d.sender(&msg, m)
		newNick := param(&msg, 0)
		if s.Self {
			d.c.setNick(newNick)
		}
		d.out(model.Nick{EventMeta: m, Sender: s, NewNick: newNick})
	case "INVITE":
		d.out(model.Invite{EventMeta: m, Target: param(&msg, 0), Channel: param(&msg, 1),
			Sender: d.sender(&msg, m)})
	case "TOPIC":
		topic := param(&msg, 1)
		ch := param(&msg, 0)
		d.topics[d.props.Fold(ch)] = topic
		d.out(model.Topic{EventMeta: m, Channel: ch, Sender: d.sender(&msg, m),
			Topic: topic, Cleared: topic == ""})
	case "MODE":
		var args []string
		if len(msg.Params) > 2 {
			args = msg.Params[2:]
		}
		d.out(model.Mode{EventMeta: m, Target: param(&msg, 0), Sender: d.sender(&msg, m),
			ModeString: param(&msg, 1), Args: args})
	case "AWAY":
		away := len(msg.Params) > 0 && msg.Params[0] != ""
		d.out(model.AwayChange{EventMeta: m, Sender: d.sender(&msg, m),
			Away: away, Message: param(&msg, 0)})
	case "ACCOUNT":
		acct := param(&msg, 0)
		if acct == "*" {
			acct = ""
		}
		d.out(model.AccountChange{EventMeta: m, Sender: d.sender(&msg, m), Account: acct})
	case "CHGHOST":
		d.out(model.ChgHost{EventMeta: m, Sender: d.sender(&msg, m),
			NewUser: param(&msg, 0), NewHost: param(&msg, 1)})
	case "SETNAME":
		d.out(model.SetName{EventMeta: m, Sender: d.sender(&msg, m), RealName: param(&msg, 0)})
	case "RENAME":
		d.out(model.ChannelRenamed{EventMeta: m, From: param(&msg, 0), To: param(&msg, 1),
			Reason: param(&msg, 2)})
	case "REDACT":
		d.out(model.Redact{EventMeta: m, Target: param(&msg, 0), Sender: d.sender(&msg, m),
			TargetMsgID: param(&msg, 1), Reason: param(&msg, 2)})
	case "MARKREAD":
		d.markread(&msg, m)
	case "CHATHISTORY":
		d.chathistory(&msg, m)
	case "BATCH":
		d.batch(&msg, m)
	case "CAP":
		d.cap(&msg, m)
	case "ERROR":
		text := param(&msg, 0)
		d.setError(text)
		d.out(model.ServerError{EventMeta: m, Message: text})
	case "FAIL", "WARN", "NOTE":
		// standard-replies are not numerics, but they are exactly the same
		// shape and they answer labels, so they travel as Numeric with the
		// command as its code.
		d.out(model.Numeric{EventMeta: m, Code: strings.ToUpper(msg.Command),
			Params: msg.Params, Text: lastParam(&msg)})
	case "PING", "PONG", "AUTHENTICATE":
		// PING/PONG are the library's keepalive; AUTHENTICATE carries
		// credentials and is never turned into an event.
	default:
		if isNumeric(msg.Command) {
			d.numeric(&msg, m)
			return
		}
		d.out(model.Numeric{EventMeta: m, Code: strings.ToUpper(msg.Command),
			Params: msg.Params, Text: lastParam(&msg)})
	}
}

// out delivers one decoded event, collecting it instead when it belongs to a
// batch that assembles or replays its contents. The walk is innermost-first,
// so a multiline batch nested inside a chathistory batch collects into the
// multiline batch, and the merged message then lands in the history chunk.
func (d *decoder) out(ev model.Event) {
	if ref := ev.Meta().Batch; ref != "" && len(d.collect) > 0 {
		for _, b := range d.batches.Chain(ref) {
			col, ok := d.collect[b.Ref]
			if !ok {
				continue
			}
			if col.kind == collectAggregate && !isMessageEvent(ev) {
				continue // only message parts merge; the rest passes outward
			}
			if !col.add(ev) {
				break // the batch outgrew its bound; stream the rest live
			}
			return
		}
	}
	d.c.emit(ev)
}

func isMessageEvent(ev model.Event) bool {
	switch ev.(type) {
	case model.PrivMsg, model.Notice:
		return true
	}
	return false
}

// meta builds an event envelope from a line's tags. Playback is resolved here,
// once, from the real batch registry.
func (d *decoder) meta(msg *ircmsg.Message) model.EventMeta {
	m := model.EventMeta{Network: d.c.cfg.Network}
	tags := msg.AllTags()
	if len(tags) > 0 {
		m.Tags = tags
	}
	if v, ok := tags["time"]; ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			m.Time, m.ServerTime = t.Local(), true
		}
	}
	if !m.ServerTime {
		m.Time = d.c.clock.Now()
	}
	m.MsgID = tags["msgid"]
	m.Label = tags["label"]
	m.Batch = tags["batch"]
	if m.Batch != "" {
		m.Playback = d.batches.IsPlayback(m.Batch)
		if m.Label == "" && len(d.grouped) > 0 {
			m.Label = d.groupLabel(m.Batch)
		}
	}
	return m
}

// groupLabel finds the label of the nearest enclosing labeled-response batch.
// A server may answer one labelled command with several lines, in which case
// only the BATCH carries the label; without this the echo of a multi-line
// reply would arrive uncorrelated and the pending message would never resolve.
func (d *decoder) groupLabel(ref string) string {
	for _, b := range d.batches.Chain(ref) {
		if lbl, ok := d.grouped[b.Ref]; ok {
			return lbl
		}
	}
	return ""
}

// sender parses the line's source. A zero Sender means the server itself
// spoke; a source with no user or host (a server name) still fills Nick,
// because the store needs the name to route server notices.
func (d *decoder) sender(msg *ircmsg.Message, m model.EventMeta) model.Sender {
	if msg.Source == "" {
		return model.Sender{}
	}
	s := model.Sender{Nick: msg.Source}
	if nuh, err := ircmsg.ParseNUH(msg.Source); err == nil {
		s = model.Sender{Nick: nuh.Name, User: nuh.User, Host: nuh.Host}
	}
	if acct, ok := m.Tag("account"); ok && acct != "*" {
		s.Account = acct
	}
	s.Self = d.isSelf(s.Nick)
	return s
}

// isSelf folds with the network's casemapping, never strings.ToLower: on a
// network with an rfc1459 casemapping "Nick[a]" and "nick{a}" are the same
// user, and v1 got that wrong everywhere.
func (d *decoder) isSelf(nick string) bool {
	if nick == "" {
		return false
	}
	cur := d.conn.CurrentNick()
	if cur == "" {
		cur = d.c.cfg.Nick
	}
	return d.props.Equal(nick, cur)
}

// --- messages ---------------------------------------------------------------

func (d *decoder) message(msg *ircmsg.Message, m model.EventMeta, notice bool) {
	if len(msg.Params) < 2 {
		return
	}
	target, text := msg.Params[0], msg.Params[1]
	ctcp, body, action := splitCTCP(text)
	sender := d.sender(msg, m)
	if notice {
		d.out(model.Notice{EventMeta: m, Target: target, Sender: sender, Text: body, CTCP: ctcp})
		return
	}
	d.out(model.PrivMsg{EventMeta: m, Target: target, Sender: sender, Text: body,
		Action: action, CTCP: ctcp})
	if ctcp != "" {
		d.ctcpReply(sender, ctcp, body, m)
	}
}

// ctcpReply answers the CTCP queries a client is expected to answer. It is
// protocol, not presentation: the reply is a fixed protocol string, and
// leaving it to a layer above would mean every layer above had to remember.
// Replayed history is never answered - that would fire one reply per replayed
// query on every reconnect.
func (d *decoder) ctcpReply(sender model.Sender, ctcp, args string, m model.EventMeta) {
	if m.Playback || sender.Nick == "" || sender.Self {
		return
	}
	var body string
	switch ctcp {
	case "VERSION":
		body = "VERSION " + d.c.cfg.Version
	case "CLIENTINFO":
		body = "CLIENTINFO ACTION CLIENTINFO PING TIME VERSION"
	case "TIME":
		body = "TIME " + d.c.clock.Now().UTC().Format(time.RFC1123)
	case "USERINFO":
		body = "USERINFO " + d.c.cfg.User
	case "PING":
		// A PING is answered with its own payload, untouched: it is the
		// asker's round-trip token, not ours to reformat.
		body = "PING " + args
		if args == "" {
			body = "PING"
		}
	default:
		return
	}
	reply := ircmsg.MakeMessage(nil, "", "NOTICE", sender.Nick, "\x01"+body+"\x01")
	reply.ForceTrailing()
	_ = d.conn.SendIRCMessage(reply)
}

// splitCTCP unwraps a CTCP body. An ACTION comes back as (\"\", text, true);
// any other CTCP comes back with its command and its argument; an ordinary
// message is returned untouched.
func splitCTCP(text string) (ctcp, body string, action bool) {
	if len(text) < 2 || text[0] != '\x01' {
		return "", text, false
	}
	inner := strings.TrimSuffix(text[1:], "\x01")
	cmd, args, _ := strings.Cut(inner, " ")
	if strings.EqualFold(cmd, "ACTION") {
		return "", args, true
	}
	return strings.ToUpper(cmd), args, false
}

func (d *decoder) tagmsg(msg *ircmsg.Message, m model.EventMeta) {
	v, ok := m.Tag("+typing")
	if !ok {
		return // a TAGMSG carrying nothing eco models is not an event
	}
	state := model.TypingDone
	switch strings.ToLower(v) {
	case "active":
		state = model.TypingActive
	case "paused":
		state = model.TypingPaused
	}
	d.out(model.Typing{EventMeta: m, Target: param(msg, 0), Sender: d.sender(msg, m), State: state})
}

func (d *decoder) markread(msg *ircmsg.Message, m model.EventMeta) {
	target, sel := param(msg, 0), param(msg, 1)
	ev := model.MarkRead{EventMeta: m, Target: target}
	if ts, ok := strings.CutPrefix(sel, "timestamp="); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			ev.Timestamp = t.Local()
		} else {
			ev.Unset = true
		}
	} else {
		ev.Unset = true // the wire spells "no marker" as "*"
	}
	d.out(ev)
}

// chathistory decodes the one CHATHISTORY line a server sends *to* us:
// `CHATHISTORY TARGETS <target> <timestamp>`, one per conversation with
// activity in the window we asked about. Everything else the subcommand can
// carry comes back as a chathistory BATCH and is decoded there.
//
// The targets batch has no registry entry, deliberately: it is transparent, so
// these lines flow through live and each one is an event of its own rather
// than a block that has to be assembled before anything can act on it.
func (d *decoder) chathistory(msg *ircmsg.Message, m model.EventMeta) {
	if !strings.EqualFold(param(msg, 0), "TARGETS") {
		return
	}
	target := param(msg, 1)
	if target == "" {
		return
	}
	ev := model.HistoryTarget{EventMeta: m, Target: target}
	if t, err := time.Parse(time.RFC3339Nano, param(msg, 2)); err == nil {
		ev.At = t.Local()
	}
	d.out(ev)
}

// --- batches ----------------------------------------------------------------

func (d *decoder) batch(msg *ircmsg.Message, m model.EventMeta) {
	arg := param(msg, 0)
	if len(arg) < 2 {
		return
	}
	op, ref := arg[0], arg[1:]
	// The BATCH line's own batch tag names the batch it is nested in, which
	// is exactly the parent link the registry needs.
	parent := m.Batch

	switch op {
	case '+':
		typ := param(msg, 1)
		var params []string
		if len(msg.Params) > 2 {
			params = msg.Params[2:]
		}
		if d.batches.Len() >= maxOpenBatches {
			// A server that opens batches and never closes them must not be
			// able to grow the registry without bound. Past the cap the batch
			// is still announced, but it is not tracked and its contents flow
			// through live.
			d.c.emit(model.BatchOpen{EventMeta: m, Ref: ref, Type: typ, Params: params, Parent: parent})
			return
		}
		ob, err := d.batches.Open(ref, typ, params, parent)
		if err != nil {
			// A duplicate or malformed ref: carry on with an unregistered
			// batch rather than dropping everything it contains.
			ob = proto.OpenBatch{Ref: ref, Type: proto.BatchTypeFor(typ), Params: params, Parent: parent}
		}
		switch {
		case ob.Type.Aggregate:
			d.collect[ref] = &collector{ref: ref, kind: collectAggregate, batch: ob, meta: m}
		case ob.Type.Playback:
			d.collect[ref] = &collector{ref: ref, kind: collectHistory, batch: ob, meta: m}
		case ob.Type.Transparent && m.Label != "":
			d.grouped[ref] = m.Label
		}
		d.c.emit(model.BatchOpen{EventMeta: m, Ref: ref, Type: typ, Params: params, Parent: parent})

	case '-':
		ob, known := d.batches.Lookup(ref)
		parentRef := ""
		if known {
			parentRef = ob.Parent
		}
		_, _ = d.batches.Close(ref)
		delete(d.grouped, ref)

		if col, ok := d.collect[ref]; ok {
			delete(d.collect, ref)
			playback := d.batches.IsPlayback(parentRef)
			switch col.kind {
			case collectAggregate:
				if ev, ok := col.merged(parentRef, playback); ok {
					d.out(ev)
				}
			case collectHistory:
				req, hit := d.c.ledger.lookup(col.meta.Label)
				if hit && req.Kind != CmdHistory {
					hit = false
				}
				d.out(col.chunk(parentRef, req, hit))
				d.c.ledger.forget(col.meta.Label)
			}
		}
		d.c.emit(model.BatchClose{EventMeta: m, Ref: ref, Type: ob.Type.Name})
	}
}

// --- capabilities -----------------------------------------------------------

func (d *decoder) cap(msg *ircmsg.Message, m model.EventMeta) {
	if len(msg.Params) < 3 {
		return
	}
	sub := strings.ToUpper(msg.Params[1])
	list := msg.Params[len(msg.Params)-1]
	more := sub == "LS" && len(msg.Params) > 3 && msg.Params[2] == "*"
	caps := proto.ParseCapTokens(list)
	names := strings.Fields(list)

	var op model.CapOp
	switch sub {
	case "LS":
		op = model.CapLS
		// The request itself is the library's during registration, so the
		// list LS hands back is not written here; this mirrors state.
		d.caps.LS(caps, more)
	case "NEW":
		op = model.CapNEW
		if req := d.caps.New(caps); len(req) > 0 {
			_ = d.conn.Send("CAP", "REQ", strings.Join(req, " "))
		}
	case "DEL":
		op = model.CapDEL
		d.caps.Del(names)
	case "ACK":
		op = model.CapACK
		d.caps.Ack(names)
	case "NAK":
		op = model.CapNAK
		d.caps.Nak(names)
	default:
		return
	}
	d.c.caps.Store(d.caps.Clone())
	d.c.emit(model.CapChange{EventMeta: m, Op: op, Caps: caps,
		Enabled: d.caps.EnabledNames(), More: more})
}

// --- numerics ---------------------------------------------------------------

func (d *decoder) numeric(msg *ircmsg.Message, m model.EventMeta) {
	code := msg.Command
	switch code {
	case "001": // RPL_WELCOME
		d.c.setNick(param(msg, 0))

	case "005": // RPL_ISUPPORT
		tokens := proto.ISUPPORTTokens(msg.Params)
		reset := !d.sawISupport
		if reset {
			// A reconnect may land on a different server in the same pool;
			// the previous one's properties must not survive it.
			d.props = proto.DefaultProps()
			d.sawISupport = true
		}
		d.props.Apply(tokens) // best effort: a bad token never discards the rest
		d.c.props.Store(d.props.Clone())
		d.out(model.ISupport{EventMeta: m, Tokens: tokens, Reset: reset})
		return

	case "305", "306": // RPL_UNAWAY / RPL_NOWAWAY: our own away state
		// away-notify tells us about everyone else; these two numerics are
		// the server's answer about US, and are the only reply guaranteed
		// when we send AWAY. Without them the identity chip could never
		// learn that the away it just asked for actually took.
		d.out(model.AwayChange{EventMeta: m,
			Sender: model.Sender{Nick: d.c.CurrentNick(), Self: true},
			Away:   msg.Command == "306"})
		return

	case "322": // RPL_LIST
		d.out(model.ListItem{EventMeta: m, Channel: param(msg, 1),
			Users: leadingInt(param(msg, 2)), Topic: param(msg, 3)})
		return
	case "323": // RPL_LISTEND
		d.out(model.ListEnd{EventMeta: m})
		return

	case "324": // RPL_CHANNELMODEIS - authoritative, replaces what we hold
		var args []string
		if len(msg.Params) > 3 {
			args = msg.Params[3:]
		}
		d.out(model.Mode{EventMeta: m, Target: param(msg, 1),
			ModeString: param(msg, 2), Args: args, Full: true})
		return

	case "331": // RPL_NOTOPIC
		ch := param(msg, 1)
		delete(d.topics, d.props.Fold(ch))
		d.out(model.Topic{EventMeta: m, Channel: ch, Cleared: true})
		return
	case "332": // RPL_TOPIC
		ch, topic := param(msg, 1), param(msg, 2)
		d.topics[d.props.Fold(ch)] = topic
		d.out(model.Topic{EventMeta: m, Channel: ch, Topic: topic, Cleared: topic == ""})
		return
	case "333": // RPL_TOPICWHOTIME - the setter, with no topic text of its own
		ch := param(msg, 1)
		topic, ok := d.topics[d.props.Fold(ch)]
		if !ok {
			break // no topic to attach it to; fall through to Numeric
		}
		ev := model.Topic{EventMeta: m, Channel: ch, Topic: topic, SetBy: param(msg, 2)}
		if secs, err := strconv.ParseInt(param(msg, 3), 10, 64); err == nil {
			ev.SetAt = time.Unix(secs, 0)
		}
		d.out(ev)
		return

	case "353": // RPL_NAMREPLY - entries keep their prefixes; the store splits
		d.out(model.Names{EventMeta: m, Channel: param(msg, 2),
			Names: strings.Fields(param(msg, 3))})
		return
	case "366": // RPL_ENDOFNAMES
		d.out(model.Names{EventMeta: m, Channel: param(msg, 1), End: true})
		return

	case "376", "422": // RPL_ENDOFMOTD / ERR_NOMOTD
		d.out(model.Numeric{EventMeta: m, Code: code, Params: rest(msg), Text: lastParam(msg)})
		d.endOfRegistration()
		return

	case "900": // RPL_LOGGEDIN
		d.account = param(msg, 2)
	case "903": // RPL_SASLSUCCESS
		d.authDone = true
		d.c.emit(model.AuthResult{EventMeta: m, Success: true,
			Mechanism: d.mechanism(), Account: d.account})
		if d.authNeeded {
			d.becomeReady()
		}
		return
	case "902", "904", "905", "906", "907": // nick locked / failed / too long / aborted / already
		d.authDone = true
		text := lastParam(msg)
		d.c.emit(model.AuthResult{EventMeta: m, Mechanism: d.mechanism(),
			Aborted: code == "906", Message: text})
		if !d.c.cfg.SASL.Optional {
			d.authFail.Store(true)
			d.setError(text)
			if d.authNeeded {
				// Post-registration failure leaves a usable socket that must
				// not simply sit there: end it so the supervisor can report a
				// terminal failure instead of a client stuck "authenticating".
				d.conn.Quit()
			}
		} else if d.authNeeded {
			d.becomeReady()
		}
		return
	}

	if code == "403" || isJoinRefusal(code) {
		if ev, ok := d.joinRefusal(msg, m, code); ok {
			d.out(ev)
			return
		}
	}
	d.out(model.Numeric{EventMeta: m, Code: code, Params: rest(msg), Text: lastParam(msg)})
}

// joinRefusalCodes are the numerics that can only be a refused JOIN. 403
// (no such channel) is deliberately absent: it also answers PART, TOPIC and
// MODE, so it is classified by its label instead.
func isJoinRefusal(code string) bool {
	switch code {
	case "405", "471", "473", "474", "475", "476", "477", "479":
		return true
	}
	return false
}

func (d *decoder) joinRefusal(msg *ircmsg.Message, m model.EventMeta, code string) (model.Event, bool) {
	ch := param(msg, 1)
	if ch == "" {
		return nil, false
	}
	if code == "403" {
		// Labelled responses make this exact: a 403 answering our own PART is
		// a part refusal, not a join refusal. v1 had to guess from a pending
		// map and closed live buffers when it guessed wrong.
		sent, ok := d.c.ledger.lookup(m.Label)
		if !ok || sent.Kind != CmdJoin {
			return nil, false
		}
	}
	return model.JoinRefused{EventMeta: m, Channel: ch, Code: code,
		Reason: lastParam(msg), NeedKey: code == "475"}, true
}

func (d *decoder) mechanism() string {
	if d.c.cfg.SASL.Mechanism != "" {
		return d.c.cfg.SASL.Mechanism
	}
	return MechPlain
}

// endOfRegistration runs at the end of the MOTD, which is where the protocol
// says a client is registered. With a post-registration mechanism the client
// is not yet ready: authentication is sequenced before readiness so that
// nothing above ever joins a +R channel a moment too early.
func (d *decoder) endOfRegistration() {
	if !d.authNeeded || d.authDone {
		d.becomeReady()
		return
	}
	if !d.caps.Enabled("sasl") {
		d.authDone = true
		d.c.emit(model.AuthResult{EventMeta: d.c.meta(), Mechanism: d.mechanism(), Aborted: true})
		d.becomeReady()
		return
	}
	d.c.setStatus(model.ConnAuthenticating)
	d.c.emit(model.ConnState{EventMeta: d.c.meta(), Status: model.ConnAuthenticating,
		Nick: d.c.CurrentNick()})
}

func (d *decoder) becomeReady() {
	if d.ready {
		return
	}
	d.ready = true
	if n := d.conn.CurrentNick(); n != "" {
		d.c.setNick(n)
	}
	d.c.setStatus(model.ConnReady)
	d.c.emit(model.ConnState{EventMeta: d.c.meta(), Status: model.ConnReady, Nick: d.c.CurrentNick()})
}

// --- small helpers ----------------------------------------------------------

func param(msg *ircmsg.Message, i int) string {
	if i < len(msg.Params) {
		return msg.Params[i]
	}
	return ""
}

func lastParam(msg *ircmsg.Message) string {
	if len(msg.Params) == 0 {
		return ""
	}
	return msg.Params[len(msg.Params)-1]
}

// rest drops the leading nick every numeric repeats back at us.
func rest(msg *ircmsg.Message) []string {
	if len(msg.Params) < 2 {
		return nil
	}
	return msg.Params[1:]
}

func isNumeric(cmd string) bool {
	if len(cmd) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if cmd[i] < '0' || cmd[i] > '9' {
			return false
		}
	}
	return true
}

// leadingInt parses leading decimal digits, tolerating trailing text, without
// reflection. RPL_LIST user counts have been seen with suffixes.
func leadingInt(v string) int {
	n := 0
	for i := 0; i < len(v) && v[i] >= '0' && v[i] <= '9'; i++ {
		n = n*10 + int(v[i]-'0')
	}
	return n
}
