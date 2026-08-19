package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// The two overlay widgets as keyboards, which is the only way anybody drives
// them: focus lives in the field, so every key the panel cares about has to be
// answered there or it lands in the text box instead.

// palette builds a Palette over a fixed row set and records what it did.
type paletteHarness struct {
	*Palette
	rows      []PaletteRow
	chosen    int
	cancelled int
	queries   []string
}

func newPaletteHarness(t *testing.T, titles ...string) *paletteHarness {
	t.Helper()
	testApp(t)
	h := &paletteHarness{chosen: -1}
	for _, s := range titles {
		h.rows = append(h.rows, PaletteRow{Title: s})
	}
	h.Palette = NewPalette(PaletteOptions{
		Placeholder: "type",
		Rows:        func() []PaletteRow { return h.rows },
		Changed:     func(q string) { h.queries = append(h.queries, q) },
		Chosen:      func(i int) { h.chosen = i },
		Cancel:      func() { h.cancelled++ },
		Empty:       "nothing here",
	})
	w := test.NewWindow(h.Palette)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(500, 400))
	h.Reload()
	return h
}

func (h *paletteHarness) key(name fyne.KeyName) {
	h.Palette.entry.TypedKey(&fyne.KeyEvent{Name: name})
}

// Arrows move the selection and clamp at both ends. They deliberately do not
// wrap: a best-first list has a top, and arriving back at it by holding Down is
// disorienting.
func TestPaletteArrowsMoveAndClamp(t *testing.T) {
	h := newPaletteHarness(t, "#one", "#two", "#three")
	if h.Selected() != 0 {
		t.Fatalf("a fresh palette selects %d, want the top row", h.Selected())
	}
	h.key(fyne.KeyUp)
	if h.Selected() != 0 {
		t.Errorf("Up from the top moved to %d", h.Selected())
	}
	// One row per press. Clamping at the end hides a stride bug - three Downs
	// over three rows land on the last row whether each press moved one or two -
	// so the step is asserted before the clamp is.
	h.key(fyne.KeyDown)
	if got := h.Selected(); got != 1 {
		t.Errorf("one Down selected %d, want the next row (1)", got)
	}
	h.key(fyne.KeyDown)
	h.key(fyne.KeyDown)
	if got := h.Selected(); got != 2 {
		t.Errorf("three Downs over three rows selected %d, want the last (2)", got)
	}
	h.key(fyne.KeyUp)
	if got := h.Selected(); got != 1 {
		t.Errorf("Up from the last row selected %d, want one back (1)", got)
	}
}

// Enter takes the highlighted row, Escape closes, and neither reaches the text
// box.
func TestPaletteEnterChoosesAndEscapeCancels(t *testing.T) {
	h := newPaletteHarness(t, "#one", "#two")
	h.key(fyne.KeyDown)
	h.key(fyne.KeyReturn)
	if h.chosen != 1 {
		t.Errorf("Enter chose %d, want the highlighted row (1)", h.chosen)
	}
	h.key(fyne.KeyEscape)
	if h.cancelled != 1 {
		t.Errorf("Escape cancelled %d times, want 1", h.cancelled)
	}
	if h.Query() != "" {
		t.Errorf("a key the panel claimed reached the text box: %q", h.Query())
	}
}

// Typing tells the host and rebuilds the list, and the selection goes back to
// the top - the row Enter takes must never be one that scrolled out from under
// the answer.
func TestPaletteTypingReloadsAndResetsTheSelection(t *testing.T) {
	h := newPaletteHarness(t, "#one", "#two", "#three")
	h.key(fyne.KeyDown)
	h.key(fyne.KeyDown)

	h.rows = []PaletteRow{{Title: "#two"}}
	h.Palette.entry.SetText("two")
	if len(h.queries) != 1 || h.queries[0] != "two" {
		t.Errorf("Changed saw %v, want one call with %q", h.queries, "two")
	}
	if h.Selected() != 0 {
		t.Errorf("after retyping the selection is %d, want the top row", h.Selected())
	}
	if got := len(h.Rows()); got != 1 {
		t.Errorf("the panel shows %d rows, want the reloaded 1", got)
	}
}

// An empty answer says so rather than showing an empty box.
func TestPaletteSaysWhenThereIsNothing(t *testing.T) {
	h := newPaletteHarness(t)
	if h.Selected() != -1 {
		t.Errorf("Selected = %d on an empty list, want -1", h.Selected())
	}
	if !h.note.Visible() || h.note.Text != "nothing here" {
		t.Errorf("empty note is %q (visible=%v), want the configured sentence",
			h.note.Text, h.note.Visible())
	}
	h.key(fyne.KeyReturn)
	if h.chosen != -1 {
		t.Errorf("Enter on an empty list chose %d", h.chosen)
	}
}

// --- the search bar ------------------------------------------------------------

type searchHarness struct {
	*SearchBar
	steps     []int
	cancelled int
	queries   []string
}

func newSearchHarness(t *testing.T) *searchHarness {
	t.Helper()
	testApp(t)
	h := &searchHarness{}
	h.SearchBar = NewSearchBar(SearchBarOptions{
		Changed: func(q string) { h.queries = append(h.queries, q) },
		Step:    func(d int) { h.steps = append(h.steps, d) },
		Cancel:  func() { h.cancelled++ },
	})
	w := test.NewWindow(h.SearchBar)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(500, 60))
	return h
}

func (h *searchHarness) key(name fyne.KeyName) {
	h.SearchBar.entry.TypedKey(&fyne.KeyEvent{Name: name})
}

// Enter steps forward, Shift+Enter steps back, Escape closes.
func TestSearchBarStepsBothWays(t *testing.T) {
	h := newSearchHarness(t)
	h.key(fyne.KeyReturn)
	h.SearchBar.entry.KeyDown(&fyne.KeyEvent{Name: "LeftShift"})
	h.key(fyne.KeyReturn)
	h.SearchBar.entry.KeyUp(&fyne.KeyEvent{Name: "LeftShift"})
	h.key(fyne.KeyReturn)

	want := []int{1, -1, 1}
	if len(h.steps) != len(want) {
		t.Fatalf("steps = %v, want %v", h.steps, want)
	}
	for i := range want {
		if h.steps[i] != want[i] {
			t.Fatalf("steps = %v, want %v", h.steps, want)
		}
	}
	h.key(fyne.KeyEscape)
	if h.cancelled != 1 {
		t.Errorf("Escape cancelled %d times, want 1", h.cancelled)
	}
}

// What the bar says about where you are. "1 match" and not "1 matches", and
// nothing at all before anything is typed.
func TestSearchBarCountWording(t *testing.T) {
	h := newSearchHarness(t)
	h.SetCount(-1, 0)
	if got := h.Count(); got != "" {
		t.Errorf("with an empty field the bar says %q, want nothing", got)
	}
	h.SetQuery("deploy")
	for _, c := range []struct {
		at, total int
		want      string
	}{
		{-1, 0, "no matches"},
		{-1, 1, "1 match"},
		{-1, 12, "12 matches"},
		{0, 12, "1 of 12"},
		{11, 12, "12 of 12"},
	} {
		h.SetCount(c.at, c.total)
		if got := h.Count(); got != c.want {
			t.Errorf("SetCount(%d, %d) says %q, want %q", c.at, c.total, got, c.want)
		}
	}
}

// Clicking a row means the same as Enter on it, and moving with the arrows
// does not. widget.List reports both through one callback, so this is the one
// place the two can be told apart at all.
func TestClickingARowChoosesItAndArrowsDoNot(t *testing.T) {
	h := newPaletteHarness(t, "#one", "#two", "#three")
	h.key(fyne.KeyDown)
	h.key(fyne.KeyDown)
	if h.chosen != -1 {
		t.Fatalf("moving the selection chose row %d", h.chosen)
	}
	h.Palette.list.Select(1) // what a click does
	if h.chosen != 1 {
		t.Errorf("clicking row 1 chose %d, want 1", h.chosen)
	}
}

// An app combo passes through the field. Focus lives there while an overlay is
// open, and Fyne hands a shortcut to the focused widget - so a panel that did
// not offer them on would swallow Ctrl+Q.
func TestPalettePassesAppShortcutsThrough(t *testing.T) {
	testApp(t)
	var seen []fyne.Shortcut
	p := NewPalette(PaletteOptions{
		Rows:     func() []PaletteRow { return nil },
		Shortcut: func(s fyne.Shortcut) bool { seen = append(seen, s); return true },
	})
	w := test.NewWindow(p)
	t.Cleanup(w.Close)
	p.entry.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierControl})
	if len(seen) != 1 {
		t.Fatalf("the field passed %d shortcuts through, want 1", len(seen))
	}

	b := NewSearchBar(SearchBarOptions{
		Shortcut: func(s fyne.Shortcut) bool { seen = append(seen, s); return true },
	})
	b.entry.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyB, Modifier: fyne.KeyModifierControl})
	if len(seen) != 2 {
		t.Fatalf("the search bar passed %d shortcuts through, want 2", len(seen)-1)
	}
}
