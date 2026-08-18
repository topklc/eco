package model

import "time"

// Clock is the one time source the layers above are allowed to read. Every
// component that needs "now" takes a Clock; nothing calls time.Now directly,
// so every timestamp in a test is chosen by the test.
type Clock func() time.Time

// SystemClock returns a Clock reading the wall clock.
func SystemClock() Clock { return time.Now }

// FixedClock returns a Clock that always reports t.
func FixedClock(t time.Time) Clock { return func() time.Time { return t } }

// StepClock returns a Clock that starts at start and advances by step on every
// call, so a test can generate strictly increasing timestamps without sleeping.
// The returned Clock is not safe for concurrent use.
func StepClock(start time.Time, step time.Duration) Clock {
	next := start
	return func() time.Time {
		t := next
		next = next.Add(step)
		return t
	}
}

// Now reads c, falling back to the wall clock when c is nil. Constructors take
// a Clock and store it; call sites use Now so a zero-valued config still works.
func (c Clock) Now() time.Time {
	if c == nil {
		return time.Now()
	}
	return c()
}
