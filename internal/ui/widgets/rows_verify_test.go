package widgets

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The row-model rules the M11 mutation sweep found nothing holding: which
// lines may share a header at all, what a divider does to the group under it,
// where the read marker's boundary sits, and which zone the day turns over in.
// Each test here is one mutation that stayed green until it existed.

// TestStatusLinesNeverShareAHeader. §4.2: author grouping is "never grouping
// status lines". A run of joins and parts must keep every bullet — grouping
// them would leave rows with no author mark at all, and the sweep showed the
// suite could not tell the difference.
// Both shapes are covered on purpose. Today lineOf never gives a status line a
// nick, so the nick-less run is the one that ships and the `!l.Status` guard in
// groupable is belt and braces; the nick-carrying run pins the rule before M13
// starts attributing joins and parts, which is exactly when the guard would
// begin to carry weight and exactly when nobody would think to add this test.
func TestStatusLinesNeverShareAHeader(t *testing.T) {
	for _, c := range []struct {
		name string
		nick func(int) string
	}{
		{"as the app builds them (no nick)", func(int) string { return "" }},
		{"if one ever carried a nick", func(i int) string { return []string{"alice", "alice", "bob"}[i] }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var lines []model.Line
			for i, text := range []string{"alice joined", "alice set +o", "bob left"} {
				lines = append(lines, model.Line{
					Time: at.Add(time.Duration(i) * time.Second), Status: true,
					Nick: c.nick(i), Text: text})
			}
			for i, r := range messageRows(BuildRows(lines, RowOptions{})) {
				if !r.Head {
					t.Errorf("status row %d (%q) was grouped under the one above it", i, r.Line.Text)
				}
			}
		})
	}
}

// TestActionsNeverShareAHeader. An action carries its author inside the
// sentence ("* alice waves"), so a second one stacked under the first would
// draw with an empty nick column and read as nobody's.
func TestActionsNeverShareAHeader(t *testing.T) {
	lines := []model.Line{
		msg("alice", "hello", 0),
		{Time: at.Add(time.Second), Nick: "alice", Text: "waves", Action: true},
		{Time: at.Add(2 * time.Second), Nick: "alice", Text: "waves again", Action: true},
	}
	got := messageRows(BuildRows(lines, RowOptions{}))
	for i, r := range got {
		if !r.Head {
			t.Errorf("row %d (%q, action=%v) lost its header", i, r.Line.Text, r.Line.Action)
		}
	}
}

// TestArtRowsAlwaysCarryTheirOwnHeader. An art row is a character grid that
// draws its own gutter and nick inside the grid; it can never be a
// continuation of the row above, whoever wrote it.
func TestArtRowsAlwaysCarryTheirOwnHeader(t *testing.T) {
	lines := []model.Line{msg("alice", "art one", 0), msg("alice", "art two", time.Second)}
	rows := BuildRows(lines, RowOptions{IsArt: func(model.Line) bool { return true }})
	for _, r := range rows {
		if r.Kind != RowArt {
			continue
		}
		if !r.Head {
			t.Errorf("art row %q was grouped under the row above it", r.Line.Text)
		}
	}
}

// TestADividerBreaksTheGroupBelowIt. A day rule or the NEW rule between two
// messages from one nick means the lower one is the first of its group: it
// must draw its own nick, or it reads as a continuation of a message that is
// now on the other side of a landmark.
func TestADividerBreaksTheGroupBelowIt(t *testing.T) {
	// a minute apart, one either side of local midnight: inside the grouping
	// window, so ONLY the divider can be what starts the new group. Spanning a
	// whole day instead would prove nothing — the window would have broken the
	// group on its own and the divider's own duty would go untested.
	loc := time.Local
	before := time.Date(2026, 6, 1, 23, 59, 30, 0, loc)
	after := time.Date(2026, 6, 2, 0, 0, 30, 0, loc)
	for _, c := range []struct {
		name  string
		lines []model.Line
		opts  RowOptions
	}{
		{"day rule", []model.Line{
			{Time: before, Nick: "alice", Text: "last night"},
			{Time: after, Nick: "alice", Text: "this morning"}}, RowOptions{}},
		{"NEW rule", []model.Line{msg("alice", "read", 0), msg("alice", "unread", time.Second)},
			RowOptions{Since: at}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := BuildRows(c.lines, c.opts)
			checked, justBelow := 0, false
			for _, r := range rows {
				switch r.Kind {
				case RowDay, RowNew:
					justBelow = true
				case RowMessage:
					if justBelow {
						checked++
						if !r.Head {
							t.Errorf("%q sits under a divider without its own nick", r.Line.Text)
						}
						if r.Gap {
							t.Errorf("%q got author air as well as a divider", r.Line.Text)
						}
					}
					justBelow = false
				}
			}
			// every fixture here puts a message directly under every divider,
			// so anything less means the loop above walked past the one that
			// mattered — which is how this test first passed against a bug
			if want := countKind(rows, RowDay) + countKind(rows, RowNew); checked != want {
				t.Fatalf("checked the row under %d of %d dividers in %v", checked, want, kinds(rows))
			}
		})
	}
}

// TestTheMarkerBoundaryIsExclusive. The read marker is the timestamp of the
// last message the reader HAS seen, so a line stamped exactly at it belongs
// above the NEW rule. Off by one here puts the last thing you read into the
// unread run every time you reopen the buffer.
func TestTheMarkerBoundaryIsExclusive(t *testing.T) {
	lines := []model.Line{
		msg("alice", "before", -time.Second),
		msg("alice", "exactly at the marker", 0),
		msg("bob", "after", time.Second),
	}
	rows := BuildRows(lines, RowOptions{Since: at})
	var above []string
	for _, r := range rows {
		if r.Kind == RowNew {
			break
		}
		if r.Kind == RowMessage {
			above = append(above, r.Line.Text)
		}
	}
	if len(above) != 2 || above[1] != "exactly at the marker" {
		t.Errorf("above the NEW rule: %q, want the marker's own line to be the last of them", above)
	}
}

// TestARunOfStatusLinesStaysTight. groupKey files every status line under one
// key so a burst of joins and parts does not get four pixels of air between
// each pair — §4.2 allows exactly one airy concession and this is not it.
// The nicks here are the point: groupKey files every status line under ONE key
// so that a burst of them is one block of air, not four. As above, today they
// arrive without nicks and the rule is dormant; it stops being dormant the
// moment a join line learns whose it is.
func TestARunOfStatusLinesStaysTight(t *testing.T) {
	lines := []model.Line{
		msg("alice", "hello", 0),
		{Time: at.Add(time.Second), Status: true, Nick: "bob", Text: "bob joined"},
		{Time: at.Add(2 * time.Second), Status: true, Nick: "carol", Text: "carol joined"},
		{Time: at.Add(3 * time.Second), Status: true, Nick: "dave", Text: "dave left"},
	}
	rows := messageRows(BuildRows(lines, RowOptions{}))
	// rows[1] is the first status line: it IS an author change and keeps its air
	if !rows[1].Gap {
		t.Error("the first status line after a message got no air")
	}
	for i, r := range rows[2:] {
		if r.Gap {
			t.Errorf("status row %d (%q) opened a gap: a run of joins and parts must stay tight",
				i+2, r.Line.Text)
		}
	}
}

// TestDayDividersTurnOverInTheReadERsZone is the defect the sweep found rather
// than a mutation: lines arrive as UTC instants (the archive stores ms and
// hands back .UTC(); a server-time tag parses to Z) while the gutter beside
// the rule and DayLabel above it both print local time. Splitting on the
// instants' own zone therefore drew the rule at UTC midnight — a day rule in
// the middle of an ordinary evening, and none where the date really turns.
func TestDayDividersTurnOverInTheReaderSZone(t *testing.T) {
	for _, zone := range []string{"Europe/Istanbul", "America/Los_Angeles", "Pacific/Kiritimati"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("%s unavailable: %v", zone, err)
		}
		t.Run(zone, func(t *testing.T) {
			// the reader's zone IS time.Local — that is the zone the gutter
			// prints in — so pin it, or this test only ever proves whatever
			// the build machine happens to be set to (and proves nothing at
			// all on a CI box running in UTC).
			pinZone(t, loc)
			count := func(lines []model.Line) int {
				n := 0
				for _, r := range BuildRows(lines, RowOptions{}) {
					if r.Kind == RowDay {
						n++
					}
				}
				return n
			}
			// as the archive hands them over: UTC instants either side of the
			// reader's own midnight
			across := []model.Line{
				{Time: time.Date(2026, 6, 1, 23, 30, 0, 0, loc).UTC(), Nick: "alice", Text: "before midnight"},
				{Time: time.Date(2026, 6, 2, 0, 30, 0, 0, loc).UTC(), Nick: "alice", Text: "after midnight"},
			}
			if got := count(across); got != 2 {
				t.Errorf("straddling local midnight: %d day rules, want 2 (one per local day)", got)
			}
			// and a stretch that stays inside one local day but crosses UTC's
			within := []model.Line{
				{Time: time.Date(2026, 6, 2, 1, 0, 0, 0, loc).UTC(), Nick: "alice", Text: "early"},
				{Time: time.Date(2026, 6, 2, 12, 0, 0, 0, loc).UTC(), Nick: "alice", Text: "noon"},
				{Time: time.Date(2026, 6, 2, 23, 0, 0, 0, loc).UTC(), Nick: "alice", Text: "late"},
			}
			if got := count(within); got != 1 {
				t.Errorf("inside one local day: %d day rules, want 1", got)
			}
		})
	}
}

// TestDayLabelAndTheSplitAgree: whatever zone the caller is in, the rule the
// model places and the words drawn on it name the same day.
func TestDayLabelAndTheSplitAgree(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Skip(err)
	}
	pinZone(t, loc)
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, loc)
	lines := []model.Line{
		{Time: time.Date(2026, 6, 1, 23, 30, 0, 0, loc).UTC(), Nick: "alice", Text: "yesterday"},
		{Time: time.Date(2026, 6, 2, 0, 30, 0, 0, loc).UTC(), Nick: "alice", Text: "today"},
	}
	var labels []string
	for _, r := range BuildRows(lines, RowOptions{}) {
		if r.Kind == RowDay {
			labels = append(labels, DayLabel(r.At, now))
		}
	}
	want := []string{"Yesterday", "Today"}
	if len(labels) != 2 || labels[0] != want[0] || labels[1] != want[1] {
		t.Errorf("day rules read %q, want %q", labels, want)
	}
}

// pinZone makes time.Local a named zone for the length of one test, so a
// zone-sensitive rule is proved rather than merely exercised in whatever zone
// the machine is set to. Safe because nothing in this package runs parallel.
func pinZone(t *testing.T, loc *time.Location) {
	t.Helper()
	prev := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = prev })
}

// messageRows is every row that draws a line, dividers dropped.
func messageRows(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if r.Kind == RowMessage || r.Kind == RowArt {
			out = append(out, r)
		}
	}
	return out
}
