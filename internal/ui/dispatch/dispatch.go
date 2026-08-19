package dispatch

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

// Window is the coalescing tick. 80ms is not a guess: it is the value v1's
// message pane settled on after a chathistory playback of a hundred lines cost
// a hundred relayouts, and it is short enough that no one has ever reported the
// delay. It is kept as it is, and moved from the UI into the funnel.
const Window = 80 * time.Millisecond

// Queue is the default bound on the queue, counted in Emits. Deep enough that a
// reconnect burst never touches it, shallow enough that a wedged UI thread
// stops the wire instead of eating memory.
const Queue = 256

// Options configures a Dispatcher. Only Sink is required.
type Options struct {
	// Sink receives every batch, on the Do thread.
	Sink Sink
	// Window is the coalescing tick; zero means Window.
	Window time.Duration
	// Queue bounds the queue in Emits; zero means Queue.
	Queue int
	// Do runs a function on the UI thread. Zero means fyne.Do, which is the
	// production wiring; tests pass a direct call so the queue, the coalescing
	// and the liveness rule can be exercised with no Fyne app in the process.
	// This is the ONLY seam through which this package touches the toolkit.
	Do func(func())
}

// Dispatcher is the funnel. See the package doc for the contract.
type Dispatcher struct {
	sink   Sink
	do     func(func())
	window time.Duration

	in    chan batch
	flush chan chan struct{}
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once

	mu   sync.Mutex
	live map[SessionID]bool
}

// batch is one Emit: the items of a single store step, tagged with the session
// they came from. Keeping an Emit whole through the queue is what makes "the
// items of one step arrive contiguously" true rather than probable.
type batch struct {
	id    SessionID
	items []Item
}

// New starts a Dispatcher and its goroutine. Stop ends it.
func New(o Options) *Dispatcher {
	if o.Sink == nil {
		panic("dispatch.New: nil Sink")
	}
	d := &Dispatcher{
		sink:   o.Sink,
		do:     o.Do,
		window: o.Window,
		flush:  make(chan chan struct{}),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		live:   map[SessionID]bool{},
	}
	if d.do == nil {
		d.do = fyne.Do
	}
	if d.window <= 0 {
		d.window = Window
	}
	size := o.Queue
	if size <= 0 {
		size = Queue
	}
	d.in = make(chan batch, size)
	go d.run()
	return d
}

// Open registers a live session. Nothing a session emits reaches the Sink
// before this, and nothing reaches it after Close.
func (d *Dispatcher) Open(id SessionID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.live[id] = true
}

// Close retires a session the app has torn down. Everything it emitted and the
// funnel has not delivered yet is dropped, along with anything a goroutine that
// has not noticed the teardown yet still emits. This is the single place a
// stale session is filtered out; there is deliberately no second guard in the
// UI.
func (d *Dispatcher) Close(id SessionID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.live, id)
}

func (d *Dispatcher) isLive(id SessionID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.live[id]
}

// Emit queues one step's worth of items. It is called from the session's own
// goroutine, never from the Sink's thread, and it takes ownership of items -
// the caller must not touch the slice afterwards.
//
// It blocks while the queue is full, which is the backpressure the package doc
// describes, and returns without queueing anything once the Dispatcher has
// stopped.
func (d *Dispatcher) Emit(id SessionID, items []Item) {
	if len(items) == 0 {
		return
	}
	select {
	case d.in <- batch{id: id, items: items}:
	case <-d.done:
	}
}

// Flush delivers everything already emitted and returns once the Sink has seen
// it. It is the synchronous barrier the offline suites assert behind, in the
// same family as testsrv's PING/PONG: it replaces "sleep long enough for the
// tick" with "the screen is up to date now".
//
// It must not be called from the Sink's thread - it waits for work that runs
// there.
func (d *Dispatcher) Flush() {
	ack := make(chan struct{})
	select {
	case d.flush <- ack:
	case <-d.done:
		return
	}
	select {
	case <-ack:
	case <-d.done:
	}
}

// Stop drains the queue, delivers what is left as one final batch and returns
// once the goroutine has exited - so "the funnel leaks no goroutine" is an
// assertion a test can make rather than a hope. Emit and Flush are safe (and
// inert) afterwards, and Stop itself may be called more than once.
//
// It must not be called from the Sink's thread, for Flush's reason. The running
// app therefore does not call it: the funnel lives exactly as long as the
// process, and the teardown path that matters at runtime is Close, per session.
func (d *Dispatcher) Stop() {
	d.once.Do(func() { close(d.stop) })
	<-d.done
}

// run is the one goroutine. State - what is pending, and whether a tick is
// running - lives on its stack, so nothing but the liveness map is shared.
//
// The tick is armed by the first item of a burst and abandoned (tick = nil,
// never selected on again) by anything that flushes early, rather than being
// stopped and drained: a Timer that has already fired cannot be drained
// portably, and a fire nobody listens for costs nothing.
func (d *Dispatcher) run() {
	defer close(d.done)

	var pending []queued
	var tick <-chan time.Time

	for {
		select {
		case b := <-d.in:
			pending = append(pending, spread(b)...)
			if tick == nil {
				tick = time.After(d.window)
			}

		case <-tick:
			tick = nil
			pending = d.emit(pending, nil)

		case ack := <-d.flush:
			// Take whatever is already queued first: Emit and Flush arrive on
			// different channels, so without this drain a select could answer
			// the barrier before the items it is a barrier for.
			pending = drain(d.in, pending)
			tick = nil
			pending = d.emit(pending, ack)

		case <-d.stop:
			pending = drain(d.in, pending)
			d.emit(pending, nil)
			return
		}
	}
}

// spread flattens one Emit into per-item queue entries, keeping its order.
func spread(b batch) []queued {
	out := make([]queued, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, queued{id: b.id, item: it})
	}
	return out
}

// queued is one item with the session that emitted it.
type queued struct {
	id   SessionID
	item Item
}

// drain moves everything already in the queue onto pending without blocking.
func drain(in <-chan batch, pending []queued) []queued {
	for {
		select {
		case b := <-in:
			pending = append(pending, spread(b)...)
		default:
			return pending
		}
	}
}

// emit hands one batch to the Sink: exactly one Do, in arrival order, dropping
// anything from a session that is no longer open. It returns the (empty)
// pending slice, because ownership of the old one has moved to the closure.
func (d *Dispatcher) emit(pending []queued, ack chan struct{}) []queued {
	if len(pending) == 0 {
		if ack != nil {
			close(ack)
		}
		return nil
	}
	items := pending
	d.do(func() {
		delivered := false
		for _, q := range items {
			// The liveness rule, applied once, at delivery: a session torn
			// down while this batch was landing stops being delivered to from
			// the very next item.
			if !d.isLive(q.id) {
				continue
			}
			delivered = deliver(d.sink, q.id, q.item) || delivered
		}
		if delivered {
			d.sink.BatchDone()
		}
		if ack != nil {
			close(ack)
		}
	})
	return nil
}
