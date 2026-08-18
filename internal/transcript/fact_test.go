package transcript

import (
	"sort"
	"strconv"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
)

// Fact is the canonical form a run is normalised onto: one user-visible thing
// that happened, stripped of everything that cannot be held to account.
//
// Time is deliberately absent, and so is the store's MessageID — both are
// bookkeeping, and pinning them would make every record a hostage to an id
// counter. What is left is what a user could notice: which buffer, what kind of
// thing, who said it, what it said, and the flags that drive rendering and
// attention.
type Fact struct {
	Kind   string
	Buffer string // folded buffer name; "server" is the network's status buffer
	Nick   string
	Text   string
	Flags  string // sorted, comma separated
}

// The canonical kinds. The normaliser may only produce these; an unmapped
// Change records itself as "unmapped:<name>" so a new one cannot slip into a
// record unnoticed.
const (
	kindMsg       = "msg"       // a chat message reached a buffer
	kindStatus    = "status"    // a client-generated status line reached a buffer
	kindTombstone = "tombstone" // what a redacted message left behind
	kindRail      = "rail"      // the buffer list changed
	kindMembers   = "members"   // a buffer's roster, as it now stands
	kindTopic     = "topic"     // a buffer's topic changed
	kindRename    = "rename"    // a buffer's identity moved
	kindRedact    = "redact"    // a message was withdrawn
	kindJoinFail  = "joinfail"  // a JOIN was refused, and the UI must say so
	kindConn      = "conn"      // the connection state moved
	kindSTS       = "sts"       // an STS policy was advertised
	kindListDone  = "listdone"  // a channel scan finished
	kindList      = "list"      // LIST rows were handed over
	kindResolve   = "resolve"   // a pending message was confirmed by its echo
	kindMsgFail   = "msgfail"   // the server rejected a message we sent
	kindHistory   = "history"   // a chathistory batch closed
	kindSelf      = "self"      // our own identity on the network changed
	kindAuth      = "auth"      // SASL settled
	kindUnread    = "unread"    // a buffer's unread counters moved
	kindMarker    = "marker"    // a read marker moved
	kindTyping    = "typing"    // someone's typing state changed
	kindMode      = "mode"      // a channel's mode state changed
	kindBuffer    = "buffer"    // a state snapshot's header line, for reports only
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

// serverBufferName is what the network's status buffer is called in canonical
// form; the store gives it the empty folded key.
const serverBufferName = "server"

// foldName is the suite's one folding rule, used for the names a transcript
// spells out (a refused channel, say). internal/testsrv advertises
// CASEMAPPING=ascii, under which it agrees exactly with proto.Fold — a
// transcript that advertised rfc1459 would fold "[]\" differently here than in
// the core, and the disagreement would show up in the record rather than hide.
func foldName(name string) string {
	if name == "" {
		return serverBufferName
	}
	return strings.ToLower(name)
}

// bufName is foldName for an already-folded BufferID.
func bufName(id model.BufferID) string {
	if id.Folded == "" {
		return serverBufferName
	}
	return id.Folded
}

// rosterText renders a member list as a members fact's payload. A members fact
// says what the roster IS, not merely that something touched it: an opaque "the
// roster changed" ping is fungible, two fungible facts cancel out in any diff,
// and a core that silently stopped reporting who joined would read as no change
// at all. The mutation sweep found exactly that hole and this is the fix.
func rosterText(members []string) string {
	if len(members) == 0 {
		return "(empty)"
	}
	return strings.Join(members, ",")
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

// --- normalising a run ---------------------------------------------------------

// factFromMessage normalises one store Message.
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

// factsFromChange normalises one Change. Every Change produces a fact: nothing
// is dropped here, because a normaliser that decides what does not matter is a
// normaliser that can be taught to overlook a regression.
//
// roster answers "what is this buffer's member list now" — a MembersChanged
// carries a delta, and a delta is not what a core can be held to, since the same
// room can be reached by a delta or by a NAMES reset. The resulting roster is.
func factsFromChange(c model.Change, roster func(model.BufferID) string) []Fact {
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
		// The chunk is one fact, and so is every message inside it: a record
		// that said only "history arrived" would not notice it arriving empty.
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
		return []Fact{{Kind: kindMembers, Buffer: buf, Text: roster(meta.Buffer)}}
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

func factsFromChanges(cs []model.Change, roster func(model.BufferID) string) []Fact {
	var out []Fact
	for _, c := range cs {
		out = append(out, factsFromChange(c, roster)...)
	}
	return out
}

// --- normalisation ------------------------------------------------------------

// sortFacts orders a group's facts canonically. A group holds one transcript
// step's worth of consequences, and the order two facts about the same line come
// out in is not behaviour — the dispatcher coalesces them into one repaint
// (§5.3). Sorting is what stops a record from pinning it.
func sortFacts(fs []Fact) []Fact {
	out := append([]Fact(nil), fs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// collapseRosters reduces a step's roster traffic to the question a user can
// answer by looking at the screen: after this step, who is in the room?
//
//   - within a step, only the last roster for a buffer survives. A join burst
//     emits several — the join delta, then the NAMES reset — and the
//     intermediate ones are never drawn.
//   - a roster identical to the one already on screen is dropped, so a redundant
//     republish records as the no-op it is.
//
// Both are reductions the dispatcher performs for real, and both keep the
// PAYLOAD: what is dropped is a repeat, never the content. last carries across
// steps and belongs to one run.
func collapseRosters(fs []Fact, last map[string]string) []Fact {
	newest := map[string]int{}
	for i, f := range fs {
		if f.Kind == kindMembers {
			newest[f.Buffer] = i
		}
	}
	out := make([]Fact, 0, len(fs))
	for i, f := range fs {
		if f.Kind == kindMembers {
			if newest[f.Buffer] != i {
				continue
			}
			if last[f.Buffer] == f.Text {
				continue
			}
			last[f.Buffer] = f.Text
		}
		out = append(out, f)
	}
	return out
}
