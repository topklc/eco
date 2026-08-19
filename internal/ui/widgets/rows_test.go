package widgets

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// kinds is a row list written the way the tests read it.
func kinds(rows []Row) []RowKind {
	out := make([]RowKind, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func (k RowKind) String() string {
	switch k {
	case RowMessage:
		return "message"
	case RowArt:
		return "art"
	case RowDay:
		return "day"
	case RowNew:
		return "NEW"
	case RowLoading:
		return "loading"
	}
	return "?"
}

func heads(t *testing.T, rows []Row) []bool {
	t.Helper()
	var out []bool
	for _, r := range rows {
		if r.Kind == RowMessage {
			out = append(out, r.Head)
		}
	}
	return out
}

// TestAuthorGrouping: consecutive messages from one nick inside the window
// share a single nick header, and everything else starts a new one.
func TestAuthorGrouping(t *testing.T) {
	cases := []struct {
		name  string
		lines []model.Line
		want  []bool // Head, per message row
	}{
		{
			name:  "same nick inside the window",
			lines: []model.Line{msg("alice", "one", 0), msg("alice", "two", 30*time.Second)},
			want:  []bool{true, false},
		},
		{
			name:  "same nick past the window",
			lines: []model.Line{msg("alice", "one", 0), msg("alice", "two", 3*time.Minute)},
			want:  []bool{true, true},
		},
		{
			name:  "exactly on the window still groups",
			lines: []model.Line{msg("alice", "one", 0), msg("alice", "two", GroupWindow)},
			want:  []bool{true, false},
		},
		{
			name:  "a different nick breaks it",
			lines: []model.Line{msg("alice", "one", 0), msg("bob", "two", time.Second), msg("alice", "three", 2*time.Second)},
			want:  []bool{true, true, true},
		},
		{
			name: "a status line never joins a group and breaks it",
			lines: []model.Line{
				msg("alice", "one", 0),
				{Time: at.Add(time.Second), Status: true, Text: "bob joined"},
				msg("alice", "two", 2*time.Second),
			},
			want: []bool{true, true, true},
		},
		{
			name: "two status lines each keep their own bullet",
			lines: []model.Line{
				{Time: at, Status: true, Text: "bob joined"},
				{Time: at.Add(time.Second), Status: true, Text: "carol joined"},
			},
			want: []bool{true, true},
		},
		{
			name: "an action carries its nick in the sentence, so it never groups",
			lines: []model.Line{
				msg("alice", "one", 0),
				{Time: at.Add(time.Second), Nick: "alice", Text: "waves", Action: true},
				msg("alice", "two", 2*time.Second),
			},
			want: []bool{true, true, true},
		},
		{
			name: "notices group among themselves but not with chat",
			lines: []model.Line{
				{Time: at, Nick: "serv", Text: "one", Notice: true},
				{Time: at.Add(time.Second), Nick: "serv", Text: "two", Notice: true},
				msg("serv", "three", 2*time.Second),
			},
			want: []bool{true, false, true},
		},
		{
			name:  "our own messages group like anyone else's",
			lines: []model.Line{{Time: at, Nick: "me", Text: "one", Self: true}, {Time: at.Add(time.Second), Nick: "me", Text: "two", Self: true}},
			want:  []bool{true, false},
		},
		{
			name:  "a clock that runs backwards starts a new group",
			lines: []model.Line{msg("alice", "one", time.Minute), msg("alice", "two", 0)},
			want:  []bool{true, true},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := BuildRows(c.lines, RowOptions{Now: at})
			got := heads(t, rows)
			if len(got) != len(c.want) {
				t.Fatalf("%d message rows, want %d (%v)", len(got), len(c.want), kinds(rows))
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("message %d: Head=%v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestAuthorGapIsTheOnlyAir: the gap appears when the author changes and
// nowhere else - not inside a group, not after a divider, not between two
// status lines.
func TestAuthorGapIsTheOnlyAir(t *testing.T) {
	lines := []model.Line{
		msg("alice", "one", 0),
		msg("alice", "two", time.Second),
		msg("bob", "three", 2*time.Second),
		{Time: at.Add(3 * time.Second), Status: true, Text: "carol joined"},
		{Time: at.Add(4 * time.Second), Status: true, Text: "dave joined"},
	}
	rows := BuildRows(lines, RowOptions{Now: at})
	want := map[int]bool{0: false, 1: false, 2: true, 3: true, 4: false}
	i := 0
	for _, r := range rows {
		if r.Kind != RowMessage {
			continue
		}
		if r.Gap != want[i] {
			t.Errorf("message %d (%q): Gap=%v, want %v", i, r.Line.Text, r.Gap, want[i])
		}
		i++
	}
	if rows[0].Kind != RowDay || rows[1].Gap {
		t.Error("the first row after a divider must not add air of its own")
	}
}

// TestDayDividers: one rule above the first line of every calendar day, and
// none anywhere else.
func TestDayDividers(t *testing.T) {
	day2 := at.AddDate(0, 0, 1)
	lines := []model.Line{
		{Time: at, Nick: "a", Text: "one"},
		{Time: at.Add(time.Hour), Nick: "a", Text: "two"},
		{Time: day2, Nick: "a", Text: "three"},
		{Time: day2.Add(time.Minute), Nick: "a", Text: "four"},
	}
	rows := BuildRows(lines, RowOptions{Now: day2})
	got := kinds(rows)
	want := []RowKind{RowDay, RowMessage, RowMessage, RowDay, RowMessage, RowMessage}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("rows %v, want %v", got, want)
		}
	}
	if !rows[3].At.Equal(day2) {
		t.Errorf("the second day rule names %v, want %v", rows[3].At, day2)
	}
	if rows[4].Head != true {
		t.Error("the first message of a new day starts a new group")
	}
}

// TestNewDivider is the read marker made visible: one rule, in one place,
// and it does not move afterwards.
func TestNewDivider(t *testing.T) {
	marker := at.Add(90 * time.Second)
	lines := []model.Line{
		msg("alice", "read", 0),
		msg("alice", "read too", time.Minute),
		msg("bob", "unread", 2*time.Minute),
		msg("bob", "also unread", 3*time.Minute),
	}
	rows := BuildRows(lines, RowOptions{Since: marker, Now: at})
	seen := 0
	var before string
	for i, r := range rows {
		if r.Kind == RowNew {
			seen++
			if !r.At.Equal(marker) {
				t.Errorf("the NEW rule names %v, want the marker %v", r.At, marker)
			}
			if i+1 < len(rows) {
				before = rows[i+1].Line.Text
			}
		}
	}
	if seen != 1 {
		t.Fatalf("%d NEW rules, want exactly one (%v)", seen, kinds(rows))
	}
	if before != "unread" {
		t.Errorf("the NEW rule sits above %q, want %q", before, "unread")
	}
}

func TestNewDividerCases(t *testing.T) {
	marker := at.Add(90 * time.Second)
	cases := []struct {
		name  string
		opts  RowOptions
		lines []model.Line
		want  bool
	}{
		{
			name:  "no marker, no rule",
			opts:  RowOptions{Now: at},
			lines: []model.Line{msg("alice", "hi", 5*time.Minute)},
			want:  false,
		},
		{
			name:  "everything already read",
			opts:  RowOptions{Since: marker, Now: at},
			lines: []model.Line{msg("alice", "hi", 0)},
			want:  false,
		},
		{
			name: "our own message never opens the unread run",
			opts: RowOptions{Since: marker, Now: at},
			lines: []model.Line{
				{Time: at.Add(2 * time.Minute), Nick: "me", Text: "mine", Self: true},
			},
			want: false,
		},
		{
			name: "a status line never opens it either",
			opts: RowOptions{Since: marker, Now: at},
			lines: []model.Line{
				{Time: at.Add(2 * time.Minute), Status: true, Text: "bob joined"},
			},
			want: false,
		},
		{
			name: "playback does: after a restart, replayed lines are the unread ones",
			opts: RowOptions{Since: marker, Now: at},
			lines: []model.Line{
				{Time: at.Add(2 * time.Minute), Nick: "bob", Text: "while you were out", Playback: true},
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := false
			for _, r := range BuildRows(c.lines, c.opts) {
				got = got || r.Kind == RowNew
			}
			if got != c.want {
				t.Errorf("NEW rule present = %v, want %v", got, c.want)
			}
		})
	}
}

// TestLoadingRowIsFirst puts the backfill state where the backfill is.
func TestLoadingRowIsFirst(t *testing.T) {
	rows := BuildRows([]model.Line{msg("alice", "hi", 0)}, RowOptions{Loading: true, Now: at})
	if rows[0].Kind != RowLoading {
		t.Fatalf("rows %v, want the loading row first", kinds(rows))
	}
	if rows[0].Index != -1 {
		t.Error("an invented row must not claim a line index")
	}
}

// TestRowsKeepLineOrderAndIndex: whatever the dividers do, the message rows
// stay in order and point back at the line they came from.
func TestRowsKeepLineOrderAndIndex(t *testing.T) {
	var lines []model.Line
	for i := 0; i < 30; i++ {
		l := msg("alice", "line", time.Duration(i)*time.Minute)
		if i%7 == 0 {
			l.Status, l.Nick = true, ""
		}
		lines = append(lines, l)
	}
	rows := BuildRows(lines, RowOptions{Since: at.Add(9 * time.Minute), Loading: true, Now: at})
	next := 0
	for _, r := range rows {
		switch r.Kind {
		case RowMessage, RowArt:
			if r.Index != next {
				t.Fatalf("message row %d claims line %d", next, r.Index)
			}
			if !r.Line.Time.Equal(lines[next].Time) {
				t.Fatalf("row %d carries the wrong line", next)
			}
			next++
		default:
			if r.Index != -1 {
				t.Fatalf("%v row claims line %d", r.Kind, r.Index)
			}
		}
	}
	if next != len(lines) {
		t.Fatalf("%d message rows for %d lines", next, len(lines))
	}
}

// TestAppendingEqualsRebuilding is what makes the fast path legal: adding one
// line to a built row list must produce exactly what building the whole
// buffer from scratch would have.
func TestAppendingEqualsRebuilding(t *testing.T) {
	var lines []model.Line
	day := at
	for i := 0; i < 40; i++ {
		l := msg("alice", "line", 0)
		l.Time = day.Add(time.Duration(i) * 3 * time.Minute)
		switch i % 5 {
		case 1:
			l.Nick = "bob"
		case 2:
			l.Status, l.Nick = true, ""
		case 3:
			l.Action = true
		}
		if i == 20 {
			day = day.AddDate(0, 0, 1) // the clock rolls over mid-buffer
		}
		lines = append(lines, l)
	}
	opts := RowOptions{Since: at.Add(30 * time.Minute), Now: at}

	b := newRowBuilder(opts)
	for _, l := range lines {
		b.add(l)
	}
	want := BuildRows(lines, opts)
	if len(b.rows) != len(want) {
		t.Fatalf("incremental %d rows, whole-buffer %d", len(b.rows), len(want))
	}
	for i := range want {
		g, w := b.rows[i], want[i]
		if g.Kind != w.Kind || g.Index != w.Index || g.Head != w.Head || g.Gap != w.Gap || !g.At.Equal(w.At) {
			t.Fatalf("row %d: incremental %+v, whole-buffer %+v", i, g, w)
		}
	}
}

// TestDayLabel names a day the way a person would.
func TestDayLabel(t *testing.T) {
	now := time.Date(2025, 6, 12, 10, 0, 0, 0, time.Local) // a Thursday
	cases := []struct {
		day  time.Time
		want string
	}{
		{now, "Today"},
		{now.Add(-9 * time.Hour), "Today"},
		{now.AddDate(0, 0, -1), "Yesterday"},
		{now.AddDate(0, 0, -3), "Monday"},
		{now.AddDate(0, 0, -30), "Tuesday, 13 May"},
		{now.AddDate(-1, 0, 0), "12 June 2024"},
	}
	for _, c := range cases {
		if got := DayLabel(c.day, now); got != c.want {
			t.Errorf("DayLabel(%v) = %q, want %q", c.day.Format("2006-01-02"), got, c.want)
		}
	}
}
