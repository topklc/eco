package app

import "testing"

func TestApplyModeDelta(t *testing.T) {
	b := &Buffer{}
	b.applyModeDelta([]string{"+ntk", "sekrit"})
	if b.Modes != "ntk" || b.Key != "sekrit" {
		t.Fatalf("got modes %q key %q", b.Modes, b.Key)
	}
	b.applyModeDelta([]string{"+o", "somenick"}) // member mode: not tracked
	if b.Modes != "ntk" {
		t.Fatalf("member mode leaked into %q", b.Modes)
	}
	b.applyModeDelta([]string{"-k+im", "sekrit"})
	if b.Modes != "ntim" || b.Key != "" {
		t.Fatalf("got modes %q key %q", b.Modes, b.Key)
	}
	b.applyModeDelta([]string{"+b-i", "bad!*@*"})
	if b.Modes != "ntm" {
		t.Fatalf("got modes %q", b.Modes)
	}
}
