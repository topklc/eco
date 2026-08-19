package command

import (
	"go/build"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// withExtraCommand adds one entry to the table for the duration of a test. It
// is what lets a test ask the question the milestone actually promises - "does
// adding a command change everything that is supposed to be generated from
// it?" - instead of the much weaker question the other tests ask, which is
// whether today's list happens to agree with today's table. A frozen
// hand-written copy passes the weak question forever.
func withExtraCommand(t *testing.T, c *Command) {
	t.Helper()
	t.Cleanup(AddForTest(c))
}

// TestAddForTestRefusesNonsense: the hook mutates the table, so it is worth
// knowing it will not do so on a whim.
func TestAddForTestRefusesNonsense(t *testing.T) {
	for _, c := range []*Command{nil, {}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("AddForTest(%v) was accepted", c)
				}
			}()
			AddForTest(c)
		}()
	}
	if n := len(All()); n == 0 {
		t.Fatal("the table did not survive the refusals")
	}
}

// TestAddingACommandChangesEverythingDerived is the drift gate. Tab completion,
// the /help summary and the documentation page are all supposed to BE the
// table; if any of them were a second copy that merely agreed with it, this
// test would fail the moment a command it has never heard of appears.
func TestAddingACommandChangesEverythingDerived(t *testing.T) {
	beforeVerbs := Verbs()
	beforeSummary := Summary()
	beforeHelp := HelpMarkdown()

	withExtraCommand(t, &Command{
		Name: "zzdrift", Aliases: []string{"zzd"}, Group: GroupChat,
		Help: "prove the generated lists are generated",
		Args: ArgSpec{Usage: "argument", Min: 1},
		Run:  func(in Input, e Env) { e.UI().ShowHint("drift") },
	})

	if got := Verbs(); reflect.DeepEqual(got, beforeVerbs) {
		t.Error("Verbs() did not change when a command was added: the completion list is not the table")
	}
	for _, v := range []string{"/zzdrift", "/zzd"} {
		found := false
		for _, got := range Verbs() {
			if got == v {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is in the table but not in Verbs(): Tab completion would never offer it", v)
		}
	}
	if !sort.StringsAreSorted(Verbs()) {
		t.Error("Verbs() is no longer sorted after an entry was added")
	}
	if got := Complete("/zzd"); !reflect.DeepEqual(got, []string{"/zzd", "/zzdrift"}) {
		t.Errorf("Complete(\"/zzd\") = %q, want the new command and its alias: "+
			"Tab completion is not answered from the table", got)
	}
	if got := Complete("/"); !reflect.DeepEqual(got, Verbs()) {
		t.Errorf("Complete(\"/\") = %q, want the whole of Verbs()", got)
	}

	s := Summary()
	if s == beforeSummary {
		t.Error("Summary() did not change: /help's list is not the table")
	}
	if !strings.Contains(s, " /zzdrift") {
		t.Errorf("Summary() does not mention the new command:\n%s", s)
	}

	md := HelpMarkdown()
	if md == beforeHelp {
		t.Error("HelpMarkdown() did not change: the documentation page is not the table")
	}
	for _, want := range []string{"`/zzdrift argument`", "prove the generated lists are generated", "`/zzd`"} {
		if !strings.Contains(md, want) {
			t.Errorf("HelpMarkdown() does not contain %q:\n%s", want, md)
		}
	}

	// ...and the entry is reachable, with its usage generated from the same spec
	if c := Lookup("zzd"); c == nil || c.Name != "zzdrift" {
		t.Fatalf("the alias does not resolve: %v", c)
	}
	env := fakeEnv{}
	Run(Parse("/zzdrift", inChannel), &env)
	if want := []string{"hint Usage: /zzdrift argument"}; !reflect.DeepEqual(env.log, want) {
		t.Errorf("the new command did %q, want %q", env.log, want)
	}
}

// TestRemovingACommandChangesEverythingDerived is the same gate from the other
// side: a verb that has left the table must leave completion and the help with
// it, or the client offers a command it no longer has.
func TestRemovingACommandChangesEverythingDerived(t *testing.T) {
	old := table
	var kept []*Command
	for _, c := range old {
		if c.Name == "knock" {
			continue
		}
		kept = append(kept, c)
	}
	table = kept
	index()
	t.Cleanup(func() { table = old; index() })

	for _, v := range Verbs() {
		if v == "/knock" {
			t.Error("/knock left the table but is still completable")
		}
	}
	if strings.Contains(Summary(), " /knock") {
		t.Error("/knock left the table but is still in the summary")
	}
	if strings.Contains(HelpMarkdown(), "`/knock") {
		t.Error("/knock left the table but is still in the help page")
	}
	if in := Parse("/knock #x", inChannel); in.Kind != KindUnknown {
		t.Errorf("/knock left the table but still parses as %v", in.Kind)
	}
}

// TestDuplicateVerbsAreRefused: two entries claiming one verb would make the
// second silently shadow the first - which command runs would depend on the
// order of a slice. The index refuses to build at all instead, and this is the
// test that keeps that refusal there.
func TestDuplicateVerbsAreRefused(t *testing.T) {
	old := table
	defer func() {
		table = old
		index()
		if recover() == nil {
			t.Error("a table with two /join entries built an index without complaining")
		}
	}()
	table = append(append([]*Command(nil), old...), &Command{
		Name: "join", Help: "a second claim on the same verb",
		Run: func(in Input, e Env) {},
	})
	index()
}

// TestAllIsACopy: the table is the program's one source of truth, so handing a
// caller the slice itself would let anyone reorder or truncate it.
func TestAllIsACopy(t *testing.T) {
	got := All()
	if len(got) == 0 {
		t.Fatal("no commands at all")
	}
	first := got[0]
	got[0] = nil
	if again := All(); again[0] == nil {
		t.Error("All() handed out the table's own slice")
	}
	if All()[0] != first {
		t.Error("All() no longer starts with the table's first entry")
	}
}

// TestNeedsChannelHonoursTheServerFlag: "am I in a channel" is answered by the
// caller's flag, not by the buffer's name. A network log that happens to be
// called "#something" is still not a channel, and /kick in it must not reach
// the wire.
func TestNeedsChannelHonoursTheServerFlag(t *testing.T) {
	logNamedLikeAChannel := Context{Buffer: "#network-log", Server: true}
	for _, in := range []string{"/kick bob", "/topic hello", "/names", "/cycle", "/op alice", "/ban bob", "/invite bob"} {
		env := fakeEnv{}
		Run(Parse(in, logNamedLikeAChannel), &env)
		if len(env.log) != 1 || !strings.HasPrefix(env.log[0], "hint /") ||
			!strings.HasSuffix(env.log[0], " only works in a channel.") {
			t.Errorf("%q in the network log did %q, want only the channel-only refusal", in, env.log)
		}
	}
}

// TestPackageStaysNarrow is the milestone's structural claim, checked instead
// of asserted in a comment: internal/command sees three small interfaces and
// the standard library. The moment it imports the IRC layer, the store or Fyne,
// the table stops being testable headless and the env has stopped being the
// only door.
func TestPackageStaysNarrow(t *testing.T) {
	const module = "github.com/toprakkilic/eco/"
	forbidden := []string{
		"fyne.io/",
		"github.com/ergochat/",
		module + "internal/irc",
		module + "internal/app",
		module + "internal/state",
		module + "internal/archive",
		module + "internal/ui",
		module + "internal/render",
		module + "internal/config",
		module + "internal/notify",
		"database/sql",
		"net",
		"net/http",
		"os/exec",
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	var walk func(dir, name string)
	walk = func(dir, name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		for _, imp := range pkg.Imports {
			for _, bad := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("%s imports %q: internal/command must stay narrow enough to test headless", name, imp)
				}
			}
			if rest, ok := strings.CutPrefix(imp, module); ok {
				walk(filepath.Join(repo, rest), imp)
			}
		}
	}
	walk(".", module+"internal/command")

	// and the whole reachable set is worth printing when this ever fails
	if t.Failed() {
		var all []string
		for p := range seen {
			all = append(all, p)
		}
		sort.Strings(all)
		t.Logf("packages reachable from internal/command: %v", all)
	}
}
