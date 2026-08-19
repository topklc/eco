package widgets

import (
	"math"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
)

// TestMeasureIsAdditive is the assumption the whole height model rests on:
// Fyne measures a string by summing glyph advances, so a word's width does
// not depend on what it sits next to, and word widths can be cached and
// added up instead of re-measured.
//
// It is exact for Latin. Across scripts it is not quite — a line mixing CJK
// with spaces measures a pixel wider word by word than whole — so the
// property this test actually defends is the one that matters: the sum is
// never SHORT. Short is what clips the last line of a message; long costs at
// worst a pixel of the row it was going to wrap on anyway.
func TestMeasureIsAdditive(t *testing.T) {
	a := testApp(t)
	size := a.Settings().Theme().Size(theme.SizeNameText)
	latin := map[string]bool{
		"hello there this is a fairly typical irc message": true,
		"AVA WWW iii lll ... mixed WIDTHS here":            true,
	}
	for _, s := range []string{
		"hello there this is a fairly typical irc message",
		"AVA WWW iii lll ... mixed WIDTHS here",
		"日本語 の テキスト",
		"emoji 🎉 and 🚀 words",
	} {
		whole := fyne.MeasureText(s, size, fyne.TextStyle{}).Width
		sum := float32(0)
		for i, part := range strings.Split(s, " ") {
			if i > 0 {
				sum += fyne.MeasureText(" ", size, fyne.TextStyle{}).Width
			}
			sum += fyne.MeasureText(part, size, fyne.TextStyle{}).Width
		}
		if sum < whole-0.01 {
			t.Errorf("%q: sum of words %.4f is SHORT of the whole string %.4f", s, sum, whole)
		}
		if latin[s] && math.Abs(float64(sum-whole)) > 0.01 {
			t.Errorf("%q: sum of words %.4f, whole string %.4f — additivity was exact here", s, sum, whole)
		}
	}
}

// wrapCorpus is realistic message text: plain prose, mIRC colour, links,
// long unbroken tokens, CJK, and the empty line.
var wrapCorpus = []string{
	"",
	"hi",
	"hello there, this is a perfectly ordinary line of chat",
	"a much longer message that will certainly have to wrap at least twice at the widths this test uses, because that is the case the arithmetic exists for",
	"look at \x0304this\x03 and \x02that\x02 and also \x1funderlined\x1f text",
	"https://example.com/a/very/long/path/that/never/breaks/anywhere?query=1&more=2",
	"supercalifragilisticexpialidociousandthensome" + strings.Repeat("x", 120),
	"日本語のテキストが混ざっている行もあります、これも折り返しの対象です",
	"one  two   three    four with runs of spaces between the words",
	"trailing spaces at the end of the line   ",
	"emoji 🎉 in the middle 🚀 of the text 🐧 and at the end 🌍",
}

// TestWrapMatchesRichText holds the arithmetic against the real thing, in
// the units that matter: pixels of body height. Coming out short clips the
// last line of a message, so short is an error; coming out tall leaves a
// band of dead space, so more than one spare line is an error too.
func TestWrapMatchesRichText(t *testing.T) {
	th := testApp(t).Settings().Theme()
	size := th.Size(theme.SizeNameText)
	pad := th.Size(theme.SizeNameInnerPadding)
	m := newMeasurer(size)

	var slack, cases int
	for _, text := range wrapCorpus {
		for _, mono := range []bool{false, true} {
			segs := render.Body(model.Line{Time: time.Now(), Nick: "alice", Text: text},
				render.Options{Mono: mono})
			rt := widget.NewRichText(segs...)
			rt.Wrapping = fyne.TextWrapWord
			rt.Scroll = fyne.ScrollNone
			for _, w := range []float32{120, 240, 380, 700} {
				rt.Resize(fyne.NewSize(w+2*pad, 1000))
				rt.Refresh()
				rows, lineH := m.rows(segs, w)
				got := float32(rows) * lineH
				want := rt.MinSize().Height - 2*pad
				cases++
				switch {
				case got < want-0.01:
					t.Errorf("w=%.0f mono=%v: arithmetic gives %.3f, RichText needs %.3f\n  %q",
						w, mono, got, want, text)
				case got > want+lineH+0.01:
					t.Errorf("w=%.0f mono=%v: arithmetic gives %.3f, RichText needs only %.3f\n  %q",
						w, mono, got, want, text)
				case got > want+0.01:
					slack++
					t.Logf("w=%.0f mono=%v: one spare line (%.3f vs %.3f)\n  %q", w, mono, got, want, text)
				}
			}
		}
	}
	if slack*5 > cases {
		t.Errorf("%d of %d cases carry a spare line; the arithmetic is too loose", slack, cases)
	}
}
