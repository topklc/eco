package proto

import (
	"errors"
	"fmt"
)

// OpenBatch is one BATCH the server has opened and not yet closed.
type OpenBatch struct {
	// Ref is the batch reference tag, unique within the connection while open.
	Ref string
	// Type is the registry entry for the batch's type.
	Type BatchType
	// Params are the batch's parameters, as sent.
	Params []string
	// Parent is the ref of the batch this one was opened inside, "" at the top
	// level. Nesting is what makes "multiline inside chathistory" resolvable.
	Parent string
	// Depth is 0 for a top-level batch, 1 for one nested inside it, and so on.
	Depth int
}

// Target returns the batch's first parameter, which for chathistory and
// multiline batches is the buffer it concerns.
func (b OpenBatch) Target() string {
	if len(b.Params) == 0 {
		return ""
	}
	return b.Params[0]
}

var (
	// ErrDuplicateBatch is returned when a server opens a batch whose ref is
	// already open.
	ErrDuplicateBatch = errors.New("proto: batch reference already open")
	// ErrUnknownBatch is returned when a server closes a batch that is not open.
	ErrUnknownBatch = errors.New("proto: batch reference is not open")
)

// BatchTracker is the open-batch registry: pure state, no clock, no goroutine.
// It answers the question every incoming line has to ask — "am I inside a
// playback batch?" — by walking the enclosing chain, which is the correct
// version of v1's isPlayback() guess.
//
// The zero value is not usable; build one with NewBatchTracker.
type BatchTracker struct {
	open map[string]OpenBatch
}

// NewBatchTracker returns an empty tracker.
func NewBatchTracker() *BatchTracker {
	return &BatchTracker{open: map[string]OpenBatch{}}
}

// Reset drops every open batch. Call it when a connection ends: an unclosed
// batch does not survive it.
func (t *BatchTracker) Reset() { t.open = map[string]OpenBatch{} }

// Len returns how many batches are open.
func (t *BatchTracker) Len() int { return len(t.open) }

// Open records "BATCH +ref type params...". parent is the batch tag carried by
// the BATCH message itself, "" at the top level. It returns the resulting
// OpenBatch, whose Type is resolved through the registry and whose Depth is set
// from the parent chain.
func (t *BatchTracker) Open(ref, typ string, params []string, parent string) (OpenBatch, error) {
	if ref == "" {
		return OpenBatch{}, fmt.Errorf("proto: BATCH open with empty reference")
	}
	if _, dup := t.open[ref]; dup {
		return OpenBatch{}, fmt.Errorf("%w: %q", ErrDuplicateBatch, ref)
	}
	b := OpenBatch{Ref: ref, Type: BatchTypeFor(typ), Parent: parent}
	if len(params) > 0 {
		b.Params = append([]string(nil), params...)
	}
	if parent != "" {
		if p, ok := t.open[parent]; ok {
			b.Depth = p.Depth + 1
		} else {
			// A parent we never saw open: keep the batch usable rather than
			// rejecting it, but do not invent a chain through it.
			b.Parent = ""
		}
	}
	t.open[ref] = b
	return b, nil
}

// Close records "BATCH -ref" and returns the batch that closed.
func (t *BatchTracker) Close(ref string) (OpenBatch, error) {
	b, ok := t.open[ref]
	if !ok {
		return OpenBatch{}, fmt.Errorf("%w: %q", ErrUnknownBatch, ref)
	}
	delete(t.open, ref)
	return b, nil
}

// Lookup returns an open batch by ref.
func (t *BatchTracker) Lookup(ref string) (OpenBatch, bool) {
	b, ok := t.open[ref]
	return b, ok
}

// Chain returns the enclosing batches from ref outward: the batch itself first,
// then its parent, and so on. An unknown ref yields nil. The walk is bounded by
// the number of open batches, so a server that somehow produced a cycle cannot
// hang the client.
func (t *BatchTracker) Chain(ref string) []OpenBatch {
	var out []OpenBatch
	seen := make(map[string]bool, len(t.open))
	for ref != "" && !seen[ref] {
		seen[ref] = true
		b, ok := t.open[ref]
		if !ok {
			break
		}
		out = append(out, b)
		ref = b.Parent
	}
	return out
}

// IsPlayback reports whether anything in ref's enclosing chain is a playback
// batch. This is the single source of truth for "is this line history?" —
// replacing the v1 rule "the line has a batch tag, so it must be history",
// which mislabelled every live multiline message.
func (t *BatchTracker) IsPlayback(ref string) bool {
	for _, b := range t.Chain(ref) {
		if b.Type.Playback {
			return true
		}
	}
	return false
}

// IsBulkMembership reports whether anything in ref's enclosing chain is a
// netsplit/netjoin batch, so member churn can be collapsed in the UI.
func (t *BatchTracker) IsBulkMembership(ref string) bool {
	for _, b := range t.Chain(ref) {
		if b.Type.BulkMembership {
			return true
		}
	}
	return false
}

// Aggregating returns the innermost enclosing batch that assembles its contents
// into one message (draft/multiline), and whether there is one.
func (t *BatchTracker) Aggregating(ref string) (OpenBatch, bool) {
	for _, b := range t.Chain(ref) {
		if b.Type.Aggregate {
			return b, true
		}
	}
	return OpenBatch{}, false
}
