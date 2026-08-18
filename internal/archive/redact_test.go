package archive

import (
	"testing"
)

// TestRedactRemovesTheRowAndTheIndexEntry pins the sharpest privacy promise
// this package makes: after a REDACT the words are gone from the messages
// table *and* from the search index, and what is left is a tombstone.
func TestRedactRemovesTheRowAndTheIndexEntry(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "keep1", at(1), "alice", "morning"),
		msg("#eco", "gone", at(2), "bob", "my password is hunter2"),
		msg("#eco", "keep2", at(3), "alice", "morning bob"),
	)

	if hits, err := db.Search(ctx, "hunter2", Filters{}, 10); err != nil || len(hits) != 1 {
		t.Fatalf("before redaction the search found %v (%v), want the message", hitBodies(hits), err)
	}

	ok, err := db.Redact(ctx, "gone", "bob", "typo", at(9))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Redact reported that there was nothing to redact")
	}

	// Gone from the table.
	page, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 3 {
		t.Fatalf("page = %v, want three rows with a tombstone in the middle", bodies(page))
	}
	tomb := page[1]
	if tomb.Body != "" {
		t.Errorf("the tombstone still carries a body: %q", tomb.Body)
	}
	if !tomb.Redacted {
		t.Error("the tombstone is not marked redacted")
	}
	if tomb.MsgID != "gone" {
		t.Errorf("the tombstone lost the msgid (%q), so a replay would resurrect the message", tomb.MsgID)
	}
	if tomb.Sender != "bob" || !tomb.Time.Equal(at(2)) {
		t.Errorf("the tombstone lost its place: sender %q at %s", tomb.Sender, tomb.Time)
	}
	if tomb.Tags["redact-by"] != "bob" || tomb.Tags["redact-reason"] != "typo" {
		t.Errorf("the tombstone lost who deleted it: %v", tomb.Tags)
	}

	// Gone from the index. This is the assertion an UPDATE-based redaction
	// would fail: FTS5 external content keeps the old tokens unless the row
	// is deleted.
	if hits, err := db.Search(ctx, "hunter2", Filters{}, 10); err != nil || len(hits) != 0 {
		t.Errorf("after redaction the search still finds %v (%v)", hitBodies(hits), err)
	}
	if hits, err := db.Search(ctx, "password", Filters{}, 10); err != nil || len(hits) != 0 {
		t.Errorf("after redaction a word from the body still matches: %v (%v)", hitBodies(hits), err)
	}
	// Not even by its sender, which the tombstone does keep.
	if hits, err := db.Search(ctx, "from:bob", Filters{}, 10); err != nil || len(hits) != 0 {
		t.Errorf("a tombstone showed up in a from: search: %v (%v)", hitBodies(hits), err)
	}
	// The neighbours are untouched.
	if hits, err := db.Search(ctx, "morning", Filters{}, 10); err != nil || len(hits) != 2 {
		t.Errorf("redaction disturbed the neighbours: %v (%v)", hitBodies(hits), err)
	}
}

// TestRedactKeepsThePlaceInTheTimeline pins that a tombstone occupies the same
// rowid, so pagination anchors that pointed at the message still land where
// they did and the conversation keeps its shape.
func TestRedactKeepsThePlaceInTheTimeline(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	msgs := seed(t, db, "#eco", 10)
	target := msgs[4]

	before, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 3, BeforeRowID: target.RowID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Redact(ctx, target.MsgID, "alice", "", at(99)); err != nil {
		t.Fatal(err)
	}
	after, err := db.Page(ctx, PageQuery{Buffer: "#eco", Limit: 3, BeforeRowID: target.RowID})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(bodies(before), bodies(after)) {
		t.Errorf("paging from the redacted message changed: %v then %v", bodies(before), bodies(after))
	}

	all, err := db.Latest(ctx, "#eco", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10 {
		t.Fatalf("the buffer holds %d rows after a redaction, want 10", len(all))
	}
	if all[4].RowID != target.RowID {
		t.Errorf("the tombstone moved to rowid %d, want %d", all[4].RowID, target.RowID)
	}
}

// TestRedactSurvivesReplay pins why the tombstone keeps the msgid: when the
// server sends the message again in a CHATHISTORY batch, the dedupe collapses
// onto the tombstone rather than resurrecting the text.
func TestRedactSurvivesReplay(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	original := msg("#eco", "gone", at(2), "bob", "my password is hunter2")
	record(t, r, original)

	if _, err := db.Redact(ctx, "gone", "bob", "", at(9)); err != nil {
		t.Fatal(err)
	}
	record(t, r, original) // the server replays it

	page, err := db.Latest(ctx, "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("after a replay the buffer holds %v", bodies(page))
	}
	if page[0].Body != "" || !page[0].Redacted {
		t.Errorf("a replay resurrected a redacted message: %+v", page[0])
	}
	if hits, err := db.Search(ctx, "hunter2", Filters{}, 10); err != nil || len(hits) != 0 {
		t.Errorf("a replay put the words back in the index: %v (%v)", hitBodies(hits), err)
	}
}

// TestRedactEdgeCases is the table for the asks that are not "delete this".
func TestRedactEdgeCases(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("#eco", "m1", at(1), "alice", "hello"))

	if ok, err := db.Redact(ctx, "not-here", "alice", "", at(2)); err != nil || ok {
		t.Errorf("redacting an unknown msgid = %v, %v; want a clean false", ok, err)
	}
	if _, err := db.Redact(ctx, "", "alice", "", at(2)); err == nil {
		t.Error("redacting an empty msgid succeeded")
	}
	// Redacting twice is idempotent, not an error, and leaves one tombstone.
	if ok, err := db.Redact(ctx, "m1", "alice", "", at(2)); err != nil || !ok {
		t.Fatalf("first redaction = %v, %v", ok, err)
	}
	if ok, err := db.Redact(ctx, "m1", "alice", "", at(3)); err != nil || !ok {
		t.Fatalf("second redaction = %v, %v", ok, err)
	}
	if n, err := db.Count(ctx, "#eco"); err != nil || n != 1 {
		t.Errorf("the buffer holds %d rows after two redactions (%v), want 1", n, err)
	}
	// A redaction with no author or reason writes no tags rather than empty ones.
	page, err := db.Latest(ctx, "#eco", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := page[0].Tags["redact-reason"]; ok {
		t.Errorf("a reasonless redaction stored a reason: %v", page[0].Tags)
	}
}

// TestForgetErasesABufferWithoutTombstones pins the other delete: "forget this
// conversation" leaves nothing behind, and the buffer's own settings survive so
// that turning logging off and erasing the history is one gesture.
func TestForgetErasesABufferWithoutTombstones(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	seed(t, db, "#eco", 12)
	seed(t, db, "#other", 3)
	if err := db.SetLogging(ctx, "#eco", false); err != nil {
		t.Fatal(err)
	}

	n, err := db.Forget(ctx, "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Errorf("Forget removed %d messages, want 12", n)
	}
	if got, err := db.Count(ctx, "#eco"); err != nil || got != 0 {
		t.Errorf("#eco holds %d rows after Forget (%v)", got, err)
	}
	if hits, err := db.Search(ctx, "line", Filters{Buffer: "#eco"}, 50); err != nil || len(hits) != 0 {
		t.Errorf("Forget left %d entries in the search index (%v)", len(hits), err)
	}
	if got, err := db.Count(ctx, "#other"); err != nil || got != 3 {
		t.Errorf("Forget touched another buffer: #other holds %d rows (%v)", got, err)
	}
	b, ok, err := db.Buffer(ctx, "#eco")
	if err != nil || !ok {
		t.Fatalf("Forget deleted the buffer's settings row: %v, %v", ok, err)
	}
	if b.Logging {
		t.Error("Forget turned logging back on")
	}
	if b.LastMsgID != "" {
		t.Errorf("Forget left the CHATHISTORY anchor at %q, pointing at a message that is gone", b.LastMsgID)
	}
	if _, err := db.Forget(ctx, ""); err == nil {
		t.Error("Forget with no buffer name succeeded")
	}
}
