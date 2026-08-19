package proto

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
)

// Unlimited is the value of a numeric ServerProps limit the server advertised
// as having no bound (e.g. "MODES" with no value). A limit of 0 means the
// server advertised nothing, so no client-side limit should be enforced either
// - the difference matters only for the error message when the server refuses.
const Unlimited = -1

// Defaults for the properties a server may leave unadvertised. These are the
// values a client must assume, not values we invented; see each field on
// ServerProps.
const (
	DefaultChanTypes  = "#&"     // RFC 1459 channel prefixes
	DefaultPrefixSpec = "(ov)@+" // op and voice
	DefaultModes      = 3        // mode changes per MODE when MODES is absent
	DefaultNickLen    = 9        // RFC 1459
	DefaultChannelLen = 200      // RFC 2812
	DefaultChanModesA = "beI"    // ban, ban-exception, invite-exception
	DefaultChanModesB = "k"      // key
	DefaultChanModesC = "l"      // user limit
	DefaultChanModesD = "imnpst" // the classic paramless flags
)

// PrefixMap is the parsed ISUPPORT PREFIX token: the channel membership modes
// and the prefix character each one grants, in descending rank order. It is the
// replacement for v1's hardcoded "~&@%+" - on a server with PREFIX=(Yohv)!@%+,
// rank is whatever the server says it is.
type PrefixMap struct {
	// Modes and Prefixes are parallel and equal length: Modes[i] grants
	// Prefixes[i], and index 0 is the highest rank.
	Modes    string
	Prefixes string
}

// ParsePrefix parses a PREFIX value of the form "(ov)@+". An empty value means
// the server supports no membership prefixes at all, which is legal.
func ParsePrefix(v string) (PrefixMap, error) {
	if v == "" {
		return PrefixMap{}, nil
	}
	if !strings.HasPrefix(v, "(") {
		return PrefixMap{}, fmt.Errorf("proto: PREFIX %q: missing '('", v)
	}
	end := strings.IndexByte(v, ')')
	if end < 0 {
		return PrefixMap{}, fmt.Errorf("proto: PREFIX %q: missing ')'", v)
	}
	modes, prefixes := v[1:end], v[end+1:]
	if len(modes) != len(prefixes) {
		return PrefixMap{}, fmt.Errorf("proto: PREFIX %q: %d modes but %d prefixes", v, len(modes), len(prefixes))
	}
	return PrefixMap{Modes: modes, Prefixes: prefixes}, nil
}

// String renders the map back into ISUPPORT form.
func (p PrefixMap) String() string {
	if p.Modes == "" {
		return ""
	}
	return "(" + p.Modes + ")" + p.Prefixes
}

// IsMemberMode reports whether m is a channel membership mode (one of PREFIX's
// modes) rather than an ordinary channel mode.
func (p PrefixMap) IsMemberMode(m byte) bool { return strings.IndexByte(p.Modes, m) >= 0 }

// IsPrefixChar reports whether b is one of the prefix characters.
func (p PrefixMap) IsPrefixChar(b byte) bool { return strings.IndexByte(p.Prefixes, b) >= 0 }

// PrefixOf returns the prefix character a membership mode grants, or 0.
func (p PrefixMap) PrefixOf(mode byte) byte {
	if i := strings.IndexByte(p.Modes, mode); i >= 0 {
		return p.Prefixes[i]
	}
	return 0
}

// ModeOf returns the membership mode a prefix character corresponds to, or 0.
func (p PrefixMap) ModeOf(prefix byte) byte {
	if i := strings.IndexByte(p.Prefixes, prefix); i >= 0 {
		return p.Modes[i]
	}
	return 0
}

// Rank returns the rank of a prefix character: 0 is the highest, and -1 means
// the character is not a prefix on this network. Sorting members by rank is
// therefore a table lookup, not a search in a hardcoded string.
func (p PrefixMap) Rank(prefix byte) int {
	if i := strings.IndexByte(p.Prefixes, prefix); i >= 0 {
		return i
	}
	return -1
}

// Highest returns the highest-ranked prefix character in a member's prefix set,
// or 0 when the set is empty. This is the character the roster shows.
func (p PrefixMap) Highest(prefixes string) byte {
	best, bestRank := byte(0), len(p.Prefixes)
	for i := 0; i < len(prefixes); i++ {
		if r := p.Rank(prefixes[i]); r >= 0 && r < bestRank {
			best, bestRank = prefixes[i], r
		}
	}
	return best
}

// Sort returns the prefix set in descending rank order, with duplicates and
// unknown characters removed. Member prefix strings are always stored sorted so
// that two equal sets compare equal.
func (p PrefixMap) Sort(prefixes string) string {
	if prefixes == "" {
		return ""
	}
	var out []byte
	for i := 0; i < len(p.Prefixes); i++ {
		if strings.IndexByte(prefixes, p.Prefixes[i]) >= 0 {
			out = append(out, p.Prefixes[i])
		}
	}
	return string(out)
}

// Split separates the leading membership prefixes from a NAMES entry:
// Split("@+nick") is ("@+", "nick"). With multi-prefix there may be several;
// without it there is at most one.
func (p PrefixMap) Split(entry string) (prefixes, nick string) {
	i := 0
	for i < len(entry) && p.IsPrefixChar(entry[i]) {
		i++
	}
	return entry[:i], entry[i:]
}

// ChanModes is the parsed ISUPPORT CHANMODES token: the four classes that say
// whether a mode letter takes a parameter and what it means. This is what makes
// mode-delta parsing correct instead of a hardcoded letter list.
type ChanModes struct {
	A string // always takes a parameter; adds to / removes from a list (b, e, I)
	B string // always takes a parameter; sets or unsets a single value (k)
	C string // takes a parameter only when being set (l)
	D string // never takes a parameter (i, m, n, p, s, t)
}

// ParseChanModes parses a CHANMODES value of the form "A,B,C,D". Servers may
// advertise more than four groups; per the spec, later groups are treated as
// taking no parameter, so they are folded into D.
func ParseChanModes(v string) (ChanModes, error) {
	parts := strings.Split(v, ",")
	if len(parts) < 4 {
		return ChanModes{}, fmt.Errorf("proto: CHANMODES %q: want at least 4 comma-separated groups, got %d", v, len(parts))
	}
	cm := ChanModes{A: parts[0], B: parts[1], C: parts[2], D: parts[3]}
	for _, extra := range parts[4:] {
		cm.D += extra
	}
	return cm, nil
}

// String renders the classes back into ISUPPORT form.
func (c ChanModes) String() string { return c.A + "," + c.B + "," + c.C + "," + c.D }

// ServerProps is everything a network told us about itself in RPL_ISUPPORT
// (005), plus the defaults for what it did not tell us. One per network, owned
// by that network's store, replaced wholesale on reconnect.
//
// It is a plain value with no locking: build it on the connection goroutine,
// hand it to the store, and read it from the single writer. Clone before
// sharing it across goroutines.
type ServerProps struct {
	// Network is the NETWORK token: the server's own name for itself.
	Network string

	// CaseMapping is how names fold. Default CaseRFC1459.
	CaseMapping CaseMapping
	// CaseMappingRaw is the token exactly as advertised, kept because an
	// unrecognised value is worth showing in diagnostics rather than silently
	// treating as the default.
	CaseMappingRaw string

	// ChanTypes lists the characters that begin a channel name. Default "#&".
	ChanTypes string
	// StatusMsg lists the prefixes that may precede a channel name to address
	// a subset of its members ("@#chan"). Empty means the server does not
	// support them, which is the default.
	StatusMsg string

	// Prefix is the membership mode/prefix table. Default "(ov)@+".
	Prefix PrefixMap
	// ChanModes is the four-class mode table. Defaults to beI,k,l,imnpst.
	ChanModes ChanModes

	// Modes is the maximum number of parameterised mode changes one MODE
	// command may carry. Default DefaultModes; Unlimited when the server said
	// there is no limit.
	Modes int

	// NickLen, ChannelLen and TopicLen are the advertised length limits.
	// TopicLen is 0 when unadvertised, meaning "unknown, do not enforce".
	NickLen    int
	ChannelLen int
	TopicLen   int

	// MonitorLimit is the MONITOR list size: 0 when MONITOR is unsupported,
	// Unlimited when supported without a bound.
	MonitorLimit int
	// ChatHistoryLimit is the largest message count CHATHISTORY will return,
	// 0 when the server does not advertise chathistory.
	ChatHistoryLimit int

	// Excepts and Invex are the ban-exception and invite-exception mode
	// letters, advertised separately from CHANMODES by some servers. 0 when
	// unsupported. ModeClass consults them so "+e mask" still consumes its
	// parameter on a server that advertises EXCEPTS but no CHANMODES.
	Excepts byte
	Invex   byte

	// Tokens is every token as advertised, unescaped, keyed by uppercase name.
	// A valueless token maps to "". It is kept whole so a later milestone can
	// read a token this struct does not model yet without another parse pass.
	Tokens map[string]string
}

// DefaultProps returns the properties to assume before (and in the absence of)
// any RPL_ISUPPORT. Everything a server can advertise has an entry here, so no
// code path ever has to special-case "we have not seen 005 yet".
func DefaultProps() *ServerProps {
	prefix, err := ParsePrefix(DefaultPrefixSpec)
	if err != nil {
		panic("proto: DefaultPrefixSpec is invalid: " + err.Error()) // unreachable: constant input
	}
	return &ServerProps{
		CaseMapping:    CaseRFC1459,
		CaseMappingRaw: CaseRFC1459.String(),
		ChanTypes:      DefaultChanTypes,
		Prefix:         prefix,
		ChanModes:      ChanModes{A: DefaultChanModesA, B: DefaultChanModesB, C: DefaultChanModesC, D: DefaultChanModesD},
		Modes:          DefaultModes,
		NickLen:        DefaultNickLen,
		ChannelLen:     DefaultChannelLen,
		Tokens:         map[string]string{},
	}
}

// Clone returns a deep copy, safe to hand to another goroutine.
func (p *ServerProps) Clone() *ServerProps {
	if p == nil {
		return DefaultProps()
	}
	c := *p
	c.Tokens = make(map[string]string, len(p.Tokens))
	for k, v := range p.Tokens {
		c.Tokens[k] = v
	}
	return &c
}

// Fold returns the casefolded form of a nick or channel name under this
// network's casemapping. Every name-keyed map goes through here.
func (p *ServerProps) Fold(s string) string {
	if p == nil {
		return CaseRFC1459.Fold(s)
	}
	return p.CaseMapping.Fold(s)
}

// Equal reports whether two names are the same name on this network.
func (p *ServerProps) Equal(a, b string) bool {
	if p == nil {
		return CaseRFC1459.Equal(a, b)
	}
	return p.CaseMapping.Equal(a, b)
}

// BufferID builds the canonical id for a buffer named name on network n. This
// is the one helper every layer uses; nothing else is allowed to construct a
// BufferID from a name, because nothing else knows the casemapping.
func (p *ServerProps) BufferID(n model.NetworkID, name string) model.BufferID {
	return model.BufferID{Network: n, Folded: p.Fold(name)}
}

// IsChannel reports whether name begins with one of this network's CHANTYPES
// characters. It replaces every hardcoded strings.HasPrefix(name, "#").
func (p *ServerProps) IsChannel(name string) bool {
	if name == "" {
		return false
	}
	types := DefaultChanTypes
	if p != nil {
		types = p.ChanTypes
	}
	return strings.IndexByte(types, name[0]) >= 0
}

// BufferKind classifies a raw target name: a channel, or a query with a nick.
// The caller decides separately whether a target is the network status buffer.
func (p *ServerProps) BufferKind(name string) model.BufferKind {
	if p.IsChannel(name) {
		return model.BufferChannel
	}
	return model.BufferQuery
}

// SplitStatusMsg separates a STATUSMSG prefix from a target: on a network with
// STATUSMSG=@+ and CHANTYPES=#, SplitStatusMsg("@#chan") is ("@", "#chan").
// A target that is not a prefixed channel comes back unchanged with an empty
// prefix - including a plain nick that happens to start with a prefix
// character, since the remainder must still be a channel name.
func (p *ServerProps) SplitStatusMsg(target string) (prefix, name string) {
	if p == nil || p.StatusMsg == "" || target == "" {
		return "", target
	}
	// Consume prefix characters one at a time and stop at the shortest split
	// whose remainder is a channel name. The one-at-a-time check matters when a
	// character is both a STATUSMSG prefix and a CHANTYPES character - '&' is
	// both on Ergo - where consuming greedily would eat the channel's own
	// leading '&' and then fail to recognise what was left.
	for i := 0; i < len(target) && strings.IndexByte(p.StatusMsg, target[i]) >= 0; {
		i++
		if p.IsChannel(target[i:]) {
			return target[:i], target[i:]
		}
	}
	return "", target
}

// ParseNamesEntry turns one RPL_NAMREPLY entry into a Member. It handles the
// three shapes a server can send, driven by PREFIX rather than by guesswork:
//
//	nick                     plain
//	@nick                    one prefix
//	~&@nick                   several, with multi-prefix
//	@nick!user@host          with userhost-in-names
//
// Prefixes come back sorted by rank so two equal sets compare equal, and Folded
// is set with this network's casemapping - the members map is keyed on it, not
// on Nick, which is the v1 desync where a server-cased NICK left a ghost entry.
// An entry that is empty or all prefix characters yields a zero Member.
func (p *ServerProps) ParseNamesEntry(entry string) model.Member {
	prefix := PrefixMap{Modes: "ov", Prefixes: "@+"}
	if p != nil {
		prefix = p.Prefix
	}
	prefixes, rest := prefix.Split(entry)
	nick := rest
	if i := strings.IndexByte(rest, '!'); i >= 0 {
		nick = rest[:i]
	}
	if nick == "" {
		return model.Member{}
	}
	return model.Member{
		Nick:     nick,
		Folded:   p.Fold(nick),
		Prefixes: prefix.Sort(prefixes),
	}
}

// MaxModesUnbounded is what MaxModesPerCommand reports when the server said it
// has no limit: large enough that no caller ever chunks, small enough to stay a
// sane slice capacity.
const MaxModesUnbounded = 1 << 16

// MaxModesPerCommand returns how many parameterised mode changes to put in one
// outgoing MODE. It never returns less than 1.
func (p *ServerProps) MaxModesPerCommand() int {
	switch {
	case p == nil:
		return DefaultModes
	case p.Modes == Unlimited:
		return MaxModesUnbounded
	case p.Modes <= 0:
		return DefaultModes
	default:
		return p.Modes
	}
}

// Token returns a raw ISUPPORT token's value and whether the server advertised
// it at all. A valueless token reports ("", true).
func (p *ServerProps) Token(name string) (string, bool) {
	if p == nil {
		return "", false
	}
	v, ok := p.Tokens[strings.ToUpper(name)]
	return v, ok
}

// ISUPPORTTokens extracts the token list from a raw RPL_ISUPPORT parameter
// list. The first parameter is the client's own nick and the last is
// human-readable text ("are supported by this server"); neither is a token.
// Real tokens never contain a space, which is how the trailing text is
// recognised even from a server that omits it.
func ISUPPORTTokens(params []string) []string {
	if len(params) < 2 {
		return nil
	}
	toks := params[1:]
	if last := toks[len(toks)-1]; strings.Contains(last, " ") || last == "" {
		toks = toks[:len(toks)-1]
	}
	if len(toks) == 0 {
		return nil
	}
	return toks
}

// Apply folds a batch of ISUPPORT tokens into p. Tokens accumulate across the
// several 005 lines a server sends; a token prefixed with '-' resets that
// property to its default.
//
// Apply is best-effort by design: a malformed token must never cost us the
// twenty valid ones beside it. Every token that fails to parse is applied as
// far as it can be, recorded in the returned slice, and skipped. A nil return
// means every token parsed cleanly.
func (p *ServerProps) Apply(tokens []string) []error {
	var errs []error
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "-") {
			p.reset(strings.ToUpper(strings.TrimPrefix(tok, "-")))
			continue
		}
		name, value, hasValue := strings.Cut(tok, "=")
		name = strings.ToUpper(name)
		if hasValue {
			value = unescapeISUPPORT(value)
		}
		p.Tokens[name] = value
		if err := p.set(name, value, hasValue); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// set applies one recognised token. Unrecognised tokens are kept in Tokens and
// are not an error: servers advertise plenty we do not model.
func (p *ServerProps) set(name, value string, hasValue bool) error {
	num := func(dflt int) (int, error) {
		if !hasValue || value == "" {
			return dflt, nil
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return dflt, fmt.Errorf("proto: ISUPPORT %s=%q: %w", name, value, err)
		}
		return n, nil
	}

	switch name {
	case "NETWORK":
		p.Network = value

	case "CASEMAPPING":
		cm, ok := ParseCaseMapping(value)
		p.CaseMapping, p.CaseMappingRaw = cm, value
		if !ok {
			return fmt.Errorf("proto: ISUPPORT CASEMAPPING=%q is unknown, using %s", value, cm)
		}

	case "CHANTYPES":
		// An explicitly empty CHANTYPES means "no channels", which is legal.
		p.ChanTypes = value

	case "STATUSMSG":
		p.StatusMsg = value

	case "PREFIX":
		pm, err := ParsePrefix(value)
		if err != nil {
			return err
		}
		p.Prefix = pm

	case "CHANMODES":
		cm, err := ParseChanModes(value)
		if err != nil {
			return err
		}
		p.ChanModes = cm

	case "EXCEPTS":
		p.Excepts = firstByteOr(value, 'e')

	case "INVEX":
		p.Invex = firstByteOr(value, 'I')

	case "MODES":
		// "MODES" with no value means no limit.
		if !hasValue || value == "" {
			p.Modes = Unlimited
			return nil
		}
		n, err := num(DefaultModes)
		if err != nil {
			return err
		}
		p.Modes = n

	case "NICKLEN", "MAXNICKLEN":
		n, err := num(p.NickLen)
		if err != nil {
			return err
		}
		p.NickLen = n

	case "CHANNELLEN":
		n, err := num(p.ChannelLen)
		if err != nil {
			return err
		}
		p.ChannelLen = n

	case "TOPICLEN":
		n, err := num(p.TopicLen)
		if err != nil {
			return err
		}
		p.TopicLen = n

	case "MONITOR":
		if !hasValue || value == "" {
			p.MonitorLimit = Unlimited
			return nil
		}
		n, err := num(Unlimited)
		if err != nil {
			return err
		}
		p.MonitorLimit = n

	case "CHATHISTORY", "DRAFT/CHATHISTORY":
		n, err := num(0)
		if err != nil {
			return err
		}
		p.ChatHistoryLimit = n
	}
	return nil
}

// reset returns one property to its default, for the "-TOKEN" negation form.
func (p *ServerProps) reset(name string) {
	delete(p.Tokens, name)
	d := DefaultProps()
	switch name {
	case "NETWORK":
		p.Network = ""
	case "CASEMAPPING":
		p.CaseMapping, p.CaseMappingRaw = d.CaseMapping, d.CaseMappingRaw
	case "CHANTYPES":
		p.ChanTypes = d.ChanTypes
	case "STATUSMSG":
		p.StatusMsg = ""
	case "PREFIX":
		p.Prefix = d.Prefix
	case "CHANMODES":
		p.ChanModes = d.ChanModes
	case "EXCEPTS":
		p.Excepts = 0
	case "INVEX":
		p.Invex = 0
	case "MODES":
		p.Modes = d.Modes
	case "NICKLEN", "MAXNICKLEN":
		p.NickLen = d.NickLen
	case "CHANNELLEN":
		p.ChannelLen = d.ChannelLen
	case "TOPICLEN":
		p.TopicLen = 0
	case "MONITOR":
		p.MonitorLimit = 0
	case "CHATHISTORY", "DRAFT/CHATHISTORY":
		p.ChatHistoryLimit = 0
	}
}

func firstByteOr(v string, dflt byte) byte {
	if v == "" {
		return dflt
	}
	return v[0]
}

// unescapeISUPPORT expands the \xHH escapes ISUPPORT values use to carry bytes
// that cannot appear raw in a parameter (notably \x20 for space). An
// unrecognised or truncated escape is left verbatim rather than dropped.
func unescapeISUPPORT(v string) string {
	i := strings.IndexByte(v, '\\')
	if i < 0 {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	b.WriteString(v[:i])
	for i < len(v) {
		if v[i] == '\\' && i+3 < len(v) && (v[i+1] == 'x' || v[i+1] == 'X') {
			if n, err := strconv.ParseUint(v[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 4
				continue
			}
		}
		b.WriteByte(v[i])
		i++
	}
	return b.String()
}
