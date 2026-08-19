package command

import "strings"

// Kind is what a line of composer text turned out to be.
type Kind int

const (
	// KindEmpty is a line with nothing in it: nothing happens.
	KindEmpty Kind = iota
	// KindMessage is ordinary text for the current buffer, including a line
	// escaped with a leading "//".
	KindMessage
	// KindCommand is a verb the table claims, with its arguments split.
	KindCommand
	// KindUnknown is a /verb nothing in the table claims. It is NOT sent to the
	// server: see Input.Hint and the intercept in Run.
	KindUnknown
)

// Context is where the text was typed. It is what lets /part, /topic, /kick and
// the rest mean "here" without the table asking the UI anything.
type Context struct {
	// Buffer is the name of the buffer on screen.
	Buffer string
	// Server reports that Buffer is the network log rather than a conversation.
	// The name of that buffer belongs to the UI, so the caller states the fact
	// instead of this package guessing at it.
	Server bool
}

// IsChannel reports whether the buffer on screen is a channel.
func (c Context) IsChannel() bool {
	return !c.Server && strings.HasPrefix(c.Buffer, "#")
}

// Input is one parsed line: what it is, which command it named, and that
// command's arguments split the way its [ArgSpec] asked.
type Input struct {
	Kind Kind
	Ctx  Context
	// Verb is the command as typed, lower-cased and without its slash.
	Verb string
	// Cmd is the table entry Verb resolved to (KindCommand only).
	Cmd *Command
	// Args are the arguments, split per Cmd.Args. For KindUnknown only Raw is
	// filled, because nothing knows the shape of a command that does not exist.
	Args Args
	// Text is the message body (KindMessage only).
	Text string
	// Suggest is the nearest verb the table does know (KindUnknown only), or
	// "" when nothing is close enough to be worth guessing at.
	Suggest string
}

// Args is one command's arguments.
//
// Rest is deliberately NOT trimmed: it is the tail of the line exactly as
// typed, because for /msg, /notice, /kick and friends that tail is a message
// and the spaces in it are the user's. Nothing here processes quotes - a
// quotation mark is a character in a message, and a nick with a space in it is
// not a thing IRC has.
type Args struct {
	// Raw is everything after the verb, with surrounding space removed.
	Raw string
	// Fields are the leading words the spec asked to be split out.
	Fields []string
	// Rest is what followed those words, verbatim.
	Rest string
	// N is how many arguments the user actually supplied, which is what Min
	// and Max are checked against.
	N int
}

// Field returns the i'th split word, or "" when the user did not supply it.
func (a Args) Field(i int) string {
	if i >= 0 && i < len(a.Fields) {
		return a.Fields[i]
	}
	return ""
}

// satisfies reports whether the user supplied enough arguments, and not more
// than the command can use.
func (a Args) satisfies(spec ArgSpec) bool {
	return a.N >= spec.Min && (spec.Max <= 0 || a.N <= spec.Max)
}

// AllWords asks the parser to split every word of the argument into Fields,
// leaving Rest empty. It is what /mode, /op and /register want.
const AllWords = -1

// ArgSpec is the shape of one command's arguments: what to split, how much is
// required, and how to say so when it is missing.
type ArgSpec struct {
	// Usage is the argument shape, shown in help and in the usage error:
	// "nick [message]" makes "Usage: /msg nick [message]".
	Usage string
	// Words is how many leading words to peel off into Fields. Zero leaves the
	// whole argument in Raw (one argument, whatever it contains); AllWords
	// splits all of them.
	Words int
	// Min is how many arguments are required. Zero makes them optional.
	Min int
	// Max is the most this command can use, zero for no limit.
	Max int
}

// Parse classifies one line of composer text against the table.
//
// The rules are v1's, exactly: a line that does not start with "/" is a
// message; a line that starts with "//" is a message too, with one slash eaten
// (that is the escape that lets "//me" reach the channel as text); anything
// else is a command, split on its first space into a lower-cased verb and an
// argument string, and the argument string is then split the way that command's
// spec asks. A verb the table does not have comes back KindUnknown rather than
// being passed to the server.
func Parse(text string, ctx Context) Input {
	in := Input{Ctx: ctx}
	text = strings.TrimRight(text, "\n")
	switch {
	case text == "":
		in.Kind = KindEmpty
		return in
	case !strings.HasPrefix(text, "/"), strings.HasPrefix(text, "//"):
		in.Kind, in.Text = KindMessage, strings.TrimPrefix(text, "/")
		return in
	}

	verb, rest, _ := strings.Cut(text[1:], " ")
	in.Verb = strings.ToLower(verb)
	raw := strings.TrimSpace(rest)

	if in.Cmd = Lookup(in.Verb); in.Cmd == nil {
		in.Kind = KindUnknown
		in.Args = Args{Raw: raw}
		in.Suggest = Suggest(in.Verb)
		return in
	}
	in.Kind = KindCommand
	in.Args = splitArgs(raw, in.Cmd.Args)
	return in
}

// splitArgs cuts the argument string the way a spec asks.
func splitArgs(raw string, spec ArgSpec) Args {
	a := Args{Raw: raw}
	switch {
	case spec.Words == AllWords:
		a.Fields = strings.Fields(raw)
		a.N = len(a.Fields)
	case spec.Words > 0:
		parts := strings.SplitN(raw, " ", spec.Words+1)
		if len(parts) > spec.Words {
			a.Rest = parts[spec.Words]
			parts = parts[:spec.Words]
		}
		for _, p := range parts {
			if p != "" {
				a.Fields = append(a.Fields, p)
			}
		}
		a.N = len(a.Fields)
		if a.Rest != "" {
			a.N++
		}
	default:
		if raw != "" {
			a.N = 1
		}
	}
	return a
}
