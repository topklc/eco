package proto

import (
	"errors"
	"fmt"
	"strings"
)

// ModeClass says how a mode letter behaves, which is the only thing that
// decides whether it consumes a parameter. The classes are ISUPPORT
// CHANMODES's A/B/C/D plus membership modes, which come from PREFIX and always
// take a nick.
type ModeClass uint8

const (
	// ModeClassFlag is CHANMODES group D: never takes a parameter (+i, +m).
	// It is also what an unknown mode letter is assumed to be, because
	// guessing "no parameter" desynchronises at most this one mode, while
	// guessing "parameter" eats the argument of the next mode as well.
	ModeClassFlag ModeClass = iota
	// ModeClassList is CHANMODES group A: always takes a parameter, and the
	// parameter is added to or removed from a list (+b mask, -b mask).
	ModeClassList
	// ModeClassKey is CHANMODES group B: always takes a parameter, setting or
	// clearing a single value (+k key, -k key).
	ModeClassKey
	// ModeClassLimit is CHANMODES group C: takes a parameter when set, none
	// when cleared (+l 50, -l).
	ModeClassLimit
	// ModeClassMember is a PREFIX mode: always takes a nick, and grants or
	// removes that nick's prefix character (+o nick).
	ModeClassMember
)

// String returns the debug name of the class.
func (c ModeClass) String() string {
	switch c {
	case ModeClassFlag:
		return "flag"
	case ModeClassList:
		return "list"
	case ModeClassKey:
		return "key"
	case ModeClassLimit:
		return "limit"
	case ModeClassMember:
		return "member"
	}
	return "unknown"
}

// ModeChange is one applied mode letter from a MODE delta.
type ModeChange struct {
	// Add is true for a '+' change, false for '-'.
	Add bool
	// Mode is the mode letter.
	Mode byte
	// Class is how the letter behaves on this network.
	Class ModeClass
	// Param is the letter's parameter, "" when it takes none. For
	// ModeClassMember it is the nick, raw and unfolded — the caller folds it.
	Param string
	// Prefix is the prefix character a ModeClassMember change grants or
	// removes ('@' for +o), and 0 for every other class.
	Prefix byte
	// Known is false when the letter appeared in neither PREFIX nor CHANMODES
	// and was therefore assumed to be a flag. The store can still apply it; the
	// field exists so a diagnostic can say the server told us nothing about it.
	Known bool
}

// String renders the change for logs and test failures.
func (c ModeChange) String() string {
	sign := "-"
	if c.Add {
		sign = "+"
	}
	if c.Param == "" {
		return fmt.Sprintf("%s%c(%s)", sign, c.Mode, c.Class)
	}
	return fmt.Sprintf("%s%c(%s) %s", sign, c.Mode, c.Class, c.Param)
}

// ErrEmptyModeString is returned when a MODE delta carries no mode string.
var ErrEmptyModeString = errors.New("proto: empty mode string")

// MissingParamError reports a mode letter whose parameter the server did not
// send. The change is still returned (a "-k" with no key still clears the key);
// the error exists so a malformed server is visible rather than silently
// reinterpreted.
type MissingParamError struct {
	Mode byte
	Add  bool
}

// Error implements error.
func (e MissingParamError) Error() string {
	sign := "-"
	if e.Add {
		sign = "+"
	}
	return fmt.Sprintf("proto: mode %s%c is missing its parameter", sign, e.Mode)
}

// ModeClass classifies a channel mode letter on this network. PREFIX wins over
// CHANMODES, because RFC 1459 nominally puts membership modes in group B and
// some servers still list them there; treating +o as a key would be a
// spectacular way to lose a member list.
func (p *ServerProps) ModeClass(m byte) (ModeClass, bool) {
	cm := ChanModes{A: DefaultChanModesA, B: DefaultChanModesB, C: DefaultChanModesC, D: DefaultChanModesD}
	prefix := PrefixMap{Modes: "ov", Prefixes: "@+"}
	var excepts, invex byte
	if p != nil {
		cm, prefix, excepts, invex = p.ChanModes, p.Prefix, p.Excepts, p.Invex
	}
	switch {
	case prefix.IsMemberMode(m):
		return ModeClassMember, true
	case strings.IndexByte(cm.A, m) >= 0:
		return ModeClassList, true
	case strings.IndexByte(cm.B, m) >= 0:
		return ModeClassKey, true
	case strings.IndexByte(cm.C, m) >= 0:
		return ModeClassLimit, true
	case strings.IndexByte(cm.D, m) >= 0:
		return ModeClassFlag, true
	case m != 0 && (m == excepts || m == invex):
		// EXCEPTS/INVEX advertised without a CHANMODES that lists them.
		return ModeClassList, true
	}
	return ModeClassFlag, false
}

// ParseModeDelta turns a MODE delta into typed changes, driven entirely by the
// network's CHANMODES and PREFIX. It supersedes v1's hardcoded switch over mode
// letters (Buffer.applyModeDelta), which consumed parameters for a fixed list
// and therefore desynchronised on any server with different modes.
//
// params[0] is the mode string ("+ik-m"); params[1:] are its parameters in
// order. For a raw MODE message that means passing msg.Params[1:] — the target
// is not part of the delta. A mode string with no leading sign is treated as an
// addition, as servers send in RPL_CHANNELMODEIS.
//
// Parsing is best-effort: a mode whose parameter is missing still produces a
// change and records a MissingParamError, and parsing continues, because the
// paramless letters after it are still meaningful. The returned error joins
// every problem found; the changes are always usable.
func ParseModeDelta(p *ServerProps, params []string) ([]ModeChange, error) {
	if len(params) == 0 || params[0] == "" {
		return nil, ErrEmptyModeString
	}
	modes, args := params[0], params[1:]
	next := 0
	takeArg := func() (string, bool) {
		if next >= len(args) {
			return "", false
		}
		a := args[next]
		next++
		return a, true
	}

	changes := make([]ModeChange, 0, len(modes))
	var errs []error
	add := true
	for i := 0; i < len(modes); i++ {
		m := modes[i]
		switch m {
		case '+':
			add = true
			continue
		case '-':
			add = false
			continue
		}

		class, known := p.ModeClass(m)
		ch := ModeChange{Add: add, Mode: m, Class: class, Known: known}

		needsParam := false
		switch class {
		case ModeClassList, ModeClassKey, ModeClassMember:
			needsParam = true
		case ModeClassLimit:
			// Group C takes a parameter only when being set.
			needsParam = add
		}
		if needsParam {
			arg, ok := takeArg()
			if !ok {
				errs = append(errs, MissingParamError{Mode: m, Add: add})
			}
			ch.Param = arg
		}
		if class == ModeClassMember {
			ch.Prefix = p.prefixOf(m)
		}
		changes = append(changes, ch)
	}
	return changes, errors.Join(errs...)
}

// prefixOf is PrefixMap.PrefixOf with the nil-props default applied.
func (p *ServerProps) prefixOf(mode byte) byte {
	if p == nil {
		return PrefixMap{Modes: "ov", Prefixes: "@+"}.PrefixOf(mode)
	}
	return p.Prefix.PrefixOf(mode)
}

// ParseUserModeDelta turns a MODE delta addressed to a nick into typed changes.
// User modes never take parameters in any server eco supports, so every change
// comes back as ModeClassFlag; the separate entry point exists so a caller
// cannot accidentally classify user modes against CHANMODES.
func ParseUserModeDelta(params []string) ([]ModeChange, error) {
	if len(params) == 0 || params[0] == "" {
		return nil, ErrEmptyModeString
	}
	changes := make([]ModeChange, 0, len(params[0]))
	add := true
	for i := 0; i < len(params[0]); i++ {
		switch m := params[0][i]; m {
		case '+':
			add = true
		case '-':
			add = false
		default:
			changes = append(changes, ModeChange{Add: add, Mode: m, Class: ModeClassFlag, Known: true})
		}
	}
	return changes, nil
}

// ApplyFlags folds the flag-class changes of a delta into a mode string,
// keeping it sorted so two equal mode sets compare equal. Parameterised classes
// are ignored: their state lives in the buffer's key/limit/lists, not in the
// flag string.
func ApplyFlags(modes string, changes []ModeChange) string {
	set := map[byte]bool{}
	for i := 0; i < len(modes); i++ {
		set[modes[i]] = true
	}
	for _, c := range changes {
		switch c.Class {
		case ModeClassFlag, ModeClassKey, ModeClassLimit:
			// +k and +l are visible flags too ("this channel has a key").
			if c.Add {
				set[c.Mode] = true
			} else {
				delete(set, c.Mode)
			}
		}
	}
	out := make([]byte, 0, len(set))
	for i := 0; i < 256; i++ {
		if set[byte(i)] {
			out = append(out, byte(i))
		}
	}
	return string(out)
}
