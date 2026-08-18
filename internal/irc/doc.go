// Package irc is eco's wire client: one connection to one IRC network,
// decoded into the typed model.Event stream the rest of the app consumes.
//
// # Threading contract
//
// irc emits on its own goroutine. A Client owns one supervisor goroutine
// (the connect/backoff loop); while a connection is up, events are produced
// by that connection's reader goroutine. The two never overlap — the
// supervisor emits only before a connection starts and after it has been
// fully joined — so every consumer sees one totally ordered stream on
// Events(), in wire order, with no locking of its own.
//
// The package imports no fyne and holds no display state. The only strings
// it produces are protocol strings: nothing here is written to be read by a
// human.
//
// # Shape
//
//	cfg → New → Start → Events() <-chan model.Event → Close
//
// Configuration in, events out. Everything the caller can ask the network to
// do goes through the Send* / Join / Part / History methods, which return a
// Handle carrying the labels the echoes will come back with.
//
// # What this package fixes, deliberately
//
//   - Every inbound line is decoded from one raw hook, so BATCH framing,
//     labeled responses and message tags are read by eco rather than by the
//     library underneath it. There is exactly one decode path.
//   - Batches are tracked for real (proto.BatchTracker): playback is
//     "something in my enclosing chain is a chathistory batch", never "this
//     line has a batch tag". Multiline is assembled at BATCH close, not
//     guessed at from a wall-clock recency window.
//   - Every outgoing message carries a label, and the echo is correlated by
//     that label. Two identical messages sent in a row are distinguishable,
//     and our own messages arrive with their real msgid. A reply the server
//     grouped into a labeled-response batch inherits the batch's label, and a
//     command the server answers with a bare ACK arrives as
//     model.Numeric{Code: "ACK"} carrying it — so a pending message resolves
//     whichever of the three shapes the reply takes.
//   - Reconnect is exponential backoff with jitter, driven by an injectable
//     timer, and it reports itself through ConnState events.
//
// # Test seams
//
// Config.Dial replaces the dialer (internal/testsrv hands one end of a
// net.Pipe through it), Config.Clock pins "now", Config.Timer replaces the
// backoff sleep, and Config.Rand replaces the jitter source. Production
// leaves all four nil.
package irc
