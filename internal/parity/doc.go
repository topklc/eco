// Package parity is the M6 dual-core parity gate: the harness that drives the
// v1 core (app.LegacySession) and the v2 core (internal/irc.Client +
// internal/state.Store) from one scripted fake-server transcript and fails when
// they disagree.
//
// # Its status after the M7 cutover
//
// The app now runs on the v2 core: app.Session is the adapter over irc+state.
// The v1 core did not go with it — it was renamed to app.LegacySession and left
// in place, unreachable from the app, for exactly this harness. That is what
// keeps the gate meaningful: a gate whose two sides are the same code proves
// nothing, so the reference side is the pre-cutover behaviour, frozen. The gate
// and app.LegacySession are deleted together in the milestone that retires it.
//
// # Why the package is empty
//
// Every line of the harness lives in _test.go files. The harness imports
// internal/app; keeping it test-only means the production build graph never
// contains that dependency, `make build` never compiles a line of it, and
// retiring the gate is one `rm -r` plus one file. This file exists so the
// directory is a package that `go build ./...` and `go vet ./...` accept.
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
// Every fact carries its payload, and that is load-bearing rather than
// decorative: a fact reduced to "something about the roster happened" is
// fungible, two fungible facts cancel in the diff, and a core that quietly
// stopped reporting a change would read as agreement. So a members fact says
// who is in the room now (see collapseRosters).
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
// THE CARDINAL RULE OF M6 WAS: the new core ships dark, and nothing here
// changes which core internal/app runs. M7 flipped that — the app runs on the
// new core — and the rule that replaces it is the mirror image: nothing here
// may change app.LegacySession, because it is the measuring stick now, not a
// thing that ships.
package parity
