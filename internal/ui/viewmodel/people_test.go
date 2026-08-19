package viewmodel

import (
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// The People pane's model: sectioning by the SERVER's prefix table, the filter
// box, and the per-buffer visibility Ctrl+U remembers.

// roster is shorthand for "nick with these prefixes".
func roster(pairs ...string) []model.Member {
	out := make([]model.Member, 0, len(pairs))
	for _, p := range pairs {
		i := 0
		for i < len(p) && strings.ContainsRune("~&@%+!", rune(p[i])) {
			i++
		}
		out = append(out, model.Member{Nick: p[i:], Prefixes: p[:i]})
	}
	return out
}

// render prints the rows the way the pane reads: "== Title (n)" for a section.
func render(rows []PeopleRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Kind == PeopleSection {
			out = append(out, "== "+r.Title)
			continue
		}
		out = append(out, r.Member.Prefixes+r.Member.Nick)
	}
	return out
}

// Sections come from ISUPPORT PREFIX, in the server's order, with the modes it
// actually has. v1 hardcoded "~&@%+" in four places; a network with
// PREFIX=(Yohv)!@%+ has a rank that string cannot name and an order it gets
// wrong, and this is the table that proves the difference.
func TestSectionsComeFromThePrefixTable(t *testing.T) {
	cases := []struct {
		name           string
		modes, chars   string
		members        []model.Member
		want           []string
		wantFirstTitle string
	}{
		{
			name:  "the usual five",
			modes: "qaohv", chars: "~&@%+",
			members: roster("+dave", "@bob", "carol", "~alice", "%erin", "&frank"),
			want: []string{
				"== Owners", "~alice",
				"== Admins", "&frank",
				"== Operators", "@bob",
				"== Half-ops", "%erin",
				"== Voiced", "+dave",
				"== " + restTitle, "carol",
			},
		},
		{
			name:  "a mode the client has never heard of is titled by its own prefix",
			modes: "Yohv", chars: "!@%+",
			members: roster("@bob", "!helper", "carol"),
			want: []string{
				"== !", "!helper",
				"== Operators", "@bob",
				"== " + restTitle, "carol",
			},
		},
		{
			name:  "a member with two prefixes ranks by the highest",
			modes: "ov", chars: "@+",
			members: []model.Member{{Nick: "bob", Prefixes: "+@"}, {Nick: "carol", Prefixes: "+"}},
			want: []string{
				"== Operators", "+@bob",
				"== Voiced", "+carol",
			},
		},
		{
			name:  "no PREFIX data means one rank, so no headings at all",
			modes: "", chars: "",
			members: roster("@bob", "carol"),
			want:    []string{"@bob", "carol"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			h.SetPrefixes(c.modes, c.chars)
			h.SetMembers(c.members)
			if got := render(h.PeopleRows()); !eq(got, c.want) {
				t.Errorf("rows\n%v\nwant\n%v", got, c.want)
			}
		})
	}
}

// One section is not a section: a heading over the entire list separates
// nothing from nothing, and the rail applies the same rule to its headers.
func TestASingleSectionDrawsNoHeading(t *testing.T) {
	h := newHarness()
	h.SetPrefixes("ov", "@+")
	h.SetMembers(roster("alice", "bob", "carol"))

	if got, want := render(h.PeopleRows()), []string{"alice", "bob", "carol"}; !eq(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	h.SetMembers(roster("@alice", "bob"))
	if got, want := render(h.PeopleRows()), []string{"== Operators", "@alice", "== " + restTitle, "bob"}; !eq(got, want) {
		t.Errorf("rows %v, want %v once there are two ranks", got, want)
	}
}

// The filter box narrows by substring, folded, and takes the emptied sections
// with it - a heading over nobody is the same lie as a heading over everybody.
func TestTheFilterNarrowsAndDropsEmptySections(t *testing.T) {
	h := newHarness()
	h.SetPrefixes("ov", "@+")
	h.SetMembers(roster("@Alice", "@bob", "+carol", "dave", "alicia"))

	cases := []struct {
		filter string
		want   []string
	}{
		{"", []string{"== Operators", "@Alice", "@bob", "== Voiced", "+carol", "== " + restTitle, "dave", "alicia"}},
		{"ali", []string{"== Operators", "@Alice", "== " + restTitle, "alicia"}},
		{"ALI", []string{"== Operators", "@Alice", "== " + restTitle, "alicia"}},
		{"  car  ", []string{"+carol"}}, // one section left, so no heading
		{"nobody", nil},
	}
	for _, c := range cases {
		h.SetPeopleFilter(c.filter)
		if got := render(h.PeopleRows()); !eq(got, c.want) {
			t.Errorf("filter %q: rows %v, want %v", c.filter, got, c.want)
		}
		if got, want := h.MemberCount(), 5; got != want {
			t.Errorf("filter %q: MemberCount = %d, want %d - the filter is a lens, not a roster", c.filter, got, want)
		}
	}
}

// Away and account reach the pane untouched: the widget dims one and badges
// the other, and neither is a fact this layer invents.
func TestAwayAndAccountSurvive(t *testing.T) {
	h := newHarness()
	h.SetPrefixes("ov", "@+")
	h.SetMembers([]model.Member{
		{Nick: "alice", Account: "alice", Away: true},
		{Nick: "bob"},
	})
	rows := h.PeopleRows()
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	if !rows[0].Member.Away || rows[0].Member.Account != "alice" {
		t.Errorf("row 0 = %+v, want away and registered", rows[0].Member)
	}
	if rows[1].Member.Away || rows[1].Member.Account != "" {
		t.Errorf("row 1 = %+v, want neither", rows[1].Member)
	}
}

// Switching buffers clears the filter. The next channel is a different set of
// people, and a filter left over from the last one hides them without saying
// why.
func TestSwitchingBuffersClearsTheFilter(t *testing.T) {
	h := twoNetworks(t)
	h.Select(ref(sA, "#go"))
	h.SetPeopleFilter("ali")

	h.Select(ref(sA, ServerBuffer))
	if got := h.PeopleFilter(); got != "" {
		t.Errorf("filter %q survived the switch", got)
	}
}

// Visibility is per buffer and remembered (§4.1). The default is the honest
// one: a buffer with a roster shows it, a private conversation has nobody to
// list - and once someone says otherwise with Ctrl+U, that is what is kept.
func TestPeopleVisibilityIsRememberedPerBuffer(t *testing.T) {
	h := twoNetworks(t)

	h.Select(ref(sA, "#go"))
	h.SetMembers(roster("@alice", "bob"))
	if !h.PeopleVisible() {
		t.Error("a channel with a roster hides the pane by default")
	}
	if h.TogglePeople() {
		t.Error("Ctrl+U on an open pane did not close it")
	}

	h.Select(ref(sA, "alice"))
	h.SetMembers(nil)
	if h.PeopleVisible() {
		t.Error("a private conversation with nobody in it opens the pane by default")
	}
	if !h.TogglePeople() {
		t.Error("Ctrl+U on a closed pane did not open it")
	}

	// back to the channel: its own choice is what comes back, not the DM's
	h.Select(ref(sA, "#go"))
	h.SetMembers(roster("@alice", "bob"))
	if h.PeopleVisible() {
		t.Error("the channel forgot that it was closed")
	}
	h.Select(ref(sA, "alice"))
	h.SetMembers(nil)
	if !h.PeopleVisible() {
		t.Error("the conversation forgot that it was opened")
	}
}

// A buffer's choice is scoped to its network profile, like every other
// per-buffer fact in this package: two networks with a #go each are two
// different rooms.
func TestPeopleVisibilityIsScopedToTheNetwork(t *testing.T) {
	h := twoNetworks(t)
	h.SetBuffers(sB, []string{ServerBuffer, "#go"})

	h.Select(ref(sA, "#go"))
	h.SetMembers(roster("alice"))
	h.SetPeopleVisible(false)

	h.Select(ref(sB, "#go"))
	h.SetMembers(roster("bob"))
	if !h.PeopleVisible() {
		t.Error("closing the pane in one network's #go closed it in the other's")
	}
}
