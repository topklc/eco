package command

import "strings"

// Run performs a parsed line against env.
//
// The guards are here rather than in each command because they are the same
// three questions every time: is there a connection, is this a channel, and did
// the user give enough arguments. A command's own Run is reached only when all
// three are answered, which is what makes the table's entries as small as they
// are.
//
// Nothing is passed through to the server by accident. v1's default branch
// upper-cased whatever was typed and wrote it to the wire, which turned a typo
// into a protocol error from the server (or, on a bad day, into a command the
// user did not mean); an unknown verb now says so and names /raw as the way to
// do it on purpose.
func Run(in Input, e Env) {
	switch in.Kind {
	case KindEmpty:
		return
	case KindMessage:
		if s := e.Session(); s != nil {
			s.Send(in.Ctx.Buffer, in.Text)
		}
		return
	case KindUnknown:
		e.UI().ShowHint(in.Hint())
		return
	}
	c := in.Cmd
	if c == nil {
		return
	}
	// No connection: silently do nothing, as v1 did. The window has no chrome
	// for saying it here, and every path that CAN say it (sending a message,
	// joining) says it for itself.
	if c.NeedsConn && (e.Wire() == nil || e.Session() == nil) {
		return
	}
	if c.NeedsChannel && !in.Ctx.IsChannel() {
		e.UI().ShowHint("/" + c.Name + " only works in a channel.")
		return
	}
	if !in.Args.satisfies(c.Args) {
		e.UI().ShowHint(c.Usage())
		return
	}
	c.Run(in, e)
}

// Hint is what the user is told about a verb the table does not have: the
// closest command it does have, and the explicit way to send the line anyway.
// It is only meaningful for KindUnknown.
func (in Input) Hint() string {
	if in.Verb == "" {
		return "That is not a command. Type /help to see the list, and start a " +
			"message with // if you want it to begin with a slash."
	}
	var b strings.Builder
	b.WriteString("Unknown command /")
	b.WriteString(in.Verb)
	b.WriteString(".")
	if in.Suggest != "" {
		b.WriteString(" Did you mean /")
		b.WriteString(in.Suggest)
		b.WriteString("?")
	}
	b.WriteString(" Type /help for the list, or send it to the server anyway with /raw ")
	b.WriteString(in.Passthrough())
	b.WriteString(".")
	return b.String()
}

// Passthrough is the line v1 would have written to the wire for an unknown
// command: the verb upper-cased, then the arguments. It is what the intercept
// offers to /raw, so taking the offer sends exactly what used to be sent.
func (in Input) Passthrough() string {
	return strings.TrimSpace(strings.ToUpper(in.Verb) + " " + in.Args.Raw)
}
