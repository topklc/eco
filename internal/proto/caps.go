package proto

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// DefaultCapRequest returns the capabilities eco asks for, in a fresh slice so
// callers cannot mutate a shared list. Requesting an unsupported capability is
// harmless, so the strategy is to ask for everything useful — including Ergo's
// drafts — and let the negotiated set decide what the client actually does.
func DefaultCapRequest() []string {
	return []string{
		"account-notify",
		"account-tag",
		"away-notify",
		"batch",
		"cap-notify",
		"chghost",
		"echo-message",
		"extended-join",
		"extended-monitor",
		"invite-notify",
		"labeled-response",
		"message-tags",
		"multi-prefix",
		"sasl",
		"server-time",
		"setname",
		"standard-replies",
		"userhost-in-names",
		// drafts (Ergo supports most of these)
		"draft/chathistory",
		"draft/event-playback",
		"draft/read-marker",
		"draft/message-redaction",
		"draft/multiline",
		"draft/channel-rename",
		"draft/relaymsg",
		"draft/languages",
		"draft/persistence",
		"draft/pre-away",
		"draft/account-registration",
		"draft/typing",
	}
}

// CapSet is the capability negotiation state for one connection: what we want,
// what the server advertised, and what is actually enabled. It is pure state —
// it sends nothing and knows nothing about a socket; the irc layer feeds it CAP
// lines and writes whatever ToRequest hands back.
//
// The zero CapSet is not usable; build one with NewCapSet.
type CapSet struct {
	wanted    map[string]bool
	available map[string]string
	enabled   map[string]string
	complete  bool
}

// NewCapSet returns a CapSet that wants the given capabilities.
func NewCapSet(wanted []string) *CapSet {
	c := &CapSet{
		wanted:    make(map[string]bool, len(wanted)),
		available: map[string]string{},
		enabled:   map[string]string{},
	}
	for _, w := range wanted {
		c.wanted[w] = true
	}
	return c
}

// Reset clears everything the server told us, keeping the wanted set. Call it
// when a new connection starts: capabilities do not survive a reconnect.
func (c *CapSet) Reset() {
	c.available = map[string]string{}
	c.enabled = map[string]string{}
	c.complete = false
}

// Clone returns a deep copy.
func (c *CapSet) Clone() *CapSet {
	n := &CapSet{
		wanted:    make(map[string]bool, len(c.wanted)),
		available: make(map[string]string, len(c.available)),
		enabled:   make(map[string]string, len(c.enabled)),
		complete:  c.complete,
	}
	for k, v := range c.wanted {
		n.wanted[k] = v
	}
	for k, v := range c.available {
		n.available[k] = v
	}
	for k, v := range c.enabled {
		n.enabled[k] = v
	}
	return n
}

// ParseCapTokens splits a CAP token list ("multi-prefix sasl=PLAIN,EXTERNAL")
// into name/value pairs, preserving order. A token with no '=' has an empty
// value, which is distinct from a token that is absent entirely.
func ParseCapTokens(list string) []model.Cap {
	fields := strings.Fields(list)
	out := make([]model.Cap, 0, len(fields))
	for _, f := range fields {
		name, value, _ := strings.Cut(f, "=")
		out = append(out, model.Cap{Name: name, Value: value})
	}
	return out
}

// LS records a CAP LS advertisement. more is true for a continuation line
// ("CAP * LS * :..."), meaning the advertised set is not complete yet. It
// returns the capabilities that should be requested now: wanted, advertised,
// and not already enabled — empty until the final line, because requesting
// before the list is complete loses whatever the next line would have offered.
func (c *CapSet) LS(caps []model.Cap, more bool) []string {
	for _, cp := range caps {
		c.available[cp.Name] = cp.Value
	}
	if more {
		c.complete = false
		return nil
	}
	c.complete = true
	return c.ToRequest()
}

// New records a CAP NEW advertisement and returns the newly requestable
// capabilities.
func (c *CapSet) New(caps []model.Cap) []string {
	var out []string
	for _, cp := range caps {
		c.available[cp.Name] = cp.Value
		if c.wanted[cp.Name] {
			if _, on := c.enabled[cp.Name]; !on {
				out = append(out, cp.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Del records a CAP DEL and returns the names that were enabled and are now
// gone, so the caller can stand down whatever depended on them.
func (c *CapSet) Del(names []string) []string {
	var lost []string
	for _, n := range names {
		delete(c.available, n)
		if _, on := c.enabled[n]; on {
			delete(c.enabled, n)
			lost = append(lost, n)
		}
	}
	sort.Strings(lost)
	return lost
}

// Ack records a CAP ACK. A name prefixed with '-' is a capability the server
// confirms it has disabled.
func (c *CapSet) Ack(names []string) {
	for _, n := range names {
		if off, ok := strings.CutPrefix(n, "-"); ok {
			delete(c.enabled, off)
			continue
		}
		c.enabled[n] = c.available[n]
	}
}

// Nak records a CAP NAK: the server refused these, so they are not enabled and
// must not be asked for again on this connection.
func (c *CapSet) Nak(names []string) {
	for _, n := range names {
		delete(c.enabled, n)
		delete(c.available, n)
	}
}

// ToRequest returns the capabilities to ask for: wanted ∩ advertised, minus
// those already enabled, sorted so a transcript test is deterministic.
func (c *CapSet) ToRequest() []string {
	out := make([]string, 0, len(c.wanted))
	for name := range c.available {
		if c.wanted[name] {
			if _, on := c.enabled[name]; !on {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Complete reports whether the CAP LS advertisement finished.
func (c *CapSet) Complete() bool { return c.complete }

// Enabled reports whether a capability is negotiated and in force.
func (c *CapSet) Enabled(name string) bool {
	_, ok := c.enabled[name]
	return ok
}

// Value returns an enabled capability's value ("" for a valueless one) and
// whether it is enabled at all.
func (c *CapSet) Value(name string) (string, bool) {
	v, ok := c.enabled[name]
	return v, ok
}

// Advertised returns a capability's advertised value and whether the server
// offered it. A capability can be advertised without being enabled (we did not
// want it, or it was NAKed).
func (c *CapSet) Advertised(name string) (string, bool) {
	v, ok := c.available[name]
	return v, ok
}

// EnabledNames returns the negotiated set, sorted.
func (c *CapSet) EnabledNames() []string { return sortedKeys(c.enabled) }

// AvailableNames returns the advertised set, sorted.
func (c *CapSet) AvailableNames() []string { return sortedKeys(c.available) }

// Wanted returns the requested set, sorted.
func (c *CapSet) Wanted() []string {
	out := make([]string, 0, len(c.wanted))
	for k := range c.wanted {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- capability values ------------------------------------------------------

// CapValues splits a capability value into its comma-separated key=value parts:
// "max-bytes=4096,max-lines=24" becomes {"max-bytes": "4096", "max-lines": "24"}.
// A part with no '=' maps to "".
func CapValues(value string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		out[k] = v
	}
	return out
}

// DefaultMultilineMaxLines is the line count to assume when the server enables
// draft/multiline without advertising a max-lines value. It matches Ergo's
// default; the point of having a constant is that the fallback is one
// documented number rather than a magic literal at each send site.
const DefaultMultilineMaxLines = 24

// MultilineLimits is the parsed draft/multiline capability value. A zero field
// means the server advertised no limit for it.
type MultilineLimits struct {
	MaxBytes int
	MaxLines int
}

// ParseMultiline parses a draft/multiline capability value.
func ParseMultiline(value string) MultilineLimits {
	kv := CapValues(value)
	lim := MultilineLimits{}
	if n, err := strconv.Atoi(kv["max-bytes"]); err == nil {
		lim.MaxBytes = n
	}
	if n, err := strconv.Atoi(kv["max-lines"]); err == nil {
		lim.MaxLines = n
	}
	return lim
}

// Lines returns the effective maximum number of lines per multiline message,
// substituting DefaultMultilineMaxLines when the server advertised none.
func (m MultilineLimits) Lines() int {
	if m.MaxLines > 0 {
		return m.MaxLines
	}
	return DefaultMultilineMaxLines
}

// Multiline returns the negotiated draft/multiline limits and whether the
// capability is enabled at all.
func (c *CapSet) Multiline() (MultilineLimits, bool) {
	v, ok := c.Value("draft/multiline")
	if !ok {
		return MultilineLimits{}, false
	}
	return ParseMultiline(v), true
}

// ParseSASLMechanisms parses a sasl capability value ("PLAIN,EXTERNAL") into an
// uppercased mechanism list. An empty value means the server did not say, in
// which case the client may try any mechanism.
func ParseSASLMechanisms(value string) []string {
	var out []string
	for _, m := range strings.Split(value, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, strings.ToUpper(m))
		}
	}
	return out
}

// SASLMechanisms returns the mechanisms the server advertised on the sasl
// capability, and whether sasl is enabled. A nil list with ok true means sasl
// is available but the server named no mechanisms.
func (c *CapSet) SASLMechanisms() ([]string, bool) {
	v, ok := c.Value("sasl")
	if !ok {
		return nil, false
	}
	return ParseSASLMechanisms(v), true
}

// SupportsSASL reports whether mech is acceptable to the server: either it is
// in the advertised list, or the server advertised no list at all.
func (c *CapSet) SupportsSASL(mech string) bool {
	mechs, ok := c.SASLMechanisms()
	if !ok {
		return false
	}
	if len(mechs) == 0 {
		return true
	}
	mech = strings.ToUpper(mech)
	for _, m := range mechs {
		if m == mech {
			return true
		}
	}
	return false
}

// STSPolicy is a parsed sts capability value. The two halves are mutually
// exclusive on the wire: a plaintext connection is told which port to reconnect
// to, a secure one is told how long to remember that this host is TLS-only.
type STSPolicy struct {
	// Port is the TLS port to upgrade to; valid only when HasPort.
	Port int
	// Duration is how long the policy is valid; valid only when HasDuration.
	// A HasDuration policy with Duration == 0 is a revocation.
	Duration time.Duration
	// Preload marks a host on a preload list.
	Preload bool

	HasPort     bool
	HasDuration bool
}

// Revocation reports whether the policy withdraws a previously stored one
// (duration=0), which must not be confused with "no duration advertised".
func (s STSPolicy) Revocation() bool { return s.HasDuration && s.Duration == 0 }

// ParseSTSPolicy parses an sts capability value such as
// "duration=2592000,preload" or "port=6697". Unparseable numbers are reported;
// the rest of the policy still comes back so a typo in one field does not
// discard the other.
func ParseSTSPolicy(value string) (STSPolicy, error) {
	var (
		p    STSPolicy
		errs []string
	)
	for k, v := range CapValues(value) {
		switch k {
		case "port":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > 65535 {
				errs = append(errs, fmt.Sprintf("bad port %q", v))
				continue
			}
			p.Port, p.HasPort = n, true
		case "duration":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				errs = append(errs, fmt.Sprintf("bad duration %q", v))
				continue
			}
			p.Duration, p.HasDuration = time.Duration(n)*time.Second, true
		case "preload":
			p.Preload = true
		}
	}
	if len(errs) > 0 {
		return p, fmt.Errorf("proto: sts %q: %s", value, strings.Join(errs, "; "))
	}
	return p, nil
}

// STS returns the advertised sts policy and whether one was advertised. It
// reads the *advertised* set, not the enabled one: sts is meaningful even
// though a client never requests it.
func (c *CapSet) STS() (STSPolicy, bool, error) {
	v, ok := c.Advertised("sts")
	if !ok {
		return STSPolicy{}, false, nil
	}
	p, err := ParseSTSPolicy(v)
	return p, true, err
}
