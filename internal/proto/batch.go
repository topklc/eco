package proto

import "sort"

// BatchType is what eco knows about one IRCv3 BATCH type. It is the answer to
// the two questions v1 never asked - "is this playback?" and "does this mutate
// members?" - as data rather than as the guess session.go made from the mere
// presence of a batch tag.
//
// BatchType is a comparable value with no reference fields, so handing one out
// cannot let a caller mutate the registry.
type BatchType struct {
	// Name is the batch type as it appears on the wire.
	Name string

	// Playback marks a batch whose contents are replayed history, not live
	// traffic. Messages inside must not raise notifications, must not move the
	// unread marker forward, and must be merged by msgid rather than appended.
	// Resolved through the whole enclosing chain: a multiline batch nested
	// inside a chathistory batch is playback.
	Playback bool

	// Aggregate marks a batch whose enclosed messages are parts of one logical
	// message, assembled at BATCH close and emitted once. This replaces v1's
	// incremental last-line mutation and its two-minute wall-clock recency
	// heuristic, both of which existed only because live and replayed multiline
	// were indistinguishable without batch types.
	Aggregate bool

	// BulkMembership marks a batch whose JOIN/QUIT contents are server churn
	// rather than people acting: a netsplit or netjoin. Member lists still
	// change, but the UI collapses the whole batch into one line instead of a
	// hundred.
	BulkMembership bool

	// Transparent marks a batch with no semantics of its own; it exists to
	// group a reply. Contents are processed exactly as if the batch were not
	// there, and correlation happens on the label instead.
	Transparent bool

	// Params documents the batch type's parameters, for diagnostics only.
	Params string

	// Known is false for a type absent from the registry. An unknown batch is
	// treated as transparent live traffic - the spec's required behaviour.
	Known bool
}

// batchTypes is the registry. It is package-private and never mutated after
// init; BatchType is a value type, so lookups cannot reach back into it.
var batchTypes = map[string]BatchType{
	"chathistory": {
		Name:     "chathistory",
		Playback: true,
		Params:   "target",
	},
	// Some deployments still emit the draft name; identical semantics.
	"draft/chathistory": {
		Name:     "draft/chathistory",
		Playback: true,
		Params:   "target",
	},
	"draft/multiline": {
		Name:      "draft/multiline",
		Aggregate: true,
		Params:    "target",
	},
	"multiline": {
		Name:      "multiline",
		Aggregate: true,
		Params:    "target",
	},
	"netsplit": {
		Name:           "netsplit",
		BulkMembership: true,
		Params:         "server1 server2",
	},
	"netjoin": {
		Name:           "netjoin",
		BulkMembership: true,
		Params:         "server1 server2",
	},
	"labeled-response": {
		Name:        "labeled-response",
		Transparent: true,
	},
}

// LookupBatchType returns the registry entry for a batch type and whether it is
// known.
func LookupBatchType(name string) (BatchType, bool) {
	t, ok := batchTypes[name]
	if ok {
		t.Known = true
	}
	return t, ok
}

// BatchTypeFor returns the registry entry for a batch type, or an entry with
// Known false for one eco does not model. The zero-valued semantics of that
// entry - not playback, not aggregate, no membership meaning - are exactly the
// required "pass unknown batches through as live" behaviour, so callers can use
// the result without checking.
func BatchTypeFor(name string) BatchType {
	if t, ok := batchTypes[name]; ok {
		t.Known = true
		return t
	}
	return BatchType{Name: name}
}

// KnownBatchTypes returns every registered type, sorted by name. It exists for
// tests and diagnostics; it allocates a fresh slice each call.
func KnownBatchTypes() []BatchType {
	out := make([]BatchType, 0, len(batchTypes))
	for _, t := range batchTypes {
		t.Known = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
