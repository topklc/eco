package archive

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestRetentionPolicies is the table for the five settings the privacy
// contract offers. Every sweep runs against the injected clock, so "90 days
// ago" is a fact rather than a race with midnight.
func TestRetentionPolicies(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	ages := []int{0, 15, 45, 200, 400} // days ago

	cases := []struct {
		policy Retention
		want   []int // the ages that survive
	}{
		{RetainForever, []int{0, 15, 45, 200, 400}},
		{Retain365, []int{0, 15, 45, 200}},
		{Retain90, []int{0, 15, 45}},
		{Retain30, []int{0, 15}},
		{RetainSession, nil},
	}
	for _, tc := range cases {
		t.Run(string(tc.policy), func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "r.db"), Options{Clock: model.FixedClock(now)})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := testCtx(t)

			r := newRecorder(t, db, RecorderOptions{})
			for _, age := range ages {
				m := msg("#eco", fmt.Sprintf("age%d", age), now.AddDate(0, 0, -age), "alice", fmt.Sprintf("%d days ago", age))
				record(t, r, m)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}

			if err := db.SetRetention(ctx, tc.policy); err != nil {
				t.Fatal(err)
			}
			deleted, err := db.ApplyRetention(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if want := int64(len(ages) - len(tc.want)); deleted != want {
				t.Errorf("ApplyRetention deleted %d, want %d", deleted, want)
			}

			got, err := db.Latest(ctx, "#eco", 100)
			if err != nil {
				t.Fatal(err)
			}
			var wantBodies []string
			for i := len(tc.want) - 1; i >= 0; i-- { // oldest first
				wantBodies = append(wantBodies, fmt.Sprintf("%d days ago", tc.want[i]))
			}
			if !equalStrings(bodies(got), wantBodies) {
				t.Errorf("after %s the archive holds %v, want %v", tc.policy, bodies(got), wantBodies)
			}

			// Expired messages leave the search index with their rows.
			hits, err := db.Search(ctx, "days", Filters{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != len(tc.want) {
				t.Errorf("the index still holds %d of %d messages after the sweep", len(hits), len(tc.want))
			}
		})
	}
}

// TestRetentionIsStoredWithTheData pins that the policy lives in the file it
// governs, so a profile's retention cannot be lost by a settings migration or
// applied to the wrong archive.
func TestRetentionIsStoredWithTheData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.db")
	db := openTestDBAt(t, path)
	ctx := testCtx(t)

	if got, err := db.Retention(ctx); err != nil || got != RetainForever {
		t.Errorf("a fresh archive retains %q (%v), want forever", got, err)
	}
	if err := db.SetRetention(ctx, Retain90); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRetention(ctx, "a fortnight"); err == nil {
		t.Error("an unknown retention policy was accepted")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, err := again.Retention(ctx); err != nil || got != Retain90 {
		t.Errorf("after a restart the policy is %q (%v), want 90", got, err)
	}
}

// TestSessionOnlyRetentionSurvivesNoRestart pins the strongest privacy
// setting: messages are written while the app runs, so scroll-up works, and
// they are gone the next time the archive opens.
func TestSessionOnlyRetentionSurvivesNoRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	ctx := testCtx(t)

	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetRetention(ctx, RetainSession); err != nil {
		t.Fatal(err)
	}
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("#eco", "s1", at(1), "alice", "in this session"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Within the session it is a normal archive: this is what makes
	// scroll-up work at all.
	if got, err := db.Latest(ctx, "#eco", 10); err != nil || len(got) != 1 {
		t.Errorf("during the session the buffer holds %v (%v), want the message", bodies(got), err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, err := again.Count(ctx, ""); err != nil || got != 0 {
		t.Errorf("after a restart the session-only archive holds %d rows (%v), want none", got, err)
	}
	// The buffer's settings are not messages, and they stay.
	if _, ok, err := again.Buffer(ctx, "#eco"); err != nil || !ok {
		t.Errorf("session-only retention deleted the buffer's settings row (%v, %v)", ok, err)
	}
}

// TestSessionOnlyPurgesWhatACrashLeft pins the honest half of the promise: a
// hard kill can leave rows on disk, and the next Open is what removes them.
func TestSessionOnlyPurgesWhatACrashLeft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crashed.db")
	ctx := testCtx(t)

	// Write with retention set, then abandon the DB without closing it -
	// which is what a kill -9 leaves behind.
	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetRetention(ctx, RetainSession); err != nil {
		t.Fatal(err)
	}
	r, err := NewRecorder(ctx, db, RecorderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Record(msg("#eco", "s1", at(1), "alice", "left behind")) {
		t.Fatal("Record refused")
	}
	if err := r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	db.closePools() // no Close: the purge on shutdown never ran

	again, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, err := again.Count(ctx, ""); err != nil || got != 0 {
		t.Errorf("Open left %d rows a crashed session-only run had written (%v)", got, err)
	}
}

// TestVacuumReturnsTheSpace pins the vacuum path: deleting rows frees pages
// inside the file, and only a vacuum gives them back to the filesystem.
func TestVacuumReturnsTheSpace(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	r := newRecorder(t, db, RecorderOptions{Batch: 1000})
	body := strings.Repeat("a large message to make the file grow. ", 60)
	var ms []Message
	for i := 0; i < 1200; i++ {
		ms = append(ms, msg("#eco", fmt.Sprintf("v%d", i), at(i), "alice", body))
	}
	record(t, r, ms...)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Vacuum(ctx); err != nil { // checkpoint the WAL into the file first
		t.Fatal(err)
	}
	full, err := db.SizeOnDisk()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.PurgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := db.SizeOnDisk()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
	afterVacuum, err := db.SizeOnDisk()
	if err != nil {
		t.Fatal(err)
	}

	if afterVacuum >= full {
		t.Errorf("vacuum did not shrink the file: %d bytes full, %d after delete, %d after vacuum",
			full, afterDelete, afterVacuum)
	}
	t.Logf("archive size: %d bytes with 1200 large messages, %d after deleting them, %d after vacuum",
		full, afterDelete, afterVacuum)

	// The file still works afterwards.
	r2 := newRecorder(t, db, RecorderOptions{})
	record(t, r2, msg("#eco", "after", at(1), "alice", "still writable"))
	if got, err := db.Latest(ctx, "#eco", 10); err != nil || len(got) != 1 {
		t.Errorf("after a vacuum the archive holds %v (%v)", bodies(got), err)
	}
	if err := db.Optimize(ctx); err != nil {
		t.Errorf("Optimize: %v", err)
	}
}

// TestPurgeRepointsTheHistoryAnchor pins that a sweep cannot leave the client
// asking a server for messages after a msgid it has just thrown away.
func TestPurgeRepointsTheHistoryAnchor(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	db, err := Open(filepath.Join(t.TempDir(), "anchor.db"), Options{Clock: model.FixedClock(now)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := testCtx(t)

	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "old", now.AddDate(0, 0, -100), "alice", "ancient"),
		msg("#eco", "new", now.AddDate(0, 0, -1), "alice", "recent"),
		msg("#dead", "older", now.AddDate(0, 0, -200), "alice", "all gone"),
	)
	if got, _ := db.LastMsgID(ctx, "#eco"); got != "new" {
		t.Fatalf("LastMsgID before the sweep = %q", got)
	}

	if err := db.SetRetention(ctx, Retain30); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := db.LastMsgID(ctx, "#eco"); err != nil || got != "new" {
		t.Errorf("LastMsgID = %q (%v), want the surviving message", got, err)
	}
	if got, err := db.LastMsgID(ctx, "#dead"); err != nil || got != "" {
		t.Errorf("a buffer whose messages all expired still anchors at %q (%v)", got, err)
	}
}

// TestRetentionDaysAndValidity pins the little enum both the settings page and
// the sweep read.
func TestRetentionDaysAndValidity(t *testing.T) {
	cases := []struct {
		r     Retention
		days  int
		valid bool
	}{
		{RetainForever, 0, true},
		{Retain365, 365, true},
		{Retain90, 90, true},
		{Retain30, 30, true},
		{RetainSession, 0, true},
		{Retention("30 days"), 0, false},
		{Retention(""), 0, false},
	}
	for _, tc := range cases {
		if got := tc.r.Days(); got != tc.days {
			t.Errorf("%q.Days() = %d, want %d", tc.r, got, tc.days)
		}
		if got := tc.r.Valid(); got != tc.valid {
			t.Errorf("%q.Valid() = %v, want %v", tc.r, got, tc.valid)
		}
	}
}
