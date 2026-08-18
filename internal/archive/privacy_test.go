package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// The privacy contract, checked where it is actually kept.
//
// Search() is not a witness for "the words left the index". It joins
// messages_fts against messages and drops rows with redacted = 1, so the two
// ways an external-content FTS5 index can go wrong are both invisible to it: an
// entry left behind for a deleted row finds nothing to join to, and an entry
// left behind for a redacted one joins to the tombstone and is filtered out.
// Either way the search comes back empty while the words are still in the
// index. These tests ask messages_fts directly — and then ask the FILE, which
// is the only question a plaintext archive's user is really asking.

// ftsMatches counts index entries matching a query, with no join and no
// redacted filter: what the index itself still knows.
func ftsMatches(t *testing.T, db *DB, match string) int {
	t.Helper()
	var n int
	if err := db.r.QueryRowContext(context.Background(),
		`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH ?`, match).Scan(&n); err != nil {
		t.Fatalf("querying the index for %q: %v", match, err)
	}
	return n
}

// fileHolds reports whether the archive's bytes on disk — database, WAL and
// shared-memory file — still contain a string.
func fileHolds(t *testing.T, path, needle string) bool {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(path + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("reading %s%s: %v", path, suffix, err)
		}
		if strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

// TestRedactLeavesNothingInTheIndex asks messages_fts itself, which is the
// question TestRedactRemovesTheRowAndTheIndexEntry cannot ask through Search.
func TestRedactLeavesNothingInTheIndex(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "keep1", at(1), "alice", "morning"),
		msg("#eco", "gone", at(2), "bob", "my passphrase is correcthorse"),
		msg("#eco", "keep2", at(3), "alice", "morning bob"),
	)
	if got := ftsMatches(t, db, "correcthorse"); got != 1 {
		t.Fatalf("before the redaction the index holds %d entries for the word, want 1", got)
	}

	if ok, err := db.Redact(ctx, "gone", "bob", "typo", at(9)); err != nil || !ok {
		t.Fatalf("Redact = %v, %v", ok, err)
	}

	for _, word := range []string{"correcthorse", "passphrase"} {
		if got := ftsMatches(t, db, word); got != 0 {
			t.Errorf("after the redaction the search index still holds %d entries for %q — "+
				"the row went but its tokens did not", got, word)
		}
	}
	// The index and the table still agree with each other: a 'delete' that
	// passed the wrong values would leave them disagreeing, which is how the
	// tokens survive.
	var ic string
	err := db.w.QueryRowContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES('integrity-check')`).Scan(&ic)
	if err != nil && !strings.Contains(err.Error(), "no rows in result set") {
		t.Errorf("the FTS index fails its own integrity-check after a redaction: %v", err)
	}
	// The neighbours are still indexed.
	if got := ftsMatches(t, db, "morning"); got != 2 {
		t.Errorf("redaction disturbed the neighbours' index entries: %d, want 2", got)
	}
}

// TestForgetLeavesNothingInTheIndex is the same question for "erase this
// conversation", which deletes rows outright and leaves no tombstone.
func TestForgetLeavesNothingInTheIndex(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#private", "p1", at(1), "carol", "the passphrase is zebrafinch"),
		msg("#eco", "e1", at(2), "alice", "unrelated zebrafinch mention"),
	)
	if _, err := db.Forget(ctx, "#private"); err != nil {
		t.Fatal(err)
	}
	if got := ftsMatches(t, db, "zebrafinch"); got != 1 {
		t.Errorf("after Forget the index holds %d entries for the word, want 1 (the other buffer's)", got)
	}
}

// TestRetentionLeavesNothingInTheIndex is the same question for the age sweep.
func TestRetentionLeavesNothingInTheIndex(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "old", epoch.AddDate(0, 0, -100), "alice", "ancient bluewhale"),
		msg("#eco", "new", epoch, "alice", "recent bluewhale"),
	)
	if err := db.SetRetention(ctx, Retain30); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ApplyRetention(ctx); err != nil || n != 1 {
		t.Fatalf("ApplyRetention = %d, %v, want 1 deleted", n, err)
	}
	if got := ftsMatches(t, db, "ancient"); got != 0 {
		t.Errorf("an expired message left %d entries in the search index", got)
	}
	if got := ftsMatches(t, db, "recent"); got != 1 {
		t.Errorf("the sweep took the surviving message's index entry too: %d", got)
	}
}

// TestScrubTakesTheWordsOutOfTheFile is the promise "erase" has to keep to mean
// anything on a plaintext archive: afterwards the words are not in the bytes.
//
// Deleting the row is not enough on its own and neither is VACUUM — see
// DB.Scrub for why both halves are needed and in which order. This test is the
// reason that method exists.
func TestScrubTakesTheWordsOutOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scrub.db")
	db := openTestDBAt(t, path)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "s1", at(1), "alice", "morning"),
		msg("#eco", "s2", at(2), "bob", "my passphrase is stagbeetle"),
	)
	if !fileHolds(t, path, "stagbeetle") {
		t.Fatal("the message never reached the file, so this test proves nothing")
	}

	if ok, err := db.Redact(ctx, "s2", "bob", "typo", at(9)); err != nil || !ok {
		t.Fatalf("Redact = %v, %v", ok, err)
	}
	if err := db.Scrub(ctx); err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	if fileHolds(t, path, "stagbeetle") {
		t.Error("after a redaction and a scrub the withdrawn word is still in the archive's bytes")
	}
	// What was not withdrawn is still there, and still findable.
	if hits, err := db.Search(ctx, "morning", Filters{}, 10); err != nil || len(hits) != 1 {
		t.Errorf("the scrub took the surviving message with it: %v (%v)", hitBodies(hits), err)
	}
	if page, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 10}); err != nil || len(page) != 2 {
		t.Errorf("the scrub changed the shape of the conversation: %v (%v)", bodies(page), err)
	}
}

// TestScrubAfterForget is the same for "erase this conversation", which is the
// button a user presses when they mean it.
func TestScrubAfterForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forget.db")
	db := openTestDBAt(t, path)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("carol", "f1", at(1), "carol", "my passphrase is muskoxen"))
	if _, err := db.Forget(ctx, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := db.Scrub(ctx); err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	if fileHolds(t, path, "muskoxen") {
		t.Error("after erasing a conversation and scrubbing, its words are still in the file")
	}
}

// TestSessionOnlyPurgesAtCloseAndLeavesNoWords pins the half of session-only
// retention that the restart test cannot see: Open purges too, so a Close that
// quietly stopped purging would still look right one line later. This one asks
// the file, and asks it without going through the Open that would fix it.
func TestSessionOnlyPurgesAtCloseAndLeavesNoWords(t *testing.T) {
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
	record(t, r, msg("#eco", "t1", at(1), "alice", "todays passphrase is pangolin"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A read-only open neither migrates nor purges, so what it sees is what
	// Close left.
	ro, err := Open(path, Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopening read-only: %v", err)
	}
	if got, err := ro.Count(ctx, ""); err != nil || got != 0 {
		t.Errorf("Close left %d rows in a session-only archive (%v) — the purge is only happening at Open", got, err)
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	if fileHolds(t, path, "pangolin") {
		t.Error("session-only retention left the session's words in the file after Close")
	}
}
