package state

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// TestGlobMatch pins the hostmask matcher. It is iterative on purpose: a
// pathological pattern must not be able to blow the stack.
func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"alice", "alice", true},
		{"alice", "alicia", false},
		{"ali*", "alice", true},
		{"*ice", "alice", true},
		{"a*e", "alice", true},
		{"a*e", "alicia", false},
		{"al?ce", "alice", true},
		{"al?ce", "alce", false},
		{"*.example", "host.example", true},
		{"*.example", "example", false},
		{"**a**", "banana", true},
		{"", "", true},
		{"", "x", false},
		{"*x", "", false},
	}
	for _, tc := range tests {
		if got := globMatch(tc.pattern, tc.s); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

// TestIgnoreMasks pins per-network hostmask ignores: a bare word is a nick
// mask, the nick folds with the network's casemapping, and user/host are
// ASCII-lowercased the way hostmasks are.
func TestIgnoreMasks(t *testing.T) {
	tests := []struct {
		mask   string
		sender model.Sender
		want   bool
	}{
		{"spammer", model.Sender{Nick: "spammer", User: "s", Host: "h"}, true},
		{"spammer", model.Sender{Nick: "SPAMMER", User: "s", Host: "h"}, true},
		{"spammer", model.Sender{Nick: "spammer2", User: "s", Host: "h"}, false},
		{"*!*@spam.example", model.Sender{Nick: "anyone", User: "x", Host: "spam.example"}, true},
		{"*!*@spam.example", model.Sender{Nick: "anyone", User: "x", Host: "SPAM.example"}, true},
		{"*!*@spam.example", model.Sender{Nick: "anyone", User: "x", Host: "ok.example"}, false},
		{"bot!*@*", model.Sender{Nick: "bot", User: "u", Host: "h"}, true},
		{"*@evil.*", model.Sender{Nick: "n", User: "u", Host: "evil.example"}, true},
		{"nick[]", model.Sender{Nick: "nick{}", User: "u", Host: "h"}, true}, // rfc1459 folding
	}
	for _, tc := range tests {
		h := newHarness(t)
		if !h.s.AddIgnore(h.n, tc.mask) {
			t.Fatalf("AddIgnore(%q) refused the mask", tc.mask)
		}
		if got := h.s.Ignored(h.n, tc.sender); got != tc.want {
			t.Errorf("mask %q vs %+v = %v, want %v", tc.mask, tc.sender, got, tc.want)
		}
	}
}

// TestIgnoreBookkeeping pins add/remove/list.
func TestIgnoreBookkeeping(t *testing.T) {
	h := newHarness(t)
	if h.s.AddIgnore(h.n, "  ") {
		t.Error("an empty mask was accepted")
	}
	if !h.s.AddIgnore(h.n, "b") || !h.s.AddIgnore(h.n, "a") {
		t.Fatal("AddIgnore refused a fresh mask")
	}
	if h.s.AddIgnore(h.n, "a") {
		t.Error("a duplicate mask was added twice")
	}
	if got := h.s.Ignores(h.n); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Ignores = %v, want [a b]", got)
	}
	if !h.s.RemoveIgnore(h.n, "a") {
		t.Error("RemoveIgnore did not remove an existing mask")
	}
	if h.s.RemoveIgnore(h.n, "a") {
		t.Error("RemoveIgnore removed a mask twice")
	}
	// removing by the normalised form works too
	if !h.s.RemoveIgnore(h.n, "b!*@*") {
		t.Error("RemoveIgnore did not match the normalised form")
	}
	if got := h.s.Ignores(h.n); len(got) != 0 {
		t.Errorf("Ignores = %v, want empty", got)
	}
}
