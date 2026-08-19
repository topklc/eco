package proto

import (
	"reflect"
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

func TestDefaultProps(t *testing.T) {
	p := DefaultProps()
	if p.CaseMapping != CaseRFC1459 {
		t.Errorf("default casemapping = %s, want rfc1459 (what a client must assume)", p.CaseMapping)
	}
	if p.ChanTypes != "#&" {
		t.Errorf("default CHANTYPES = %q, want %q", p.ChanTypes, "#&")
	}
	if p.StatusMsg != "" {
		t.Errorf("default STATUSMSG = %q, want empty (assume unsupported)", p.StatusMsg)
	}
	if got := p.Prefix.String(); got != "(ov)@+" {
		t.Errorf("default PREFIX = %q, want %q", got, "(ov)@+")
	}
	if got := p.ChanModes.String(); got != "beI,k,l,imnpst" {
		t.Errorf("default CHANMODES = %q, want %q", got, "beI,k,l,imnpst")
	}
	if p.Modes != 3 || p.NickLen != 9 || p.ChannelLen != 200 {
		t.Errorf("default limits = MODES %d NICKLEN %d CHANNELLEN %d; want 3/9/200", p.Modes, p.NickLen, p.ChannelLen)
	}
	if p.TopicLen != 0 || p.MonitorLimit != 0 || p.ChatHistoryLimit != 0 {
		t.Errorf("unadvertised limits must stay 0, got %+v", p)
	}
	// A default-constructed props must be immediately usable.
	if !p.IsChannel("#go") || !p.IsChannel("&local") || p.IsChannel("nick") {
		t.Error("default IsChannel is wrong")
	}
}

// TestApplyErgoISUPPORT walks a realistic Ergo 005 burst.
func TestApplyErgoISUPPORT(t *testing.T) {
	p := DefaultProps()
	errs := p.Apply([]string{
		"AWAYLEN=390", "BOT=B", "CASEMAPPING=ascii", "CHANLIMIT=#:100",
		"CHANMODES=beI,k,fl,CEMRUimnstu", "CHANNELLEN=64", "CHANTYPES=#",
		"CHATHISTORY=1000", "ELIST=U", "EXCEPTS", "EXTBAN=,m", "FORWARD=f",
		"INVEX", "KICKLEN=390", "MAXLIST=beI:60", "MAXTARGETS=4", "MODES",
		"MONITOR=100", "NETWORK=ErgoTest", "NICKLEN=32", "PREFIX=(qaohv)~&@%+",
		"STATUSMSG=~&@%+", "TOPICLEN=390", "UTF8ONLY", "WHOX",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}

	if p.Network != "ErgoTest" {
		t.Errorf("NETWORK = %q", p.Network)
	}
	if p.CaseMapping != CaseASCII {
		t.Errorf("CASEMAPPING = %s, want ascii", p.CaseMapping)
	}
	if p.ChanTypes != "#" {
		t.Errorf("CHANTYPES = %q", p.ChanTypes)
	}
	if p.StatusMsg != "~&@%+" {
		t.Errorf("STATUSMSG = %q", p.StatusMsg)
	}
	if got := p.Prefix.String(); got != "(qaohv)~&@%+" {
		t.Errorf("PREFIX = %q", got)
	}
	if got := p.ChanModes.String(); got != "beI,k,fl,CEMRUimnstu" {
		t.Errorf("CHANMODES = %q", got)
	}
	if p.Modes != Unlimited {
		t.Errorf("MODES with no value = %d, want Unlimited", p.Modes)
	}
	if p.NickLen != 32 || p.ChannelLen != 64 || p.TopicLen != 390 {
		t.Errorf("limits = %d/%d/%d, want 32/64/390", p.NickLen, p.ChannelLen, p.TopicLen)
	}
	if p.MonitorLimit != 100 || p.ChatHistoryLimit != 1000 {
		t.Errorf("MONITOR/CHATHISTORY = %d/%d", p.MonitorLimit, p.ChatHistoryLimit)
	}
	if p.Excepts != 'e' || p.Invex != 'I' {
		t.Errorf("EXCEPTS/INVEX with no value = %q/%q, want e/I", p.Excepts, p.Invex)
	}
	// Unmodelled tokens are still retrievable.
	if v, ok := p.Token("maxlist"); !ok || v != "beI:60" {
		t.Errorf("Token(MAXLIST) = (%q, %v)", v, ok)
	}
	if v, ok := p.Token("UTF8ONLY"); !ok || v != "" {
		t.Errorf("valueless token should be present with an empty value, got (%q,%v)", v, ok)
	}
	if _, ok := p.Token("NOPE"); ok {
		t.Error("Token reported a token the server never sent")
	}
}

func TestApplyEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		tokens  []string
		wantErr bool
		check   func(t *testing.T, p *ServerProps)
	}{
		{
			name:   "no tokens leaves defaults",
			tokens: nil,
			check: func(t *testing.T, p *ServerProps) {
				if p.ChanTypes != DefaultChanTypes {
					t.Errorf("CHANTYPES = %q", p.ChanTypes)
				}
			},
		},
		{
			name:   "empty token is skipped",
			tokens: []string{"", "NETWORK=x"},
			check: func(t *testing.T, p *ServerProps) {
				if p.Network != "x" {
					t.Errorf("NETWORK = %q", p.Network)
				}
			},
		},
		{
			name:   "explicitly empty CHANTYPES means no channels",
			tokens: []string{"CHANTYPES="},
			check: func(t *testing.T, p *ServerProps) {
				if p.ChanTypes != "" {
					t.Errorf("CHANTYPES = %q, want empty", p.ChanTypes)
				}
				if p.IsChannel("#x") {
					t.Error("nothing is a channel when CHANTYPES is empty")
				}
			},
		},
		{
			name:   "empty PREFIX means no membership modes",
			tokens: []string{"PREFIX="},
			check: func(t *testing.T, p *ServerProps) {
				if p.Prefix.Modes != "" || p.Prefix.Prefixes != "" {
					t.Errorf("PREFIX = %+v, want empty", p.Prefix)
				}
			},
		},
		{
			name:    "malformed PREFIX is rejected and the default kept",
			tokens:  []string{"PREFIX=(ov)@"},
			wantErr: true,
			check: func(t *testing.T, p *ServerProps) {
				if p.Prefix.String() != "(ov)@+" {
					t.Errorf("PREFIX = %q, want the default preserved", p.Prefix)
				}
			},
		},
		{
			name:    "PREFIX without parens is rejected",
			tokens:  []string{"PREFIX=ov@+"},
			wantErr: true,
		},
		{
			name:    "CHANMODES with too few groups is rejected",
			tokens:  []string{"CHANMODES=b,k,l"},
			wantErr: true,
			check: func(t *testing.T, p *ServerProps) {
				if p.ChanModes.String() != "beI,k,l,imnpst" {
					t.Errorf("CHANMODES = %q, want the default preserved", p.ChanModes)
				}
			},
		},
		{
			name:   "CHANMODES with five groups folds the extra into D",
			tokens: []string{"CHANMODES=beI,k,l,imnpst,XY"},
			check: func(t *testing.T, p *ServerProps) {
				if p.ChanModes.D != "imnpstXY" {
					t.Errorf("D = %q, want imnpstXY", p.ChanModes.D)
				}
			},
		},
		{
			name:    "non-numeric NICKLEN is reported and the default kept",
			tokens:  []string{"NICKLEN=lots"},
			wantErr: true,
			check: func(t *testing.T, p *ServerProps) {
				if p.NickLen != DefaultNickLen {
					t.Errorf("NICKLEN = %d, want %d", p.NickLen, DefaultNickLen)
				}
			},
		},
		{
			name:    "unknown CASEMAPPING falls back to rfc1459 but is reported",
			tokens:  []string{"CASEMAPPING=precis"},
			wantErr: true,
			check: func(t *testing.T, p *ServerProps) {
				if p.CaseMapping != CaseRFC1459 {
					t.Errorf("CASEMAPPING = %s", p.CaseMapping)
				}
				if p.CaseMappingRaw != "precis" {
					t.Errorf("raw value not kept: %q", p.CaseMappingRaw)
				}
			},
		},
		{
			name:   "one bad token does not poison the good ones beside it",
			tokens: []string{"NICKLEN=oops", "CHANTYPES=#", "NETWORK=Good"},
			// The bad token is reported, but everything else applied.
			wantErr: true,
			check: func(t *testing.T, p *ServerProps) {
				if p.ChanTypes != "#" || p.Network != "Good" {
					t.Errorf("good tokens lost: %q %q", p.ChanTypes, p.Network)
				}
			},
		},
		{
			name:   "MODES=0 means unlimited on the wire but is clamped for senders",
			tokens: []string{"MODES=0"},
			check: func(t *testing.T, p *ServerProps) {
				if p.Modes != 0 {
					t.Errorf("MODES = %d", p.Modes)
				}
				if got := p.MaxModesPerCommand(); got != DefaultModes {
					t.Errorf("MaxModesPerCommand = %d, want %d", got, DefaultModes)
				}
			},
		},
		{
			name:   "MONITOR without a value means unlimited",
			tokens: []string{"MONITOR"},
			check: func(t *testing.T, p *ServerProps) {
				if p.MonitorLimit != Unlimited {
					t.Errorf("MONITOR = %d, want Unlimited", p.MonitorLimit)
				}
			},
		},
		{
			name:   "draft CHATHISTORY spelling is accepted",
			tokens: []string{"draft/CHATHISTORY=50"},
			check: func(t *testing.T, p *ServerProps) {
				if p.ChatHistoryLimit != 50 {
					t.Errorf("CHATHISTORY = %d", p.ChatHistoryLimit)
				}
			},
		},
		{
			name:   "lowercase token names are accepted",
			tokens: []string{"network=lower", "chantypes=#"},
			check: func(t *testing.T, p *ServerProps) {
				if p.Network != "lower" || p.ChanTypes != "#" {
					t.Errorf("lowercase tokens ignored: %q %q", p.Network, p.ChanTypes)
				}
			},
		},
		{
			name:   "escaped value is unescaped",
			tokens: []string{`NETWORK=Two\x20Words`},
			check: func(t *testing.T, p *ServerProps) {
				if p.Network != "Two Words" {
					t.Errorf("NETWORK = %q, want %q", p.Network, "Two Words")
				}
			},
		},
		{
			name:   "truncated escape is left verbatim",
			tokens: []string{`NETWORK=a\x2`},
			check: func(t *testing.T, p *ServerProps) {
				if p.Network != `a\x2` {
					t.Errorf("NETWORK = %q", p.Network)
				}
			},
		},
		{
			name:   "value may contain equals signs",
			tokens: []string{"EXTBAN=~,a=b"},
			check: func(t *testing.T, p *ServerProps) {
				if v, _ := p.Token("EXTBAN"); v != "~,a=b" {
					t.Errorf("EXTBAN = %q", v)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultProps()
			errs := p.Apply(tc.tokens)
			if got := len(errs) > 0; got != tc.wantErr {
				t.Fatalf("errors = %v, wantErr = %v", errs, tc.wantErr)
			}
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

// TestApplyNegation covers the "-TOKEN" form, which withdraws a property.
func TestApplyNegation(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"CASEMAPPING=ascii", "CHANTYPES=#", "STATUSMSG=@+", "PREFIX=(qaohv)~&@%+", "MONITOR=5", "TOPICLEN=100"})
	p.Apply([]string{"-CASEMAPPING", "-CHANTYPES", "-STATUSMSG", "-PREFIX", "-MONITOR", "-TOPICLEN"})

	d := DefaultProps()
	if p.CaseMapping != d.CaseMapping {
		t.Errorf("CASEMAPPING after negation = %s, want %s", p.CaseMapping, d.CaseMapping)
	}
	if p.ChanTypes != d.ChanTypes {
		t.Errorf("CHANTYPES after negation = %q, want %q", p.ChanTypes, d.ChanTypes)
	}
	if p.StatusMsg != "" {
		t.Errorf("STATUSMSG after negation = %q, want empty", p.StatusMsg)
	}
	if p.Prefix.String() != d.Prefix.String() {
		t.Errorf("PREFIX after negation = %q", p.Prefix)
	}
	if p.MonitorLimit != 0 || p.TopicLen != 0 {
		t.Errorf("numeric tokens after negation = %d/%d, want 0/0", p.MonitorLimit, p.TopicLen)
	}
	if _, ok := p.Token("CHANTYPES"); ok {
		t.Error("negated token still present in Tokens")
	}
}

// TestApplyNegationRestoresEveryModelledToken sets each modelled token to a
// non-default value, negates it, and requires the whole struct to be
// indistinguishable from a fresh one. A reset that forgets a field would leave
// a stale property behind for the rest of the connection.
func TestApplyNegationRestoresEveryModelledToken(t *testing.T) {
	cases := []struct{ set, negate string }{
		{"NETWORK=Zeta", "-NETWORK"},
		{"CASEMAPPING=ascii", "-CASEMAPPING"},
		{"CHANTYPES=!", "-CHANTYPES"},
		{"STATUSMSG=@+", "-STATUSMSG"},
		{"PREFIX=(qaohv)~&@%+", "-PREFIX"},
		{"CHANMODES=b,k,l,z", "-CHANMODES"},
		{"EXCEPTS=x", "-EXCEPTS"},
		{"INVEX=y", "-INVEX"},
		{"MODES=12", "-MODES"},
		{"NICKLEN=32", "-NICKLEN"},
		{"MAXNICKLEN=32", "-MAXNICKLEN"},
		{"CHANNELLEN=64", "-CHANNELLEN"},
		{"TOPICLEN=390", "-TOPICLEN"},
		{"MONITOR=100", "-MONITOR"},
		{"CHATHISTORY=1000", "-CHATHISTORY"},
		{"draft/CHATHISTORY=1000", "-draft/CHATHISTORY"},
		// A token we do not model must also vanish from Tokens.
		{"MAXLIST=beI:60", "-MAXLIST"},
	}
	for _, tc := range cases {
		t.Run(tc.set, func(t *testing.T) {
			p := DefaultProps()
			if errs := p.Apply([]string{tc.set}); len(errs) > 0 {
				t.Fatalf("setup %q: %v", tc.set, errs)
			}
			p.Apply([]string{tc.negate})

			want := DefaultProps()
			if len(p.Tokens) != 0 {
				t.Errorf("Tokens after negation = %v, want empty", p.Tokens)
			}
			p.Tokens, want.Tokens = nil, nil
			if !reflect.DeepEqual(p, want) {
				t.Errorf("after %q then %q:\n got: %+v\nwant: %+v", tc.set, tc.negate, *p, *want)
			}
		})
	}
}

// TestNegatingAnUnknownTokenIsHarmless: a server may withdraw something we
// never modelled or never saw.
func TestNegatingAnUnknownTokenIsHarmless(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"-NEVERHEARDOFIT", "-"})
	want := DefaultProps()
	p.Tokens, want.Tokens = nil, nil
	if !reflect.DeepEqual(p, want) {
		t.Errorf("negating unknown tokens changed the props: %+v", *p)
	}
}

// TestApplyAccumulatesAcrossLines: a server splits ISUPPORT across several 005
// lines; each Apply amends what came before.
func TestApplyAccumulatesAcrossLines(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"NETWORK=Libera", "CHANTYPES=#"})
	p.Apply([]string{"PREFIX=(ohv)@%+"})
	p.Apply([]string{"CASEMAPPING=rfc1459"})

	if p.Network != "Libera" || p.ChanTypes != "#" {
		t.Errorf("first line lost: %q %q", p.Network, p.ChanTypes)
	}
	if p.Prefix.String() != "(ohv)@%+" {
		t.Errorf("PREFIX = %q", p.Prefix)
	}
	if p.CaseMapping != CaseRFC1459 {
		t.Errorf("CASEMAPPING = %s", p.CaseMapping)
	}
}

func TestISUPPORTTokens(t *testing.T) {
	cases := []struct {
		name   string
		params []string
		want   []string
	}{
		{
			name:   "nick and trailing text stripped",
			params: []string{"eco", "CHANTYPES=#", "PREFIX=(ov)@+", "are supported by this server"},
			want:   []string{"CHANTYPES=#", "PREFIX=(ov)@+"},
		},
		{
			name:   "no trailing text",
			params: []string{"eco", "CHANTYPES=#"},
			want:   []string{"CHANTYPES=#"},
		},
		{
			name:   "trailing text only",
			params: []string{"eco", "are supported by this server"},
			want:   nil,
		},
		{
			name:   "empty trailing param is dropped",
			params: []string{"eco", "CHANTYPES=#", ""},
			want:   []string{"CHANTYPES=#"},
		},
		{
			name:   "too few params",
			params: []string{"eco"},
			want:   nil,
		},
		{
			name:   "nothing at all",
			params: nil,
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ISUPPORTTokens(tc.params)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("ISUPPORTTokens(%q) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}

func TestPrefixMap(t *testing.T) {
	pm, err := ParsePrefix("(qaohv)~&@%+")
	if err != nil {
		t.Fatal(err)
	}

	if got := pm.PrefixOf('o'); got != '@' {
		t.Errorf("PrefixOf('o') = %q", got)
	}
	if got := pm.PrefixOf('z'); got != 0 {
		t.Errorf("PrefixOf of a non-member mode = %q, want 0", got)
	}
	if got := pm.ModeOf('%'); got != 'h' {
		t.Errorf("ModeOf('%%') = %q", got)
	}
	if got := pm.ModeOf('!'); got != 0 {
		t.Errorf("ModeOf of a non-prefix = %q, want 0", got)
	}
	if !pm.IsMemberMode('q') || pm.IsMemberMode('b') {
		t.Error("IsMemberMode is wrong")
	}
	if !pm.IsPrefixChar('~') || pm.IsPrefixChar('#') {
		t.Error("IsPrefixChar is wrong")
	}

	// Rank: index order is rank order, highest first.
	for i, want := range map[byte]int{'~': 0, '&': 1, '@': 2, '%': 3, '+': 4, '!': -1} {
		if got := pm.Rank(i); got != want {
			t.Errorf("Rank(%q) = %d, want %d", i, got, want)
		}
	}

	for _, tc := range []struct {
		in   string
		want byte
	}{
		{"", 0},
		{"+", '+'},
		{"@+", '@'},
		{"+@", '@'},
		{"~&@%+", '~'},
		{"!?", 0},
	} {
		if got := pm.Highest(tc.in); got != tc.want {
			t.Errorf("Highest(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"+@", "@+"},
		{"%~", "~%"},
		{"@@+", "@+"}, // duplicates collapse
		{"@!+", "@+"}, // unknown characters drop
		{"~&@%+", "~&@%+"},
	} {
		if got := pm.Sort(tc.in); got != tc.want {
			t.Errorf("Sort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, tc := range []struct{ in, wantPrefix, wantNick string }{
		{"nick", "", "nick"},
		{"@nick", "@", "nick"},
		{"~&@nick", "~&@", "nick"}, // multi-prefix
		{"", "", ""},
		{"@", "@", ""},
	} {
		gp, gn := pm.Split(tc.in)
		if gp != tc.wantPrefix || gn != tc.wantNick {
			t.Errorf("Split(%q) = (%q,%q), want (%q,%q)", tc.in, gp, gn, tc.wantPrefix, tc.wantNick)
		}
	}
}

func TestParseNamesEntry(t *testing.T) {
	p := ergoProps(t)
	cases := []struct {
		name                   string
		entry                  string
		nick, folded, prefixes string
	}{
		{"plain", "Alice", "Alice", "alice", ""},
		{"one prefix", "@Alice", "Alice", "alice", "@"},
		{"multi-prefix", "~&@Alice", "Alice", "alice", "~&@"},
		{"prefixes out of rank order are sorted", "+@Alice", "Alice", "alice", "@+"},
		{"userhost-in-names", "@Alice!alice@example.org", "Alice", "alice", "@"},
		{"multi-prefix and userhost", "~+Bob!b@h", "Bob", "bob", "~+"},
		{"rfc1459 characters in the nick", "@Nick[Away]", "Nick[Away]", "nick[away]", "@"},
		{"empty", "", "", "", ""},
		{"prefixes only", "@@", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.ParseNamesEntry(tc.entry)
			if got.Nick != tc.nick || got.Folded != tc.folded || got.Prefixes != tc.prefixes {
				t.Errorf("ParseNamesEntry(%q) = %+v, want nick %q folded %q prefixes %q",
					tc.entry, got, tc.nick, tc.folded, tc.prefixes)
			}
		})
	}

	// The fold follows the network's casemapping, not strings.ToLower.
	rfc := DefaultProps()
	if got := rfc.ParseNamesEntry("@Nick[Away]"); got.Folded != "nick{away}" {
		t.Errorf("rfc1459 folded = %q, want nick{away}", got.Folded)
	}
	// And with no props at all, the classic defaults apply.
	var nilProps *ServerProps
	if got := nilProps.ParseNamesEntry("@Alice"); got.Prefixes != "@" || got.Folded != "alice" {
		t.Errorf("nil props ParseNamesEntry = %+v", got)
	}
}

func TestPrefixMapEmpty(t *testing.T) {
	var pm PrefixMap
	if pm.String() != "" {
		t.Errorf("empty PrefixMap.String() = %q", pm.String())
	}
	if pm.Highest("@") != 0 || pm.Sort("@") != "" {
		t.Error("empty PrefixMap should recognise nothing")
	}
	gp, gn := pm.Split("@nick")
	if gp != "" || gn != "@nick" {
		t.Errorf("Split with no prefixes = (%q,%q)", gp, gn)
	}
}

func TestSplitStatusMsg(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"CHANTYPES=#&", "STATUSMSG=~&@%+"})

	cases := []struct {
		in                 string
		wantPrefix, wantCh string
	}{
		{"#chan", "", "#chan"},
		{"@#chan", "@", "#chan"},
		{"%+#chan", "%+", "#chan"},
		{"~#chan", "~", "#chan"},
		// A nick is not a status target even when it starts with a prefix
		// character: the remainder has to be a channel.
		{"@nick", "", "@nick"},
		{"&local", "", "&local"},   // '&' is a channel type here, not a statusmsg target
		{"@&local", "@", "&local"}, // ...but prefixed it still splits
		{"", "", ""},
		{"@", "", "@"},
	}
	for _, tc := range cases {
		gp, gc := p.SplitStatusMsg(tc.in)
		if gp != tc.wantPrefix || gc != tc.wantCh {
			t.Errorf("SplitStatusMsg(%q) = (%q,%q), want (%q,%q)", tc.in, gp, gc, tc.wantPrefix, tc.wantCh)
		}
	}

	// With no STATUSMSG advertised, nothing splits - which is what makes v1's
	// bogus "@#chan" query buffer impossible here.
	plain := DefaultProps()
	if gp, gc := plain.SplitStatusMsg("@#chan"); gp != "" || gc != "@#chan" {
		t.Errorf("without STATUSMSG, SplitStatusMsg = (%q,%q)", gp, gc)
	}
}

func TestIsChannelAndBufferKind(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"CHANTYPES=#&"})

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"#go", true},
		{"&local", true}, // the v1 bug: '&' channels were treated as queries
		{"nick", false},
		{"", false},
		{"##double", true},
	} {
		if got := p.IsChannel(tc.name); got != tc.want {
			t.Errorf("IsChannel(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
	if p.BufferKind("&local") != model.BufferChannel {
		t.Error("&local should be a channel buffer")
	}
	if p.BufferKind("nick") != model.BufferQuery {
		t.Error("nick should be a query buffer")
	}
}

func TestBufferIDUsesCasemapping(t *testing.T) {
	rfc := DefaultProps() // rfc1459 by default
	ascii := DefaultProps()
	ascii.Apply([]string{"CASEMAPPING=ascii"})

	const net = model.NetworkID("n1")
	got := rfc.BufferID(net, "Nick[Away]")
	want := model.BufferID{Network: net, Folded: "nick{away}"}
	if got != want {
		t.Errorf("rfc1459 BufferID = %+v, want %+v", got, want)
	}
	if got := ascii.BufferID(net, "Nick[Away]"); got.Folded != "nick[away]" {
		t.Errorf("ascii BufferID folded = %q", got.Folded)
	}
	// Two spellings of the same name must produce one identical id.
	if rfc.BufferID(net, "#Chan") != rfc.BufferID(net, "#CHAN") {
		t.Error("BufferID is not stable across casing")
	}
}

// TestNilPropsAreUsable: every accessor must survive a nil receiver, so no
// caller has to nil-check props it has not received a 005 for yet.
func TestNilPropsAreUsable(t *testing.T) {
	var p *ServerProps
	if got := p.Fold("Nick["); got != "nick{" {
		t.Errorf("nil Fold = %q, want rfc1459 folding", got)
	}
	if !p.Equal("A", "a") {
		t.Error("nil Equal")
	}
	if !p.IsChannel("#x") || !p.IsChannel("&x") || p.IsChannel("x") {
		t.Error("nil IsChannel")
	}
	if pre, name := p.SplitStatusMsg("@#c"); pre != "" || name != "@#c" {
		t.Errorf("nil SplitStatusMsg = (%q,%q)", pre, name)
	}
	if got := p.MaxModesPerCommand(); got != DefaultModes {
		t.Errorf("nil MaxModesPerCommand = %d", got)
	}
	if _, ok := p.Token("X"); ok {
		t.Error("nil Token reported a token")
	}
	if got := p.BufferID("n", "#C"); got.Folded != "#c" {
		t.Errorf("nil BufferID = %+v", got)
	}
	if c := p.Clone(); c == nil || c.ChanTypes != DefaultChanTypes {
		t.Error("nil Clone should yield defaults")
	}
}

func TestClone(t *testing.T) {
	p := DefaultProps()
	p.Apply([]string{"NETWORK=A", "CHANTYPES=#", "MAXLIST=beI:60"})
	c := p.Clone()

	c.Apply([]string{"NETWORK=B", "CHANTYPES=#&", "MAXLIST=b:10"})
	if p.Network != "A" || p.ChanTypes != "#" {
		t.Errorf("mutating a clone changed the original: %q %q", p.Network, p.ChanTypes)
	}
	if v, _ := p.Token("MAXLIST"); v != "beI:60" {
		t.Errorf("Tokens map was shared with the clone: %q", v)
	}
}

func TestMaxModesPerCommand(t *testing.T) {
	cases := []struct {
		modes int
		want  int
	}{
		{3, 3},
		{20, 20},
		{Unlimited, MaxModesUnbounded},
		{0, DefaultModes},
		{-7, DefaultModes},
	}
	for _, tc := range cases {
		p := DefaultProps()
		p.Modes = tc.modes
		if got := p.MaxModesPerCommand(); got != tc.want {
			t.Errorf("Modes=%d: MaxModesPerCommand = %d, want %d", tc.modes, got, tc.want)
		}
	}
}

func TestUnescapeISUPPORT(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`a\x20b`, "a b"},
		{`\x41\x42`, "AB"},
		{`\X41`, "A"},
		{`trailing\`, `trailing\`},
		{`\xZZ`, `\xZZ`},
		{`\x2`, `\x2`},
		{``, ``},
		{`100%\x20off`, "100% off"},
	}
	for _, tc := range cases {
		if got := unescapeISUPPORT(tc.in); got != tc.want {
			t.Errorf("unescapeISUPPORT(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
