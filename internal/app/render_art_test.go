package app

// Golden-image tests for the render engine — blueprint M1, "freeze the
// crown jewel". Every corpus file under testdata/art renders offscreen and
// is byte-compared against a checked-in PNG master, across both palettes
// (light/dark, pinned via forcedTheme) and two font scales (13pt/18pt,
// pinned via forcedTheme.fontScale — never preferences). These goldens
// gate every later milestone (R-13): a diff here means the art engine
// changed; regenerate only deliberately with -update and written intent.

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
)

// -update regenerates the masters instead of comparing:
//
//	go test -run TestRenderArtGoldens -update .
var updateGoldens = flag.Bool("update", false, "regenerate golden PNGs in testdata/golden")

// artCorpusTime is the fixed timestamp rendered into the gutter of every
// golden line. Goldens must never depend on the wall clock: time.Now()
// here would make every run's pixels differ.
var artCorpusTime = time.Date(2025, 6, 1, 15, 4, 0, 0, time.UTC)

// Fixed offscreen window size; wider art clips deterministically.
const (
	artWindowW = 760
	artWindowH = 700
)

// artCorpusCases lists every corpus case (file base name, sorted) under
// testdata/art. New corpus files automatically join the golden matrix.
func artCorpusCases(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata", "art", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no corpus files in testdata/art")
	}
	sort.Strings(matches)
	cases := make([]string, 0, len(matches))
	for _, m := range matches {
		cases = append(cases, strings.TrimSuffix(filepath.Base(m), ".txt"))
	}
	return cases
}

// loadArtCase reads one corpus file: raw IRC-formatted lines, one message
// line per file line. Files named motd-* are Ergo MOTD sources kept in
// their on-disk $-escape form and ircfmt.Unescape'd here, exactly as ergo
// does when serving the MOTD; everything else is raw wire formatting.
func loadArtCase(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "art", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if strings.HasPrefix(name, "motd-") {
		for i, l := range lines {
			lines[i] = ircfmt.Unescape(l)
		}
	}
	return lines
}

// pinDisplayPrefs pins the display globals the render path reads, so
// golden output cannot depend on preference state or on other tests.
func pinDisplayPrefs(t *testing.T) {
	t.Helper()
	savedTS, savedLayout := showTimestamps, timeLayout
	savedNicks, savedSelf := colorfulNicks, selfNickColor
	showTimestamps, timeLayout = true, "15:04"
	colorfulNicks, selfNickColor = true, fyne.ThemeColorName("irc3")
	t.Cleanup(func() {
		showTimestamps, timeLayout = savedTS, savedLayout
		colorfulNicks, selfNickColor = savedNicks, savedSelf
	})
}

// renderArtCase renders corpus lines the way the server buffer does
// (status lines, monospace) into a fixed-size offscreen window and
// captures the canvas.
func renderArtCase(lines []string) image.Image {
	rt := widget.NewRichText()
	rt.Wrapping = fyne.TextWrapWord
	for _, l := range lines {
		rt.Segments = append(rt.Segments,
			segmentsFor(Line{Time: artCorpusTime, Text: "- " + l, Status: true}, true, nil)...)
	}
	w := test.NewWindow(rt)
	defer w.Close()
	w.Resize(fyne.NewSize(artWindowW, artWindowH))
	return w.Canvas().Capture()
}

// compareGolden byte-compares the render against the checked-in master,
// or rewrites the master under -update.
func compareGolden(t *testing.T, img image.Image, name string) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "golden", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (regenerate with -update): %v", name, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		got := filepath.Join(t.TempDir(), name)
		_ = os.WriteFile(got, buf.Bytes(), 0o644)
		t.Errorf("render differs from golden %s (actual render saved to %s; regenerate deliberately with -update)", path, got)
	}
}

// TestRenderArtGoldens is the frozen-engine gate: every corpus case, both
// palettes, both font scales, byte-compared PNG for PNG.
func TestRenderArtGoldens(t *testing.T) {
	pinDisplayPrefs(t)
	variants := []struct {
		name    string
		variant fyne.ThemeVariant
	}{
		{"light", theme.VariantLight},
		{"dark", theme.VariantDark},
	}
	cases := artCorpusCases(t)
	for _, v := range variants {
		for _, pts := range []int{13, 18} {
			a := test.NewApp()
			// the app's own theme with variant and font scale pinned
			// explicitly: light and dark are forced, never inherited
			a.Settings().SetTheme(&forcedTheme{
				Theme:     theme.DefaultTheme(),
				force:     true,
				variant:   v.variant,
				fontScale: float32(pts) / 14.0,
			})
			for _, c := range cases {
				name := fmt.Sprintf("%s_%s_%dpt", c, v.name, pts)
				t.Run(name, func(t *testing.T) {
					compareGolden(t, renderArtCase(loadArtCase(t, c)), name+".png")
				})
			}
			a.Quit()
		}
	}
}
