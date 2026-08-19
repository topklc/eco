//go:build !race

package widgets

// raceEnabled is false in an ordinary build. The Timeline's budget test uses
// it the way internal/archive's does: instrumented, a measurement says
// nothing about the shipped binary, so the race run checks that a huge buffer
// still WORKS and `make test-budget` checks how fast it is.
const raceEnabled = false
