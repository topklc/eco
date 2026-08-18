package proto

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFoldMatrix folds the same inputs under all three casemappings. The rows
// that matter are the ones containing []\~ : v1 used strings.ToLower for every
// server, which is only correct in the ascii column.
func TestFoldMatrix(t *testing.T) {
	cases := []struct {
		name                          string
		in                            string
		ascii, rfc1459, rfc1459Strict string
	}{
		{"empty", "", "", "", ""},
		{"already folded", "nick", "nick", "nick", "nick"},
		{"plain ascii", "NickName", "nickname", "nickname", "nickname"},
		{"channel", "#Channel", "#channel", "#channel", "#channel"},

		// The four national pairs. [ ] \ fold in both rfc1459 flavours;
		// ~ folds only in the non-strict one.
		{"bracket open", "nick[", "nick[", "nick{", "nick{"},
		{"bracket close", "nick]", "nick]", "nick}", "nick}"},
		{"backslash", "nick\\", "nick\\", "nick|", "nick|"},
		{"tilde", "nick~", "nick~", "nick^", "nick~"},
		{"all four", "A[]\\~", "a[]\\~", "a{}|^", "a{}|~"},

		// Their already-lowercase counterparts are fixed points everywhere.
		{"lowercase counterparts", "a{}|^", "a{}|^", "a{}|^", "a{}|^"},

		// Characters adjacent to the folded range must not move.
		{"caret and underscore", "^_`", "^_`", "^_`", "^_`"},
		{"at sign and digits", "@0129", "@0129", "@0129", "@0129"},

		// Real-world shapes.
		{"bracketed nick", "[Away]Bob", "[away]bob", "{away}bob", "{away}bob"},
		{"amp channel", "&Local", "&local", "&local", "&local"},

		// Non-ASCII passes through untouched under every mapping: the rules are
		// byte rules over ASCII, so UTF-8 sequences are preserved.
		// U+0130 is not ASCII, so it is left exactly as-is. This is deliberate:
		// IRC casemapping is a byte rule, and Unicode-lowercasing a nick would
		// make "İ" collide with "i" on a server that considers them distinct.
		{"utf8 turkish", "İstanbul", "İstanbul", "İstanbul", "İstanbul"},
		{"utf8 emoji", "nick🙂A", "nick🙂a", "nick🙂a", "nick🙂a"},
		{"utf8 cyrillic", "Пётр", "Пётр", "Пётр", "Пётр"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, m := range []struct {
				cm   CaseMapping
				want string
			}{
				{CaseASCII, tc.ascii},
				{CaseRFC1459, tc.rfc1459},
				{CaseRFC1459Strict, tc.rfc1459Strict},
			} {
				if got := m.cm.Fold(tc.in); got != m.want {
					t.Errorf("%s.Fold(%q) = %q, want %q", m.cm, tc.in, got, m.want)
				}
			}
		})
	}
}

// TestFoldIsIdempotentAndUTF8Safe: folding twice must equal folding once, and
// folding must never corrupt a valid UTF-8 string.
func TestFoldIsIdempotentAndUTF8Safe(t *testing.T) {
	inputs := []string{
		"", "ABC", "a[b]c\\d~e", "İ", "🙂🙃", "Пётр[]", strings.Repeat("A[", 64),
	}
	for _, cm := range []CaseMapping{CaseASCII, CaseRFC1459, CaseRFC1459Strict} {
		for _, in := range inputs {
			once := cm.Fold(in)
			if twice := cm.Fold(once); twice != once {
				t.Errorf("%s.Fold not idempotent on %q: %q then %q", cm, in, once, twice)
			}
			if utf8.ValidString(in) && !utf8.ValidString(once) {
				t.Errorf("%s.Fold(%q) produced invalid UTF-8 %q", cm, in, once)
			}
		}
	}
}

// TestFoldNoAllocOnFoldedInput guards the fast path: an already-folded name
// (the overwhelmingly common case, since every map key is folded once and then
// looked up forever) must not allocate.
func TestFoldNoAllocOnFoldedInput(t *testing.T) {
	in := "#already-folded{name}"
	got := testing.AllocsPerRun(100, func() { _ = CaseRFC1459.Fold(in) })
	if got != 0 {
		t.Errorf("Fold of already-folded input allocated %v times, want 0", got)
	}
}

func TestFoldReturnsSameStringWhenUnchanged(t *testing.T) {
	in := "#chan"
	if got := CaseRFC1459.Fold(in); got != in {
		t.Fatalf("Fold(%q) = %q", in, got)
	}
}

func TestParseCaseMapping(t *testing.T) {
	cases := []struct {
		in     string
		want   CaseMapping
		wantOK bool
	}{
		{"ascii", CaseASCII, true},
		{"ASCII", CaseASCII, true},
		{"rfc1459", CaseRFC1459, true},
		{"RFC1459", CaseRFC1459, true},
		{"rfc1459-strict", CaseRFC1459Strict, true},
		{"rfc7613", CaseRFC1459, false},  // PRECIS: not supported, fall back
		{"", CaseRFC1459, false},         // empty value
		{"nonsense", CaseRFC1459, false}, // hostile server
	}
	for _, tc := range cases {
		got, ok := ParseCaseMapping(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("ParseCaseMapping(%q) = (%s, %v), want (%s, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestCaseMappingString(t *testing.T) {
	for _, tc := range []struct {
		cm   CaseMapping
		want string
	}{
		{CaseASCII, "ascii"},
		{CaseRFC1459, "rfc1459"},
		{CaseRFC1459Strict, "rfc1459-strict"},
		{CaseMapping(200), "rfc1459"},
	} {
		if got := tc.cm.String(); got != tc.want {
			t.Errorf("CaseMapping(%d).String() = %q, want %q", tc.cm, got, tc.want)
		}
	}
}

func TestCaseMappingEqual(t *testing.T) {
	cases := []struct {
		cm   CaseMapping
		a, b string
		want bool
	}{
		{CaseRFC1459, "nick[]", "NICK{}", true},
		{CaseASCII, "nick[]", "NICK{}", false},
		{CaseRFC1459Strict, "nick~", "nick^", false},
		{CaseRFC1459, "nick~", "nick^", true},
		{CaseASCII, "Bob", "bob", true},
		{CaseASCII, "Bob", "bobby", false},
		{CaseASCII, "", "", true},
	}
	for _, tc := range cases {
		if got := tc.cm.Equal(tc.a, tc.b); got != tc.want {
			t.Errorf("%s.Equal(%q, %q) = %v, want %v", tc.cm, tc.a, tc.b, got, tc.want)
		}
	}
}

// TestEqualAgreesWithFold: Equal is an optimisation of Fold(a) == Fold(b) and
// must never disagree with it.
func TestEqualAgreesWithFold(t *testing.T) {
	names := []string{"", "a", "A", "nick[", "NICK{", "nick~", "nick^", "#chan", "#CHAN", "ab", "a"}
	for _, cm := range []CaseMapping{CaseASCII, CaseRFC1459, CaseRFC1459Strict} {
		for _, a := range names {
			for _, b := range names {
				want := cm.Fold(a) == cm.Fold(b)
				if got := cm.Equal(a, b); got != want {
					t.Errorf("%s: Equal(%q,%q)=%v but Fold comparison says %v", cm, a, b, got, want)
				}
			}
		}
	}
}
