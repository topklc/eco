//go:build !race

package archive

// raceEnabled is false in an ordinary build. The budget tests use it to pick a
// corpus size: under the race detector every statement costs several times
// more, and a suite that takes a minute is a suite people stop running.
const raceEnabled = false
