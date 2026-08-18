package parity

import (
	"fmt"
	"strings"
	"sync"
)

// The differ. Both cores' facts are compared exactly; a fact with no
// counterpart is a failure unless an entry in divergences explains it, and an
// entry that explains one is counted so the list itself can be tested.

// Side names which core produced a fact.
type Side int

const (
	SideV1 Side = iota
	SideV2
)

func (s Side) String() string {
	if s == SideV1 {
		return "v1"
	}
	return "v2"
}

// Divergence is one sanctioned difference between the cores: a fact one side
// produces and the other deliberately does not.
type Divergence struct {
	// ID is the stable name used in reports and in the blueprint's M6 entry.
	ID string
	// Reason says why this difference is correct. It is not decoration: an
	// entry nobody can justify in one sentence is a regression in disguise.
	Reason string
	// Match reports whether this entry explains an unmatched fact.
	Match func(side Side, f Fact) bool
	// Where, when set, restricts the entry to diffs whose label it accepts —
	// the transcript step for a stream group, "state: log of <buffer>" for a
	// log. It is how an entry stays narrow: "v1 rebuilds the roster after a
	// MODE" must not also forgive a member change v2 forgot everywhere else.
	Where func(label string) bool
}

// fired counts which divergence entries actually explained something, across
// every scenario in the package. TestParity fails on an entry that never fired:
// the list may only contain differences that are real today.
var fired = struct {
	sync.Mutex
	n map[string]int
}{n: map[string]int{}}

func markFired(id string) {
	fired.Lock()
	fired.n[id]++
	fired.Unlock()
}

func firedCount(id string) int {
	fired.Lock()
	defer fired.Unlock()
	return fired.n[id]
}

// explain returns the id of the entry that sanctions this fact, or "".
func explain(where string, side Side, f Fact) string {
	for _, d := range divergences {
		if d.Where != nil && !d.Where(where) {
			continue
		}
		if d.Match(side, f) {
			markFired(d.ID)
			return d.ID
		}
	}
	return ""
}

// mismatch is one place the two cores disagreed with nothing to explain it.
type mismatch struct {
	where string
	v1    []Fact
	v2    []Fact
}

func (m mismatch) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", m.where)
	for _, f := range m.v1 {
		fmt.Fprintf(&b, "    %s only: %s\n", SideV1, f)
	}
	for _, f := range m.v2 {
		fmt.Fprintf(&b, "    %s only: %s\n", SideV2, f)
	}
	return b.String()
}

// diffSorted compares two canonically sorted fact lists. Sorted input makes the
// merge exact: a fact is missing from one side only when the merge walks past
// it, never because a greedy alignment guessed wrong.
func diffSorted(where string, a, b []Fact) *mismatch {
	m := mismatch{where: where}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case j >= len(b) || (i < len(a) && a[i].String() < b[j].String()):
			if explain(where, SideV1, a[i]) == "" {
				m.v1 = append(m.v1, a[i])
			}
			i++
		default:
			if explain(where, SideV2, b[j]) == "" {
				m.v2 = append(m.v2, b[j])
			}
			j++
		}
	}
	if len(m.v1) == 0 && len(m.v2) == 0 {
		return nil
	}
	return &m
}

// diffOrdered compares two lists where the order is part of the contract — a
// buffer's log. It walks both, skipping whatever the divergence list explains,
// and reports the first genuine disagreement at each position.
func diffOrdered(where string, a, b []Fact) *mismatch {
	m := mismatch{where: where}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i, j = i+1, j+1
			continue
		}
		if i < len(a) && explain(where, SideV1, a[i]) != "" {
			i++
			continue
		}
		if j < len(b) && explain(where, SideV2, b[j]) != "" {
			j++
			continue
		}
		if i < len(a) {
			m.v1 = append(m.v1, a[i])
			i++
		}
		if j < len(b) {
			m.v2 = append(m.v2, b[j])
			j++
		}
	}
	if len(m.v1) == 0 && len(m.v2) == 0 {
		return nil
	}
	return &m
}

// compare runs both halves of the gate over one scenario's two runs.
func compare(name string, v1, v2 coreRun) []mismatch {
	var out []mismatch

	// The stream: what each core told the UI, step by step.
	for i := 0; i < len(v1.groups) || i < len(v2.groups); i++ {
		var av, bv []Fact
		where := ""
		if i < len(v1.groups) {
			av, where = v1.groups[i].facts, v1.groups[i].label
		}
		if i < len(v2.groups) {
			bv = v2.groups[i].facts
			if where == "" {
				where = v2.groups[i].label
			} else if v2.groups[i].label != where {
				out = append(out, mismatch{where: fmt.Sprintf(
					"%s: the two runs took different steps (v1 %q, v2 %q)",
					name, where, v2.groups[i].label)})
				continue
			}
		}
		if m := diffSorted(fmt.Sprintf("%s/stream %s", name, where), av, bv); m != nil {
			out = append(out, *m)
		}
	}

	// The state: where each core ended up.
	out = append(out, compareState(name, v1.state, v2.state)...)
	return out
}

// compareState diffs the final buffer state of the two cores.
func compareState(name string, a, b []bufferSnap) []mismatch {
	var out []mismatch
	byName := func(s []bufferSnap) map[string]bufferSnap {
		m := make(map[string]bufferSnap, len(s))
		for _, v := range s {
			m[v.Name] = v
		}
		return m
	}
	// Every diff label starts with the scenario name, so a divergence entry can
	// be scoped to the one scenario it belongs to.
	prefix := name
	am, bm := byName(a), byName(b)
	for _, bufname := range unionNames(a, b) {
		av, inA := am[bufname]
		bv, inB := bm[bufname]
		switch {
		case !inB:
			out = append(out, mismatch{where: prefix + "/state: buffer " + bufname + " exists only in v1",
				v1: append([]Fact{{Kind: kindBuffer, Buffer: bufname, Text: av.header()}}, av.Lines...)})
			continue
		case !inA:
			out = append(out, mismatch{where: prefix + "/state: buffer " + bufname + " exists only in v2",
				v2: append([]Fact{{Kind: kindBuffer, Buffer: bufname, Text: bv.header()}}, bv.Lines...)})
			continue
		}
		if av.header() != bv.header() {
			out = append(out, mismatch{
				where: prefix + "/state: buffer " + bufname,
				v1:    []Fact{{Kind: kindBuffer, Buffer: bufname, Text: av.header()}},
				v2:    []Fact{{Kind: kindBuffer, Buffer: bufname, Text: bv.header()}},
			})
		}
		if m := diffOrdered(prefix+"/state: log of "+bufname, av.Lines, bv.Lines); m != nil {
			out = append(out, *m)
		}
	}
	return out
}

func unionNames(a, b []bufferSnap) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range [][]bufferSnap{a, b} {
		for _, v := range s {
			if !seen[v.Name] {
				seen[v.Name] = true
				out = append(out, v.Name)
			}
		}
	}
	return out
}

// report renders the mismatches for a test failure.
func report(name string, ms []mismatch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "scenario %q: the two cores disagree in %d place(s) "+
		"that the divergence list does not cover\n", name, len(ms))
	for _, m := range ms {
		b.WriteString(m.String())
	}
	b.WriteString("\nEither the new core is wrong (v1 is the reference), or the\n" +
		"difference is deliberate — in which case add an entry to divergences\n" +
		"in divergence_test.go with the reason, and record it in the blueprint.\n")
	return b.String()
}
