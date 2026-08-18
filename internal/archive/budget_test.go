package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The performance budgets (blueprint §5.4, risk R-10: numbers are set at first
// real profiling — this is it — and may be revised only with a written note).
//
// These are alarms, not benchmarks. They are set roughly an order of magnitude
// above what this milestone measured on a 1M-row archive, so they catch "the
// index stopped being used" and "every page now scans the table" without
// failing on a loaded CI box. The numbers actually measured are in the commit
// message and are reproduced by BenchmarkPage/BenchmarkSearch below.
const (
	budgetLatest = 50 * time.Millisecond  // opening a buffer
	budgetPage   = 50 * time.Millisecond  // one scroll-up page, however deep
	budgetSearch = 750 * time.Millisecond // a two-word search over the archive
	budgetBurst  = 2000                   // messages per second, reconnect burst
)

// budgetRows is how big the corpus is. The default keeps the everyday suite
// honest and quick; the real 1M-row profiling run is
//
//	ECO_ARCHIVE_BUDGET_ROWS=1000000 go test -run Budget -v ./internal/archive/
//
// which is what produced the numbers in the commit message.
func budgetRows(t testing.TB) int {
	if v := os.Getenv("ECO_ARCHIVE_BUDGET_ROWS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1000 {
			t.Fatalf("ECO_ARCHIVE_BUDGET_ROWS=%q is not a row count", v)
		}
		return n
	}
	if raceEnabled {
		return 20000
	}
	return 60000
}

// skipUnderRace keeps the performance budgets out of the race-detector run.
// Instrumented, a pure-Go SQLite writes about twenty times slower, so a
// latency measured there says nothing about the shipped binary — and a suite
// that takes minutes is a suite people stop running. `make test-budget` is the
// run these numbers come from; the concurrency tests that -race exists for
// (Record, Close, SetLogging) stay in the race run.
func skipUnderRace(t *testing.T) {
	t.Helper()
	if raceEnabled {
		t.Skip("performance budgets are meaningless under the race detector; run make test-budget")
	}
	if testing.Short() {
		t.Skip("builds a large corpus")
	}
}

// budgetBuffers is how many channels the corpus is spread over: the shape of a
// user who is in a lot of channels, which is the shape that makes the index
// matter.
const budgetBuffers = 40

// corpusWords is the vocabulary the fake conversation is built from. Word
// frequency is what decides a search's cost, so the corpus has the three cases
// on purpose: "zarquon" appears in one message in 503, "deploy" in one
// in seven, and "the" in every one of them — the pathological case a real
// archive also contains.
var corpusWords = []string{
	"morning", "anyone", "seen", "the", "build", "failing", "again", "on", "main",
	"looks", "like", "a", "flake", "restarting", "it", "now", "thanks", "for", "the", "fix",
	"reviewing", "that", "patch", "later", "today", "lunch", "coffee", "meeting", "notes",
	"wiki", "page", "updated", "server", "restarted", "logs", "look", "clean", "merged",
}

// fillCorpus writes n messages across budgetBuffers buffers, bypassing the
// Recorder — this is scaffolding for the query budgets, and the Recorder has
// its own budget below.
func fillCorpus(tb testing.TB, db *DB, n int) {
	tb.Helper()
	ctx := context.Background()
	const perTx = 5000
	start := epoch.Add(-time.Duration(n) * time.Second)

	for done := 0; done < n; {
		tx, err := db.w.BeginTx(ctx, nil)
		if err != nil {
			tb.Fatal(err)
		}
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO messages(msgid, buffer_folded, ts, sender, account, kind, body, tags_json, pending, redacted)
VALUES(?, ?, ?, ?, '', 0, ?, NULL, 0, 0)`)
		if err != nil {
			tb.Fatal(err)
		}
		for i := 0; i < perTx && done < n; i, done = i+1, done+1 {
			buffer := fmt.Sprintf("#chan%02d", done%budgetBuffers)
			sender := fmt.Sprintf("nick%03d", done%250)
			if _, err := stmt.ExecContext(ctx,
				fmt.Sprintf("corpus-%08d", done), buffer,
				start.Add(time.Duration(done)*time.Second).UnixMilli(),
				sender, corpusBody(done)); err != nil {
				tb.Fatal(err)
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			tb.Fatal(err)
		}
	}
	if err := db.Optimize(ctx); err != nil {
		tb.Fatal(err)
	}
}

// corpusBody builds one plausible line of chat, deterministically.
func corpusBody(i int) string {
	var b strings.Builder
	b.WriteString("the ") // in every message: the worst case for a search
	for w := 0; w < 12; w++ {
		b.WriteString(corpusWords[(i*7+w*13)%len(corpusWords)])
		b.WriteByte(' ')
	}
	// 7 and 503 are coprime with the buffer and nick counts, so both words
	// land in every channel and every nick's messages rather than aliasing
	// onto a few of them.
	if i%7 == 0 {
		b.WriteString("deploy ")
	}
	if i%503 == 0 {
		b.WriteString("zarquon ")
	}
	return strings.TrimSpace(b.String())
}

// percentiles reports p50/p95/max of a set of durations.
func percentiles(ds []time.Duration) (p50, p95, max time.Duration) {
	if len(ds) == 0 {
		return 0, 0, 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	at := func(q float64) time.Duration {
		i := int(q * float64(len(ds)))
		if i >= len(ds) {
			i = len(ds) - 1
		}
		return ds[i]
	}
	return at(0.50), at(0.95), ds[len(ds)-1]
}

// TestQueryBudgetAtScale is the query half of the performance budget: opening
// a buffer, paging deep into its history and searching the whole archive all
// stay in the tens of milliseconds at a million rows, because each one is an
// index seek plus a short walk rather than a scan.
func TestQueryBudgetAtScale(t *testing.T) {
	skipUnderRace(t)
	rows := budgetRows(t)
	db, err := Open(filepath.Join(t.TempDir(), "budget.db"), Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	filled := time.Now()
	fillCorpus(t, db, rows)
	fillTook := time.Since(filled)
	ctx := testCtx(t) // after the corpus: its deadline is for the queries

	total, err := db.Count(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	size, err := db.SizeOnDisk()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("corpus: %d rows over %d buffers, %.1f MB on disk, built in %s",
		total, budgetBuffers, float64(size)/(1<<20), fillTook.Round(time.Millisecond))

	const samples = 60

	// 1. Opening a buffer.
	var latest []time.Duration
	for i := 0; i < samples; i++ {
		buffer := fmt.Sprintf("#chan%02d", i%budgetBuffers)
		start := time.Now()
		if _, err := db.Latest(ctx, buffer, 100); err != nil {
			t.Fatal(err)
		}
		latest = append(latest, time.Since(start))
	}
	report(t, "Latest(100)", latest, budgetLatest)

	// 2. Scrolling up. Anchors are spread through the whole history, so this
	// measures a deep page as well as a recent one.
	var deep []time.Duration
	for i := 0; i < samples; i++ {
		buffer := fmt.Sprintf("#chan%02d", i%budgetBuffers)
		anchor := int64(rows) * int64(i) / int64(samples)
		if anchor < 1 {
			anchor = 1
		}
		start := time.Now()
		if _, err := db.Page(ctx, PageQuery{Buffer: buffer, BeforeRowID: anchor, Limit: 100}); err != nil {
			t.Fatal(err)
		}
		deep = append(deep, time.Since(start))
	}
	report(t, "Page(100, deep anchor)", deep, budgetPage)

	// 3. Search: a rare word, a common word, a two-word query and a
	// filtered one — the four shapes the search box produces.
	for _, q := range []struct{ name, query string }{
		{"rare word (1 in 503)", "zarquon"},
		{"mid word (1 in 7)", "deploy"},
		{"the worst case: a word in every message", "the"},
		{"two words", "build deploy"},
		{"word in one buffer", "deploy in:#chan07"},
		{"common word in one buffer", "the in:#chan07"},
		{"from a nick", "from:nick042"},
		{"word from a nick", "deploy from:nick042"},
	} {
		var ds []time.Duration
		for i := 0; i < samples/4; i++ {
			start := time.Now()
			hits, err := db.Search(ctx, q.query, Filters{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) == 0 {
				t.Errorf("search %q found nothing, so its timing means nothing", q.query)
			}
			ds = append(ds, time.Since(start))
		}
		report(t, "Search("+q.name+")", ds, budgetSearch)
	}

	// 4. And the anchor lookups the pager does on the way.
	var markers []time.Duration
	for i := 0; i < samples; i++ {
		buffer := fmt.Sprintf("#chan%02d", i%budgetBuffers)
		start := time.Now()
		if _, err := db.LastMsgID(ctx, buffer); err != nil {
			t.Fatal(err)
		}
		markers = append(markers, time.Since(start))
	}
	report(t, "LastMsgID", markers, budgetPage)
}

func report(t *testing.T, name string, ds []time.Duration, budget time.Duration) {
	t.Helper()
	p50, p95, max := percentiles(ds)
	t.Logf("%-28s p50 %8s  p95 %8s  max %8s  (budget p95 %s)",
		name, p50.Round(time.Microsecond), p95.Round(time.Microsecond), max.Round(time.Microsecond), budget)
	if p95 > budget {
		t.Errorf("%s p95 = %s, over the budget of %s", name, p95, budget)
	}
}

// TestReconnectBurstBudget is the write half: a reconnect that replays forty
// channels of history has to be absorbed by the write-behind without dropping
// anything and without the producer waiting on the disk.
func TestReconnectBurstBudget(t *testing.T) {
	skipUnderRace(t)
	db, err := Open(filepath.Join(t.TempDir(), "burst.db"), Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := testCtx(t)

	const perChannel = 500
	total := budgetBuffers * perChannel

	r, err := NewRecorder(ctx, db, RecorderOptions{Queue: 8192, Batch: 256, Interval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	// One CHATHISTORY batch per channel, the way a reconnect actually
	// arrives: the client flushes between batches, so nothing is queued for
	// long and the measurement is of the archive, not of a buffer in RAM.
	var slowest time.Duration
	start := time.Now()
	for c := 0; c < budgetBuffers; c++ {
		buffer := fmt.Sprintf("#chan%02d", c)
		for i := 0; i < perChannel; i++ {
			callStart := time.Now()
			if !r.Record(msg(buffer, fmt.Sprintf("burst-%02d-%04d", c, i), at(i), "alice", "replayed history line")) {
				t.Fatalf("the recorder dropped %s/%d during the burst", buffer, i)
			}
			if d := time.Since(callStart); d > slowest {
				slowest = d
			}
		}
		if err := r.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	st := r.Stats()
	rate := float64(total) / elapsed.Seconds()
	t.Logf("reconnect burst: %d messages over %d channels in %s = %.0f msg/s (%d transactions, slowest Record call %s)",
		total, budgetBuffers, elapsed.Round(time.Millisecond), rate, st.Batches, slowest.Round(time.Microsecond))

	if st.Dropped != 0 || st.Errors != 0 {
		t.Errorf("stats = %+v, want nothing dropped and no errors", st)
	}
	if st.Written != uint64(total) {
		t.Errorf("wrote %d of %d messages", st.Written, total)
	}
	if rate < budgetBurst {
		t.Errorf("burst throughput %.0f msg/s is under the budget of %d msg/s", rate, budgetBurst)
	}
	if slowest > 100*time.Millisecond {
		t.Errorf("the slowest Record call took %s — the write-behind blocked a producer", slowest)
	}

	// The second half of a reconnect: the server replays what we already
	// have. Dedupe has to make that cheap, or every reconnect costs as much
	// as the first sync.
	r2, err := NewRecorder(ctx, db, RecorderOptions{Queue: 8192, Batch: 256, Interval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	replayStart := time.Now()
	for c := 0; c < budgetBuffers; c++ {
		buffer := fmt.Sprintf("#chan%02d", c)
		for i := 0; i < perChannel; i++ {
			r2.Record(msg(buffer, fmt.Sprintf("burst-%02d-%04d", c, i), at(i), "alice", "replayed history line"))
		}
		if err := r2.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := r2.Close(); err != nil {
		t.Fatal(err)
	}
	replay := time.Since(replayStart)
	st2 := r2.Stats()
	t.Logf("replay of the same burst: %s = %.0f msg/s, %d written, %d deduped",
		replay.Round(time.Millisecond), float64(total)/replay.Seconds(), st2.Written, st2.Deduped)
	if st2.Written != 0 || st2.Deduped != uint64(total) {
		t.Errorf("a full replay wrote %d and deduped %d, want 0 and %d", st2.Written, st2.Deduped, total)
	}
	if n, err := db.Count(ctx, ""); err != nil || n != int64(total) {
		t.Errorf("after the replay the archive holds %d rows (%v), want %d", n, err, total)
	}
}

// --- benchmarks: the same measurements, for a number rather than an alarm ---

func benchDB(b *testing.B) *DB {
	b.Helper()
	db, err := Open(filepath.Join(b.TempDir(), "bench.db"), Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	fillCorpus(b, db, budgetRows(b))
	return db
}

func BenchmarkLatest(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.Latest(ctx, fmt.Sprintf("#chan%02d", i%budgetBuffers), 100); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPageDeep(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	rows := int64(budgetRows(b))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		anchor := rows*int64(i%100)/100 + 1
		if _, err := db.Page(ctx, PageQuery{
			Buffer: fmt.Sprintf("#chan%02d", i%budgetBuffers), BeforeRowID: anchor, Limit: 100,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	queries := []string{"zarquon", "deploy", "the", "build deploy", "deploy in:#chan07", "from:nick042"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.Search(ctx, queries[i%len(queries)], Filters{}, 100); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecorderBurst(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "bench-burst.db"), Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	r, err := NewRecorder(ctx, db, RecorderOptions{Queue: 1 << 16, Batch: 256, Interval: 50 * time.Millisecond})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Record(msg(fmt.Sprintf("#chan%02d", i%budgetBuffers), fmt.Sprintf("bench-%d", i), at(i), "alice", "one more line"))
		if i%1000 == 999 {
			if err := r.Flush(ctx); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := r.Flush(ctx); err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
}
