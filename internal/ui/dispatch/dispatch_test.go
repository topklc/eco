package dispatch

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The funnel's contract, headless: no Fyne app is created anywhere in this
// file. Options.Do is the seam — a direct call here, fyne.Do in production —
// which is what lets the queue, the coalescing window, the liveness rule and
// the shutdown be tested at all.
//
// Everything is synchronised on Flush or on a channel the recorder signals.
// The only place a wall-clock duration is asserted is TestCoalescingWindow,
// which is about the clock.

const net model.NetworkID = "n1"

// recorder is a Sink that writes down what it was told, in order, plus the
// batch structure it was told it in.
type recorder struct {
	mu    sync.Mutex
	calls []string
	// batches[i] is the calls (excluding BatchDone) that arrived in batch i.
	batches [][]string
	open    []string
	dones   int
	// doCalls counts turns of the UI thread the funnel asked for.
	doCalls int
	// signal, when non-nil, is pinged after every BatchDone.
	signal chan struct{}
}

func (r *recorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
	r.open = append(r.open, call)
}

func (r *recorder) LinesAdded(s SessionID, b Buffer, lines []model.Line, notify bool) {
	texts := make([]string, 0, len(lines))
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	call := fmt.Sprintf("lines %s %s [%s]", s, b.Name, strings.Join(texts, ","))
	if notify {
		call += " !"
	}
	r.add(call)
}
func (r *recorder) LinesPrepended(s SessionID, b Buffer, lines []model.Line) {
	texts := make([]string, 0, len(lines))
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	r.add(fmt.Sprintf("older %s %s [%s]", s, b.Name, strings.Join(texts, ",")))
}
func (r *recorder) HistoryLoading(s SessionID, b Buffer, loading bool) {
	r.add(fmt.Sprintf("loading %s %s %v", s, b.Name, loading))
}
func (r *recorder) AskDMLogging(s SessionID, b Buffer) {
	r.add(fmt.Sprintf("askdm %s %s", s, b.Name))
}
func (r *recorder) LineAmended(s SessionID, b Buffer) {
	r.add(fmt.Sprintf("amend %s %s", s, b.Name))
}
func (r *recorder) LineRedacted(s SessionID, b Buffer, msgID string) {
	r.add(fmt.Sprintf("redact %s %s %s", s, b.Name, msgID))
}
func (r *recorder) BufferListChanged(s SessionID) { r.add(fmt.Sprintf("buflist %s", s)) }
func (r *recorder) BufferRenamed(s SessionID, from, to Buffer) {
	r.add(fmt.Sprintf("rename %s %s->%s", s, from.Name, to.Name))
}
func (r *recorder) MembersChanged(s SessionID, b Buffer) {
	r.add(fmt.Sprintf("members %s %s", s, b.Name))
}
func (r *recorder) UnreadChanged(s SessionID, b Buffer, unread, highlights int) {
	r.add(fmt.Sprintf("unread %s %s %d/%d", s, b.Name, unread, highlights))
}
func (r *recorder) TopicChanged(s SessionID, b Buffer, topic string) {
	r.add(fmt.Sprintf("topic %s %s %q", s, b.Name, topic))
}
func (r *recorder) ConnStateChanged(s SessionID, c ConnState) {
	r.add(fmt.Sprintf("conn %s %v retry=%v", s, c.Status, c.Retrying))
}
func (r *recorder) JoinRefused(s SessionID, channel, reason string, needKey bool) {
	r.add(fmt.Sprintf("joinfail %s %s %q key=%v", s, channel, reason, needKey))
}
func (r *recorder) STSOffered(s SessionID, port string, seconds int64) {
	r.add(fmt.Sprintf("sts %s %s %d", s, port, seconds))
}
func (r *recorder) ChannelListDone(s SessionID, requested bool) {
	r.add(fmt.Sprintf("listdone %s req=%v", s, requested))
}

func (r *recorder) BatchDone() {
	r.mu.Lock()
	r.dones++
	r.batches = append(r.batches, r.open)
	r.open = nil
	sig := r.signal
	r.mu.Unlock()
	if sig != nil {
		select {
		case sig <- struct{}{}:
		default:
		}
	}
}

func (r *recorder) snapshot() ([]string, [][]string, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...), append([][]string(nil), r.batches...), r.dones, r.doCalls
}

func (r *recorder) countDo() { r.mu.Lock(); r.doCalls++; r.mu.Unlock() }

// newFunnel builds a Dispatcher whose UI thread is this goroutine and whose
// window is long enough that only an explicit Flush or Stop delivers anything.
// Tests that are about the clock set their own window.
func newFunnel(t *testing.T, window time.Duration) (*Dispatcher, *recorder) {
	t.Helper()
	r := &recorder{}
	d := New(Options{
		Sink:   r,
		Window: window,
		Do:     func(f func()) { r.countDo(); f() },
	})
	t.Cleanup(d.Stop)
	return d, r
}

// forever is a window no test will wait out: delivery then happens only when a
// test asks for it, which is what makes the assertions deterministic.
const forever = time.Hour

// --- helpers to build items --------------------------------------------------

func buf(name string) model.BufferID { return model.BufferID{Network: net, Folded: name} }

func line(text string) model.Line { return model.Line{Text: text} }

func added(name, text string) Item {
	return Of(model.MessageAdded{ChangeMeta: model.BufMeta(buf(name))}, name, line(text))
}

func created(name string) Item {
	return Of(model.BufferCreated{ChangeMeta: model.BufMeta(buf(name))}, name)
}

// --- ordering ----------------------------------------------------------------

// Within a batch, items are delivered in arrival order — never sorted, merged
// or de-duplicated.
//
// This is pinned here because it is pinned nowhere else: internal/transcript's
// records sort each step's facts canonically, so a chunk delivered backwards
// looks identical to them (M7 verification, blueprint progress log). The funnel
// is the layer that can promise order, so it promises it under test.
func TestOrderingWithinBatch(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	want := []string{
		"buflist s1",
		"lines s1 #eco [one]",
		"lines s1 #eco [two]",
		"members s1 #eco",
		"lines s1 #eco [three]",
		"topic s1 #eco \"hi\"",
	}
	d.Emit("s1", []Item{
		created("#eco"),
		added("#eco", "one"),
		added("#eco", "two"),
		Of(model.MembersChanged{ChangeMeta: model.BufMeta(buf("#eco"))}, "#eco"),
		added("#eco", "three"),
		Of(model.TopicChanged{ChangeMeta: model.BufMeta(buf("#eco")), Topic: "hi"}, "#eco"),
	})
	d.Flush()

	got, _, _, _ := r.snapshot()
	assertCalls(t, got, want)
}

// A history chunk is one item carrying N rows, and those rows keep the chunk's
// order: the exact bug the M7 mutation round found invisible to the records.
func TestOrderingWithinAHistoryChunk(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{Of(
		model.HistoryLoaded{ChangeMeta: model.BufMeta(buf("#eco"))}, "#eco",
		line("oldest"), line("middle"), line("newest"),
	)})
	d.Flush()

	got, _, _, _ := r.snapshot()
	assertCalls(t, got, []string{"lines s1 #eco [oldest,middle,newest]"})
}

// Emits interleave between sessions, but each session's own order survives and
// one Emit is never split apart.
func TestOrderingAcrossSessions(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Open("s2")

	d.Emit("s1", []Item{added("#a", "a1"), added("#a", "a2")})
	d.Emit("s2", []Item{added("#b", "b1")})
	d.Emit("s1", []Item{added("#a", "a3")})
	d.Flush()

	got, _, _, _ := r.snapshot()
	assertCalls(t, got, []string{
		"lines s1 #a [a1]",
		"lines s1 #a [a2]",
		"lines s2 #b [b1]",
		"lines s1 #a [a3]",
	})
}

// --- coalescing --------------------------------------------------------------

// Everything emitted inside one window lands in ONE batch: one turn of the UI
// thread, one BatchDone. This is the milestone's headline — the old code took a
// turn per callback.
func TestOneFlushPerBatch(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	for i := range 50 {
		d.Emit("s1", []Item{added("#eco", fmt.Sprintf("m%d", i))})
	}
	d.Flush()

	calls, batches, dones, doCalls := r.snapshot()
	if len(calls) != 50 {
		t.Fatalf("delivered %d calls, want 50", len(calls))
	}
	if doCalls != 1 {
		t.Errorf("took %d turns of the UI thread for one batch, want 1", doCalls)
	}
	if dones != 1 {
		t.Errorf("BatchDone fired %d times for one batch, want 1", dones)
	}
	if len(batches) != 1 || len(batches[0]) != 50 {
		t.Errorf("batch shape = %v, want one batch of 50", shape(batches))
	}
}

// The window is real: two bursts separated by more than it are two batches, and
// items inside one burst share a batch.
func TestCoalescingWindow(t *testing.T) {
	r := &recorder{signal: make(chan struct{}, 8)}
	const window = 300 * time.Millisecond
	d := New(Options{Sink: r, Window: window, Do: func(f func()) { r.countDo(); f() }})
	defer d.Stop()
	d.Open("s1")

	d.Emit("s1", []Item{added("#eco", "first")})
	d.Emit("s1", []Item{added("#eco", "second")})

	// Nothing may have reached the sink yet: the window has not elapsed.
	if calls, _, _, _ := r.snapshot(); len(calls) != 0 {
		t.Fatalf("delivered %v before the window elapsed", calls)
	}
	waitBatch(t, r)

	if calls, batches, dones, doCalls := r.snapshot(); dones != 1 || doCalls != 1 ||
		len(calls) != 2 || len(batches[0]) != 2 {
		t.Fatalf("first burst: calls=%v batches=%v dones=%d do=%d, want both lines in one batch",
			calls, shape(batches), dones, doCalls)
	}

	// A later burst is its own batch.
	d.Emit("s1", []Item{added("#eco", "third")})
	waitBatch(t, r)

	calls, batches, dones, doCalls := r.snapshot()
	if dones != 2 || doCalls != 2 {
		t.Errorf("second burst: dones=%d do=%d, want 2 and 2", dones, doCalls)
	}
	if len(calls) != 3 || len(batches) != 2 || len(batches[1]) != 1 {
		t.Errorf("batch shape = %v, want [2 1]", shape(batches))
	}
}

// A batch that delivered nothing does not wake the UI up to tell it so.
func TestBatchDoneOnlyWhenSomethingWasDelivered(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Emit("s1", []Item{added("#eco", "one")})
	d.Close("s1") // everything queued is now stale
	d.Flush()

	if calls, _, dones, doCalls := r.snapshot(); len(calls) != 0 || dones != 0 {
		t.Errorf("calls=%v dones=%d, want nothing delivered (do=%d)", calls, dones, doCalls)
	}
}

// Flush with an empty queue costs nothing and still returns.
func TestFlushWithNothingQueued(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Flush()
	d.Flush()
	if _, _, dones, doCalls := r.snapshot(); dones != 0 || doCalls != 0 {
		t.Errorf("empty flush took %d UI turns and fired %d BatchDone, want 0 and 0", doCalls, dones)
	}
}

// --- liveness ----------------------------------------------------------------

// A session that was never opened is not delivered to. Liveness is opt-in, so a
// half-wired session cannot paint.
func TestUnopenedSessionIsDropped(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Emit("ghost", []Item{added("#eco", "boo")})
	d.Flush()
	if calls, _, _, _ := r.snapshot(); len(calls) != 0 {
		t.Errorf("delivered %v for a session that was never opened", calls)
	}
}

// Closing a session drops what it emitted but has not delivered — the old
// stale() guard, now in one place — and leaves every other session alone.
func TestClosedSessionIsDropped(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Open("s2")

	d.Emit("s1", []Item{added("#a", "doomed"), created("#a")})
	d.Emit("s2", []Item{added("#b", "kept")})
	d.Close("s1")
	d.Emit("s1", []Item{added("#a", "also doomed")}) // a pump that has not noticed yet
	d.Flush()

	got, _, _, _ := r.snapshot()
	assertCalls(t, got, []string{"lines s2 #b [kept]"})
}

// A session torn down BY the sink, while its own batch is landing, stops being
// delivered to from the very next item. That is the case the twelve hand-rolled
// guards existed for, and it is the case a per-batch check would get wrong.
func TestSessionClosedMidBatch(t *testing.T) {
	r := &recorder{}
	var d *Dispatcher
	closer := &closingSink{recorder: r, on: "lines s1 #a [second]", close: func() { d.Close("s1") }}
	d = New(Options{Sink: closer, Window: forever, Do: func(f func()) { r.countDo(); f() }})
	defer d.Stop()
	d.Open("s1")
	d.Open("s2")

	d.Emit("s1", []Item{added("#a", "first"), added("#a", "second"), added("#a", "third")})
	d.Emit("s2", []Item{added("#b", "other")})
	d.Flush()

	got, _, _, _ := r.snapshot()
	assertCalls(t, got, []string{
		"lines s1 #a [first]",
		"lines s1 #a [second]",
		"lines s2 #b [other]",
	})
}

// closingSink tears a session down from inside delivery.
type closingSink struct {
	*recorder
	on    string
	close func()
}

func (c *closingSink) LinesAdded(s SessionID, b Buffer, lines []model.Line, notify bool) {
	c.recorder.LinesAdded(s, b, lines, notify)
	calls, _, _, _ := c.recorder.snapshot()
	if len(calls) > 0 && calls[len(calls)-1] == c.on {
		c.close()
	}
}

// Reopening an id after a Close makes it live again, which is what a profile
// reconnect looks like from here (a fresh id in practice, but the rule must not
// depend on that).
func TestReopenedSessionIsLiveAgain(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Close("s1")
	d.Open("s1")
	d.Emit("s1", []Item{added("#a", "hello")})
	d.Flush()
	got, _, _, _ := r.snapshot()
	assertCalls(t, got, []string{"lines s1 #a [hello]"})
}

// --- the Change vocabulary ---------------------------------------------------

// Every Change the funnel understands reaches its Sink method, and everything
// else is dropped here rather than at a dozen call sites.
func TestChangeMapping(t *testing.T) {
	meta := model.BufMeta(buf("#eco"))
	netMeta := model.NetMeta(net)

	cases := []struct {
		name string
		item Item
		want []string
	}{
		{"messageadded", added("#eco", "hi"), []string{"lines s1 #eco [hi]"}},
		{"historyloaded", Of(model.HistoryLoaded{ChangeMeta: meta}, "#eco", line("a"), line("b")),
			[]string{"lines s1 #eco [a,b]"}},
		{"messageresolved", Of(model.MessageResolved{ChangeMeta: meta}, "#eco"),
			[]string{"amend s1 #eco"}},
		{"messageredacted", Of(model.MessageRedacted{ChangeMeta: meta, MsgID: "m7"}, "#eco"),
			[]string{"redact s1 #eco m7"}},
		{"buffercreated", created("#eco"), []string{"buflist s1"}},
		{"bufferclosed", Of(model.BufferClosed{ChangeMeta: meta}, "#eco"), []string{"buflist s1"}},
		{"bufferrenamed", Item{
			Change: model.BufferRenamed{ChangeMeta: meta, From: buf("#old"), Name: "#eco"},
			Buffer: Buffer{ID: buf("#eco"), Name: "#eco"},
			From:   Buffer{ID: buf("#old"), Name: "#old"},
		}, []string{"rename s1 #old->#eco", "buflist s1"}},
		{"memberschanged", Of(model.MembersChanged{ChangeMeta: meta}, "#eco"),
			[]string{"members s1 #eco"}},
		{"unreadchanged", Of(model.UnreadChanged{ChangeMeta: meta, Unread: 3, Highlights: 1}, "#eco"),
			[]string{"unread s1 #eco 3/1"}},
		{"topicchanged", Of(model.TopicChanged{ChangeMeta: meta, Topic: "t"}, "#eco"),
			[]string{"topic s1 #eco \"t\""}},
		{"joinfailed", Of(model.JoinFailed{ChangeMeta: netMeta, Channel: "#x", Reason: "+k", NeedKey: true}, ""),
			[]string{"joinfail s1 #x \"+k\" key=true"}},
		{"connstate", Of(ConnState{ChangeMeta: netMeta, Status: model.ConnReady}, ""),
			[]string{"conn s1 " + fmt.Sprint(model.ConnReady) + " retry=false"}},
		{"stsoffered", Of(STSOffered{ChangeMeta: netMeta, Port: "6697", Duration: 300}, ""),
			[]string{"sts s1 6697 300"}},
		{"channellistdone", Of(ChannelListDone{ChangeMeta: netMeta, Requested: true}, ""),
			[]string{"listdone s1 req=true"}},

		// Not (yet) the screen's business: forwarded or not, they stop here.
		{"modechanged", Of(model.ModeChanged{ChangeMeta: meta, Modes: "imn"}, "#eco"), nil},
		{"typingchanged", Of(model.TypingChanged{ChangeMeta: meta, Nick: "bob"}, "#eco"), nil},
		{"readmarkermoved", Of(model.ReadMarkerMoved{ChangeMeta: meta}, "#eco"), nil},
		{"selfchanged", Of(model.SelfChanged{ChangeMeta: netMeta, Nick: "eco"}, ""), nil},
		{"authstatechanged", Of(model.AuthStateChanged{ChangeMeta: netMeta}, ""), nil},
		{"listupdated", Of(model.ListUpdated{ChangeMeta: netMeta, Done: true}, ""), nil},
		{"messagefailed", Of(model.MessageFailed{ChangeMeta: meta, Code: "X"}, "#eco"), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, r := newFunnel(t, forever)
			d.Open("s1")
			d.Emit("s1", []Item{c.item})
			d.Flush()
			got, _, dones, _ := r.snapshot()
			assertCalls(t, got, c.want)
			if want := min(len(c.want), 1); dones != want {
				t.Errorf("BatchDone fired %d times, want %d", dones, want)
			}
		})
	}
}

// --- backpressure and shutdown ----------------------------------------------

// A full queue blocks the emitter rather than dropping a Change: a dropped
// BufferCreated is a buffer that never appears, and no amount of later traffic
// repairs it.
func TestFullQueueBlocksThenDrains(t *testing.T) {
	r := &recorder{}
	d := New(Options{Sink: r, Window: forever, Queue: 2, Do: func(f func()) { r.countDo(); f() }})
	defer d.Stop()
	d.Open("s1")

	const n = 20
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range n {
			d.Emit("s1", []Item{added("#eco", fmt.Sprintf("m%d", i))})
		}
	}()

	// With a queue of two and a window that never elapses, the producer can only
	// finish because draining releases it.
	deadline := time.After(10 * time.Second)
	for finished := false; !finished; {
		d.Flush()
		select {
		case <-done:
			finished = true
		case <-deadline:
			t.Fatal("emitter never finished: a full queue must block and then drain, not deadlock")
		default:
		}
	}
	d.Flush()

	calls, _, _, _ := r.snapshot()
	if len(calls) != n {
		t.Fatalf("delivered %d of %d items; backpressure must never drop one", len(calls), n)
	}
	for i, c := range calls {
		if want := fmt.Sprintf("lines s1 #eco [m%d]", i); c != want {
			t.Fatalf("call %d = %q, want %q: backpressure reordered the stream", i, c, want)
		}
	}
}

// Stop delivers what is still queued, then ends the goroutine — deterministically,
// so teardown is an assertion and not a sleep.
func TestStopDrainsAndStops(t *testing.T) {
	r := &recorder{}
	d := New(Options{Sink: r, Window: forever, Do: func(f func()) { r.countDo(); f() }})
	d.Open("s1")
	d.Emit("s1", []Item{added("#eco", "last words")})

	d.Stop()

	got, _, dones, _ := r.snapshot()
	assertCalls(t, got, []string{"lines s1 #eco [last words]"})
	if dones != 1 {
		t.Errorf("BatchDone fired %d times on the final batch, want 1", dones)
	}

	// Idempotent, and everything is inert afterwards.
	d.Stop()
	d.Emit("s1", []Item{added("#eco", "too late")})
	d.Flush()
	if calls, _, _, _ := r.snapshot(); len(calls) != 1 {
		t.Errorf("the funnel kept working after Stop: %v", calls)
	}
}

// The goroutine-leak assertion M8 owes: a Dispatcher that has been stopped
// leaves nothing running.
func TestStopLeavesNoGoroutine(t *testing.T) {
	settle(t)
	before := runtime.NumGoroutine()

	for range 20 {
		r := &recorder{}
		d := New(Options{Sink: r, Window: forever, Do: func(f func()) { f() }})
		d.Open("s1")
		d.Emit("s1", []Item{added("#eco", "x")})
		d.Flush()
		d.Stop()
	}

	settle(t)
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: %d before, %d after 20 New/Stop cycles — the funnel leaks", before, after)
	}
}

// settle gives already-finished goroutines a moment to be reaped, so the leak
// check measures a leak and not a scheduling delay: it returns as soon as the
// count holds still across two samples.
func settle(t *testing.T) {
	t.Helper()
	base := -1
	for range 200 {
		runtime.Gosched()
		time.Sleep(time.Millisecond)
		n := runtime.NumGoroutine()
		if n == base {
			return
		}
		base = n
	}
}

// A blocked emitter is released by Stop rather than wedged by it.
func TestStopReleasesABlockedEmitter(t *testing.T) {
	r := &recorder{}
	d := New(Options{Sink: r, Window: forever, Queue: 1, Do: func(f func()) { f() }})
	d.Open("s1")

	blocked := make(chan struct{})
	go func() {
		defer close(blocked)
		for range 50 {
			d.Emit("s1", []Item{added("#eco", "x")})
		}
	}()

	d.Stop()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("an emitter blocked on a full queue was not released by Stop")
	}
}

// --- assertions --------------------------------------------------------------

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("delivered %d calls, want %d:\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("call %d = %q, want %q:\n got: %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}

func shape(batches [][]string) []int {
	out := make([]int, 0, len(batches))
	for _, b := range batches {
		out = append(out, len(b))
	}
	return out
}

// waitBatch blocks until one more batch has been delivered.
func waitBatch(t *testing.T, r *recorder) {
	t.Helper()
	select {
	case <-r.signal:
	case <-time.After(10 * time.Second):
		t.Fatal("no batch arrived: the coalescing tick never fired")
	}
}
