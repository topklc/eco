package state

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// TestContainsWord pins whole-word highlight matching. v1's version compared
// ASCII bytes; this one folds Unicode and tests boundaries on runes, so a
// mention next to a non-ASCII letter is not a match and a nick with a bracket
// in it still matches.
func TestContainsWord(t *testing.T) {
	tests := []struct {
		text, word string
		want       bool
	}{
		{"eco: ship it", "eco", true},
		{"hey eco", "eco", true},
		{"eco", "eco", true},
		{"(eco)", "eco", true},
		{"eco, are you there?", "eco", true},
		{"ECO: shout", "eco", true},
		{"tell Eco please", "eco", true},
		{"the economy is fine", "eco", false},
		{"deco", "eco", false},
		{"reco-rd", "eco", false},
		{"say eco_ hi", "eco", false},
		{"nick[afk] left", "nick[afk]", true},
		{"ping nick[afk]!", "nick[afk]", true},
		{"ünal: merhaba", "Ünal", true},
		{"künal", "ünal", false},
		{"ünalx", "ünal", false},
		{"日本語 テスト", "テスト", true},
		{"", "eco", false},
		{"eco", "", false},
	}
	n := &network{props: proto.DefaultProps()}
	for _, tc := range tests {
		if got := n.containsWord(tc.text, tc.word); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tc.text, tc.word, got, tc.want)
		}
	}
}

// TestHighlightFoldsPerCasemapping pins §4.4's "Unicode-folded per CASEMAPPING"
// as the composition it is: Unicode lowercase, and then the server's rule on
// top of it. Either half alone gets one of these columns wrong — and the
// rfc1459 column is not a curiosity, it is the same equivalence the store's own
// buffer keys already use, so a nick that IS you would otherwise fail to
// highlight you.
func TestHighlightFoldsPerCasemapping(t *testing.T) {
	rfc := &network{props: proto.DefaultProps()} // no CASEMAPPING advertised: rfc1459
	ascii := &network{props: propsWithCasemapping(t, "ascii")}

	for _, tc := range []struct {
		name       string
		n          *network
		text, word string
		want       bool
	}{
		{"unicode folds under rfc1459", rfc, "ünal: hi", "Ünal", true},
		{"unicode folds under ascii", ascii, "ünal: hi", "Ünal", true},
		{"rfc1459 folds the bracket pair", rfc, "poke nick{afk}", "nick[afk]", true},
		{"rfc1459 folds the backslash pair", rfc, "hi a|b", "a\\b", true},
		{"rfc1459 folds tilde to caret", rfc, "hi a^b", "a~b", true},
		{"ascii does not fold brackets", ascii, "poke nick{afk}", "nick[afk]", false},
		{"neither invents a match", rfc, "poke someone", "nick[afk]", false},
	} {
		if got := tc.n.containsWord(tc.text, tc.word); got != tc.want {
			t.Errorf("%s: containsWord(%q, %q) = %v, want %v", tc.name, tc.text, tc.word, got, tc.want)
		}
	}
}

// propsWithCasemapping builds a ServerProps advertising one CASEMAPPING.
func propsWithCasemapping(t *testing.T, mapping string) *proto.ServerProps {
	t.Helper()
	p := proto.DefaultProps()
	if errs := p.Apply([]string{"CASEMAPPING=" + mapping}); len(errs) > 0 {
		t.Fatalf("Apply(CASEMAPPING=%s) = %v", mapping, errs)
	}
	if got := p.CaseMapping.String(); got != mapping {
		t.Fatalf("casemapping = %q, want %q", got, mapping)
	}
	return p
}

// TestHighlightWordList pins the user's own highlight words, which are matched
// with the same whole-word rule as the nick.
func TestHighlightWordList(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.HighlightWords = []string{"release", "deploy"} })
	h.joinChannel("#eco", "alice")

	highlighted := func(text string) bool {
		added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
			EventMeta: h.meta(), Target: "#eco", Sender: user("alice"), Text: text,
		}))
		return added.Message.Highlight
	}
	if !highlighted("the release is out") {
		t.Error("a highlight word did not match")
	}
	if highlighted("released yesterday") {
		t.Error("a highlight word matched inside another word")
	}
	if highlighted("nothing to see") {
		t.Error("an unrelated message highlighted")
	}

	h.s.SetHighlightWords(h.n, nil)
	if highlighted("the release is out") {
		t.Error("clearing the word list did not take effect")
	}
	if got := h.s.HighlightWords(h.n); len(got) != 0 {
		t.Errorf("HighlightWords = %v, want empty", got)
	}
}
