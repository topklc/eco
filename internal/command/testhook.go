package command

import "testing"

// AddForTest registers one extra command for the duration of a test and returns
// the function that takes it away again. It panics outside a test binary.
//
// It exists for exactly one job. The packages that CONSUME this table's
// generated lists - the composer's Tab completion, the documentation window's
// Commands section - are supposed to have no list of their own, and the only
// proof of that which a frozen copy cannot also pass is a command the copy has
// never heard of. Every other check ("does your list agree with mine?") is
// satisfied forever by a snapshot taken on the day it was written, which is
// precisely how v1's two lists drifted.
//
// It is not safe to call while anything is reading the table, and nothing but a
// test ever should: the table is built once, at init, because Lookup and
// Complete are on the keystroke path.
func AddForTest(c *Command) (remove func()) {
	if !testing.Testing() {
		panic("command: AddForTest is for tests only")
	}
	if c == nil || c.Name == "" {
		panic("command: AddForTest needs a named command")
	}
	old := table
	table = append(append([]*Command(nil), old...), c)
	index()
	return func() {
		table = old
		index()
	}
}
