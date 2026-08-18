package parity

import (
	"sort"
	"strconv"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
)

// Fact is the canonical form both cores are normalised onto: one user-visible
// thing that happened, stripped of everything that cannot be compared.
//
// Time is deliberately absent (v1 stamps time.Now() where v2 stamps an injected
// clock), and so is the store's MessageID (v1 has no such concept). What is left
// is what a user could notice: which buffer, what kind of thing, who said it,
// what it said, and the flags that drive rendering and attention.
type Fact struct {
	Kind   string
	Buffer string // folded buffer name; "server" is the network's status buffer
	Nick   string
	Text   string
	Flags  string // sorted, comma separated
}

// The canonical kinds. Both normalisers may only produce these.
const (
	kindMsg       = "msg"             // a chat message reached a buffer
	kindStatus    = "status"          // a client-generated status line reached a buffer
	kindTombstone = "tombstone"       // what a redacted message left behind
	kindAmend     = "amend"           // v1: the last line of a buffer was rewritten in place
	kindRail      = "rail"            // the buffer list changed (v1's BufferList carries no name)
	kindMembers   = "members"         // a buffer's member list changed
	kindCleared   = "members-cleared" // a buffer's member list was emptied
	kindTopic     = "topic"           // a buffer's topic changed
	kindRename    = "rename"          // a buffer's identity moved
	kindRedact    = "redact"          // a message was withdrawn
	kindJoinFail  = "joinfail"        // a JOIN was refused, and the UI must say so
	kindConn      = "conn"            // the connection state moved
	kindSTS       = "sts"             // an STS policy was advertised
	kindHighlight = "highlight"       // v1: an amended line became a mention
	kindListDone  = "listdone"        // a channel scan finished
	kindList      = "list"            // LIST rows were handed over
	kindResolve   = "resolve"         // v2: a pending message was confirmed by its echo
	kindMsgFail   = "msgfail"         // v2: the server rejected a message we sent
	kindHistory   = "history"         // v2: a chathistory batch closed
	kindSelf      = "self"            // v2: our own identity on the network changed
	kindAuth      = "auth"            // v2: SASL settled
	kindUnread    = "unread"          // v2: a buffer's unread counters moved
	kindMarker    = "marker"          // v2: a read marker moved
	kindTyping    = "typing"          // v2: someone's typing state changed
	kindMode      = "mode"            // v2: a channel's mode state changed
	kindBuffer    = "buffer"          // a state snapshot's header line, for reports only
)

// String renders a fact for a diff. It is the sort key too, so it must contain
// every field.
func (f Fact) String() string {
	if f.Kind == kindBuffer {
		// A snapshot header is already a sentence; quoting it would bury the
		// interesting part behind escaped quotes in the one place a reader is
		// looking for it.
		return "buffer " + f.Buffer + ": " + f.Text
	}
	var b strings.Builder
	b.WriteString(f.Kind)
	if f.Buffer != "" {
		b.WriteString(" " + f.Buffer)
	}
	if f.Nick != "" {
		b.WriteString(" <" + f.Nick + ">")
	}
	if f.Flags != "" {
		b.WriteString(" [" + f.Flags + "]")
	}
	if f.Text != "" {
		b.WriteString(" " + strconv.Quote(f.Text))
	}
	return b.String()
}

// serverBufferName is what both cores' status buffer is called in canonical
// form: v1 names it "server", v2 gives it the empty folded key.
const serverBufferName = "server"

// foldName is the harness's one folding rule. internal/testsrv advertises
// CASEMAPPING=ascii, under which it agrees exactly with v2's proto fold; a
// transcript that advertised rfc1459 would fold "[]\" differently on the two
// sides, which is the folded-identity divergence and would show up here rather
// than hide.
func foldName(name string) string {
	if name == "" {
		return serverBufferName
	}
	return strings.ToLower(name)
}

// bufName is foldName for an already-folded v2 id.
func bufName(id model.BufferID) string {
	if id.Folded == "" {
		return serverBufferName
	}
	return id.Folded
}

// flags builds the sorted flag string. Empty names are dropped so callers can
// write flags(cond("self", l.Self), ...).
func flags(names ...string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// cond returns name when on, "" otherwise.
func cond(name string, on bool) string {
	if on {
		return name
	}
	return ""
}

// --- v1 -----------------------------------------------------------------------

// factFromLine normalises one v1 Line. Status lines keep their text: v2's
// reducers were ported from these handler bodies and produce the same sentences,
// so the wording is comparable — where it deliberately is not, the divergence
// list says so.
func factFromLine(buffer string, l model.Line) Fact {
	if l.Status {
		return Fact{Kind: kindStatus, Buffer: foldName(buffer), Text: l.Text}
	}
	return Fact{
		Kind:   kindMsg,
		Buffer: foldName(buffer),
		Nick:   l.Nick,
		Text:   l.Text,
		Flags: flags(
			cond("action", l.Action),
			cond("highlight", l.Highlight),
			cond("notice", l.Notice),
			cond("playback", l.Playback),
			cond("self", l.Self),
		),
	}
}

// --- v2 -----------------------------------------------------------------------

// factFromMessage normalises one v2 Message onto the same shape a v1 Line takes.
func factFromMessage(m model.Message) Fact {
	switch m.Kind {
	case model.MsgStatus:
		return Fact{Kind: kindStatus, Buffer: bufName(m.Buffer), Text: m.Text}
	case model.MsgTombstone:
		return Fact{Kind: kindTombstone, Buffer: bufName(m.Buffer)}
	}
	return Fact{
		Kind:   kindMsg,
		Buffer: bufName(m.Buffer),
		Nick:   m.Sender.Nick,
		Text:   m.Text,
		Flags: flags(
			cond("action", m.Kind == model.MsgAction),
			cond("highlight", m.Highlight),
			cond("notice", m.Kind == model.MsgNotice),
			cond("playback", m.Playback),
			cond("self", m.Sender.Self),
		),
	}
}

// factsFromChange normalises one Change. A change with no user-visible
// counterpart still produces a fact — nothing is dropped here, because dropping
// is the divergence list's job and it has to be explicit.
func factsFromChange(c model.Change) []Fact {
	meta := c.Meta()
	buf := bufName(meta.Buffer)
	if meta.Buffer.IsZero() {
		buf = ""
	}
	switch v := c.(type) {
	case model.MessageAdded:
		return []Fact{factFromMessage(v.Message)}
	case model.MessageResolved:
		return []Fact{{Kind: kindResolve, Buffer: buf, Text: v.Message.MsgID}}
	case model.MessageFailed:
		return []Fact{{Kind: kindMsgFail, Buffer: buf, Text: v.Code}}
	case model.MessageRedacted:
		return []Fact{{Kind: kindRedact, Buffer: buf, Text: v.MsgID}}
	case model.HistoryLoaded:
		// The chunk itself is a v2 concept; its messages are exactly what v1
		// delivered one line at a time, so they are expanded back out.
		out := []Fact{{Kind: kindHistory, Buffer: buf, Text: v.Direction.String()}}
		for _, m := range v.Messages {
			out = append(out, factFromMessage(m))
		}
		return out
	case model.BufferCreated:
		return []Fact{{Kind: kindRail}}
	case model.BufferClosed:
		return []Fact{{Kind: kindRail}}
	case model.BufferRenamed:
		return []Fact{{Kind: kindRename, Buffer: buf, Text: v.From.Folded}}
	case model.MembersChanged:
		// "the roster was emptied" is its own fact: it is the one member change
		// v1 makes without telling anyone, so it needs to be distinguishable
		// from every ordinary delta rather than lumped in with them.
		if v.Reset != nil && len(v.Reset) == 0 {
			return []Fact{{Kind: kindCleared, Buffer: buf}}
		}
		return []Fact{{Kind: kindMembers, Buffer: buf}}
	case model.TopicChanged:
		return []Fact{{Kind: kindTopic, Buffer: buf, Text: v.Topic}}
	case model.ModeChanged:
		return []Fact{{Kind: kindMode, Buffer: buf, Text: v.Modes}}
	case model.UnreadChanged:
		return []Fact{{Kind: kindUnread, Buffer: buf}}
	case model.ReadMarkerMoved:
		return []Fact{{Kind: kindMarker, Buffer: buf}}
	case model.TypingChanged:
		return []Fact{{Kind: kindTyping, Buffer: buf, Nick: v.Nick, Text: v.State.String()}}
	case model.ConnStateChanged:
		return []Fact{{Kind: kindConn, Text: v.Status.String()}}
	case model.SelfChanged:
		return []Fact{{Kind: kindSelf, Nick: v.Nick}}
	case model.AuthStateChanged:
		return []Fact{{Kind: kindAuth, Text: v.Mechanism, Flags: cond("ok", v.Success)}}
	case model.ListUpdated:
		out := []Fact{{Kind: kindList, Text: strconv.Itoa(len(v.Items))}}
		if v.Done {
			out = append(out, Fact{Kind: kindListDone})
		}
		return out
	case model.JoinFailed:
		return []Fact{{Kind: kindJoinFail, Buffer: foldName(v.Channel),
			Text: v.Reason, Flags: cond("needkey", v.NeedKey)}}
	}
	return []Fact{{Kind: "unmapped:" + c.ChangeName(), Buffer: buf}}
}

func factsFromChanges(cs []model.Change) []Fact {
	var out []Fact
	for _, c := range cs {
		out = append(out, factsFromChange(c)...)
	}
	return out
}

// --- normalisation ------------------------------------------------------------

// collapseAmends folds v1's in-place line rewrites into the message they
// rewrite. v1 hands the UI the first part of a multiline message and then
// amends that row per continuation part; v2 assembles the whole message at BATCH
// close and hands it over once. The two are the same message, so the amendments
// are folded back into it and the *mechanism* difference is what the divergence
// list records — not the text, which must match.
//
// An amend with no message in front of it is left alone: that is a real
// disagreement, and hiding it is exactly what this harness must not do.
func collapseAmends(in []Fact) []Fact {
	out := make([]Fact, 0, len(in))
	for _, f := range in {
		if f.Kind == kindAmend {
			if i := lastMsgIn(out, f.Buffer); i >= 0 {
				out[i].Text = f.Text
				out[i].Flags = f.Flags
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// lastMsgIn returns the index of the last kindMsg fact for a buffer, or -1.
func lastMsgIn(fs []Fact, buffer string) int {
	for i := len(fs) - 1; i >= 0; i-- {
		if fs[i].Kind == kindMsg && fs[i].Buffer == buffer {
			return i
		}
	}
	return -1
}

// sortFacts orders a group's facts canonically. Both cores see the same wire
// line at the same point, so a group holds one line's worth of consequences and
// the order the two cores emit them in (v1 fires MembersChange after its status
// line, v2 emits the delta before it) is not a behaviour difference.
func sortFacts(fs []Fact) []Fact {
	out := dedupeRepaints(fs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// dedupeRepaints collapses repeated "this buffer's roster changed" facts inside
// one group. Both cores can emit several for one line — v1 fires MembersChange
// on the JOIN and again on RPL_ENDOFNAMES, v2 emits the join delta and then the
// NAMES reset — and how many arrive is not a behaviour difference: the
// dispatcher coalesces on its 80ms tick and repaints once (blueprint §5.3).
// That the roster changed at all is compared; so is the roster itself, in the
// state snapshot.
func dedupeRepaints(fs []Fact) []Fact {
	seen := map[string]bool{}
	out := make([]Fact, 0, len(fs))
	for _, f := range fs {
		if f.Kind == kindMembers {
			if seen[f.Buffer] {
				continue
			}
			seen[f.Buffer] = true
		}
		out = append(out, f)
	}
	return out
}
