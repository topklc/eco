package archive

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seed fills a buffer with n messages one second apart, bodies "line 0".."line n-1",
// and returns them as read back (so the test has their rowids).
func seed(t *testing.T, db *DB, buffer string, n int) []Message {
	t.Helper()
	r := newRecorder(t, db, RecorderOptions{Batch: 500})
	ms := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		ms = append(ms, msg(buffer, fmt.Sprintf("%s-%d", buffer, i), at(i), "alice", fmt.Sprintf("line %d", i)))
	}
	record(t, r, ms...)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := db.Latest(testCtx(t), buffer, n)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("seeded %d messages, read back %d", n, len(got))
	}
	return got
}

// TestLatestOpensAtTheEnd pins what opening a buffer shows: the newest
// messages, oldest first, so the caller renders the slice as it is.
func TestLatestOpensAtTheEnd(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	seed(t, db, "#eco", 20)

	got, err := db.Latest(ctx, "#eco", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"line 15", "line 16", "line 17", "line 18", "line 19"}
	if !equalStrings(bodies(got), want) {
		t.Errorf("Latest(5) = %v, want %v", bodies(got), want)
	}

	// A limit past the end is not an error, and a buffer nobody has spoken
	// in is empty rather than missing.
	if got, err := db.Latest(ctx, "#eco", 1000); err != nil || len(got) != 20 {
		t.Errorf("Latest(1000) = %d messages (%v), want all 20", len(got), err)
	}
	if got, err := db.Latest(ctx, "#quiet", 10); err != nil || len(got) != 0 {
		t.Errorf("Latest on an unknown buffer = %v, %v, want empty and no error", bodies(got), err)
	}
}

// TestPageWalksBackwardsWithoutGapsOrRepeats pins scroll-up: paging from the
// end to the beginning yields every message exactly once, in order, whichever
// kind of anchor the caller uses.
func TestPageWalksBackwardsWithoutGapsOrRepeats(t *testing.T) {
	const total, page = 97, 10
	for _, anchor := range []string{"rowid", "msgid"} {
		t.Run(anchor, func(t *testing.T) {
			db := openTestDB(t)
			ctx := testCtx(t)
			seed(t, db, "#eco", total)

			var seen []string
			q := PageQuery{Buffer: "#eco", Limit: page}
			for turns := 0; ; turns++ {
				if turns > total {
					t.Fatal("paging did not terminate")
				}
				got, err := db.Page(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) == 0 {
					break
				}
				// Each page is itself in oldest-first order.
				for i := 1; i < len(got); i++ {
					if got[i-1].Time.After(got[i].Time) {
						t.Fatalf("page came back out of order: %v", bodies(got))
					}
				}
				seen = append(bodies(got), seen...)
				q = PageQuery{Buffer: "#eco", Limit: page}
				if anchor == "rowid" {
					q.BeforeRowID = got[0].RowID
				} else {
					q.BeforeMsgID = got[0].MsgID
				}
			}

			want := make([]string, 0, total)
			for i := 0; i < total; i++ {
				want = append(want, fmt.Sprintf("line %d", i))
			}
			if !equalStrings(seen, want) {
				t.Errorf("paging saw %d messages, want %d; first difference at %s",
					len(seen), len(want), firstDiff(seen, want))
			}
		})
	}
}

func firstDiff(a, b []string) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("index %d: %q vs %q", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("length %d vs %d", len(a), len(b))
}

// TestPagingIsStableAcrossDuplicateTimestamps pins the tiebreaker. A netsplit,
// a bridged relay or a server with second-resolution time all put many
// messages in one millisecond; ordering by ts alone would let a page boundary
// skip or repeat them.
func TestPagingIsStableAcrossDuplicateTimestamps(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	same := at(100)
	var ms []Message
	for i := 0; i < 30; i++ {
		ms = append(ms, msg("#eco", fmt.Sprintf("dup%02d", i), same, "alice", fmt.Sprintf("line %d", i)))
	}
	record(t, r, ms...)

	var seen []string
	q := PageQuery{Buffer: "#eco", Limit: 7}
	for turns := 0; turns < 20; turns++ {
		got, err := db.Page(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			break
		}
		seen = append(bodies(got), seen...)
		q = PageQuery{Buffer: "#eco", Limit: 7, BeforeRowID: got[0].RowID}
	}
	want := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		want = append(want, fmt.Sprintf("line %d", i))
	}
	if !equalStrings(seen, want) {
		t.Errorf("paging 30 messages that share one timestamp saw %v", seen)
	}

	// The same request twice gives the same answer: nothing here depends on
	// the order the rows happen to come back in.
	first, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(bodies(first), bodies(second)) {
		t.Errorf("the same page twice = %v then %v", bodies(first), bodies(second))
	}
}

// TestPageEdgeCases is the table for the awkward asks.
func TestPageEdgeCases(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	msgs := seed(t, db, "#eco", 10)
	seed(t, db, "#other", 10)

	cases := []struct {
		name string
		q    PageQuery
		want []string
	}{
		{"no anchor is the newest page", PageQuery{Buffer: "#eco", Limit: 3},
			[]string{"line 7", "line 8", "line 9"}},
		{"anchored at the oldest message is empty", PageQuery{Buffer: "#eco", Limit: 3, BeforeRowID: msgs[0].RowID},
			nil},
		{"an anchor the archive does not know is the newest page", PageQuery{Buffer: "#eco", Limit: 2, BeforeMsgID: "never-existed"},
			[]string{"line 8", "line 9"}},
		{"a page shorter than the limit stops at the start", PageQuery{Buffer: "#eco", Limit: 50, BeforeRowID: msgs[2].RowID},
			[]string{"line 0", "line 1"}},
		{"another buffer's messages never leak in", PageQuery{Buffer: "#other", Limit: 2},
			[]string{"line 8", "line 9"}},
		{"zero limit is the default page", PageQuery{Buffer: "#eco"},
			bodies(msgs)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.Page(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if !equalStrings(bodies(got), tc.want) {
				t.Errorf("Page = %v, want %v", bodies(got), tc.want)
			}
		})
	}

	if _, err := db.Page(ctx, PageQuery{Limit: 5}); err == nil {
		t.Error("Page with no buffer name succeeded")
	}
}

// TestPageLimitIsCapped pins that a caller cannot ask the pager for the whole
// archive by accident.
func TestPageLimitIsCapped(t *testing.T) {
	if got := pageLimit(0); got != DefaultPageLimit {
		t.Errorf("pageLimit(0) = %d, want the default %d", got, DefaultPageLimit)
	}
	if got := pageLimit(-1); got != DefaultPageLimit {
		t.Errorf("pageLimit(-1) = %d, want the default %d", got, DefaultPageLimit)
	}
	if got := pageLimit(1 << 20); got != maxPageLimit {
		t.Errorf("pageLimit(1M) = %d, want the cap %d", got, maxPageLimit)
	}
}

// TestAroundGivesASearchHitItsContext pins what a search result needs to
// become a place in the timeline.
func TestAroundGivesASearchHitItsContext(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	msgs := seed(t, db, "#eco", 20)

	got, err := db.Around(ctx, msgs[10].RowID, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"line 8", "line 9", "line 10", "line 11", "line 12", "line 13"}
	if !equalStrings(bodies(got), want) {
		t.Errorf("Around = %v, want %v", bodies(got), want)
	}

	// At the very start there is simply less context, not an error.
	got, err = db.Around(ctx, msgs[0].RowID, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(bodies(got), []string{"line 0", "line 1"}) {
		t.Errorf("Around at the start = %v", bodies(got))
	}

	// A rowid that is gone (retention removed it) is empty, not an error.
	if got, err := db.Around(ctx, 999999, 2, 2); err != nil || len(got) != 0 {
		t.Errorf("Around on a missing rowid = %v, %v", bodies(got), err)
	}
}

// TestPageExcludesRedactedOnRequest pins the opt-out: tombstones are in a page
// by default because a hole reads worse, and out when the caller says so.
func TestPageExcludesRedactedOnRequest(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	seed(t, db, "#eco", 5)

	if _, err := db.Redact(ctx, "#eco-2", "alice", "", at(9)); err != nil {
		t.Fatal(err)
	}
	withTombstone, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTombstone) != 5 {
		t.Errorf("page = %v, want the tombstone still in place", bodies(withTombstone))
	}
	without, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 10, ExcludeRedacted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(without) != 4 {
		t.Errorf("page excluding redacted = %v, want 4 messages", bodies(without))
	}
}

// TestCountCountsWhatIsAsked pins the counter both the budget test and the
// retention sweep read.
func TestCountCountsWhatIsAsked(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	seed(t, db, "#eco", 4)
	seed(t, db, "#other", 6)

	for _, tc := range []struct {
		buffer string
		want   int64
	}{{"#eco", 4}, {"#other", 6}, {"", 10}, {"#nothing", 0}} {
		got, err := db.Count(ctx, tc.buffer)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Count(%q) = %d, want %d", tc.buffer, got, tc.want)
		}
	}
}

// TestTimestampsSurviveTheRoundTripToMilliseconds pins the resolution of the
// ts column: server-time is milliseconds, and a page must hand back the same
// instant it was given rather than one rounded to a second.
func TestTimestampsSurviveTheRoundTripToMilliseconds(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	precise := epoch.Add(1234 * time.Millisecond)
	record(t, r, msg("#eco", "p1", precise, "alice", "precise"))
	got, err := db.Latest(ctx, "#eco", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Time.Equal(precise) {
		t.Errorf("stored %s, read back %s", precise, got[0].Time)
	}

	// A message with no time at all (a client-generated line before the
	// clock is known) stores as zero rather than as year 1.
	record(t, r, msg("#eco", "p2", time.Time{}, "alice", "timeless"))
	all, err := db.Latest(ctx, "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.MsgID == "p2" && !m.Time.IsZero() {
			t.Errorf("a message with no time read back as %s", m.Time)
		}
	}
}

// TestAPageIsASeekAndAWalkOfTheIndex pins the query PLAN both of Page's
// promises rest on.
//
// Ordering by (ts, rowid) rather than by ts alone is what makes a page boundary
// stable - thirty messages sharing a millisecond get a total order - and it
// costs nothing only because that pair is the tail of idx_messages_buffer_ts,
// so SQLite walks the index backwards instead of sorting. The same fact is what
// makes a page deep in history cost what a recent one costs. A plan carrying a
// TEMP B-TREE has lost both, and the tiebreaker in the ORDER BY has quietly
// become the only thing standing between the pager and an arbitrary order.
//
// The performance budget would eventually notice; it does not run under the
// race detector, needs a corpus, and does not say which query regressed.
func TestAPageIsASeekAndAWalkOfTheIndex(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	seed(t, db, "#eco", 200)
	if err := db.Optimize(ctx); err != nil { // ANALYZE: plan the planner really gets
		t.Fatal(err)
	}

	plan := explainPlan(t, db,
		pageSQL("m.buffer_folded = ? AND (m.ts < ? OR (m.ts = ? AND m.rowid < ?))"),
		"#eco", at(100).UnixMilli(), at(100).UnixMilli(), int64(100), 100)
	if !strings.Contains(plan, "idx_messages_buffer_ts") {
		t.Errorf("a page no longer seeks the buffer index - it is scanning:\n%s", plan)
	}
	if strings.Contains(strings.ToUpper(plan), "TEMP B-TREE") {
		t.Errorf("a page now sorts its rows instead of walking them in index order:\n%s", plan)
	}
}

func explainPlan(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.r.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scanning the plan: %v", err)
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, "\n")
}
