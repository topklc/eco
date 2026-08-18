package app

// Numeric unit tests for the gridline column math (blueprint M1): when art
// alignment breaks, these name the broken quantity — cell size, cell
// count, block geometry, or per-cell layout position — without anyone
// having to pixel-diff a golden.

import (
	"image/color"
	"math"
	"testing"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
)

func pinArtTheme(t *testing.T, fontScale float32) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	a.Settings().SetTheme(&forcedTheme{
		Theme:     theme.DefaultTheme(),
		force:     true,
		variant:   theme.VariantDark,
		fontScale: fontScale,
	})
}

// TestBlockGeom pins the exact cell-fraction geometry of every supported
// Unicode block element. All fractions are eighths/halves — exact in
// float32, so equality is exact, not approximate.
func TestBlockGeom(t *testing.T) {
	cases := []struct {
		r    rune
		want blockRect
	}{
		{0x2588, blockRect{0, 0, 1, 1, 0xFF}},         // █ full block
		{0x2580, blockRect{0, 0, 1, 0.5, 0xFF}},       // ▀ upper half
		{0x2584, blockRect{0, 0.5, 1, 0.5, 0xFF}},     // ▄ lower half
		{0x258C, blockRect{0, 0, 0.5, 1, 0xFF}},       // ▌ left half
		{0x2590, blockRect{0.5, 0, 0.5, 1, 0xFF}},     // ▐ right half
		{0x2594, blockRect{0, 0, 1, 0.125, 0xFF}},     // ▔ upper eighth
		{0x2595, blockRect{0.875, 0, 0.125, 1, 0xFF}}, // ▕ right eighth
		{0x2591, blockRect{0, 0, 1, 1, 0x40}},         // ░ light shade
		{0x2592, blockRect{0, 0, 1, 1, 0x80}},         // ▒ medium shade
		{0x2593, blockRect{0, 0, 1, 1, 0xC0}},         // ▓ dark shade
		{0x2581, blockRect{0, 0.875, 1, 0.125, 0xFF}}, // ▁ lower eighth
		{0x2582, blockRect{0, 0.75, 1, 0.25, 0xFF}},   // ▂ lower quarter
		{0x2586, blockRect{0, 0.25, 1, 0.75, 0xFF}},   // ▆ lower 3/4
		{0x2587, blockRect{0, 0.125, 1, 0.875, 0xFF}}, // ▇ lower 7/8
		{0x2589, blockRect{0, 0, 0.875, 1, 0xFF}},     // ▉ left 7/8
		{0x258F, blockRect{0, 0, 0.125, 1, 0xFF}},     // ▏ left eighth
	}
	for _, c := range cases {
		got, ok := blockGeom(c.r)
		if !ok {
			t.Errorf("blockGeom(%U): not recognized as a block", c.r)
			continue
		}
		if got != c.want {
			t.Errorf("blockGeom(%U) = %+v, want %+v", c.r, got, c.want)
		}
	}
	// non-blocks (incl. the quadrant range, unsupported by design) fall
	// through to text rendering
	for _, r := range []rune{'A', ' ', 0, '▖'} {
		if _, ok := blockGeom(r); ok {
			t.Errorf("blockGeom(%U): unexpectedly treated as a block", r)
		}
	}
}

// TestArtRouting pins which lines take the grid path: painted backgrounds
// or colored block glyphs are art; a colored sentence is still prose.
func TestArtRouting(t *testing.T) {
	cases := []struct {
		line string
		mono bool
		art  bool
	}{
		{"this is \x02bold\x02 mono text.", true, false},
		{"\x030,1  \x030,4    \x030,1  ", true, true},
		{"\x030▄▄\x034▄▄", true, true},
		{"\x034colored sentence, no blocks", true, false},
	}
	for _, c := range cases {
		if got := hasBackgroundArt(ircfmt.Split(c.line), c.mono); got != c.art {
			t.Errorf("hasBackgroundArt(%q, mono=%v) = %v, want %v", c.line, c.mono, got, c.art)
		}
	}
}

// TestArtCellMath verifies the cell-size and cell-count arithmetic at both
// golden font scales: whole-pixel advances (fractional widths accumulate
// into column drift) and MinSize = cells x cell.
func TestArtCellMath(t *testing.T) {
	for _, pts := range []int{13, 18} {
		pinArtTheme(t, float32(pts)/14.0)

		cs := artCellSize()
		if cs.Width <= 0 || cs.Height <= 0 {
			t.Fatalf("%dpt: degenerate cell size %v", pts, cs)
		}
		if cs.Width != float32(math.Round(float64(cs.Width))) ||
			cs.Height != float32(math.Round(float64(cs.Height))) {
			t.Errorf("%dpt: cell size not whole pixels: %v", pts, cs)
		}

		prefix := "15:04  · "
		cells := gridCellsFor(prefix, ircfmt.Split("\x030,4▀▀\x034,0▄▄"), theme.ColorNameForeground)
		wantCells := utf8.RuneCountInString(prefix) + 4
		if len(cells) != wantCells {
			t.Fatalf("%dpt: gridCellsFor produced %d cells, want %d (one per rune)", pts, len(cells), wantCells)
		}
		min := newArtLine(cells).MinSize()
		if want := cs.Width * float32(len(cells)); min.Width != want {
			t.Errorf("%dpt: artLine width %v, want %d cells x %v = %v", pts, min.Width, len(cells), cs.Width, want)
		}
		if min.Height != cs.Height {
			t.Errorf("%dpt: artLine height %v, want one cell %v", pts, min.Height, cs.Height)
		}
	}
}

// TestArtLineLayoutColumns walks the artLine renderer's objects and checks
// every rectangle lands exactly on its column: background rect at
// (i*cell, 0) covering the full cell, block rect at the blockGeom
// fractions of that cell.
func TestArtLineLayoutColumns(t *testing.T) {
	pinArtTheme(t, 1)

	st := &widget.CustomTextGridStyle{
		FGColor: color.NRGBA{0xFF, 0x00, 0x00, 0xFF},
		BGColor: color.NRGBA{0x00, 0x00, 0xFF, 0xFF},
	}
	runes := []rune{0x2580, 0x2584, 0x2580, 0x2584} // ▀▄▀▄
	cells := make([]widget.TextGridCell, len(runes))
	for i, r := range runes {
		cells[i] = widget.TextGridCell{Rune: r, Style: st}
	}
	objs := newArtLine(cells).CreateRenderer().Objects()
	if len(objs) != 2*len(cells) {
		t.Fatalf("renderer objects = %d, want %d (bg rect + block rect per cell)", len(objs), 2*len(cells))
	}
	cs := artCellSize()
	for i, r := range runes {
		bg, blk := objs[2*i], objs[2*i+1]
		wantX := cs.Width * float32(i)
		if p := bg.Position(); p.X != wantX || p.Y != 0 {
			t.Errorf("cell %d: bg rect at %v, want (%v, 0)", i, p, wantX)
		}
		if s := bg.Size(); s != cs {
			t.Errorf("cell %d: bg rect size %v, want full cell %v", i, s, cs)
		}
		geom, ok := blockGeom(r)
		if !ok {
			t.Fatalf("cell %d: %U not a block", i, r)
		}
		wantPos := fyne.NewPos(wantX+geom.x*cs.Width, geom.y*cs.Height)
		wantSize := fyne.NewSize(geom.w*cs.Width, geom.h*cs.Height)
		if p := blk.Position(); p != wantPos {
			t.Errorf("cell %d: block rect at %v, want %v", i, p, wantPos)
		}
		if s := blk.Size(); s != wantSize {
			t.Errorf("cell %d: block rect size %v, want %v", i, s, wantSize)
		}
		if _, isRect := blk.(*canvas.Rectangle); !isRect {
			t.Errorf("cell %d: block drawn as %T, want exact rectangle (glyph coverage leaves seams)", i, blk)
		}
	}
}

// TestGridPaletteRowAlignment: the two 16-color chart rows must produce
// identical cell counts (or the chart cannot align), and grid art must
// resolve EXACT palette colors — never the readability-tuned variants.
func TestGridPaletteRowAlignment(t *testing.T) {
	pinArtTheme(t, 1)

	rows := loadArtCase(t, "palette-16")
	if len(rows) != 2 {
		t.Fatalf("palette-16 corpus has %d rows, want 2", len(rows))
	}
	prefix := "15:04  · "
	c0 := gridCellsFor(prefix, ircfmt.Split(rows[0]), theme.ColorNamePlaceHolder)
	c1 := gridCellsFor(prefix, ircfmt.Split(rows[1]), theme.ColorNamePlaceHolder)
	if len(c0) != len(c1) {
		t.Errorf("palette rows produce %d vs %d cells — columns cannot align", len(c0), len(c1))
	}

	first := c0[utf8.RuneCountInString(prefix)] // first content cell: fg 1 on bg 0
	stl, ok := first.Style.(*widget.CustomTextGridStyle)
	if !ok {
		t.Fatalf("content cell style is %T, want *widget.CustomTextGridStyle", first.Style)
	}
	if stl.FGColor != classicPalette[1] {
		t.Errorf("grid fg = %v, want exact classic palette 1 (black) %v", stl.FGColor, classicPalette[1])
	}
	if stl.BGColor != classicPalette[0] {
		t.Errorf("grid bg = %v, want exact classic palette 0 (white) %v", stl.BGColor, classicPalette[0])
	}
}
