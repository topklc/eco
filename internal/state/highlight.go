package state

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toprakkilic/eco/internal/model"
)

// matchesHighlight reports whether a message body mentions us: our own current
// nick, or one of the user's highlight words. Matching is whole-word and
// Unicode-case-folded, so "eco" does not fire on "economy" and "Ünal" fires on
// "ünal".
func (n *network) matchesHighlight(text string) bool {
	if text == "" {
		return false
	}
	if n.nick != "" && n.containsWord(text, n.nick) {
		return true
	}
	for _, w := range n.highlightWords {
		if w != "" && n.containsWord(text, w) {
			return true
		}
	}
	return false
}

// foldMatch is the folding a highlight is compared under: Unicode lowercase
// first, then the server's CASEMAPPING on top of it (§4.4's "Unicode-folded per
// CASEMAPPING").
//
// Both halves are load-bearing and neither is enough alone. CASEMAPPING is
// ASCII-only by construction - every rule in RFC 1459 is a byte rule - so on
// its own it would never match "Ünal" against "ünal", which is the case a
// non-English user meets on their own nick. strings.ToLower on its own would
// never match "foo[bar]" against "foo{bar}", which on an rfc1459 server is not
// a near miss: it is the SAME nick, and the store's own buffer keys already say
// so. Composing them is the only answer that is right in both directions.
func (n *network) foldMatch(s string) string { return n.props.Fold(strings.ToLower(s)) }

// containsWord reports whether word occurs in text as a whole word, folded for
// this network. See foldMatch for what "folded" means here.
func (n *network) containsWord(text, word string) bool {
	return containsWordFolded(text, word, n.foldMatch)
}

// containsWordFolded reports whether word occurs in text as a whole word.
//
// This is v1's containsWord (kept: the veteran panel and the M5 suite both pin
// the behaviour) with two changes. It folds with a Unicode-aware fold instead
// of comparing ASCII bytes; and the boundary test runs on runes, so a mention
// next to a non-ASCII letter is not a match while one next to punctuation is.
//
// The fold must be length-preserving in bytes for the offsets below to mean
// anything, which both halves of foldMatch are: strings.ToLower is not, in
// general, but the pathological cases (İ, ẞ) change length only in the source
// text and the word alike, and the boundary test is re-run on the FOLDED text,
// so a shifted offset can only ever cost a match, never invent one.
func containsWordFolded(text, word string, fold func(string) string) bool {
	if word == "" {
		return false
	}
	t, w := fold(text), fold(word)
	for i := 0; i <= len(t)-len(w); {
		j := strings.Index(t[i:], w)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(w)
		if boundaryBefore(t, j) && boundaryAfter(t, end) {
			return true
		}
		_, size := utf8.DecodeRuneInString(t[j:])
		if size <= 0 {
			return false
		}
		i = j + size
	}
	return false
}

// boundaryBefore reports whether the rune ending at i is not part of a nick.
func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !isNickRune(r)
}

// boundaryAfter reports whether the rune starting at i is not part of a nick.
func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !isNickRune(r)
}

// isNickRune reports whether a rune can appear inside a nick, and therefore
// whether it breaks a whole-word match. It covers RFC 1459's special
// characters plus any Unicode letter or digit, because servers with UTF8ONLY
// hand out nicks the ASCII rule would split in half.
func isNickRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	return strings.ContainsRune("_-[]{}\\|^`", r)
}

// SetHighlightWords replaces a network's extra highlight words. Our own nick is
// always a highlight and is not part of this list.
func (s *Store) SetHighlightWords(id model.NetworkID, words []string) {
	n := s.network(id)
	n.highlightWords = append([]string(nil), words...)
}

// HighlightWords returns a network's extra highlight words.
func (s *Store) HighlightWords(id model.NetworkID) []string {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	return append([]string(nil), n.highlightWords...)
}
