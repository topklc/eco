package command

import "strings"

// Suggest returns the verb the user probably meant, or "" when nothing in the
// table is close enough to be worth guessing at.
//
// Two things count as close: a verb the typed text is a prefix of (an
// abbreviation is not a typo - "/wh" means /who), and a verb within a couple of
// edits of it ("/jion" is /join). Anything further away gets no suggestion,
// because a wrong guess is worse than none: it reads as a correction.
func Suggest(verb string) string {
	verb = strings.ToLower(verb)
	if verb == "" {
		return ""
	}
	best, bestDist := "", 0
	for _, v := range verbs { // sorted, so equal candidates resolve the same way every time
		name := strings.TrimPrefix(v, "/")
		d := distance(verb, name)
		if strings.HasPrefix(name, verb) {
			d = 0
		}
		if best == "" || d < bestDist || (d == bestDist && len(name) < len(best)) {
			best, bestDist = name, d
		}
	}
	// Two edits is the limit, and never more than the typed word can afford:
	// with a two-letter verb, two edits is a different command entirely.
	if bestDist > 2 || bestDist >= len(verb) {
		return ""
	}
	return best
}

// distance is the edit distance between two short words, over runes so a pasted
// unicode verb cannot index into the middle of one.
//
// Swapping two letters counts as ONE edit (optimal string alignment, not plain
// Levenshtein), because that is the typo people actually make: "/jion" is
// "/join" with two fingers in the wrong order, and under plain Levenshtein it
// costs two - which puts it the same distance from /join as from /ison, and the
// tie-break then answers with nonsense.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	// three rows: the one before last is what a transposition looks back at
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(rb)]
}
