// Package command turns a line of composer text into an action.
//
// [Parse] classifies one line - an ordinary message, one of the table's
// commands with its arguments split the way that command's [ArgSpec] asks, or
// an unknown /verb - and [Run] performs it against an [Env], which is the whole
// world a command may touch: the session it was typed at, the wire, and a small
// set of screen effects. Nothing here reaches the UI toolkit, the store or the
// IRC library, so ~90% of the table is exercised headless against a fake env
// (blueprint §5.3).
//
// There is exactly one table and everything reads it: Tab completion asks for
// [Verbs], /help renders [Summary] and [HelpMarkdown], and the unknown-command
// intercept searches it for the verb the user probably meant. A second list of
// command names anywhere in the program is a bug - that is the duplication this
// package exists to delete.
//
// Threading: this package holds no mutable state and starts no goroutine. Parse
// is pure; Run is exactly as concurrent as the Env it is handed - internal/app
// calls both on the Fyne thread, where the composer lives.
package command
