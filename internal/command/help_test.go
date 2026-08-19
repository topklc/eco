package command

import (
	"sort"
	"strings"
	"testing"
)

// TestVerbsAreDerivedFromTheTable is the deletion this milestone is about: the
// completion list used to be a hand-written slice in entry.go that nobody
// updated. It is now this, and this is the table.
func TestVerbsAreDerivedFromTheTable(t *testing.T) {
	var want []string
	for _, c := range All() {
		if c.Hidden {
			continue
		}
		want = append(want, "/"+c.Name)
		for _, a := range c.Aliases {
			want = append(want, "/"+a)
		}
	}
	sort.Strings(want)

	got := Verbs()
	if len(got) != len(want) {
		t.Fatalf("Verbs() has %d entries, the table has %d visible verbs:\n got %q\nwant %q",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Verbs()[%d] = %q, want %q (the list must be sorted and complete)",
				i, got[i], want[i])
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Error("Verbs() is not sorted: Tab would cycle in a random order")
	}
	for _, v := range got {
		if !strings.HasPrefix(v, "/") {
			t.Errorf("verb %q has no slash: it is typed with one", v)
		}
	}
}

// TestVerbsIsACopy: a caller that filters the list in place must not be able to
// corrupt the table's own.
func TestVerbsIsACopy(t *testing.T) {
	got := Verbs()
	if len(got) == 0 {
		t.Fatal("no verbs at all")
	}
	got[0] = "/mutated"
	if again := Verbs(); again[0] == "/mutated" {
		t.Error("Verbs() handed out its own slice")
	}
}

// TestHiddenCommandsAreOutOfSight but still run: /testnotify is a diagnostic,
// not something to offer a beginner.
func TestHiddenCommandsAreOutOfSight(t *testing.T) {
	hidden := 0
	for _, c := range All() {
		if !c.Hidden {
			continue
		}
		hidden++
		if strings.Contains(Summary(), "/"+c.Name) {
			t.Errorf("/%s is hidden but listed in the summary", c.Name)
		}
		if strings.Contains(HelpMarkdown(), "/"+c.Name) {
			t.Errorf("/%s is hidden but in the help page", c.Name)
		}
		for _, v := range Verbs() {
			if v == "/"+c.Name {
				t.Errorf("/%s is hidden but completable", c.Name)
			}
		}
		if Lookup(c.Name) == nil {
			t.Errorf("/%s is hidden AND unreachable, which makes it dead", c.Name)
		}
	}
	if hidden == 0 {
		t.Skip("no hidden commands to check")
	}
}

// TestSummaryListsEveryVisibleCommand: the sentence /help writes is generated,
// so a command cannot exist without being in it. v1's was a string literal and
// had already drifted from the switch it claimed to describe.
func TestSummaryListsEveryVisibleCommand(t *testing.T) {
	s := Summary()
	for _, c := range All() {
		if c.Hidden {
			continue
		}
		if !strings.Contains(s, " /"+c.Name) {
			t.Errorf("/%s is missing from the summary", c.Name)
		}
	}
	for _, g := range groups() {
		if !strings.Contains(s, g.name+":") {
			t.Errorf("group %q is missing from the summary", g.name)
		}
	}
	if !strings.Contains(s, "/raw") {
		t.Error("the summary must name /raw: it is the passthrough the intercept offers")
	}
}

// TestHelpMarkdownDescribesEveryVisibleCommand — the documentation page's
// command section is the same table, rendered.
func TestHelpMarkdownDescribesEveryVisibleCommand(t *testing.T) {
	md := HelpMarkdown()
	for _, c := range All() {
		if c.Hidden {
			continue
		}
		if !strings.Contains(md, "`/"+c.Name) {
			t.Errorf("/%s is missing from the help page", c.Name)
		}
		if !strings.Contains(md, c.Help) {
			t.Errorf("/%s's help line is missing from the help page", c.Name)
		}
		for _, a := range c.Aliases {
			if !strings.Contains(md, "`/"+a+"`") {
				t.Errorf("/%s's alias /%s is missing from the help page", c.Name, a)
			}
		}
	}
	for _, g := range groups() {
		if !strings.Contains(md, "### "+g.name) {
			t.Errorf("group %q has no heading in the help page", g.name)
		}
	}
}

// TestUsageComesFromTheSpec: the sentence a user is shown when they get the
// arguments wrong is generated from the same field the parser reads, so the two
// cannot drift.
func TestUsageComesFromTheSpec(t *testing.T) {
	for _, tc := range []struct{ verb, want string }{
		{"join", "Usage: /join #channel [key]"},
		{"msg", "Usage: /msg nick [message]"},
		{"list", "Usage: /list"},
		{"op", "Usage: /op nick [nick...]"},
	} {
		if got := Lookup(tc.verb).Usage(); got != tc.want {
			t.Errorf("/%s usage = %q, want %q", tc.verb, got, tc.want)
		}
	}
}

// TestTableInvariants are the mistakes this shape of table makes easy: a
// command with no help, a channel command that forgot it needs a connection, an
// arity that cannot be satisfied, a verb with a slash in it.
func TestTableInvariants(t *testing.T) {
	for _, c := range All() {
		switch {
		case c.Name == "":
			t.Error("a command with no name")
		case strings.HasPrefix(c.Name, "/"):
			t.Errorf("/%s: the name carries its own slash", c.Name)
		case c.Name != strings.ToLower(c.Name):
			t.Errorf("%s: verbs are matched lower-cased", c.Name)
		}
		if c.NeedsChannel && !c.NeedsConn {
			t.Errorf("/%s needs a channel but not a connection", c.Name)
		}
		if c.Args.Max > 0 && c.Args.Max < c.Args.Min {
			t.Errorf("/%s can never be satisfied: min %d, max %d", c.Name, c.Args.Min, c.Args.Max)
		}
		if c.Args.Min > 0 && c.Args.Usage == "" {
			t.Errorf("/%s requires arguments but does not say what they are", c.Name)
		}
		if c.Args.Words == 0 && c.Args.Min > 1 {
			t.Errorf("/%s wants %d arguments but asks for the whole line as one",
				c.Name, c.Args.Min)
		}
		if int(c.Group) >= len(groupNames) {
			t.Errorf("/%s is in no group", c.Name)
		}
	}
}
