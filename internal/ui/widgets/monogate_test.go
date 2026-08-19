package widgets

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
)

// The mono gate - blueprint R-10, risk #6, run at M11(c).
//
// The question the gate answers: is the whole chat pane monospace, or is the
// body proportional with a monospace gutter, nick column and art? The decision
// and its evidence are recorded in the blueprint's M11(c) entry; what lives
// here is the measurement that produced it, so the answer can be re-checked
// on another machine (font fallback is per-machine, which is half the point)
// and so a future change of mind has to argue with numbers.
//
// Set ECO_MONOGATE_OUT=/some/dir to also write the rendered corpus as PNGs.

// gateSample is one line of the corpus the gate measures: one per script, the same
// text the golden art corpus carries, plus the prose case chat is mostly made
// of. Each is measured in both faces.
type gateSample struct {
	name string
	text string
}

func gateSamples(t testing.TB) []gateSample {
	t.Helper()
	corpus := func(file string, line int) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "render", "testdata", "art", file))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if line >= len(lines) {
			t.Fatalf("%s has %d lines, wanted %d", file, len(lines), line+1)
		}
		return lines[line]
	}
	return []gateSample{
		{"ascii", "the quick brown fox jumps over the lazy dog"},
		{"japanese", corpus("text-cjk.txt", 0)},
		{"chinese", corpus("text-cjk.txt", 1)},
		{"korean", corpus("text-cjk.txt", 2)},
		{"fullwidth", corpus("text-doublewidth.txt", 0)},
		{"boxdrawing", corpus("text-doublewidth.txt", 2)},
		{"emoji", corpus("text-emoji.txt", 0)},
		{"arabic", corpus("text-rtl.txt", 0)},
		{"hebrew", corpus("text-rtl.txt", 1)},
		{"bidi-mixed", corpus("text-rtl.txt", 2)},
	}
}

// gateChat is the corpus as a channel would carry it: real nicks, real
// timestamps, prose between the scripts. The gate renders CHAT, not art -
// the art corpus is already frozen in the render goldens, and what is being
// decided here is how a conversation reads.
func gateChat(t testing.TB) []model.Line {
	t.Helper()
	start := time.Date(2025, 6, 2, 14, 2, 0, 0, time.UTC)
	nicks := []string{"alice", "bo", "carol_mcbrides", "dave", "erin"}
	var lines []model.Line
	add := func(text string) {
		i := len(lines)
		lines = append(lines, model.Line{
			Time: start.Add(time.Duration(i) * 37 * time.Second),
			Nick: nicks[i%len(nicks)],
			Text: text,
		})
	}
	add("the point of this pane is that a long paragraph of ordinary prose stays comfortable to read for an hour at a time, which is most of what a chat client is ever asked to do")
	for _, s := range gateSamples(t) {
		add(s.text)
	}
	add("and back to prose: mixed into a channel, the scripts above sit between messages like this one rather than on a page of their own")
	add("a url in passing https://example.org/some/fairly/long/path?with=query and text after it")
	lines = append(lines, model.Line{
		Time: start.Add(time.Duration(len(lines)) * 37 * time.Second),
		Text: "someone joined the channel", Status: true,
	})
	return lines
}

// TestMonoGate measures both candidate typographies over the corpus and logs
// the table the blueprint's decision cites.
func TestMonoGate(t *testing.T) {
	testApp(t)
	size := fyne.CurrentApp().Settings().Theme().Size(fyne.ThemeSizeName("text"))
	mono := fyne.TextStyle{Monospace: true}
	prop := fyne.TextStyle{}
	cell := fyne.MeasureText("M", size, mono).Width

	t.Logf("text size %.1f, monospace cell %.3fpx", size, cell)
	// the premise of the whole comparison: two faces, one of which is a grid
	// for the runes it has. If the monospace face ever stops being monospace
	// (a missing shipped face measures as the proportional one), every number
	// below is meaningless and so is the gutter's alignment.
	if w := fyne.MeasureText("i", size, mono).Width; w != cell {
		t.Fatalf("'i' is %.2fpx and 'M' is %.2fpx in the monospace face: it is not monospace", w, cell)
	}
	if fyne.MeasureText("i", size, prop).Width == fyne.MeasureText("M", size, prop).Width {
		t.Fatal("the proportional face measures 'i' and 'M' the same; the two candidates are one")
	}
	t.Logf("%-11s %9s %9s %9s  %s", "sample", "mono px", "prop px", "cells", "monospace advance is a whole number of cells")
	for _, s := range gateSamples(t) {
		mw := fyne.MeasureText(s.text, size, mono).Width
		pw := fyne.MeasureText(s.text, size, prop).Width
		cells := mw / cell
		whole := math.Abs(float64(cells)-math.Round(float64(cells))) < 0.02
		t.Logf("%-11s %9.2f %9.2f %9.3f  %v", s.name, mw, pw, cells, whole)
		if mw <= 0 || pw <= 0 {
			t.Errorf("%s measures %vx%v: the corpus line did not reach a font", s.name, mw, pw)
		}
	}

	// Per-rune: the alignment promise of a monospace pane is that every glyph
	// is one cell (or a clean two). Fyne resolves a rune the shipped mono face
	// does not have against whatever the machine offers for family
	// "monospace", so this is a per-machine answer - which is exactly why the
	// promise cannot be made in the first place.
	t.Logf("%-11s %9s %9s %9s", "rune", "mono px", "cells", "prop px")
	for _, r := range []rune{'M', 'i', '日', '한', '你', 'Ｆ', '│', '🙂', '❤', 'م', 'ש'} {
		w := fyne.MeasureText(string(r), size, mono).Width
		t.Logf("%-11q %9.2f %9.3f %9.2f", r, w, w/cell, fyne.MeasureText(string(r), size, prop).Width)
	}

	// The two facts the decision rests on, ASSERTED rather than logged.
	//
	// Until the M11 verification pass these were only printed, which made the
	// gate re-runnable but not binding: a font or toolkit change could make the
	// blueprint's argument false and every test would still pass. The argument
	// is (1) monospace changes nothing for the scripts it was meant to protect,
	// because they come from the same fallback face either way, and (2) the
	// grid it is chosen for does not survive contact with them. If either stops
	// holding, the decision is owed a re-run, and this is what says so.
	for _, r := range []rune{'日', '한', '你', 'Ｆ', '🙂', 'ש'} {
		mw := fyne.MeasureText(string(r), size, mono).Width
		pw := fyne.MeasureText(string(r), size, prop).Width
		if mw != pw {
			t.Errorf("%q is %.2fpx monospace and %.2fpx proportional: the gate's first premise "+
				"(the at-risk scripts resolve to the same fallback face in both) no longer holds "+
				"on this machine - re-run the decision", r, mw, pw)
		}
	}

	// The column claim, tested directly. Each of these is ten terminal
	// columns wide by East Asian width - in a terminal they stack into a
	// grid. Whether they do here is the whole of what "monospace" buys the
	// body text.
	t.Logf("%-14s %9s %9s  %s", "ten columns", "mono px", "cells", "sample")
	ascii := float32(0)
	for _, s := range []string{"ABCDEFGHIJ", "|--------|", "日本語テキ", "한국어텍스", "ＡＢＣＤＥ", "🙂🙂🙂🙂🙂"} {
		w := fyne.MeasureText(s, size, mono).Width
		t.Logf("%-14s %9.2f %9.3f  %s", s, w, w/cell, s)
		if ascii == 0 {
			ascii = w // "ABCDEFGHIJ": ten columns of Latin, the grid's own unit
			if w != 10*cell {
				t.Fatalf("ten ASCII columns are %.2fpx and ten cells are %.2fpx: "+
					"the monospace face is not a grid even for Latin", w, 10*cell)
			}
			continue
		}
		if strings.ContainsFunc(s, func(r rune) bool { return r > 0x2500 }) && w == ascii {
			t.Errorf("ten columns of %q measure the same %.2fpx as ten ASCII columns: "+
				"the gate's second premise (the grid does not exist above ASCII) no longer "+
				"holds - re-run the decision", s, w)
		}
	}

	// Prose density at one pane width: how many rows the same paragraph
	// occupies in each face, which is how much of the window is text.
	m := newMeasurer(size)
	para := gateChat(t)[0].Text
	for _, tc := range []struct {
		name  string
		style fyne.TextStyle
	}{{"monospace", mono}, {"proportional", prop}} {
		for _, w := range []float32{420, 640, 900} {
			segs := render.Body(model.Line{Text: para}, render.Options{Mono: tc.style.Monospace})
			rows, lineH := m.rows(segs, w)
			t.Logf("prose %-12s width %3.0f: %d rows, line height %.1f, %.1f px of text per row",
				tc.name, w, rows, lineH, fyne.MeasureText(para, size, tc.style).Width/float32(rows))
		}
	}
}

// TestMonoGateRender draws the corpus in both typographies. With
// ECO_MONOGATE_OUT set it writes the two PNGs the blueprint's decision was
// read off; without it, it still proves both render and that the columns of
// the two agree to the pixel - which is the structural half of the argument.
func TestMonoGateRender(t *testing.T) {
	testApp(t)
	lines := gateChat(t)
	out := os.Getenv("ECO_MONOGATE_OUT")

	bodyX := map[bool]float32{}
	for _, monoBody := range []bool{true, false} {
		m := NewMessageList(Options{
			Render: func() render.Options {
				return render.Options{
					Mono: monoBody, ShowTimestamps: true,
					TimeLayout: "15:04", ColorfulNicks: true,
				}
			},
			Now: func() time.Time { return time.Date(2025, 6, 2, 15, 0, 0, 0, time.UTC) },
		})
		w := test.NewWindow(m)
		defer w.Close()
		w.Resize(fyne.NewSize(760, 900))
		m.SetBuffer(lines, time.Time{})
		if len(m.Rows()) < len(lines) {
			t.Fatalf("%d rows for %d lines", len(m.Rows()), len(lines))
		}
		bodyX[monoBody] = m.geom.bodyX(m.headWidth(lines[0]))
		t.Logf("mono body %-5v: %d rows, %.0fpx of pane for the same %d messages (line height %.1f)",
			monoBody, len(m.Rows()), m.total, len(lines), m.geom.lineH)

		if out == "" {
			continue
		}
		name := "gate-proportional-body.png"
		if monoBody {
			name = "gate-mono-body.png"
		}
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Log("wrote " + filepath.Join(out, name))
	}

	// The decisive structural fact: the gutter and the nick column are drawn
	// by the row, in monospace, at a position the row computes - so the body's
	// face cannot move them. Column alignment is not what is being traded.
	if bodyX[true] != bodyX[false] {
		t.Fatalf("body starts at %v with a monospace body and %v with a proportional one: "+
			"the columns are supposed to be the row's, not the font's", bodyX[true], bodyX[false])
	}
	if !nickStyle(lines[0]).Monospace {
		t.Fatal("the nick column is not monospace; the gutter promise is broken")
	}
}
