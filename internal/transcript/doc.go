// Package transcript is eco's characterization suite: 21 scripted fake-server
// transcripts replayed against the protocol core (internal/irc.Client +
// internal/state.Store), with everything the core reports normalised onto one
// canonical Fact and compared, line for line, against a recorded run checked in
// under testdata.
//
// # Where its authority comes from
//
// This package was the M6 dual-core parity gate. It drove the v1 core
// (app.LegacySession) and the new core from the same transcript and failed when
// they disagreed about anything the divergence list did not explain - and the
// evidence that the comparison was strict enough to mean something was a
// mutation sweep: eighteen bugs injected one at a time into internal/state,
// internal/proto and internal/irc, all eighteen caught. Two of them were caught
// only after the sweep exposed real holes in the gate, which is how the members
// fact came to carry the roster instead of a ping (see rosterText).
//
// At M7 the app cut over to the new core and the v1 core was deleted, because a
// gate whose two sides are the same code proves nothing. What could not be
// deleted with it are the scenarios and that strictness - they are the record of
// what correct behaviour means. So the reference side stopped being a second
// implementation and became testdata: the files under testdata are the facts the
// dual-core gate compared green, recorded from the same core at the same commit,
// with the comparison unchanged in every other respect.
//
// A record is therefore not "whatever the code did when someone ran -update". It
// is v1's behaviour, plus the fifteen changes the blueprint made on purpose,
// frozen at the last moment both could be checked against each other. The
// divergence table in the blueprint's M6 progress entry names those changes; the
// records are what they turned into.
//
// # Using it
//
//	go test ./internal/transcript            # compare against the records
//	go test ./internal/transcript -update    # re-record, then READ THE DIFF
//
// -update is a decision, not a fix. A diff here is a behaviour change: either it
// is the regression this suite exists to catch, or it is deliberate and belongs
// in the blueprint next to the record it rewrote.
//
// # What is recorded
//
// Two things, because one is not enough. The stream - every Change the core
// emitted, grouped by transcript step and sorted inside the group, since the
// order two facts about one line come out in is coalesced away by the dispatcher
// (§5.3) and is not behaviour. And the final state of every buffer: topic,
// modes, key, members, and the whole message log in order.
//
// The state half is not decoration. A multiline message is assembled at BATCH
// close and a redaction leaves a tombstone; both are things the stream mentions
// and only the log can show.
//
// # The anchors
//
// characterization_test.go holds a handful of tests that assert behaviour
// directly rather than through a record - the labelled echo carrying the
// server's msgid, the STS policy reaching the client, the user's own /me being
// delivered, a members fact naming the room. They exist because -update can
// rewrite a record but cannot rewrite them: they are the claims the dual-core
// gate's divergence list and self-tests used to make, kept as executable
// statements now that the reference core is gone.
//
// # Why the package is empty
//
// Every line of the suite lives in _test.go files, so the production build graph
// never contains it and `make build` never compiles a line of it. This file
// exists so the directory is a package that `go build ./...` and `go vet ./...`
// accept.
package transcript
