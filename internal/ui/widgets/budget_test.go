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
	"github.com/toprakkilic/eco/internal/render"
)

// The Timeline's performance budgets — blueprint risk #1, the one risky
// architectural bet in the whole rewrite: "every row's wrapped height
// recomputes on resize". This test is the gate that bet had to pass before
// the flag could exist.
//
// These are alarms, not benchmarks. Each is set roughly an order of magnitude
// above what a 10,000-line buffer measured on this machine, so they catch the
// two regressions that would kill the design — heights going back to being
// measured by layout, and heights going back to being written into a List
// that is already on screen — without failing on a loaded CI box. The
// measured numbers are logged by the test itself and recorded in the
// blueprint.
const (
	budgetBuffer = 400 * time.Millisecond // opening a 10k-line buffer
	budgetResize = 150 * time.Millisecond // one width change over 10k rows
	budgetAppend = 50 * time.Millisecond  // one message arriving at the bottom
	budgetScroll = 20 * time.Millisecond  // one scroll step
)

// budgetLines is how big the buffer is. 10,000 is the blueprint's number.
func budgetLines(t testing.TB) int {
	if v := os.Getenv("ECO_TIMELINE_BUDGET_LINES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 100 {
			t.Fatalf("ECO_TIMELINE_BUDGET_LINES=%q is not a line count", v)
		}
		return n
	}
	if raceEnabled {
		// the race run is not measuring anything, so it only has to be big
		// enough to prove a buffer far past any cap still behaves
		return 3000
	}
	return 10000
}

// budgetCorpus is a buffer shaped like a busy channel: mostly one-line chat
// from a handful of nicks, a fifth of it long enough to wrap, joins and parts
// through it, a day boundary in the middle, and some mIRC colour.
func budgetCorpus(n int) []model.Line {
	nicks := []string{"alice", "bob", "carol", "dave", "erin_with_a_long_nick"}
	long := "this one is long enough that it has to wrap at any sensible pane width, which is the case the height arithmetic exists for, and it repeats words the way real chat does"
	start := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	lines := make([]model.Line, 0, n)
	for i := 0; i < n; i++ {
		l := model.Line{
			Time: start.Add(time.Duration(i) * 7 * time.Second),
			Nick: nicks[i%len(nicks)],
			Text: fmt.Sprintf("message %d in the channel", i),
		}
		switch {
		case i%5 == 0:
			l.Text = long
		case i%23 == 0:
			l.Status, l.Nick = true, ""
			l.Text = "someone joined"
		case i%37 == 0:
			l.Text = "look at \x0304this\x03 and \x02that\x02"
		}
		lines = append(lines, l)
	}
	return lines
}

func timelineOptions() Options {
	return Options{
		Render: func() render.Options {
			return render.Options{ShowTimestamps: true, TimeLayout: "15:04", ColorfulNicks: true}
		},
		Now: func() time.Time { return time.Date(2025, 6, 2, 9, 0, 0, 0, time.UTC) },
	}
}

// TestTimelineBudget is the gate. Under -race it only proves a 10k buffer
// still behaves; the numbers come from `make test-budget`.
func TestTimelineBudget(t *testing.T) {
	testApp(t)
	n := budgetLines(t)
	lines := budgetCorpus(n)

	m := NewMessageList(timelineOptions())
	w := test.NewWindow(m)
	defer w.Close()
	w.Resize(fyne.NewSize(900, 700))

	start := time.Now()
	m.SetBuffer(lines, lines[n/2].Time)
	buffer := time.Since(start)

	if len(m.Rows()) < n {
		t.Fatalf("%d rows for %d lines", len(m.Rows()), n)
	}
	if m.total <= 0 {
		t.Fatalf("content height %v", m.total)
	}

	// the resize: every row's wrapped height changes at once, which is the
	// risk this whole milestone was gated on
	resize := time.Duration(0)
	for i := 0; i < 5; i++ {
		start = time.Now()
		w.Resize(fyne.NewSize(900-float32(40*(i+1)), 700))
		if d := time.Since(start); d > resize {
			resize = d
		}
	}

	// one message arriving at the bottom of a full buffer
	append1 := time.Duration(0)
	for i := 0; i < 5; i++ {
		start = time.Now()
		m.Append(model.Line{Time: lines[n-1].Time.Add(time.Duration(i+1) * time.Second),
			Nick: "alice", Text: "and one more"})
		if d := time.Since(start); d > append1 {
			append1 = d
		}
	}

	// scrolling through the deep end, where the List's offset walk is longest
	scroll := time.Duration(0)
	for i := 0; i < 20; i++ {
		start = time.Now()
		m.list.ScrollToOffset(m.total - 800 - float32(i)*137)
		if d := time.Since(start); d > scroll {
			scroll = d
		}
	}

	t.Logf("%d lines / %d rows: open %v, resize %v, append %v, scroll %v",
		n, len(m.Rows()), buffer.Round(time.Microsecond), resize.Round(time.Microsecond),
		append1.Round(time.Microsecond), scroll.Round(time.Microsecond))

	if raceEnabled || testing.Short() {
		t.Skip("timings under the race detector say nothing about the shipped binary; run make test-budget")
	}
	for _, c := range []struct {
		name   string
		got    time.Duration
		budget time.Duration
	}{
		{"opening the buffer", buffer, budgetBuffer},
		{"resizing", resize, budgetResize},
		{"appending one message", append1, budgetAppend},
		{"scrolling one step", scroll, budgetScroll},
	} {
		if c.got > c.budget {
			t.Errorf("%s a %d-line buffer took %v, budget %v", c.name, n, c.got, c.budget)
		}
	}
}

// BenchmarkTimelineOpen is where the "open" number comes from.
func BenchmarkTimelineOpen(b *testing.B) {
	testApp(b)
	lines := budgetCorpus(10000)
	m := NewMessageList(timelineOptions())
	w := test.NewWindow(m)
	defer w.Close()
	w.Resize(fyne.NewSize(900, 700))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.SetBuffer(lines, time.Time{})
	}
}

// BenchmarkTimelineHeights isolates the height pass — the arithmetic that
// replaced measuring 10,000 rows by laying them out.
func BenchmarkTimelineHeights(b *testing.B) {
	testApp(b)
	lines := budgetCorpus(10000)
	m := NewMessageList(timelineOptions())
	w := test.NewWindow(m)
	defer w.Close()
	w.Resize(fyne.NewSize(900, 700))
	m.SetBuffer(lines, time.Time{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.measure(0)
	}
}
