// Package parity is the M6 dual-core parity gate: the harness that drives the
// v1 Session and the v2 core (internal/irc.Client + internal/state.Store) from
// one scripted fake-server transcript and fails when they disagree.
//
// # Why the package is empty
//
// Every line of the harness lives in _test.go files. The harness imports
// internal/app, which M7 deletes; keeping it test-only means the production
// build graph never contains that dependency, `make build` never compiles a
// line of it, and retiring the gate at cutover is one `rm -r`. This file exists
// so the directory is a package that `go build ./...` and `go vet ./...` accept.
//
// # What it compares
//
// Both cores are driven, in turn, through the same []step transcript against
// their own internal/testsrv server. Each core records what it told the UI —
// the v1 Session its twelve UICallbacks, the v2 core its []model.Change — and
// both streams are normalised onto one canonical Fact (folded buffer, kind,
// nick, text, flags). Two things are then diffed:
//
//   - the stream, grouped by transcript step, sorted inside each group (both
//     cores see the same line at the same point, so the grouping is exact and
//     the sort only forgives the order two facts about one line come out in);
//   - the final state of every buffer: topic, modes, members and the whole
//     line log, in order.
//
// # Deliberate differences
//
// The blueprint changes behaviour on purpose — labeled-response echo
// resolution, batch-derived playback, folded identity — so a bare diff would
// be red forever. Those differences are named entries in divergences (see
// divergence_test.go), each with a reason, and the differ explains an
// unmatched fact only when an entry matches it. Anything else is a failure,
// and the list is itself tested: an entry that never fires is dead and fails
// the suite, so it cannot become a place to hide a regression.
//
// THE CARDINAL RULE OF M6: the new core ships dark. Nothing here changes which
// core internal/app runs; the gate exists so M7 can delete the old one.
package parity
