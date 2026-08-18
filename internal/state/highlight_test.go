package state

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
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
	for _, tc := range tests {
		if got := containsWord(tc.text, tc.word); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tc.text, tc.word, got, tc.want)
		}
	}
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
