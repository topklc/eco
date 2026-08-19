package archive

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestReadsDoNotWaitForWrites pins the two-pool, WAL design: while the
// Recorder writes as fast as the disk will take it, reads keep completing and
// none of them ever waits on the writer.
//
// What is asserted is what can be attributed: no read fails (a shared writer
// connection would surface SQLITE_BUSY), reads keep making progress, and no
// single read takes anywhere near the busy timeout - which is what "this read
// is queued behind a writer" looks like. The rates are logged rather than
// asserted, because a synthetic writer saturating a core slows reads for
// reasons that have nothing to do with locking.
func TestReadsDoNotWaitForWrites(t *testing.T) {
	db := openTestDB(t)
	seed(t, db, "#eco", 200)

	readLoop := func(d time.Duration, readers int) (int64, time.Duration, error) {
		var (
			wg      sync.WaitGroup
			reads   atomic.Int64
			slowest atomic.Int64
			failed  atomic.Value // error
			stop    = make(chan struct{})
		)
		for g := 0; g < readers; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := context.Background()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					var err error
					began := time.Now()
					switch i % 3 {
					case 0:
						_, err = db.Latest(ctx, "#eco", 50)
					case 1:
						_, err = db.Page(ctx, PageQuery{Buffer: "#eco", BeforeRowID: int64(i%150) + 1, Limit: 50})
					case 2:
						_, err = db.Search(ctx, "deploy", Filters{Buffer: "#eco"}, 20)
					}
					if err != nil {
						failed.Store(err)
						return
					}
					if took := int64(time.Since(began)); took > slowest.Load() {
						slowest.Store(took)
					}
					reads.Add(1)
				}
			}()
		}
		time.Sleep(d)
		close(stop)
		wg.Wait()
		if err, ok := failed.Load().(error); ok && err != nil {
			return reads.Load(), time.Duration(slowest.Load()), err
		}
		return reads.Load(), time.Duration(slowest.Load()), nil
	}

	const window = 300 * time.Millisecond
	solo, soloSlowest, err := readLoop(window, 4)
	if err != nil {
		t.Fatalf("a read failed with nothing else running: %v", err)
	}
	if solo == 0 {
		t.Fatal("no reads completed at all")
	}

	// Now the same readers, with the Recorder writing as hard as it can.
	r := newRecorder(t, db, RecorderOptions{Queue: 4096, Batch: 64, Interval: 5 * time.Millisecond})
	var (
		writes   atomic.Int64
		stopWrit = make(chan struct{})
		writerWG sync.WaitGroup
	)
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; ; i++ {
			select {
			case <-stopWrit:
				return
			default:
			}
			if r.Record(msg("#eco", fmt.Sprintf("live-%06d", i), at(1000+i), "alice", "a live line about the deploy")) {
				writes.Add(1)
			}
		}
	}()

	together, slowest, err := readLoop(window, 4)
	close(stopWrit)
	writerWG.Wait()
	if err != nil {
		t.Fatalf("a read failed while the recorder was writing: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("recorder Close: %v", err)
	}
	if writes.Load() == 0 {
		t.Fatal("no writes happened, so nothing was contended")
	}

	t.Logf("reads in %s: %d alone (slowest %s), %d while %d messages were being written (slowest %s)",
		window, solo, soloSlowest.Round(time.Microsecond), together, writes.Load(), slowest.Round(time.Microsecond))
	if together == 0 {
		t.Error("no read completed while the recorder was writing - readers are starved")
	}
	// A read that waited for the writer would be waiting on a lock, and that
	// wait is measured against the busy timeout (5s), not against the odd
	// slow query.
	if slowest > time.Second {
		t.Errorf("the slowest read took %s while writing - that is a lock wait, not a query", slowest)
	}
}

// TestTwoProcessesShareTheFile pins the busy-timeout contract: a second eco on
// the same profile (a second window, or a restart racing its predecessor)
// waits for the writer rather than failing, and neither corrupts the file.
func TestTwoProcessesShareTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	ctx := testCtx(t)

	first := openTestDBAt(t, path)
	second, err := Open(path, Options{Clock: model.FixedClock(epoch), BusyTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("opening the same archive twice: %v", err)
	}
	defer second.Close()

	r1 := newRecorder(t, first, RecorderOptions{Batch: 32})
	r2, err := NewRecorder(ctx, second, RecorderOptions{Batch: 32})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i, r := range []*Recorder{r1, r2} {
		wg.Add(1)
		go func(i int, r *Recorder) {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				r.Record(msg("#eco", fmt.Sprintf("p%d-%03d", i, j), at(j), "alice", "from one of two clients"))
			}
			if err := r.Flush(ctx); err != nil {
				t.Errorf("recorder %d flush: %v", i, err)
			}
		}(i, r)
	}
	wg.Wait()
	if err := r2.Close(); err != nil {
		t.Errorf("second recorder Close: %v", err)
	}
	if err := r1.Close(); err != nil {
		t.Errorf("first recorder Close: %v", err)
	}

	if st := r1.Stats(); st.Errors != 0 {
		t.Errorf("the first client hit %d write errors: %v", st.Errors, r1.Err())
	}
	if st := r2.Stats(); st.Errors != 0 {
		t.Errorf("the second client hit %d write errors: %v", st.Errors, r2.Err())
	}
	n, err := first.Count(ctx, "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if n != 600 {
		t.Errorf("the shared archive holds %d messages, want both clients' 600", n)
	}
	var integrity string
	if err := second.r.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Errorf("integrity_check = %q after two writers", integrity)
	}
}

// TestRecorderGoroutineStops pins that Close is the end of the goroutine, not
// just of the queue: the package starts one goroutine per Recorder and it must
// not outlive it, whatever state the archive is in.
func TestRecorderGoroutineStops(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	r, err := NewRecorder(ctx, db, RecorderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r.Record(msg("#eco", "m1", at(1), "alice", "hello"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder goroutine is still running after Close")
	}

	// Even with the archive already closed underneath it.
	r2, err := NewRecorder(ctx, db, RecorderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2.Record(msg("#eco", "m2", at(2), "alice", "hello again"))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_ = r2.Close() // the write fails; the goroutine still has to end
	select {
	case <-r2.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the recorder goroutine outlived a closed archive")
	}
}
