package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The People pane's widget half. The sections and the filtering are the
// viewmodel's and are tested there; here is what the pixels decide.

type peopleHarness struct {
	*People
	vm     *viewmodel.Model
	opened []string
	menus  []string
}

func newPeopleHarness(t testing.TB) *peopleHarness {
	t.Helper()
	testApp(t)
	h := &peopleHarness{vm: viewmodel.New(viewmodel.Hooks{})}
	h.vm.SetPrefixes("ov", "@+")
	h.People = NewPeople(PeopleOptions{
		Model: h.vm,
		Open:  func(n string) { h.opened = append(h.opened, n) },
		Menu: func(n string) []*fyne.MenuItem {
			h.menus = append(h.menus, n)
			return []*fyne.MenuItem{fyne.NewMenuItem("Whois", func() {})}
		},
	})
	return h
}

func (h *peopleHarness) row(t testing.TB, i int) *peopleRow {
	t.Helper()
	r, ok := h.vm.PeopleRow(i)
	if !ok {
		t.Fatalf("no people row %d", i)
	}
	w := newPeopleRow(h.People)
	w.set(r)
	return w
}

// Clicking a nick opens the conversation; clicking a section heading does
// nothing, because a heading is not a person.
func TestClickingAMemberOpensTheConversation(t *testing.T) {
	h := newPeopleHarness(t)
	h.vm.SetMembers([]model.Member{
		{Nick: "alice", Prefixes: "@"},
		{Nick: "bob"},
	})

	section := h.row(t, 0)
	section.Tapped(&fyne.PointEvent{})
	if len(h.opened) != 0 {
		t.Errorf("tapping the %q heading opened %v", section.row.Title, h.opened)
	}

	member := h.row(t, 1)
	member.Tapped(&fyne.PointEvent{})
	if len(h.opened) != 1 || h.opened[0] != "alice" {
		t.Errorf("opened %v, want [alice]", h.opened)
	}
}

// The hover kebab and the right-click menu are one list (the Parity Rule), and
// the kebab appears on hover and only on members.
func TestTheKebabAndRightClickAreOneMenu(t *testing.T) {
	h := newPeopleHarness(t)
	h.vm.SetMembers([]model.Member{{Nick: "alice", Prefixes: "@"}, {Nick: "bob"}})

	member := h.row(t, 1)
	if member.kebab.Visible() {
		t.Error("the kebab is drawn before the row is hovered")
	}
	member.MouseIn(nil)
	if !member.kebab.Visible() {
		t.Fatal("hovering a member does not reveal the kebab")
	}
	member.kebab.tap(fyne.NewPos(0, 0))
	member.TappedSecondary(&fyne.PointEvent{})
	if len(h.menus) != 2 || h.menus[0] != "alice" || h.menus[1] != "alice" {
		t.Errorf("menus asked for %v, want alice twice", h.menus)
	}
	member.MouseOut()
	if member.kebab.Visible() {
		t.Error("the kebab stayed after the pointer left")
	}

	section := h.row(t, 0)
	section.MouseIn(nil)
	if section.kebab.Visible() {
		t.Error("a section heading grew a kebab")
	}
	section.TappedSecondary(&fyne.PointEvent{})
	if len(h.menus) != 2 {
		t.Errorf("right-clicking a heading asked for a menu: %v", h.menus)
	}
}

// Away dims the nick and a registered account draws the badge - the two facts
// away-notify and account-tag exist to deliver.
func TestAwayDimsAndAccountBadges(t *testing.T) {
	h := newPeopleHarness(t)
	h.vm.SetMembers([]model.Member{
		{Nick: "alice", Account: "alice"},
		{Nick: "bob", Away: true},
	})

	alice, bob := h.row(t, 0), h.row(t, 1)
	if _, _, _, a := alice.dot.FillColor.RGBA(); a == 0 {
		t.Error("a registered member has no badge")
	}
	if _, _, _, a := bob.dot.FillColor.RGBA(); a != 0 {
		t.Error("an unregistered member drew a badge")
	}
	if alice.name.Color == bob.name.Color {
		t.Error("an away member is drawn the same as one who is here")
	}
}

// The filter box belongs to the buffer it was typed in: a switch clears it in
// the model, and the widget follows rather than keeping its own copy.
func TestTheFilterBoxFollowsTheModel(t *testing.T) {
	h := newPeopleHarness(t)
	h.vm.AddSession("s-1", "libera")
	h.vm.SetBuffers("s-1", []string{viewmodel.ServerBuffer, "#go", "#eco"})
	h.vm.Select(viewmodel.Ref{Session: "s-1", Name: "#go"})
	h.vm.SetMembers([]model.Member{{Nick: "alice"}, {Nick: "bob"}})

	h.filter.SetText("ali")
	if got := h.vm.PeopleFilter(); got != "ali" {
		t.Fatalf("model filter %q, want the typed text", got)
	}
	if got := h.vm.PeopleRowCount(); got != 1 {
		t.Errorf("%d rows through the filter, want 1", got)
	}

	h.vm.Select(viewmodel.Ref{Session: "s-1", Name: "#eco"})
	h.Refresh()
	if h.filter.Text != "" {
		t.Errorf("the filter box still reads %q after a buffer switch", h.filter.Text)
	}
}

// The pane is a real widget: it lays out, and its minimum width leaves the
// split free to be dragged.
func TestPeopleRenders(t *testing.T) {
	h := newPeopleHarness(t)
	h.vm.SetMembers([]model.Member{{Nick: "alice", Prefixes: "@"}, {Nick: "bob"}})

	w := test.NewWindow(h.People)
	defer w.Close()
	w.Resize(fyne.NewSize(200, 600))
	h.Refresh()

	if h.list.Length() != h.vm.PeopleRowCount() {
		t.Errorf("the list holds %d rows, the model has %d", h.list.Length(), h.vm.PeopleRowCount())
	}
}
