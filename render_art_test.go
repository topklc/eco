package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
)

// TestRenderArt renders background-colored "art" lines offscreen so column
// alignment can be inspected as pixels (scratch tool, checked in for reuse).
func TestRenderArt(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	appTheme := &forcedTheme{Theme: theme.DefaultTheme(), fontScale: 16.0 / 14.0}
	a.Settings().SetTheme(appTheme)

	lines := []string{
		"- \x030,1  \x030,4    \x030,1  ",
		"- \x030,4  \x030,0    \x030,4  ",
		"- \x030,1  \x030,4    \x030,1  ",
		"- \x031,0 00 \x030,1 01 \x030,2 02 \x030,3 03 \x031,4 04 \x030,5 05 \x030,6 06 \x031,7 07 ",
		"- \x031,8 08 \x031,9 09 \x030,10 10 \x031,11 11 \x030,12 12 \x031,13 13 \x031,14 14 \x031,15 15 ",
		"- this is \x02bold\x02 mono text.",
		// img2irc-style half-block art: fg-only colored block glyphs
		"- \x030\u2584\u2584\x034\u2584\u2584\x031\u2584\u2584\x030\u2584\u2584",
		"- \x034\u2580\u2580\x030\u2580\u2580\x034\u2580\u2580\x031\u2580\u2580",
		// mixed: fg block glyphs over colored backgrounds
		"- \x030,4\u2580\u2580\x034,0\u2580\u2580\x030,1\u2580\u2580\x031,0\u2580\u2580",
	}
	if data, err := os.ReadFile("/home/lenovo/Downloads/ircd-motd-puckman-hq.txt"); err == nil {
		lines = nil
		for _, l := range strings.Split(string(data), "\n") {
			lines = append(lines, "- "+ircfmt.Unescape(l))
		}
	}
	rt := widget.NewRichText()
	rt.Wrapping = fyne.TextWrapWord
	now := time.Now()
	for _, l := range lines {
		rt.Segments = append(rt.Segments, segmentsFor(Line{Time: now, Text: l, Status: true}, true, nil)...)
	}
	w := test.NewWindow(rt)
	defer w.Close()
	w.Resize(fyne.NewSize(760, 700))
	img := w.Canvas().Capture()
	// ECO_ART_PNG overrides the output path for eyeballing the render
	out := os.Getenv("ECO_ART_PNG")
	if out == "" {
		out = filepath.Join(t.TempDir(), "art.png")
	}
	t.Log("render written to", out)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
