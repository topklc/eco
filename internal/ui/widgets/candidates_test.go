package widgets

import (
	"strconv"
	"testing"
)

// The completion strip (§4.3's visible candidate popup). It decides one thing -
// which slice of a long list is on screen - and that one thing is here.

func TestCandidatesShowAndClear(t *testing.T) {
	testApp(t)
	c := NewCandidates()
	if c.Visible() {
		t.Fatal("a fresh strip is on screen")
	}
	c.Set([]string{"alice", "alfred"}, 1)
	if !c.Visible() {
		t.Error("the strip did not appear for a cycle")
	}
	if got := c.Selected(); got != 1 {
		t.Errorf("Selected = %d, want 1", got)
	}
	c.Clear()
	if c.Visible() || len(c.Items()) != 0 {
		t.Error("Clear left the strip on screen")
	}
	// an empty list is a clear, so the composer has one way to end a cycle
	c.Set([]string{"alice"}, 0)
	c.Set(nil, 0)
	if c.Visible() {
		t.Error("setting an empty list left the strip up")
	}
}

// A cycle longer than the strip scrolls WITH the selection: stepping past the
// last visible candidate moves the window rather than running the highlight off
// the end of it, and the count of what is hidden is honest.
func TestCandidatesWindowFollowsTheSelection(t *testing.T) {
	testApp(t)
	c := NewCandidates()
	var many []string
	for i := range maxCandidatesShown + 4 {
		many = append(many, "nick"+strconv.Itoa(i))
	}

	c.Set(many, 0)
	shown, from, hidden := c.window()
	if from != 0 || len(shown) != maxCandidatesShown {
		t.Errorf("at the start the window is [%d..%d), want the first %d", from, from+len(shown), maxCandidatesShown)
	}
	if hidden != len(many)-maxCandidatesShown {
		t.Errorf("hidden = %d, want %d", hidden, len(many)-maxCandidatesShown)
	}

	last := len(many) - 1
	c.Set(many, last)
	shown, from, _ = c.window()
	if from+len(shown) != len(many) {
		t.Errorf("at the end the window is [%d..%d), want it to reach %d", from, from+len(shown), len(many))
	}
	if shown[len(shown)-1] != many[last] {
		t.Errorf("the selected candidate is not in the window: %v", shown)
	}

	// a short list is shown whole, with nothing counted
	c.Set(many[:3], 2)
	shown, from, hidden = c.window()
	if from != 0 || len(shown) != 3 || hidden != 0 {
		t.Errorf("a short list windowed to [%d..%d) with %d hidden", from, from+len(shown), hidden)
	}
}
