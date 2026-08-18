package state

import (
	"sort"
	"strings"

	"github.com/toprakkilic/eco/internal/model"
)

// ignoreRule is one parsed ignore mask. Rules are per network (blueprint MUST
// 12): the same nick on two networks is two different people, which v1's single
// global lowercase set could not express.
type ignoreRule struct {
	// Pattern is the mask as the user typed it, kept for display and removal.
	Pattern string
	// nick, user and host are the three folded/lowercased glob fields.
	nick, user, host string
}

// parseIgnore turns a user-typed mask into a rule. A bare word is a nick mask:
// "spammer" means "spammer!*@*". Missing fields default to "*".
func parseIgnore(n *network, pattern string) (ignoreRule, bool) {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return ignoreRule{}, false
	}
	nick, rest := p, ""
	if i := strings.IndexByte(p, '!'); i >= 0 {
		nick, rest = p[:i], p[i+1:]
	} else if i := strings.IndexByte(p, '@'); i >= 0 {
		nick, rest = p[:i], "*@"+p[i+1:]
	}
	user, host := "*", "*"
	if rest != "" {
		user = rest
		if i := strings.IndexByte(rest, '@'); i >= 0 {
			user, host = rest[:i], rest[i+1:]
		}
	}
	r := ignoreRule{
		Pattern: p,
		nick:    n.props.Fold(emptyToStar(nick)),
		user:    strings.ToLower(emptyToStar(user)),
		host:    strings.ToLower(emptyToStar(host)),
	}
	return r, true
}

func emptyToStar(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

// matches reports whether a sender is covered by the rule. The nick folds with
// the network's casemapping; user and host fold as ASCII, which is what
// hostmasks are.
func (r ignoreRule) matches(n *network, s model.Sender) bool {
	return globMatch(r.nick, n.props.Fold(s.Nick)) &&
		globMatch(r.user, strings.ToLower(s.User)) &&
		globMatch(r.host, strings.ToLower(s.Host))
}

// ignored reports whether a sender is on this network's ignore list.
func (n *network) ignored(s model.Sender) bool {
	if s.Nick == "" || len(n.ignores) == 0 {
		return false
	}
	for _, r := range n.ignores {
		if r.matches(n, s) {
			return true
		}
	}
	return false
}

// globMatch reports whether s matches an IRC-style glob: '*' spans anything,
// '?' spans one byte. It is iterative, so a pathological pattern cannot blow
// the stack the way a naive recursive matcher can.
func globMatch(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	var (
		p, i        int
		star        = -1
		starMatched int
	)
	for i < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[i]):
			p++
			i++
		case p < len(pattern) && pattern[p] == '*':
			star, starMatched = p, i
			p++
		case star >= 0:
			p = star + 1
			starMatched++
			i = starMatched
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// AddIgnore adds a hostmask ignore to a network. It reports false when the mask
// is empty or already present. Ignored senders' messages never reach a buffer,
// so nothing downstream has to know the list exists.
func (s *Store) AddIgnore(id model.NetworkID, pattern string) bool {
	n := s.network(id)
	r, ok := parseIgnore(n, pattern)
	if !ok {
		return false
	}
	for _, have := range n.ignores {
		if have.nick == r.nick && have.user == r.user && have.host == r.host {
			return false
		}
	}
	n.ignores = append(n.ignores, r)
	return true
}

// RemoveIgnore drops an ignore mask, matching either the exact pattern the user
// typed or its normalised form. It reports whether anything was removed.
func (s *Store) RemoveIgnore(id model.NetworkID, pattern string) bool {
	n, ok := s.nets[id]
	if !ok {
		return false
	}
	want, valid := parseIgnore(n, pattern)
	if !valid {
		return false
	}
	for i, have := range n.ignores {
		if have.Pattern == pattern ||
			(have.nick == want.nick && have.user == want.user && have.host == want.host) {
			n.ignores = append(n.ignores[:i], n.ignores[i+1:]...)
			return true
		}
	}
	return false
}

// Ignores lists a network's ignore masks, sorted.
func (s *Store) Ignores(id model.NetworkID) []string {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(n.ignores))
	for _, r := range n.ignores {
		out = append(out, r.Pattern)
	}
	sort.Strings(out)
	return out
}

// Ignored reports whether a sender is ignored on a network.
func (s *Store) Ignored(id model.NetworkID, sender model.Sender) bool {
	n, ok := s.nets[id]
	return ok && n.ignored(sender)
}
