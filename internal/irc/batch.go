package irc

import (
	"strings"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// The two batch types eco does not simply pass through.
//
//   - An aggregate batch (draft/multiline) is one logical message split over
//     several lines. Its parts are collected and emitted as a single event at
//     BATCH close. v1 instead appended each part onto the previously rendered
//     line and used a two-minute wall-clock window to guess whether the batch
//     was live or replayed; both are gone.
//   - A playback batch (chathistory) is replayed history. Its contents are
//     collected and delivered as one HistoryChunk at close, so the store
//     prepends a block instead of interleaving history with live traffic.
//
// Everything else — netsplit, netjoin, labeled-response, anything unknown —
// flows through live with its batch ref on each event, which is what the spec
// requires of an unrecognised batch type.
type collectKind uint8

const (
	collectHistory collectKind = iota
	collectAggregate
)

// maxBatchEvents bounds what one batch may accumulate before eco stops
// holding it. A batch is server-controlled and unbounded on the wire, so
// collecting it is an invitation for a hostile server to grow the client's
// heap; past the bound the remaining contents stream through live instead and
// the resulting chunk reports itself incomplete. It is a var so a test can
// exercise the overflow path without sending ten thousand lines.
var maxBatchEvents = 8192

// maxOpenBatches bounds how many batches may be open at once. A reconnect
// gap-fill can legitimately have one chathistory batch per channel in flight,
// so the cap is well above anything real; it exists so a server that opens
// batches and never closes them cannot grow the registry forever.
const maxOpenBatches = 256

// collector accumulates the contents of one open batch.
type collector struct {
	ref   string
	kind  collectKind
	batch proto.OpenBatch
	// meta is the BATCH command's own envelope. For multiline this is where
	// the msgid and the label live — the parts carry neither — so the merged
	// message inherits its identity from here.
	meta   model.EventMeta
	events []model.Event
	// overflow records that the batch outgrew maxBatchEvents.
	overflow bool
}

// add collects one event, reporting false when the batch is full and the
// event must go out live instead.
func (col *collector) add(ev model.Event) bool {
	if len(col.events) >= maxBatchEvents {
		col.overflow = true
		return false
	}
	col.events = append(col.events, ev)
	return true
}

// multiline concatenation tags: a continuation part carrying one of these is
// joined to the previous part with no separator, which is how a single long
// line is split across the 512-byte limit.
const (
	tagConcatDraft = "draft/multiline-concat"
	tagConcat      = "multiline-concat"
)

func isConcat(m model.EventMeta) bool {
	if _, ok := m.Tag(tagConcatDraft); ok {
		return true
	}
	_, ok := m.Tag(tagConcat)
	return ok
}

// merged assembles an aggregate batch into one event. It returns false when
// the batch held no message parts at all, in which case there is nothing to
// emit and the batch was pure framing.
func (col *collector) merged(parent string, playback bool) (model.Event, bool) {
	var (
		text    strings.Builder
		first   = true
		notice  bool
		action  bool
		ctcp    string
		target  string
		sender  model.Sender
		partOne model.EventMeta
		found   bool
	)
	for _, ev := range col.events {
		var (
			body string
			meta = ev.Meta()
		)
		switch e := ev.(type) {
		case model.PrivMsg:
			if first {
				target, sender, action, ctcp = e.Target, e.Sender, e.Action, e.CTCP
			}
			body = e.Text
		case model.Notice:
			if first {
				target, sender, notice, ctcp = e.Target, e.Sender, true, e.CTCP
			}
			body = e.Text
		default:
			continue
		}
		if first {
			partOne, first, found = meta, false, true
		} else if !isConcat(meta) {
			text.WriteByte('\n')
		}
		text.WriteString(body)
	}
	if !found {
		return nil, false
	}

	meta := col.meta
	meta.Batch = parent
	meta.Playback = playback
	if meta.MsgID == "" {
		meta.MsgID = partOne.MsgID
	}
	if !meta.ServerTime && partOne.ServerTime {
		meta.Time, meta.ServerTime = partOne.Time, true
	}
	if notice {
		return model.Notice{EventMeta: meta, Target: target, Sender: sender,
			Text: text.String(), CTCP: ctcp}, true
	}
	return model.PrivMsg{EventMeta: meta, Target: target, Sender: sender,
		Text: text.String(), Action: action, CTCP: ctcp}, true
}

// chunk turns a closed playback batch into the history event. The direction
// and the limit come from the ledger entry for the request's label: a
// chathistory batch says only which target it is for, so everything else has
// to be remembered from the question.
func (col *collector) chunk(parent string, req Sent, known bool) model.HistoryChunk {
	meta := col.meta
	meta.Batch = parent
	// A history chunk is playback by definition, whatever it is nested in.
	meta.Playback = true

	dir := model.HistoryLatest
	complete := !col.overflow
	if known {
		dir = req.Direction
		// The server stops at its own limit without saying so; "we got as
		// many as we asked for" is the only available signal that more exists.
		if req.Limit > 0 && countMessages(col.events) >= req.Limit {
			complete = false
		}
	}
	return model.HistoryChunk{
		EventMeta: meta,
		Target:    col.batch.Target(),
		Direction: dir,
		Events:    col.events,
		Complete:  complete,
	}
}

// countMessages counts the events a server would have counted against a
// CHATHISTORY limit: messages, not the membership events event-playback adds.
func countMessages(evs []model.Event) int {
	n := 0
	for _, ev := range evs {
		switch ev.(type) {
		case model.PrivMsg, model.Notice:
			n++
		}
	}
	return n
}
