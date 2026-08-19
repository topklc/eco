package archive

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestRecorderWritesBehind pins the live path: messages handed to the Recorder
// are on disk after a Flush, in one batch rather than one transaction each.
func TestRecorderWritesBehind(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{Batch: 64, Interval: time.Hour})

	for i := 0; i < 50; i++ {
		if !r.Record(msg("#eco", fmt.Sprintf("m%d", i), at(i), "alice", fmt.Sprintf("line %d", i))) {
			t.Fatalf("Record %d refused", i)
		}
	}
	// Nothing has been asked to write yet: the interval is an hour away and
	// the batch is not full. This is what "write-behind" costs and it is the
	// behaviour, not an accident.
	if n, err := db.Count(ctx, "#eco"); err != nil || n != 0 {
		t.Fatalf("before Flush the archive holds %d rows (%v), want 0", n, err)
	}
	flush(t, r)

	if n, err := db.Count(ctx, "#eco"); err != nil || n != 50 {
		t.Fatalf("after Flush the archive holds %d rows (%v), want 50", n, err)
	}
	st := r.Stats()
	if st.Accepted != 50 || st.Written != 50 || st.Dropped != 0 {
		t.Errorf("stats = %+v, want 50 accepted, 50 written, 0 dropped", st)
	}
	if st.Batches != 1 {
		t.Errorf("50 messages took %d transactions, want 1", st.Batches)
	}
}

// TestRecorderDedupesOnMsgID pins ADR-3: the same msgid is one message however
// many times it arrives - live, then again in a CHATHISTORY replay, then again
// after a reconnect gap-fill.
func TestRecorderDedupesOnMsgID(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	live := msg("#eco", "abc123", at(1), "alice", "the deploy is green")
	record(t, r, live)

	replay := live
	replay.Body = "the deploy is green" // same message, replayed inside a batch
	record(t, r, replay, replay, replay)

	got, err := db.Latest(ctx, "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("the archive holds %d copies of one msgid: %v", len(got), bodies(got))
	}
	if st := r.Stats(); st.Written != 1 || st.Deduped != 3 {
		t.Errorf("stats = %+v, want 1 written and 3 deduped", st)
	}

	// Dedupe is a property of the file, not of the process: it holds across a
	// restart, which is what makes reconnect gap-fill safe.
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r2 := newRecorder(t, db, RecorderOptions{})
	record(t, r2, live)
	if n, _ := db.Count(ctx, "#eco"); n != 1 {
		t.Errorf("after a restart the same msgid produced %d rows, want 1", n)
	}
}

// TestMessagesWithoutMsgIDDoNotCollide pins the NULL-vs-empty-string rule: a
// status line and a message from a server that does not speak IRCv3 have no
// msgid, and SQLite would collapse them all into one row if they were stored
// as "".
func TestMessagesWithoutMsgIDDoNotCollide(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	var ms []Message
	for i := 0; i < 5; i++ {
		m := msg("#eco", "", at(i), "alice", fmt.Sprintf("no msgid %d", i))
		m.Kind = model.MsgStatus
		ms = append(ms, m)
	}
	record(t, r, ms...)

	got, err := db.Latest(ctx, "#eco", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("5 msgid-less messages produced %d rows: %v", len(got), bodies(got))
	}
	for _, m := range got {
		if m.MsgID != "" {
			t.Errorf("a msgid-less message read back with msgid %q", m.MsgID)
		}
	}
}

// TestRecordNeverBlocks pins the contract the UI depends on: whatever the disk
// is doing, Record returns. When the queue cannot keep up it drops and says
// so, because a chat client that stalls its UI to write a log line is the
// worse bug.
func TestRecordNeverBlocks(t *testing.T) {
	db := openTestDB(t)
	// A queue of 4 and a batch that never fills: the writer goroutine holds
	// what it has drained and the queue is full almost immediately.
	r := newRecorder(t, db, RecorderOptions{Queue: 4, Batch: 1 << 20, Interval: time.Hour})

	// Enough to overflow a queue of four many times over, without making the
	// race-detector run wait on twenty thousand instrumented inserts.
	const n = 4000
	start := time.Now()
	accepted := 0
	for i := 0; i < n; i++ {
		if r.Record(msg("#eco", fmt.Sprintf("m%d", i), at(i), "alice", "burst")) {
			accepted++
		}
	}
	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Fatalf("%d Record calls took %s - Record blocked on the disk", n, elapsed)
	}
	st := r.Stats()
	if st.Dropped == 0 {
		t.Fatal("nothing was dropped, so this test proved nothing about a full queue")
	}
	if uint64(accepted)+st.Dropped != n {
		t.Errorf("accepted %d + dropped %d != %d recorded", accepted, st.Dropped, n)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Everything Record said yes to is on disk. Nothing it said no to is.
	got, err := db.Count(context.Background(), "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(accepted) {
		t.Errorf("archive holds %d rows, want the %d Record accepted", got, accepted)
	}
}

// TestCloseFlushesUnderConcurrentProducers pins the deterministic-Close
// promise: every message Record accepted before Close returned is on disk,
// even with several goroutines still recording as Close runs.
func TestCloseFlushesUnderConcurrentProducers(t *testing.T) {
	db := openTestDB(t)
	r := newRecorder(t, db, RecorderOptions{Queue: 512, Batch: 32, Interval: 20 * time.Millisecond})

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted = map[string]bool{}
		stop     = make(chan struct{})
	)
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("g%d-%d", g, i)
				if r.Record(msg("#eco", id, at(i), "alice", id)) {
					mu.Lock()
					accepted[id] = true
					mu.Unlock()
				}
			}
		}(g)
	}
	time.Sleep(50 * time.Millisecond)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(stop)
	wg.Wait()

	// Records that raced Close may have been refused; those are dropped, and
	// the map only holds the ones Record accepted.
	mu.Lock()
	want := len(accepted)
	mu.Unlock()
	if want == 0 {
		t.Fatal("no message was accepted, so this test proved nothing")
	}

	ctx := testCtx(t)
	got, err := db.Count(ctx, "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(want) {
		t.Errorf("archive holds %d rows, want the %d Record accepted before Close returned", got, want)
	}
	// And a Record after Close is refused rather than lost silently.
	if r.Record(msg("#eco", "after-close", at(9999), "alice", "too late")) {
		t.Error("Record accepted a message after Close")
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestLoggingOptOutNeverTouchesDisk pins the privacy contract's sharpest
// promise: a buffer with logging off is not filtered out later, it never
// reaches a statement at all.
func TestLoggingOptOutNeverTouchesDisk(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	if err := r.SetLogging(ctx, "#secret", false); err != nil {
		t.Fatal(err)
	}
	if r.Record(msg("#secret", "s1", at(1), "alice", "off the record")) {
		t.Error("Record accepted a message for a buffer with logging off")
	}
	record(t, r, msg("#eco", "e1", at(1), "alice", "on the record"))

	if n, err := db.Count(ctx, "#secret"); err != nil || n != 0 {
		t.Errorf("#secret holds %d rows (%v), want none", n, err)
	}
	// Not even in the search index, which is the other half of "never
	// touched disk".
	hits, err := db.Search(ctx, "record", Filters{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Buffer != "#eco" {
		t.Errorf("search found %v, want only the #eco line", hitBodies(hits))
	}
	if st := r.Stats(); st.Skipped != 1 {
		t.Errorf("stats = %+v, want 1 skipped", st)
	}

	// Turning it back on takes effect on the next message, with no restart -
	// and through the DB's method as well as the Recorder's, because a
	// privacy control that only works through one of two doors is not one.
	if err := db.SetLogging(ctx, "#secret", true); err != nil {
		t.Fatal(err)
	}
	record(t, r, msg("#secret", "s2", at(2), "alice", "on again"))
	if n, err := db.Count(ctx, "#secret"); err != nil || n != 1 {
		t.Errorf("#secret holds %d rows (%v) after logging was turned back on, want 1", n, err)
	}

	// The opt-out is in the file, so a fresh Recorder honours it too.
	if err := db.SetLogging(ctx, "#secret", false); err != nil {
		t.Fatal(err)
	}
	if r.Record(msg("#secret", "s2b", at(2), "alice", "off again")) {
		t.Error("a running Recorder kept logging after DB.SetLogging turned it off")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r2 := newRecorder(t, db, RecorderOptions{})
	if r2.Record(msg("#secret", "s3", at(3), "alice", "still off")) {
		t.Error("a fresh Recorder logged a buffer whose opt-out is stored on disk")
	}
}

// TestRecorderMaintainsLastMsgID pins the reconnect anchor: after a batch,
// every buffer it touched knows its newest msgid, so CHATHISTORY AFTER has
// something to ask for without scanning.
func TestRecorderMaintainsLastMsgID(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	record(t, r,
		msg("#eco", "a1", at(1), "alice", "one"),
		msg("#eco", "a2", at(2), "alice", "two"),
		msg("#other", "b1", at(3), "bob", "elsewhere"),
	)
	for _, tc := range []struct{ buffer, want string }{
		{"#eco", "a2"},
		{"#other", "b1"},
		{"#never-seen", ""},
	} {
		got, err := db.LastMsgID(ctx, tc.buffer)
		if err != nil {
			t.Fatalf("LastMsgID(%q): %v", tc.buffer, err)
		}
		if got != tc.want {
			t.Errorf("LastMsgID(%q) = %q, want %q", tc.buffer, got, tc.want)
		}
	}

	// Replaying older history does not move the anchor backwards.
	record(t, r, msg("#eco", "a0", at(0), "alice", "older"))
	if got, _ := db.LastMsgID(ctx, "#eco"); got != "a2" {
		t.Errorf("after replaying older history LastMsgID = %q, want a2", got)
	}
}

// TestRecorderReportsWriteErrors pins that a failed batch is counted and
// reported rather than swallowed - the producer is long gone by then, so the
// error has nowhere else to go.
func TestRecorderReportsWriteErrors(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	var (
		mu   sync.Mutex
		errs []error
	)
	r := newRecorder(t, db, RecorderOptions{OnError: func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}})

	// Close the database out from under the recorder: the next batch cannot
	// be written.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if !r.Record(msg("#eco", "x", at(1), "alice", "into the void")) {
		t.Fatal("Record refused before the write failed")
	}
	if err := r.Flush(ctx); err == nil {
		t.Error("Flush reported success after the write failed")
	}
	if st := r.Stats(); st.Errors == 0 {
		t.Errorf("stats = %+v, want a counted error", st)
	}
	if r.Err() == nil {
		t.Error("Err returned nil after a failed write")
	}
	mu.Lock()
	n := len(errs)
	mu.Unlock()
	if n == 0 {
		t.Error("OnError was never called")
	}
	if err := r.Close(); err == nil {
		t.Error("Close hid the write error")
	}
}

// TestFromModelProjection pins the one place the store's message and the
// archive's row meet, so the wiring milestone has nothing to invent.
func TestFromModelProjection(t *testing.T) {
	in := model.Message{
		ID:     42,
		MsgID:  "abc",
		Buffer: model.BufferID{Network: "libera", Folded: "#eco"},
		Time:   at(7),
		Sender: model.Sender{Nick: "Alice", Account: "alice-acct"},
		Kind:   model.MsgAction,
		Text:   "waves",
		Tags:   map[string]string{"+draft/reply": "xyz"},
		// A pending echo has no msgid; the archive takes the state as it
		// finds it and the package doc says when to record one.
		Pending:  model.PendingFailed,
		Redacted: false,
	}
	got := FromModel(in)
	want := Message{
		MsgID:   "abc",
		Buffer:  "#eco",
		Time:    at(7),
		Sender:  "Alice",
		Account: "alice-acct",
		Kind:    model.MsgAction,
		Body:    "waves",
		Tags:    map[string]string{"+draft/reply": "xyz"},
		Pending: model.PendingFailed,
	}
	if got.MsgID != want.MsgID || got.Buffer != want.Buffer || !got.Time.Equal(want.Time) ||
		got.Sender != want.Sender || got.Account != want.Account || got.Kind != want.Kind ||
		got.Body != want.Body || got.Pending != want.Pending || got.Redacted != want.Redacted {
		t.Errorf("FromModel = %+v, want %+v", got, want)
	}
	if len(got.Tags) != 1 || got.Tags["+draft/reply"] != "xyz" {
		t.Errorf("FromModel tags = %v", got.Tags)
	}

	// and the round trip through the disk keeps every field
	db := openTestDB(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, got)
	back, err := db.Latest(testCtx(t), "#eco", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Fatalf("read back %d rows, want 1", len(back))
	}
	b := back[0]
	if b.MsgID != want.MsgID || b.Sender != want.Sender || b.Account != want.Account ||
		b.Kind != want.Kind || b.Body != want.Body || b.Pending != want.Pending ||
		!b.Time.Equal(want.Time) || b.Tags["+draft/reply"] != "xyz" {
		t.Errorf("round trip = %+v, want %+v", b, want)
	}
	if b.RowID == 0 {
		t.Error("a message read back has no rowid, so nothing can page from it")
	}
}

// TestTheStatusBufferGetsAName pins the one buffer whose folded name is empty
// upstream. Without a substitute its MOTD and connection notices would be
// indistinguishable from a caller that forgot to name a buffer, and the
// Recorder would drop them.
func TestTheStatusBufferGetsAName(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})

	line := FromModel(model.Message{
		MsgID:  "motd-1",
		Buffer: model.ServerBuffer("libera"),
		Time:   at(1),
		Kind:   model.MsgStatus,
		Text:   "- Welcome to the network -",
	})
	if line.Buffer != ServerBufferName {
		t.Fatalf("the status buffer projects to %q, want %q", line.Buffer, ServerBufferName)
	}
	record(t, r, line)
	got, err := db.Latest(ctx, ServerBufferName, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != model.MsgStatus {
		t.Errorf("the status buffer holds %v, want the MOTD line", bodies(got))
	}
	// A named buffer is untouched by the substitution.
	if got := FoldedName(model.BufferID{Network: "libera", Folded: "#eco"}); got != "#eco" {
		t.Errorf("FoldedName(#eco) = %q", got)
	}
}

// TestRecordRefusesAnUnnamedBuffer pins that a message with no buffer is a
// caller bug, refused rather than filed under "".
func TestRecordRefusesAnUnnamedBuffer(t *testing.T) {
	db := openTestDB(t)
	r := newRecorder(t, db, RecorderOptions{})
	if r.Record(msg("", "x", at(1), "alice", "nowhere")) {
		t.Error("Record accepted a message with no buffer")
	}
	if st := r.Stats(); st.Dropped != 1 {
		t.Errorf("stats = %+v, want it counted as dropped", st)
	}
}
