package model

import (
	"testing"
	"time"
)

func TestFixedClock(t *testing.T) {
	at := time.Date(2026, 8, 16, 9, 30, 0, 0, time.UTC)
	c := FixedClock(at)
	for i := 0; i < 3; i++ {
		if got := c.Now(); !got.Equal(at) {
			t.Errorf("call %d = %v, want %v", i, got, at)
		}
	}
}

func TestStepClock(t *testing.T) {
	start := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	c := StepClock(start, time.Second)

	var prev time.Time
	for i := 0; i < 5; i++ {
		got := c.Now()
		want := start.Add(time.Duration(i) * time.Second)
		if !got.Equal(want) {
			t.Errorf("call %d = %v, want %v", i, got, want)
		}
		if i > 0 && !got.After(prev) {
			t.Errorf("call %d did not advance: %v then %v", i, prev, got)
		}
		prev = got
	}
}

func TestNilClockFallsBackToTheWallClock(t *testing.T) {
	var c Clock
	before := time.Now()
	got := c.Now()
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("nil Clock.Now() = %v, not near the wall clock", got)
	}
}

func TestSystemClockAdvances(t *testing.T) {
	c := SystemClock()
	a := c.Now()
	b := c.Now()
	if b.Before(a) {
		t.Errorf("the system clock went backwards: %v then %v", a, b)
	}
}
