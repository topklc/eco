package archive

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// epoch is the fixed instant every test's timestamps hang off, so nothing in
// this package's suite reads a real clock.
var epoch = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// at returns epoch + n seconds.
func at(n int) time.Time { return epoch.Add(time.Duration(n) * time.Second) }

// openTestDB opens a fresh archive in a temp dir with a frozen clock.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	return openTestDBAt(t, filepath.Join(t.TempDir(), "archive.db"))
}

func openTestDBAt(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

// newRecorder starts a recorder that writes as soon as it is asked to, so a
// test never waits on the flush interval.
func newRecorder(t *testing.T, db *DB, opts RecorderOptions) *Recorder {
	t.Helper()
	if opts.Interval == 0 {
		opts.Interval = 5 * time.Millisecond
	}
	r, err := NewRecorder(context.Background(), db, opts)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// msg builds a plain privmsg.
func msg(buffer, msgid string, ts time.Time, sender, body string) Message {
	return Message{
		MsgID:  msgid,
		Buffer: buffer,
		Time:   ts,
		Sender: sender,
		Kind:   model.MsgPrivmsg,
		Body:   body,
	}
}

// record puts messages through the recorder and waits for them to land.
func record(t *testing.T, r *Recorder, ms ...Message) {
	t.Helper()
	for _, m := range ms {
		if !r.Record(m) {
			t.Fatalf("Record(%q/%q) refused the message", m.Buffer, m.MsgID)
		}
	}
	flush(t, r)
}

func flush(t *testing.T, r *Recorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// bodies is what a page or a hit list says, in order — the shape most
// assertions in this package want to compare.
func bodies(ms []Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Body)
	}
	return out
}

func hitBodies(hs []Hit) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Body)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ctx is a background context with a test-scoped deadline: a query that hangs
// fails the test instead of the package.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	return ctx
}
