// Package dispatch is the one funnel between the model and the screen.
//
// Threading: everything below this package runs off the Fyne thread; everything
// the Sink does runs on it. One goroutine drains Changes, coalesces them on the
// proven 80ms tick, and hands each batch to the Sink inside exactly ONE
// fyne.Do — the one call site for it in the whole program (blueprint §5.3).
//
// # What replaced what
//
// Before M8 internal/app's Session fanned Changes out through twelve callback
// fields, each of which opened its own fyne.Do and re-checked, by hand, whether
// the session it belonged to had been torn down. Twelve copies of one rule is
// twelve chances to forget it, and a hundred-line history burst was a hundred
// hops onto the UI thread. This package is that surface, once:
//
//   - one queue, one goroutine, one 80ms window, one fyne.Do per batch;
//   - one liveness rule, applied in one place (see Open/Close);
//   - one Sink interface, named for what the UI does rather than for the
//     v1 callback it grew out of.
//
// # Ordering — guaranteed, and tested
//
// Delivery order is arrival order. Concretely:
//
//   - Items inside one Emit are delivered in slice order, contiguously.
//   - Emits from one session are delivered in the order that session made them.
//   - Emits from different sessions interleave in whatever order they reached
//     the queue, and each session's own order still holds inside the result.
//   - A batch never reorders, merges or de-duplicates: coalescing decides *when*
//     the Sink is called, never *what* it is told or in which order.
//
// That last point is load bearing. internal/transcript's frozen records sort
// each step's facts canonically, so they cannot pin the order of two facts
// belonging to one step — the M7 verification found a reversed history chunk
// slipping past them for exactly that reason. Order is therefore pinned here,
// by test (TestOrderingWithinBatch, TestOrderingAcrossSessions), rather than
// assumed.
//
// # Liveness
//
// A session is identified by a SessionID that is stable for its whole life and
// is never the network's display name (which the user can edit and the server
// can re-case). Open registers one; Close retires it. Items from a session that
// is not open are dropped, once, in the delivery loop — after coalescing, at the
// moment of delivery, which is where the old stale() guards checked too. A
// session that dies while its own batch is being delivered therefore stops
// being delivered to immediately, mid-batch.
//
// # Backpressure and shutdown
//
// The queue is bounded (Options.Queue, in Emits). A full queue blocks Emit,
// which backpressures the session pump and, behind it, the wire. Changes are
// never dropped for being late: dropping one would leave the screen describing
// a state the store has never been in. The only drop is the liveness rule above.
//
// Stop drains the queue, delivers what is left as a final batch, and returns
// once the goroutine is gone, so a test can assert there is no goroutine left
// behind. Flush is the same thing without the stop: it delivers everything
// already emitted and returns once the Sink has seen it, which is what lets the
// offline suites assert on a synchronous barrier rather than on a sleep.
//
// Neither may be called from the Sink's own thread: both wait for work that has
// to run there.
package dispatch
