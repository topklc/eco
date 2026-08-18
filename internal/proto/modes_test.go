package proto

import (
	"errors"
	"strings"
	"testing"
)

// ergoProps is a server that advertises everything: five prefix ranks, list
// modes, a key, a limit, and a wide flag set.
func ergoProps(t *testing.T) *ServerProps {
	t.Helper()
	p := DefaultProps()
	if errs := p.Apply([]string{
		"CASEMAPPING=ascii",
		"CHANTYPES=#",
		"CHANMODES=beI,k,fl,CEMRUimnstu",
		"PREFIX=(qaohv)~&@%+",
		"STATUSMSG=~&@%+",
	}); len(errs) > 0 {
		t.Fatalf("props setup: %v", errs)
	}
	return p
}

// fmtChanges renders a delta as a compact string so table rows stay readable.
func fmtChanges(cs []ModeChange) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, " ")
}

func TestParseModeDelta(t *testing.T) {
	cases := []struct {
		name   string
		params []string
		want   string
	}{
		{
			name:   "paramless flags",
			params: []string{"+nt"},
			want:   "+n(flag) +t(flag)",
		},
		{
			name:   "implicit add when the mode string has no sign",
			params: []string{"nt"},
			want:   "+n(flag) +t(flag)",
		},
		{
			name:   "mixed signs",
			params: []string{"+i-m"},
			want:   "+i(flag) -m(flag)",
		},
		{
			name:   "sign with nothing after it",
			params: []string{"+n-"},
			want:   "+n(flag)",
		},

		// Membership modes: PREFIX decides, not a hardcoded letter list.
		{
			name:   "op",
			params: []string{"+o", "alice"},
			want:   "+o(member) alice",
		},
		{
			name:   "deop",
			params: []string{"-o", "alice"},
			want:   "-o(member) alice",
		},
		{
			name:   "two members in one command",
			params: []string{"+ov", "alice", "bob"},
			want:   "+o(member) alice +v(member) bob",
		},
		{
			name:   "founder and admin exist on this server",
			params: []string{"+qa", "alice", "bob"},
			want:   "+q(member) alice +a(member) bob",
		},
		{
			name:   "halfop",
			params: []string{"+h", "carol"},
			want:   "+h(member) carol",
		},

		// Key: CHANMODES group B, parameter on both set and unset.
		{
			name:   "set key",
			params: []string{"+k", "hunter2"},
			want:   "+k(key) hunter2",
		},
		{
			name:   "clear key with the key echoed back",
			params: []string{"-k", "hunter2"},
			want:   "-k(key) hunter2",
		},

		// Limit: CHANMODES group C, parameter only when set.
		{
			name:   "set limit",
			params: []string{"+l", "50"},
			want:   "+l(limit) 50",
		},
		{
			name:   "clear limit takes no parameter",
			params: []string{"-l"},
			want:   "-l(limit)",
		},
		{
			name:   "clearing a limit must not eat the next mode's parameter",
			params: []string{"-l+k", "secret"},
			want:   "-l(limit) +k(key) secret",
		},

		// List modes: CHANMODES group A.
		{
			name:   "ban add",
			params: []string{"+b", "*!*@spam.example"},
			want:   "+b(list) *!*@spam.example",
		},
		{
			name:   "ban remove",
			params: []string{"-b", "*!*@spam.example"},
			want:   "-b(list) *!*@spam.example",
		},
		{
			name:   "three bans in one command",
			params: []string{"+bbb", "a!*@*", "b!*@*", "c!*@*"},
			want:   "+b(list) a!*@* +b(list) b!*@* +b(list) c!*@*",
		},
		{
			name:   "except and invex are list modes",
			params: []string{"+eI", "e!*@*", "i!*@*"},
			want:   "+e(list) e!*@* +I(list) i!*@*",
		},

		// The multi-param batch a real server sends.
		{
			name:   "everything at once",
			params: []string{"+ovbkl-m", "alice", "bob", "*!*@bad", "sekrit", "42"},
			want:   "+o(member) alice +v(member) bob +b(list) *!*@bad +k(key) sekrit +l(limit) 42 -m(flag)",
		},
		{
			name:   "removals interleaved with additions consume parameters in order",
			params: []string{"-o+v", "alice", "bob"},
			want:   "-o(member) alice +v(member) bob",
		},

		// Modes this server declared in group D but that v1's switch never
		// heard of. Under v1 they fell through to setMode, which was right by
		// luck; a server-specific param mode would have desynchronised.
		{
			name:   "server-specific flags from CHANMODES D",
			params: []string{"+CEMRU"},
			want:   "+C(flag) +E(flag) +M(flag) +R(flag) +U(flag)",
		},
		{
			name:   "forward is a group C mode on this server",
			params: []string{"+f", "#overflow"},
			want:   "+f(limit) #overflow",
		},

		// Unknown letters: assumed paramless, and marked so.
		{
			name:   "unknown mode takes no parameter",
			params: []string{"+Z"},
			want:   "+Z(flag)",
		},
		{
			name:   "an unknown mode does not steal the next mode's parameter",
			params: []string{"+Zk", "key"},
			want:   "+Z(flag) +k(key) key",
		},

		// Extra parameters are simply unused.
		{
			name:   "surplus parameters are ignored",
			params: []string{"+n", "junk", "more"},
			want:   "+n(flag)",
		},
	}

	p := ergoProps(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseModeDelta(p, tc.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s := fmtChanges(got); s != tc.want {
				t.Errorf("ParseModeDelta(%q)\n got: %s\nwant: %s", tc.params, s, tc.want)
			}
		})
	}
}

func TestParseModeDeltaPrefixCharacters(t *testing.T) {
	p := ergoProps(t)
	changes, err := ParseModeDelta(p, []string{"+qaohv", "a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatal(err)
	}
	want := "~&@%+"
	got := make([]byte, 0, len(changes))
	for _, c := range changes {
		if c.Class != ModeClassMember {
			t.Fatalf("%s should be a member mode", c)
		}
		got = append(got, c.Prefix)
	}
	if string(got) != want {
		t.Errorf("prefix characters = %q, want %q", got, want)
	}

	// A server with a different table produces different characters from the
	// same mode letters — the whole point of not hardcoding "~&@%+".
	odd := DefaultProps()
	odd.Apply([]string{"PREFIX=(Yohv)!@%+"})
	changes, err = ParseModeDelta(odd, []string{"+Y", "boss"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Prefix != '!' || changes[0].Class != ModeClassMember {
		t.Errorf("custom PREFIX not honoured: %s", fmtChanges(changes))
	}
}

// TestParseModeDeltaWithDefaults exercises the "server told us nothing" path:
// the defaults must still classify the classic modes correctly.
func TestParseModeDeltaWithDefaults(t *testing.T) {
	cases := []struct {
		params []string
		want   string
	}{
		{[]string{"+o", "alice"}, "+o(member) alice"},
		{[]string{"+v", "bob"}, "+v(member) bob"},
		{[]string{"+b", "*!*@x"}, "+b(list) *!*@x"},
		{[]string{"+e", "*!*@x"}, "+e(list) *!*@x"},
		{[]string{"+I", "*!*@x"}, "+I(list) *!*@x"},
		{[]string{"+k", "key"}, "+k(key) key"},
		{[]string{"+l", "10"}, "+l(limit) 10"},
		{[]string{"-l"}, "-l(limit)"},
		{[]string{"+imnpst"}, "+i(flag) +m(flag) +n(flag) +p(flag) +s(flag) +t(flag)"},
	}
	for _, tc := range cases {
		got, err := ParseModeDelta(DefaultProps(), tc.params)
		if err != nil {
			t.Fatalf("%q: %v", tc.params, err)
		}
		if s := fmtChanges(got); s != tc.want {
			t.Errorf("defaults %q\n got: %s\nwant: %s", tc.params, s, tc.want)
		}
	}
}

// TestParseModeDeltaNilProps: parsing with no props at all must behave exactly
// like parsing with the defaults.
func TestParseModeDeltaNilProps(t *testing.T) {
	params := []string{"+ovbkl", "a", "b", "*!*@x", "key", "5"}
	withNil, err1 := ParseModeDelta(nil, params)
	withDefaults, err2 := ParseModeDelta(DefaultProps(), params)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if fmtChanges(withNil) != fmtChanges(withDefaults) {
		t.Errorf("nil props differ from defaults:\n%s\n%s", fmtChanges(withNil), fmtChanges(withDefaults))
	}
}

// TestExceptsInvexWithoutChanmodes: a server that advertises EXCEPTS/INVEX but
// no CHANMODES still gets its list modes classified correctly, so "+e mask"
// consumes its parameter instead of leaving it for the next mode.
func TestExceptsInvexWithoutChanmodes(t *testing.T) {
	p := DefaultProps()
	p.ChanModes = ChanModes{A: "b", B: "k", C: "l", D: "imnpst"} // e/I absent
	p.Apply([]string{"EXCEPTS=e", "INVEX=I"})

	got, err := ParseModeDelta(p, []string{"+eI", "e!*@*", "i!*@*"})
	if err != nil {
		t.Fatal(err)
	}
	if s := fmtChanges(got); s != "+e(list) e!*@* +I(list) i!*@*" {
		t.Errorf("got %s", s)
	}
}

func TestParseModeDeltaErrors(t *testing.T) {
	p := ergoProps(t)

	t.Run("empty params", func(t *testing.T) {
		if _, err := ParseModeDelta(p, nil); !errors.Is(err, ErrEmptyModeString) {
			t.Errorf("err = %v, want ErrEmptyModeString", err)
		}
	})
	t.Run("empty mode string", func(t *testing.T) {
		if _, err := ParseModeDelta(p, []string{""}); !errors.Is(err, ErrEmptyModeString) {
			t.Errorf("err = %v, want ErrEmptyModeString", err)
		}
	})
	t.Run("sign only", func(t *testing.T) {
		got, err := ParseModeDelta(p, []string{"+"})
		if err != nil || len(got) != 0 {
			t.Errorf("got %v, %v; want no changes and no error", got, err)
		}
	})

	t.Run("missing parameter is reported but the change survives", func(t *testing.T) {
		got, err := ParseModeDelta(p, []string{"-k"})
		if err == nil {
			t.Fatal("want a MissingParamError")
		}
		var mp MissingParamError
		if !errors.As(err, &mp) || mp.Mode != 'k' || mp.Add {
			t.Errorf("err = %v, want MissingParamError{k,-}", err)
		}
		// -k with no key still has to clear the key.
		if s := fmtChanges(got); s != "-k(key)" {
			t.Errorf("changes = %s, want -k(key)", s)
		}
	})

	t.Run("parsing continues past a missing parameter", func(t *testing.T) {
		got, err := ParseModeDelta(p, []string{"+ok-n", "alice"})
		if err == nil {
			t.Fatal("want an error for the keyless +k")
		}
		if s := fmtChanges(got); s != "+o(member) alice +k(key) -n(flag)" {
			t.Errorf("changes = %s", s)
		}
	})

	t.Run("several missing parameters are all reported", func(t *testing.T) {
		_, err := ParseModeDelta(p, []string{"+ovb"})
		if err == nil {
			t.Fatal("want errors")
		}
		if n := strings.Count(err.Error(), "missing its parameter"); n != 3 {
			t.Errorf("reported %d missing parameters, want 3: %v", n, err)
		}
	})
}

func TestModeClass(t *testing.T) {
	p := ergoProps(t)
	cases := []struct {
		mode      byte
		want      ModeClass
		wantKnown bool
	}{
		{'o', ModeClassMember, true},
		{'q', ModeClassMember, true},
		{'b', ModeClassList, true},
		{'e', ModeClassList, true},
		{'I', ModeClassList, true},
		{'k', ModeClassKey, true},
		{'l', ModeClassLimit, true},
		{'f', ModeClassLimit, true},
		{'n', ModeClassFlag, true},
		{'Z', ModeClassFlag, false}, // never advertised
	}
	for _, tc := range cases {
		got, known := p.ModeClass(tc.mode)
		if got != tc.want || known != tc.wantKnown {
			t.Errorf("ModeClass(%q) = (%s, %v), want (%s, %v)", tc.mode, got, known, tc.want, tc.wantKnown)
		}
	}
}

// TestModeClassPrefixWinsOverChanmodes: RFC 1459 nominally puts membership
// modes in group B, and some servers still list them there. Treating +o as a
// key would silently destroy the member list, so PREFIX must win.
func TestModeClassPrefixWinsOverChanmodes(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"CHANMODES=beI,kov,l,imnpst", "PREFIX=(ov)@+"})

	for _, m := range []byte{'o', 'v'} {
		class, _ := p.ModeClass(m)
		if class != ModeClassMember {
			t.Errorf("ModeClass(%q) = %s, want member", m, class)
		}
	}
	got, err := ParseModeDelta(p, []string{"+o", "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Class != ModeClassMember || got[0].Prefix != '@' {
		t.Errorf("got %s, want a member change granting @", fmtChanges(got))
	}
}

func TestParseUserModeDelta(t *testing.T) {
	cases := []struct {
		params  []string
		want    string
		wantErr bool
	}{
		{params: []string{"+i"}, want: "+i(flag)"},
		{params: []string{"+iw-x"}, want: "+i(flag) +w(flag) -x(flag)"},
		{params: []string{"iRZ"}, want: "+i(flag) +R(flag) +Z(flag)"},
		{params: nil, wantErr: true},
		{params: []string{""}, wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseUserModeDelta(tc.params)
		if (err != nil) != tc.wantErr {
			t.Errorf("%q: err = %v, wantErr = %v", tc.params, err, tc.wantErr)
			continue
		}
		if err == nil {
			if s := fmtChanges(got); s != tc.want {
				t.Errorf("%q = %s, want %s", tc.params, s, tc.want)
			}
		}
	}
}

func TestApplyFlags(t *testing.T) {
	p := ergoProps(t)
	cases := []struct {
		name   string
		start  string
		params []string
		want   string
	}{
		{"add flags", "", []string{"+nt"}, "nt"},
		{"remove a flag", "mnt", []string{"-m"}, "nt"},
		{"result is sorted", "", []string{"+tn"}, "nt"},
		{"key counts as a visible flag", "", []string{"+k", "secret"}, "k"},
		{"clearing the key clears the flag", "kn", []string{"-k", "secret"}, "n"},
		{"limit counts as a visible flag", "n", []string{"+l", "50"}, "ln"},
		{"clearing the limit clears the flag", "ln", []string{"-l"}, "n"},
		{"member modes never touch the flag string", "n", []string{"+o", "alice"}, "n"},
		{"list modes never touch the flag string", "n", []string{"+b", "*!*@x"}, "n"},
		{"adding a flag twice is idempotent", "n", []string{"+n"}, "n"},
		{"removing an absent flag is a no-op", "n", []string{"-t"}, "n"},
		{"uppercase and lowercase coexist", "", []string{"+CnR"}, "CRn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes, err := ParseModeDelta(p, tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if got := ApplyFlags(tc.start, changes); got != tc.want {
				t.Errorf("ApplyFlags(%q, %q) = %q, want %q", tc.start, tc.params, got, tc.want)
			}
		})
	}
}

// TestApplyFlagsFullState models what 324 RPL_CHANNELMODEIS does: rebuild the
// flag string from scratch, then track the key out of band.
func TestApplyFlagsFullState(t *testing.T) {
	p := ergoProps(t)
	changes, err := ParseModeDelta(p, []string{"+ntkl", "sekrit", "42"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ApplyFlags("", changes); got != "klnt" {
		t.Errorf("flags = %q, want klnt", got)
	}
	var key, limit string
	for _, c := range changes {
		switch c.Class {
		case ModeClassKey:
			key = c.Param
		case ModeClassLimit:
			limit = c.Param
		}
	}
	if key != "sekrit" || limit != "42" {
		t.Errorf("key/limit = %q/%q", key, limit)
	}
}
