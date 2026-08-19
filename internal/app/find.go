package app

import (
	"sort"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// In-buffer search (Ctrl+F) where it meets the app - blueprint §4.3.
//
// The bar looks at what is on screen first, because that is the fast, exact
// answer and it is the one people mean nine times in ten: the matches colour in
// place and Enter walks them. When it runs out - you have stepped past the
// oldest loaded match, or the buffer holds nothing that matches at all - it
// asks the archive the same question about the same buffer and JUMPS, which is
// the M9 pager's Around folded back into the window through the same restore
// path scroll-up uses (Session.JumpTo).
//
// The two halves are deliberately not merged into one ranked list. A search
// inside a conversation is a place, not a result: the answer is the message
// where it sits, with what was said around it, and a list of extracts would put
// the conversation on the screen twice. The Finder (Ctrl+Shift+F) is the list.

// archiveFindLimit is how many older matches the fallback pulls per buffer. It
// only needs enough to keep stepping backwards through a long afternoon; the
// Finder is the surface for "show me all of them".
const archiveFindLimit = 200

// newSearchBar builds Ctrl+F's strip.
func (u *UI) newSearchBar() *widgets.SearchBar {
	return widgets.NewSearchBar(widgets.SearchBarOptions{
		Changed:  u.findChanged,
		Step:     u.findStep,
		Cancel:   u.closeSearch,
		Shortcut: u.overlayShortcut,
	})
}

// showSearch opens the bar over the conversation on screen, offering the last
// thing looked for - which is what makes "where was that again?" two keystrokes
// the second time.
func (u *UI) showSearch() {
	if !u.built() || u.session() == nil {
		return
	}
	// the palettes and the bar are one rung of the Escape stack; only one of
	// them is ever the thing Escape means
	u.hidePalettes()
	u.searchBar.SetQuery(u.lastFind)
	u.searchBar.Show()
	u.centerBox.Refresh()
	u.applySearch(u.lastFind)
	u.win.Canvas().Focus(u.searchBar.Entry())
}

// closeSearch puts the bar away, clears the highlighting and gives the keyboard
// back to the composer through the one helper that does that (§4.1).
func (u *UI) closeSearch() {
	if u.searchBar == nil || !u.searchBar.Visible() {
		return
	}
	u.lastFind = u.searchBar.Query()
	u.searchBar.Hide()
	u.timeline.SetSearch("")
	u.centerBox.Refresh()
	u.focusComposer()
}

// findChanged re-runs the search on every keystroke.
func (u *UI) findChanged(query string) { u.applySearch(query) }

// applySearch highlights the query in the loaded buffer and says how many it
// found. It does not move the reader: Ctrl+F over a conversation you are
// already reading must not throw you somewhere else before you have finished
// typing the word.
func (u *UI) applySearch(query string) {
	n := u.timeline.SetSearch(query)
	u.searchBar.SetCount(u.timeline.SearchAt(), n)
}

// findStep is Enter and Shift+Enter: the next match in the loaded buffer, or -
// when the window has none left in that direction - the archive's.
//
// "None left in that direction" needs saying carefully, because StepMatch wraps
// on purpose: a backward step from the OLDEST loaded match comes back round to
// the newest rather than failing. That wrap is right at the end of the search
// and wrong before it, so the boundary is asked here rather than being inferred
// from a step that refused - without this line the fallback below could only
// ever fire for a buffer with no loaded match whatsoever, and the whole of
// "Ctrl+F reaches past the window" was unreachable for every real search.
//
// Only backwards. There is nothing in the archive newer than what is loaded.
func (u *UI) findStep(dir int) {
	query := u.searchBar.Query()
	if strings.TrimSpace(query) == "" {
		return
	}
	if dir < 0 && u.atOldestMatch() && u.findOlder(query) {
		return
	}
	if u.stepLoaded(dir) {
		return
	}
	if u.findOlder(query) {
		return
	}
	u.searchBar.SetCount(u.timeline.SearchAt(), u.timeline.SearchMatches())
}

// atOldestMatch reports that the cursor is standing on the oldest match the
// window holds. A cursor that has not moved yet (-1) is not: the first
// Shift+Enter belongs to the newest loaded match, not to the disk.
func (u *UI) atOldestMatch() bool {
	return u.timeline.SearchMatches() > 0 && u.timeline.SearchAt() == 0
}

// stepLoaded walks the matches already on screen and reports whether it moved.
func (u *UI) stepLoaded(dir int) bool {
	if _, ok := u.timeline.SearchStep(dir); !ok {
		return false
	}
	u.searchBar.SetCount(u.timeline.SearchAt(), u.timeline.SearchMatches())
	return true
}

// findOlder is the fallback: ask the archive for matches in this buffer that
// the window does not hold, and open the newest of them where it sits.
//
// "The newest of them" is the right one because stepping is what got us here:
// the reader has walked back through every loaded match and is asking for the
// one before them. Opening it pulls the conversation around it into the window
// (Session.JumpTo), so the next Enter finds it as an ordinary loaded match and
// the bar's count grows to match what is now on screen.
//
// It reports whether anything new arrived. False means the buffer really has
// nothing older - the search wraps around the loaded matches from there, which
// is what StepMatch already does.
func (u *UI) findOlder(query string) bool {
	sess := u.session()
	if sess == nil {
		return false
	}
	name := u.current()
	// The archive ranks by relevance, which is the right order for the Finder
	// and the wrong one here: stepping backwards through a conversation wants
	// the next one BACK, not the best one anywhere.
	hits := sess.SearchBuffer(name, query, archiveFindLimit)
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].RowID > hits[j].RowID })
	for _, h := range hits {
		if h.RowID <= 0 || sess.LineIndex(name, model.MessageID(h.RowID)) >= 0 {
			continue // already on screen; stepping would have found it
		}
		if !sess.JumpTo(name, h.RowID) {
			// further back than the window can stretch (Session.jumpReach). The
			// Finder still finds it; this bar cannot open it in place, and says
			// so rather than pretending the search ended.
			u.searchBar.SetCount(-1, u.timeline.SearchMatches())
			return false
		}
		u.repaintBuffer()
		u.applySearch(query)
		// Land ON the message that was opened. Stepping to it instead would put
		// the cursor wherever StepMatch's wrap reaches, which after a jump is a
		// different message in the same buffer - the bar would say "4 of 9"
		// about a line the reader never asked for.
		if i := sess.LineIndex(name, model.MessageID(h.RowID)); i >= 0 && u.timeline.SearchToLine(i) {
			u.searchBar.SetCount(u.timeline.SearchAt(), u.timeline.SearchMatches())
			return true
		}
		// The archive matched a body this pane does not read as a match (a
		// formatting run split across segments, say). The page is still the
		// right page; stand on its oldest match rather than nowhere.
		u.stepLoaded(1)
		return true
	}
	return false
}
