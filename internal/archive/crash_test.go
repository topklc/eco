package archive

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

const crashEnv = "ECO_ARCHIVE_CRASH_DB"

// TestCrashChild is not a test: it is the body of the process
// TestCrashMidWriteKeepsWhatWasFlushed kills. It runs only when the parent
// sets crashEnv, and it never returns — the whole point is to die with a
// transaction in flight, the way a kill -9 or a power button does.
func TestCrashChild(t *testing.T) {
	path := os.Getenv(crashEnv)
	if path == "" {
		t.Skip("not the crash child")
	}
	ctx := context.Background()
	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		fmt.Println("CHILD-ERROR", err)
		os.Exit(2)
	}
	r, err := NewRecorder(ctx, db, RecorderOptions{Batch: 50, Interval: time.Hour})
	if err != nil {
		fmt.Println("CHILD-ERROR", err)
		os.Exit(2)
	}

	const flushed = 200
	for i := 0; i < flushed; i++ {
		r.Record(msg("#eco", fmt.Sprintf("flushed-%04d", i), at(i), "alice", fmt.Sprintf("durable %d", i)))
	}
	if err := r.Flush(ctx); err != nil {
		fmt.Println("CHILD-ERROR", err)
		os.Exit(2)
	}
	fmt.Println("FLUSHED", flushed)

	// Now fill the pipeline and die inside it: some of these are in the
	// queue, some in a half-built batch, some in a transaction that will
	// never commit.
	for i := 0; i < 5000; i++ {
		r.Record(msg("#eco", fmt.Sprintf("extra-%04d", i), at(flushed+i), "alice", "in flight"))
	}
	os.Exit(7) // no Close, no flush, no unwinding
}

// TestCrashMidWriteKeepsWhatWasFlushed is the crash-safety proof: kill the
// process with writes in flight, reopen the file, and find it intact — no
// corruption, no lost rows that a Flush had already promised, and a search
// index that still agrees with the table.
func TestCrashMidWriteKeepsWhatWasFlushed(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a subprocess")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot find the test binary: %v", err)
	}
	path := filepath.Join(t.TempDir(), "crash.db")

	cmd := exec.Command(exe, "-test.run=TestCrashChild", "-test.v")
	cmd.Env = append(os.Environ(), crashEnv+"="+path)
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("the child did not die where it was told to (%v, exit %v):\n%s",
			err, cmd.ProcessState, out)
	}
	flushed := 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if n, ok := strings.CutPrefix(sc.Text(), "FLUSHED "); ok {
			flushed, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	if flushed == 0 {
		t.Fatalf("the child never reported a flush:\n%s", out)
	}

	// Reopen. This is where WAL recovery happens.
	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatalf("reopening the archive after a crash: %v", err)
	}
	defer db.Close()
	ctx := testCtx(t)

	var integrity string
	if err := db.r.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want ok", integrity)
	}

	// Everything the child flushed is here. Nothing about the in-flight
	// messages is claimed: they were never promised.
	for i := 0; i < flushed; i++ {
		id := fmt.Sprintf("flushed-%04d", i)
		var n int
		if err := db.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE msgid = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s is missing after the crash (found %d rows)", id, n)
		}
	}

	// The FTS index survived with the table: an external-content index that
	// disagrees with its content table is the corruption this design could
	// plausibly produce, so it is checked rather than assumed.
	if _, err := db.w.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES('integrity-check')`); err != nil {
		t.Errorf("the search index did not survive the crash: %v", err)
	}
	hits, err := db.Search(ctx, "durable", Filters{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < flushed {
		t.Errorf("the index holds %d of the %d flushed messages", len(hits), flushed)
	}

	// And the archive is still an archive: it takes new messages and hands
	// back a page in the right order.
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r, msg("#eco", "after-the-crash", at(999999), "alice", "still here"))
	page, err := db.Latest(ctx, "#eco", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[1].Body != "still here" {
		t.Errorf("after the crash the newest page is %v", bodies(page))
	}
}

// TestCloseMidWriteLosesNothing pins the orderly half of the same question:
// closing the recorder and the database while producers are still recording
// leaves a file that reopens with every accepted message in it.
func TestCloseMidWriteLosesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.db")
	db, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRecorder(context.Background(), db, RecorderOptions{Queue: 256, Batch: 16, Interval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted []string
		stop     = make(chan struct{})
	)
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("g%d-%05d", g, i)
				if r.Record(msg("#eco", id, at(i), "alice", id)) {
					mu.Lock()
					accepted = append(accepted, id)
					mu.Unlock()
				}
			}
		}(g)
	}
	time.Sleep(40 * time.Millisecond)
	if err := r.Close(); err != nil {
		t.Fatalf("recorder Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db Close: %v", err)
	}
	close(stop)
	wg.Wait()

	mu.Lock()
	want := append([]string(nil), accepted...)
	mu.Unlock()
	if len(want) == 0 {
		t.Fatal("nothing was accepted, so this test proved nothing")
	}

	again, err := Open(path, Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer again.Close()
	ctx := testCtx(t)

	var integrity string
	if err := again.r.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q, want ok", integrity)
	}

	rows, err := again.r.QueryContext(ctx, `SELECT msgid FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		have[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, id := range want {
		if !have[id] {
			t.Fatalf("%s was accepted before Close but is not in the reopened archive (%d of %d present)",
				id, len(have), len(want))
		}
	}
	if len(have) != len(want) {
		t.Errorf("the archive holds %d messages, want the %d that were accepted", len(have), len(want))
	}
}
