package proto

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBatchTypeRegistry(t *testing.T) {
	cases := []struct {
		name string
		want BatchType
	}{
		{"chathistory", BatchType{Name: "chathistory", Playback: true, Params: "target", Known: true}},
		{"draft/chathistory", BatchType{Name: "draft/chathistory", Playback: true, Params: "target", Known: true}},
		{"draft/multiline", BatchType{Name: "draft/multiline", Aggregate: true, Params: "target", Known: true}},
		{"multiline", BatchType{Name: "multiline", Aggregate: true, Params: "target", Known: true}},
		{"netsplit", BatchType{Name: "netsplit", BulkMembership: true, Params: "server1 server2", Known: true}},
		{"netjoin", BatchType{Name: "netjoin", BulkMembership: true, Params: "server1 server2", Known: true}},
		{"labeled-response", BatchType{Name: "labeled-response", Transparent: true, Known: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LookupBatchType(tc.name)
			if !ok {
				t.Fatalf("%q is not registered", tc.name)
			}
			if got != tc.want {
				t.Errorf("LookupBatchType(%q) = %+v, want %+v", tc.name, got, tc.want)
			}
			if BatchTypeFor(tc.name) != tc.want {
				t.Errorf("BatchTypeFor disagrees with LookupBatchType for %q", tc.name)
			}
		})
	}
}

// TestUnknownBatchTypeIsLivePassthrough: the spec requires that a batch type a
// client does not understand be processed as if the batch were not there.
func TestUnknownBatchTypeIsLivePassthrough(t *testing.T) {
	got := BatchTypeFor("znc.in/some-extension")
	if got.Known {
		t.Error("an unregistered type must report Known false")
	}
	if got.Playback || got.Aggregate || got.BulkMembership {
		t.Errorf("unknown batch must carry no semantics: %+v", got)
	}
	if got.Name != "znc.in/some-extension" {
		t.Errorf("Name = %q", got.Name)
	}
	if _, ok := LookupBatchType("znc.in/some-extension"); ok {
		t.Error("LookupBatchType claimed to know it")
	}
}

// TestBatchSemanticsAreExclusive: no registered type may claim two roles, since
// the store branches on them independently.
func TestBatchSemanticsAreExclusive(t *testing.T) {
	for _, bt := range KnownBatchTypes() {
		n := 0
		for _, on := range []bool{bt.Playback, bt.Aggregate, bt.BulkMembership, bt.Transparent} {
			if on {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%q claims %d roles, want exactly 1: %+v", bt.Name, n, bt)
		}
		if !bt.Known {
			t.Errorf("%q from KnownBatchTypes has Known false", bt.Name)
		}
	}
}

func TestKnownBatchTypesIsSortedAndFresh(t *testing.T) {
	a := KnownBatchTypes()
	if len(a) == 0 {
		t.Fatal("registry is empty")
	}
	for i := 1; i < len(a); i++ {
		if a[i-1].Name >= a[i].Name {
			t.Errorf("not sorted at %d: %q >= %q", i, a[i-1].Name, a[i].Name)
		}
	}
	a[0].Name = "mutated"
	if KnownBatchTypes()[0].Name == "mutated" {
		t.Error("KnownBatchTypes exposes the registry")
	}
	// And a value handed out cannot reach back into the table.
	bt, _ := LookupBatchType("chathistory")
	bt.Playback = false
	if again, _ := LookupBatchType("chathistory"); !again.Playback {
		t.Error("mutating a returned BatchType changed the registry")
	}
}

func TestBatchTrackerOpenClose(t *testing.T) {
	tr := NewBatchTracker()

	b, err := tr.Open("1", "chathistory", []string{"#go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Type.Playback || b.Target() != "#go" || b.Depth != 0 {
		t.Errorf("opened batch = %+v", b)
	}
	if tr.Len() != 1 {
		t.Errorf("Len = %d", tr.Len())
	}
	if got, ok := tr.Lookup("1"); !ok || got.Ref != "1" {
		t.Errorf("Lookup = %+v, %v", got, ok)
	}

	closed, err := tr.Close("1")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Ref != "1" || closed.Target() != "#go" {
		t.Errorf("closed batch = %+v", closed)
	}
	if tr.Len() != 0 {
		t.Errorf("Len after close = %d", tr.Len())
	}
	if _, ok := tr.Lookup("1"); ok {
		t.Error("a closed batch is still open")
	}
}

func TestBatchTrackerErrors(t *testing.T) {
	tr := NewBatchTracker()
	if _, err := tr.Open("", "chathistory", nil, ""); err == nil {
		t.Error("an empty reference must be rejected")
	}
	if _, err := tr.Open("1", "chathistory", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Open("1", "netjoin", nil, ""); !errors.Is(err, ErrDuplicateBatch) {
		t.Errorf("duplicate open err = %v, want ErrDuplicateBatch", err)
	}
	if _, err := tr.Close("99"); !errors.Is(err, ErrUnknownBatch) {
		t.Errorf("unknown close err = %v, want ErrUnknownBatch", err)
	}
	// The duplicate must not have replaced the original.
	if b, _ := tr.Lookup("1"); !b.Type.Playback {
		t.Error("a rejected duplicate overwrote the open batch")
	}
}

// TestBatchTrackerNesting is the case v1 could not express at all: a live
// multiline message inside a chathistory replay. Nesting resolves it; the
// two-minute wall-clock heuristic did not.
func TestBatchTrackerNesting(t *testing.T) {
	tr := NewBatchTracker()
	if _, err := tr.Open("h", "chathistory", []string{"#go"}, ""); err != nil {
		t.Fatal(err)
	}
	inner, err := tr.Open("m", "draft/multiline", []string{"#go"}, "h")
	if err != nil {
		t.Fatal(err)
	}
	if inner.Depth != 1 || inner.Parent != "h" {
		t.Errorf("nested batch = %+v", inner)
	}

	// A line inside the inner batch is playback, because its chain contains a
	// chathistory batch.
	if !tr.IsPlayback("m") {
		t.Error("a multiline batch inside chathistory is playback")
	}
	if !tr.IsPlayback("h") {
		t.Error("the chathistory batch itself is playback")
	}
	if agg, ok := tr.Aggregating("m"); !ok || agg.Ref != "m" {
		t.Errorf("Aggregating = %+v, %v", agg, ok)
	}

	chain := tr.Chain("m")
	if len(chain) != 2 || chain[0].Ref != "m" || chain[1].Ref != "h" {
		t.Errorf("Chain = %+v, want [m h] innermost first", chain)
	}

	// The same multiline batch outside any history is live.
	tr2 := NewBatchTracker()
	tr2.Open("m", "draft/multiline", []string{"#go"}, "")
	if tr2.IsPlayback("m") {
		t.Error("a standalone multiline batch is live, not playback")
	}
	if agg, ok := tr2.Aggregating("m"); !ok || agg.Ref != "m" {
		t.Errorf("standalone Aggregating = %+v, %v", agg, ok)
	}
}

func TestBatchTrackerSemanticQueries(t *testing.T) {
	tr := NewBatchTracker()
	tr.Open("ns", "netsplit", []string{"a.example", "b.example"}, "")
	tr.Open("lr", "labeled-response", nil, "")
	tr.Open("un", "vendor/unknown", nil, "")

	if !tr.IsBulkMembership("ns") {
		t.Error("netsplit is bulk membership churn")
	}
	if tr.IsPlayback("ns") {
		t.Error("netsplit is not playback")
	}
	if tr.IsPlayback("lr") || tr.IsBulkMembership("lr") {
		t.Error("labeled-response carries no semantics of its own")
	}
	if tr.IsPlayback("un") || tr.IsBulkMembership("un") {
		t.Error("an unknown batch carries no semantics")
	}
	if _, ok := tr.Aggregating("ns"); ok {
		t.Error("netsplit does not aggregate")
	}

	// Bulk membership propagates to nested batches too.
	tr.Open("inner", "vendor/unknown", nil, "ns")
	if !tr.IsBulkMembership("inner") {
		t.Error("nesting inside a netsplit is still bulk membership")
	}
}

// TestBatchTrackerUnknownRef: lines with no batch tag, and lines quoting a
// batch we never saw open, must both come out as plain live traffic rather
// than panicking or looping.
func TestBatchTrackerUnknownRef(t *testing.T) {
	tr := NewBatchTracker()
	for _, ref := range []string{"", "never-opened"} {
		if tr.IsPlayback(ref) || tr.IsBulkMembership(ref) {
			t.Errorf("ref %q should carry no semantics", ref)
		}
		if got := tr.Chain(ref); got != nil {
			t.Errorf("Chain(%q) = %+v, want nil", ref, got)
		}
		if _, ok := tr.Aggregating(ref); ok {
			t.Errorf("Aggregating(%q) claimed a batch", ref)
		}
	}
}

// TestBatchTrackerUnseenParent: a batch that claims a parent we never saw open
// stays usable, but does not invent a chain through the missing one.
func TestBatchTrackerUnseenParent(t *testing.T) {
	tr := NewBatchTracker()
	b, err := tr.Open("x", "draft/multiline", []string{"#go"}, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if b.Parent != "" || b.Depth != 0 {
		t.Errorf("batch with a missing parent = %+v", b)
	}
	if tr.IsPlayback("x") {
		t.Error("a missing parent must not be assumed to be playback")
	}
	if len(tr.Chain("x")) != 1 {
		t.Errorf("Chain = %+v, want just the batch itself", tr.Chain("x"))
	}
}

// TestBatchTrackerCycleIsBounded: a hostile or buggy server must not be able to
// hang the client with a self-referencing batch chain.
func TestBatchTrackerCycleIsBounded(t *testing.T) {
	tr := NewBatchTracker()
	tr.Open("a", "chathistory", nil, "")
	tr.Open("b", "vendor/x", nil, "a")
	// Forge a cycle: b's parent becomes itself.
	forced := tr.open["b"]
	forced.Parent = "b"
	tr.open["b"] = forced

	done := make(chan []OpenBatch, 1)
	go func() { done <- tr.Chain("b") }()
	select {
	case chain := <-done:
		if len(chain) != 1 {
			t.Errorf("Chain = %+v, want the cycle broken after one hop", chain)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chain did not terminate on a cyclic parent chain")
	}
}

func TestBatchTrackerReset(t *testing.T) {
	tr := NewBatchTracker()
	tr.Open("1", "chathistory", nil, "")
	tr.Open("2", "netjoin", nil, "")
	tr.Reset()
	if tr.Len() != 0 {
		t.Errorf("Len after Reset = %d", tr.Len())
	}
	// A ref may be reused after a reset, since batch refs are per-connection.
	if _, err := tr.Open("1", "netsplit", nil, ""); err != nil {
		t.Errorf("reopening a ref after Reset: %v", err)
	}
}

func TestOpenBatchParamsAreCopied(t *testing.T) {
	tr := NewBatchTracker()
	params := []string{"#go"}
	b, _ := tr.Open("1", "chathistory", params, "")
	params[0] = "#mutated"
	if b.Target() != "#go" {
		t.Error("Open kept the caller's slice instead of copying it")
	}
	if again, _ := tr.Lookup("1"); again.Target() != "#go" {
		t.Error("the stored batch aliased the caller's slice")
	}
}

func TestOpenBatchTargetWithNoParams(t *testing.T) {
	tr := NewBatchTracker()
	b, _ := tr.Open("1", "labeled-response", nil, "")
	if b.Target() != "" {
		t.Errorf("Target with no params = %q", b.Target())
	}
}

func TestBatchTypeNamesAreCanonical(t *testing.T) {
	for _, bt := range KnownBatchTypes() {
		if strings.TrimSpace(bt.Name) != bt.Name || bt.Name == "" {
			t.Errorf("bad registry key %q", bt.Name)
		}
		if got, _ := LookupBatchType(bt.Name); got.Name != bt.Name {
			t.Errorf("%q does not round-trip through the registry", bt.Name)
		}
	}
}
