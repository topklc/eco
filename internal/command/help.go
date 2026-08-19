package command

import "strings"

// Summary is the one-line list of commands /help writes into the buffer. It is
// generated from the table, which is what makes it impossible for a command to
// exist and not be listed — v1 kept this sentence by hand and it had already
// drifted from the switch it described.
func Summary() string {
	var b strings.Builder
	for _, g := range groups() {
		b.WriteString(g.name)
		b.WriteString(":")
		for _, c := range g.cmds {
			b.WriteString(" /")
			b.WriteString(c.Name)
		}
		b.WriteString(". ")
	}
	b.WriteString("Anything else you type is sent as a message. An unknown " +
		"/command is not sent to the server — /raw does that on purpose.")
	return b.String()
}

// HelpMarkdown is the Commands section of the documentation window: every
// command, how to type it, what it does, and what else it answers to.
func HelpMarkdown() string {
	var b strings.Builder
	for i, g := range groups() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### ")
		b.WriteString(g.name)
		b.WriteString("\n\n")
		for _, c := range g.cmds {
			b.WriteString("- `/")
			b.WriteString(c.Name)
			if c.Args.Usage != "" {
				b.WriteString(" ")
				b.WriteString(c.Args.Usage)
			}
			b.WriteString("` — ")
			b.WriteString(c.Help)
			if len(c.Aliases) > 0 {
				b.WriteString(" (also `/")
				b.WriteString(strings.Join(c.Aliases, "`, `/"))
				b.WriteString("`)")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// explain answers "/help join": one command's own line, or the nearest verb to
// what was actually typed.
func explain(name string) string {
	name, _, _ = strings.Cut(strings.TrimSpace(name), " ")
	name = strings.TrimPrefix(strings.ToLower(name), "/")
	if c := Lookup(name); c != nil {
		return c.Describe()
	}
	if s := Suggest(name); s != "" {
		return "There is no /" + name + ". Did you mean /" + s + "? Type /help for the list."
	}
	return "There is no /" + name + ". Type /help for the list."
}

// group is one heading of the generated help and the commands under it.
type group struct {
	name string
	cmds []*Command
}

// groups walks the table once, in table order, and buckets the visible
// commands by their Group. The order of the headings is the order of the Group
// constants; the order within one is the order of the table.
func groups() []group {
	out := make([]group, 0, len(groupNames))
	for g := range groupNames {
		var cmds []*Command
		for _, c := range table {
			if c.Hidden || int(c.Group) != g {
				continue
			}
			cmds = append(cmds, c)
		}
		if len(cmds) > 0 {
			out = append(out, group{name: Group(g).String(), cmds: cmds})
		}
	}
	return out
}
