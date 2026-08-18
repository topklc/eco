package proto

import "strings"

// CaseMapping is the ISUPPORT CASEMAPPING value: the rule a server uses to
// decide that two nicknames or channel names are the same name.
//
// The zero value is CaseRFC1459, which is what a client must assume when the
// server advertises no CASEMAPPING at all.
type CaseMapping uint8

const (
	// CaseRFC1459 folds A-Z plus the four "national" pairs: []\~ are the
	// uppercase forms of {}|^ because RFC 1459 treated them as the Scandinavian
	// letters they were on the terminals of the day. This is the default.
	CaseRFC1459 CaseMapping = iota

	// CaseRFC1459Strict is rfc1459 without the ~/^ pair: only []\ fold to {}|.
	CaseRFC1459Strict

	// CaseASCII folds A-Z only. Every byte >= 0x80 is left alone, so UTF-8
	// nicks survive folding unchanged.
	CaseASCII
)

// String returns the ISUPPORT spelling of the casemapping.
func (c CaseMapping) String() string {
	switch c {
	case CaseRFC1459:
		return "rfc1459"
	case CaseRFC1459Strict:
		return "rfc1459-strict"
	case CaseASCII:
		return "ascii"
	}
	return "rfc1459"
}

// ParseCaseMapping maps an ISUPPORT CASEMAPPING value to a CaseMapping. It
// reports false for a value it does not know, in which case the returned
// mapping is the safe default (CaseRFC1459) — folding too much merges two
// distinct names, folding too little splits one name into two buffers, and of
// those the spec-mandated default is the defensible choice.
func ParseCaseMapping(s string) (CaseMapping, bool) {
	switch strings.ToLower(s) {
	case "ascii":
		return CaseASCII, true
	case "rfc1459":
		return CaseRFC1459, true
	case "rfc1459-strict":
		return CaseRFC1459Strict, true
	}
	return CaseRFC1459, false
}

// foldByte applies the mapping to one byte. Every rule is ASCII-only, so bytes
// >= 0x80 pass through untouched and folding a valid UTF-8 string byte by byte
// yields a valid UTF-8 string.
func (c CaseMapping) foldByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	switch c {
	case CaseRFC1459:
		switch b {
		case '[':
			return '{'
		case ']':
			return '}'
		case '\\':
			return '|'
		case '~':
			return '^'
		}
	case CaseRFC1459Strict:
		switch b {
		case '[':
			return '{'
		case ']':
			return '}'
		case '\\':
			return '|'
		}
	}
	return b
}

// Fold returns the casefolded form of s: the canonical key for a nick or
// channel name under this casemapping. Every map that is keyed by a name — the
// store's buffers, a channel's members, the msgid index's buffer dimension —
// keys on Fold's output and never on the raw name.
//
// Fold returns s itself when nothing changes, so the common all-lowercase case
// does not allocate.
func (c CaseMapping) Fold(s string) string {
	i := 0
	for ; i < len(s); i++ {
		if c.foldByte(s[i]) != s[i] {
			break
		}
	}
	if i == len(s) {
		return s
	}
	out := []byte(s)
	for ; i < len(out); i++ {
		out[i] = c.foldByte(out[i])
	}
	return string(out)
}

// Equal reports whether a and b are the same name under this casemapping.
func (c CaseMapping) Equal(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if c.foldByte(a[i]) != c.foldByte(b[i]) {
			return false
		}
	}
	return true
}
