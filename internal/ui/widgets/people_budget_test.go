package widgets

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// The People pane's performance budget - the blueprint's "3k-nick People-pane
// budget in CI" (§7, M12).
//
// These are alarms, not benchmarks, set the same way the Timeline's are: about
// an order of magnitude above what this machine measures, so they catch the
// regression that would actually kill the pane - the sectioned row model being
// rebuilt per question instead of per change, which widget.List asks for once
// per row per repaint - without failing on a loaded CI box.
const (
	budgetPeopleOpen   = 250 * time.Millisecond // a 3,000-nick channel appearing
	budgetPeopleFilter = 100 * time.Millisecond // one keystroke in the filter box
	budgetPeopleRepeat = 20 * time.Millisecond  // a repaint that changed nothing
	budgetPeopleScroll = 20 * time.Millisecond  // one scroll step
)

// peopleBudgetNicks is how big the channel is. 3,000 is the blueprint's number.
func peopleBudgetNicks(t testing.TB) int {
	if v := os.Getenv("ECO_PEOPLE_BUDGET_NICKS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 10 {
			t.Fatalf("ECO_PEOPLE_BUDGET_NICKS=%q is not a nick count", v)
		}
		return n
	}
	return 3000
}

// peopleCorpus is a roster shaped like a huge channel: a handful of ops, some
// voiced, most of them plain, a third of them registered and a scattering away.
func peopleCorpus(n int) []model.Member {
	out := make([]model.Member, 0, n)
	for i := 0; i < n; i++ {
		m := model.Member{Nick: fmt.Sprintf("nick_%04d", i)}
		switch {
		case i%997 == 0:
			m.Prefixes = "@"
		case i%53 == 0:
			m.Prefixes = "+"
		}
		if i%3 == 0 {
			m.Account = m.Nick
		}
		m.Away = i%17 == 0
		out = append(out, m)
	}
	return out
}

// TestPeopleBudget is the gate. Under -race it only proves the pane still
// behaves at 3,000 nicks; the numbers come from `make test-budget`.
func TestPeopleBudget(t *testing.T) {
	testApp(t)
	n := peopleBudgetNicks(t)
	members := peopleCorpus(n)

	vm := viewmodel.New(viewmodel.Hooks{})
	vm.SetPrefixes("ov", "@+")
	p := NewPeople(PeopleOptions{Model: vm})
	w := test.NewWindow(p)
	defer w.Close()
	w.Resize(fyne.NewSize(220, 700))

	// the channel appearing: the roster arrives, is sectioned, and is drawn
	start := time.Now()
	vm.SetMembers(members)
	p.Refresh()
	open := time.Since(start)

	rows := vm.PeopleRowCount()
	if rows < n {
		t.Fatalf("%d rows for %d members", rows, n)
	}

	// a repaint with nothing changed: the sections must be cached, because
	// this is what every arriving message costs otherwise
	start = time.Now()
	for i := 0; i < 10; i++ {
		p.Refresh()
	}
	repeat := time.Since(start) / 10

	// one keystroke in the filter box: the whole roster is walked and
	// re-sectioned, and the pane redrawn
	filter := time.Duration(0)
	for _, s := range []string{"n", "ni", "nic", "nick_1", "nick_12"} {
		start = time.Now()
		p.filter.SetText(s)
		if d := time.Since(start); d > filter {
			filter = d
		}
	}
	if got := vm.PeopleRowCount(); got == 0 || got >= rows {
		t.Errorf("filtering to %q left %d of %d rows", p.filter.Text, got, rows)
	}
	p.filter.SetText("")
	if got := vm.PeopleRowCount(); got != rows {
		t.Errorf("clearing the filter left %d rows, want %d back", got, rows)
	}

	// scrolling deep, where the List's offset walk is longest
	scroll := time.Duration(0)
	height := float32(rows) * p.rowHeight()
	for i := 0; i < 20; i++ {
		start = time.Now()
		p.list.ScrollToOffset(height - 800 - float32(i)*137)
		if d := time.Since(start); d > scroll {
			scroll = d
		}
	}

	t.Logf("%d nicks / %d rows: open %v, idle repaint %v, filter keystroke %v, scroll %v",
		n, rows, open.Round(time.Microsecond), repeat.Round(time.Microsecond),
		filter.Round(time.Microsecond), scroll.Round(time.Microsecond))

	if raceEnabled || testing.Short() {
		t.Skip("timings under the race detector say nothing about the shipped binary; run make test-budget")
	}
	for _, c := range []struct {
		name   string
		got    time.Duration
		budget time.Duration
	}{
		{"opening the pane", open, budgetPeopleOpen},
		{"repainting an unchanged pane", repeat, budgetPeopleRepeat},
		{"one filter keystroke", filter, budgetPeopleFilter},
		{"scrolling one step", scroll, budgetPeopleScroll},
	} {
		if c.got > c.budget {
			t.Errorf("%s at %d nicks took %v, budget %v", c.name, n, c.got, c.budget)
		}
	}
}

// BenchmarkPeopleSections isolates the row model: the sectioning pass that
// every filter keystroke pays for.
func BenchmarkPeopleSections(b *testing.B) {
	members := peopleCorpus(3000)
	vm := viewmodel.New(viewmodel.Hooks{})
	vm.SetPrefixes("ov", "@+")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm.SetMembers(members)
		vm.PeopleRows()
	}
}
