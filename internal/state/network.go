package state

import (
	"sort"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// applyConnState reduces a connection state transition. Disconnecting throws
// away exactly the state that belonged to the connection — in-flight join/part
// requests, batch registry, typing, "we already fetched history" — and keeps
// what belongs to the user: the buffers, their scrollback and the channels they
// consider themselves in.
func (s *Store) applyConnState(n *network, e model.ConnState) []model.Change {
	prev := n.status
	n.status = e.Status
	var out []model.Change
	if e.Nick != "" && e.Nick != n.nick {
		n.nick, n.folded = e.Nick, n.props.Fold(e.Nick)
		out = append(out, model.SelfChanged{
			ChangeMeta: model.NetMeta(n.id), Nick: n.nick, Account: n.account, Away: n.away,
		})
	}
	out = append(out, model.ConnStateChanged{
		ChangeMeta: model.NetMeta(n.id), Status: e.Status, Nick: n.nick,
		Err: e.Err, Attempt: e.Attempt, RetryIn: e.RetryIn,
	})

	switch e.Status {
	case model.ConnReady:
		if prev != model.ConnReady {
			out = append(out, s.addStatus(n, n.server(), e.EventMeta, "Connected! You are "+n.nick)...)
		}
	case model.ConnDisconnected, model.ConnFailed:
		out = append(out, s.teardown(n, e)...)
	}
	return out
}

// teardown resets the per-connection state after a disconnect.
func (s *Store) teardown(n *network, e model.ConnState) []model.Change {
	var out []model.Change
	// An unanswered part/join request died with the connection. A stale
	// pendingPart would let a later, unrelated 442 close a live buffer.
	n.pendingPart = map[string]bool{}
	n.pendingJoin = map[string]bool{}
	n.labelPart = map[string]string{}
	n.keepOnPart = map[string]bool{}
	n.names = map[string][]model.Member{}
	n.batches.Reset()
	n.away = false

	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil {
			continue
		}
		b.joined = false
		// Let a reconnect gap-fill: msgid dedupe stops the refetch duplicating.
		b.historyDone = false
		if len(b.typing) > 0 {
			nicks := make([]string, 0, len(b.typing))
			for _, t := range b.typing {
				nicks = append(nicks, t.nick)
			}
			sort.Strings(nicks)
			b.typing = map[string]typingEntry{}
			for _, nick := range nicks {
				out = append(out, model.TypingChanged{
					ChangeMeta: b.meta(), Nick: nick, State: model.TypingDone,
				})
			}
		}
	}

	// Messages still waiting for an echo will never get one on this
	// connection. Failing them keeps the text on screen and retryable instead
	// of leaving a row that spins forever.
	for _, label := range sortedLabels(n.pending) {
		reason := e.Err
		if reason == "" {
			reason = "disconnected before the server acknowledged this message"
		}
		out = append(out, s.Fail(n.id, label, "DISCONNECTED", reason)...)
	}

	text := "Disconnected from server."
	if e.Err != "" {
		text = "Disconnected from server: " + e.Err
	}
	return append(out, s.addStatus(n, n.server(), e.EventMeta, text)...)
}

// sortedLabels returns a pending map's labels in a stable order.
func sortedLabels(m map[string]model.MessageID) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// applyISupport folds a 005 line into the network's properties. When the
// casemapping itself changes, every folded key in the network is rebuilt —
// otherwise the keys we already have were folded by a rule the server has since
// contradicted.
func (s *Store) applyISupport(n *network, e model.ISupport) []model.Change {
	if e.Reset {
		n.props = proto.DefaultProps()
	}
	before := n.props.CaseMapping
	n.props.Apply(e.Tokens)
	if n.nick != "" {
		n.folded = n.props.Fold(n.nick)
	}
	if n.props.CaseMapping == before {
		return nil
	}
	return s.refold(n)
}

// refold rebuilds every folded key after a casemapping change.
func (s *Store) refold(n *network) []model.Change {
	var out []model.Change
	bufs := make(map[string]*buffer, len(n.bufs))
	order := make([]string, 0, len(n.order))
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil {
			continue
		}
		folded := n.props.Fold(b.name)
		if b.id.IsServer() {
			folded = ""
		}
		if _, clash := bufs[folded]; clash {
			folded = k // two names that now fold together: keep them apart
		}
		if folded != b.id.Folded {
			from := b.id
			b.id = model.BufferID{Network: n.id, Folded: folded}
			for i := range b.msgs {
				b.msgs[i].Buffer = b.id
			}
			if n.focus == from {
				n.focus = b.id
			}
			out = append(out, model.BufferRenamed{ChangeMeta: b.meta(), From: from, Name: b.name})
		}
		members := make(map[string]model.Member, len(b.members))
		for _, m := range b.members {
			m.Folded = n.props.Fold(m.Nick)
			members[m.Folded] = m
		}
		b.members = members
		bufs[folded] = b
		order = append(order, folded)
	}
	n.bufs, n.order = bufs, order
	n.pendingJoin = refoldSet(n, n.pendingJoin)
	n.pendingPart = refoldSet(n, n.pendingPart)
	n.keepOnPart = refoldSet(n, n.keepOnPart)
	return out
}

// refoldSet rebuilds a set of folded channel keys. The keys are already folded,
// so the best we can do is fold them again under the new rule — which is
// idempotent for every mapping pair that matters (the ASCII subset is fixed).
func refoldSet(n *network, in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[n.props.Fold(k)] = v
	}
	return out
}

// applyCapChange keeps the network's capability set current. The store owns the
// CapSet because it needs cap values (multiline limits, SASL mechanisms) and
// because Enabled decides whether a feature's absence is a bug or a fact.
func (s *Store) applyCapChange(n *network, e model.CapChange) []model.Change {
	switch e.Op {
	case model.CapLS:
		n.caps.LS(e.Caps, e.More)
	case model.CapNEW:
		n.caps.New(e.Caps)
	case model.CapDEL:
		names := make([]string, 0, len(e.Caps))
		for _, c := range e.Caps {
			names = append(names, c.Name)
		}
		n.caps.Del(names)
	case model.CapACK:
		names := make([]string, 0, len(e.Caps))
		for _, c := range e.Caps {
			names = append(names, c.Name)
		}
		n.caps.Ack(names)
	case model.CapNAK:
		names := make([]string, 0, len(e.Caps))
		for _, c := range e.Caps {
			names = append(names, c.Name)
		}
		n.caps.Nak(names)
	}
	return nil
}

// applyAuthResult reports how SASL ended. A failure is a first-class change,
// not a log line: v1's SASLOptional silently degraded to an unauthenticated
// session and the only trace was in the server buffer.
func (s *Store) applyAuthResult(n *network, e model.AuthResult) []model.Change {
	if e.Success && e.Account != "" {
		n.account = e.Account
	}
	out := []model.Change{model.AuthStateChanged{
		ChangeMeta: model.NetMeta(n.id), Success: e.Success,
		Mechanism: e.Mechanism, Account: e.Account, Message: e.Message,
	}}
	text := "Logged in as " + e.Account + "."
	switch {
	case e.Success && e.Account == "":
		text = "Authenticated."
	case !e.Success && e.Aborted:
		text = "Authentication skipped: " + firstNonEmpty(e.Message, "no mechanism in common")
	case !e.Success:
		text = "Authentication failed: " + firstNonEmpty(e.Message, "the server rejected the credentials")
	}
	out = append(out, s.addStatus(n, n.server(), e.EventMeta, text)...)
	if e.Success && e.Account != "" {
		out = append(out, model.SelfChanged{
			ChangeMeta: model.NetMeta(n.id), Nick: n.nick, Account: n.account, Away: n.away,
		})
	}
	return out
}

// applyJoinRefused reduces a refused JOIN. Only a join the user just asked for
// may interrupt them: an autojoin or rejoin refusal stays in the log, or a
// reconnect to a network with three banned channels opens three dialogs.
func (s *Store) applyJoinRefused(n *network, e model.JoinRefused) []model.Change {
	if e.Channel == "" {
		return nil
	}
	folded := n.props.Fold(e.Channel)
	asked := n.pendingJoin[folded]
	delete(n.pendingJoin, folded)
	reason := firstNonEmpty(e.Reason, "Cannot join "+e.Channel)
	out := s.addStatus(n, n.server(), e.EventMeta, "Cannot join "+e.Channel+": "+reason)
	if !asked {
		return out
	}
	return append(out, model.JoinFailed{
		ChangeMeta: model.NetMeta(n.id), Channel: e.Channel,
		Code: e.Code, Reason: reason, NeedKey: e.NeedKey,
	})
}

// applyListItem streams one LIST row into the cache, reporting in chunks.
func (s *Store) applyListItem(n *network, e model.ListItem) []model.Change {
	if e.Channel == "" {
		return nil
	}
	if n.list.closed || !n.list.started {
		n.list = listCache{index: map[string]int{}, started: true}
	}
	item := model.ChannelInfo{Name: e.Channel, Users: e.Users, Topic: e.Topic}
	folded := n.props.Fold(e.Channel)
	if i, ok := n.list.index[folded]; ok {
		n.list.items[i] = item
	} else {
		n.list.index[folded] = len(n.list.items)
		n.list.items = append(n.list.items, item)
	}
	if len(n.list.items)-n.list.unsent < listChunk {
		return nil
	}
	return s.flushList(n, false)
}

// applyListEnd closes a LIST scan.
func (s *Store) applyListEnd(n *network, e model.ListEnd) []model.Change {
	if !n.list.started {
		return []model.Change{model.ListUpdated{ChangeMeta: model.NetMeta(n.id), Done: true}}
	}
	n.list.closed = true
	return s.flushList(n, true)
}

// flushList reports the LIST rows accumulated since the last flush.
func (s *Store) flushList(n *network, done bool) []model.Change {
	items := append([]model.ChannelInfo(nil), n.list.items[n.list.unsent:]...)
	n.list.unsent = len(n.list.items)
	if len(items) == 0 && !done {
		return nil
	}
	return []model.Change{model.ListUpdated{ChangeMeta: model.NetMeta(n.id), Items: items, Done: done}}
}

// applyServerError reduces the ERROR command: the server is closing the link
// and just said why.
func (s *Store) applyServerError(n *network, e model.ServerError) []model.Change {
	return s.addStatus(n, n.server(), e.EventMeta, "Error: "+e.Message)
}

// whoisNumerics are the WHOIS replies the store renders. They carry no target
// of their own, which is why they need the correlation map.
var whoisNumerics = map[string]bool{
	"311": true, "312": true, "313": true, "317": true,
	"318": true, "319": true, "330": true, "671": true,
}

// quietNumerics are consumed without a log line. Every one of them is either
// machine-readable state the client already reports some other way (004
// RPL_MYINFO restates 005; 329 is a channel's creation time; 333 is the topic
// setter, which the irc layer folds into the Topic event when it has the topic
// to attach it to) or a LUSERS detail line v1 dropped for being noise. The rest
// of v1's handledNumerics table is not repeated here: everything else on it has
// a typed event in v2 and never reaches this function.
var quietNumerics = map[string]bool{
	"004": true, "250": true, "252": true, "253": true, "254": true,
	"329": true, "333": true,
}

// applyNumeric routes a numeric reply that has no typed event of its own.
//
// WHOIS replies go to the buffer that asked about *that nick* (TrackWhois),
// which is the per-request correlation the blueprint demands: v1 had one
// whoisTarget slot that was never cleared, so a reply could land in whichever
// buffer last ran the command.
func (s *Store) applyNumeric(n *network, e model.Numeric) []model.Change {
	if e.Code == "FAIL" && e.Label != "" {
		code, reason := "", numericText(e)
		if len(e.Params) > 1 {
			code = e.Params[1]
		}
		if out := s.Fail(n.id, e.Label, code, reason); len(out) > 0 {
			return out
		}
	}
	if whoisNumerics[e.Code] {
		return s.applyWhois(n, e)
	}
	var out []model.Change
	if e.Code == "442" || e.Code == "403" {
		out = append(out, s.partRefused(n, e)...)
	}
	if quietNumerics[e.Code] {
		return out
	}
	text := numericText(e)
	if text == "" {
		return out
	}
	if len(e.Code) == 3 && (e.Code[0] == '4' || e.Code[0] == '5') {
		text = "Error: " + text
	}
	return append(out, s.addStatus(n, n.server(), e.EventMeta, text)...)
}

// partRefused closes a buffer whose PART the server answered with 442
// (not on channel) or 403 (no such channel) instead of a PART echo — the stale
// buffer after a failed rejoin, which v1 left open forever.
//
// v1 keyed this on "the user asked to leave a channel with this name", and
// those numerics answer TOPIC, KICK and MODE too, so an unlucky interleaving
// could close a live buffer. When the reply carries our PART's label it is
// authoritative and the name is not consulted at all.
func (s *Store) partRefused(n *network, e model.Numeric) []model.Change {
	folded := ""
	if e.Label != "" {
		f, ok := n.labelPart[e.Label]
		if !ok {
			// The reply is labelled, and the label is not one of ours: it
			// answers some other command and says nothing about our PART.
			return nil
		}
		delete(n.labelPart, e.Label)
		folded = f
	}
	if folded == "" {
		if len(e.Params) == 0 || !n.pendingPart[n.props.Fold(e.Params[0])] {
			return nil
		}
		folded = n.props.Fold(e.Params[0])
	}
	delete(n.pendingPart, folded)
	if n.keepOnPart[folded] {
		// A /cycle whose PART was refused keeps its buffer; the JOIN still runs.
		delete(n.keepOnPart, folded)
		return nil
	}
	return s.closeBuffer(n, n.bufs[folded])
}

// applyWhois formats and routes one WHOIS reply.
func (s *Store) applyWhois(n *network, e model.Numeric) []model.Change {
	p := e.Params
	if len(p) == 0 {
		return nil
	}
	nick := p[0]
	folded := n.props.Fold(nick)
	b := n.server()
	if id, ok := n.whois[folded]; ok {
		if target := n.bufs[id.Folded]; target != nil {
			b = target
		}
	}
	arg := func(i int) string {
		if i < len(p) {
			return p[i]
		}
		return ""
	}
	var text string
	switch e.Code {
	case "311":
		text = nick + " is " + arg(1) + "@" + arg(2) + " (" + e.Text + ")"
	case "312":
		text = nick + " is connected via " + arg(1) + " (" + e.Text + ")"
	case "313":
		text = nick + " is an IRC operator"
	case "317":
		text = nick + " has been idle " + arg(1) + " seconds"
	case "319":
		text = nick + " is in: " + firstNonEmpty(e.Text, arg(1))
	case "330":
		text = nick + " is logged in as " + arg(1)
	case "671":
		text = nick + " is using a secure connection"
	case "318":
		text = "End of WHOIS for " + nick
		delete(n.whois, folded)
	}
	return s.addStatus(n, b, e.EventMeta, text)
}

// numericText reconstructs a numeric's human-readable body. Params excludes the
// leading nick; Text is the trailing parameter, which may or may not also be
// the last element of Params depending on how the irc layer filled them in.
func numericText(e model.Numeric) string {
	parts := e.Params
	if e.Text != "" && (len(parts) == 0 || parts[len(parts)-1] != e.Text) {
		parts = append(append([]string{}, parts...), e.Text)
	}
	return strings.Join(parts, " ")
}
