package theme

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The contrast floors both palettes are built to (WCAG 2.1 ratios):
//
//	body   7.0  AAA for normal text — body text sits on a screen for hours
//	text   4.5  AA  for normal text — anything still meant to be read
//	object 3.0  AA  for non-text objects — dots, fills, disabled text
//	seen   1.15 not a WCAG level: the floor below which a divider or a pill
//	            stops being visible at all against the surface behind it
const (
	bodyFloor   = 7.0
	textFloor   = 4.5
	objectFloor = 3.0
	seenFloor   = 1.15
)

// surfaces is every opaque surface app text can land on.
var surfaces = []fyne.ThemeColorName{RailSurface, Background, InputBackground, Surface}

// TestBodyTextContrast: body text is comfortable — AAA — on every surface,
// in both palettes. This is the floor the whole design answers to; the
// message pane is a wall of text people read all day.
func TestBodyTextContrast(t *testing.T) {
	for _, v := range variants {
		for _, s := range surfaces {
			assertContrast(t, v.name, TextPrimary, s, v.v, bodyFloor)
		}
	}
}

// TestSecondaryTextContrast: timestamps, placeholders and the state colours
// are quieter than body text but still meant to be read — AA.
func TestSecondaryTextContrast(t *testing.T) {
	for _, v := range variants {
		for _, s := range surfaces {
			for _, tok := range []fyne.ThemeColorName{TextMuted, AccentText, Success, Warning, Danger} {
				assertContrast(t, v.name, tok, s, v.v, textFloor)
			}
		}
	}
}

// TestFaintAndFillContrast: the faint role (disabled text, the timestamp
// gutter) and the accent used as a fill are non-text objects — AA's 3:1.
func TestFaintAndFillContrast(t *testing.T) {
	for _, v := range variants {
		for _, s := range surfaces {
			assertContrast(t, v.name, TextFaint, s, v.v, objectFloor)
			assertContrast(t, v.name, Accent, s, v.v, objectFloor)
		}
	}
}

// TestPillContrast: a pill is only useful if its count can be read. Unread
// pills carry body ink, mention pills carry the accent's ink.
func TestPillContrast(t *testing.T) {
	for _, v := range variants {
		assertPair(t, v.name, "textPrimary on unreadPill",
			Color(TextPrimary, v.v), Color(UnreadPill, v.v), textFloor)
		assertPair(t, v.name, "onAccent on mentionPill",
			Color(OnAccent, v.v), Color(MentionPill, v.v), textFloor)
		assertPair(t, v.name, "onAccent on accent",
			Color(OnAccent, v.v), Color(Accent, v.v), textFloor)
	}
}

// TestQuietSeparationVisible: dividers and unread pills are deliberately
// quiet — restraint, not invisibility. This is the floor where "quiet"
// turns into "gone".
func TestQuietSeparationVisible(t *testing.T) {
	for _, v := range variants {
		for _, s := range surfaces {
			assertContrast(t, v.name, Border, s, v.v, seenFloor)
		}
		for _, s := range []fyne.ThemeColorName{RailSurface, Background} {
			assertContrast(t, v.name, UnreadPill, s, v.v, seenFloor)
		}
	}
}

// TestInkFlipsWithTheFill: Ink is what keeps a label readable on a colour
// the user chose, so it must actually flip.
func TestInkFlipsWithTheFill(t *testing.T) {
	cases := []struct {
		name string
		fill color.NRGBA
	}{
		{"logo green", Color(Accent, theme.VariantLight)},
		{"dark accent green", Color(Accent, theme.VariantDark)},
		{"deep teal", color.NRGBA{0x14, 0x90, 0x7A, 0xFF}},
		{"signal red", color.NRGBA{0xD6, 0x45, 0x45, 0xFF}},
		{"steel grey", color.NRGBA{0x6E, 0x7B, 0x85, 0xFF}},
		{"near black", color.NRGBA{0x10, 0x10, 0x10, 0xFF}},
		{"near white", color.NRGBA{0xF2, 0xF2, 0xF2, 0xFF}},
	}
	for _, c := range cases {
		ink := Ink(c.fill)
		if got := Contrast(ink, c.fill); got < objectFloor {
			t.Errorf("Ink on %s: %.2f, want >= %.2f (ink %v)", c.name, got, objectFloor, ink)
		}
	}
	if Ink(color.NRGBA{0x10, 0x10, 0x10, 0xFF}) != Color(TextPrimary, theme.VariantDark) {
		t.Error("Ink on a near-black fill should be the light ink")
	}
	if Ink(color.NRGBA{0xF2, 0xF2, 0xF2, 0xFF}) != Color(TextPrimary, theme.VariantLight) {
		t.Error("Ink on a near-white fill should be the dark ink")
	}
}

// TestCustomBackgroundKeepsBodyReadable: every background preset a user can
// pick must still carry body text, since the surfaces step off it.
func TestCustomBackgroundKeepsBodyReadable(t *testing.T) {
	for _, bg := range BackgroundPresets() {
		lum := relativeLuminance(bg)
		v := theme.VariantDark
		if lum > 0.2 {
			v = theme.VariantLight
		}
		for _, tok := range []fyne.ThemeColorName{Background, Surface, InputBackground, RailSurface} {
			step, _ := elevationStep(tok, v)
			surface := shade(bg, step)
			if got := Contrast(Color(TextPrimary, v), surface); got < bodyFloor {
				t.Errorf("body text on %s stepped from %v: %.2f, want >= %.2f",
					tok, bg, got, bodyFloor)
			}
		}
	}
}

func assertContrast(t *testing.T, variant string, fg, bg fyne.ThemeColorName, v fyne.ThemeVariant, floor float64) {
	t.Helper()
	assertPair(t, variant, string(fg)+" on "+string(bg), Color(fg, v), Color(bg, v), floor)
}

func assertPair(t *testing.T, variant, what string, fg, bg color.NRGBA, floor float64) {
	t.Helper()
	got := Contrast(fg, bg)
	if got < floor {
		t.Errorf("%s: %s is %.2f:1, want >= %.2f:1 (%v on %v)", variant, what, got, floor, fg, bg)
	}
}

// TestContrastMath pins the ratio function itself against the two values
// everyone knows, so a wrong palette can never be blamed on a wrong metric.
func TestContrastMath(t *testing.T) {
	black := color.NRGBA{0, 0, 0, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	if got := Contrast(black, white); got < 20.99 || got > 21.01 {
		t.Errorf("black on white: %.4f, want 21", got)
	}
	if got := Contrast(white, black); got < 20.99 || got > 21.01 {
		t.Errorf("contrast is not symmetric: %.4f", got)
	}
	if got := Contrast(white, white); got != 1 {
		t.Errorf("white on white: %.4f, want 1", got)
	}
	// the classic mid-grey #767676 is the darkest grey that still clears
	// 4.5:1 on white — a fixture that catches a broken transfer curve
	if got := Contrast(color.NRGBA{0x76, 0x76, 0x76, 0xFF}, white); got < 4.5 || got > 4.6 {
		t.Errorf("#767676 on white: %.4f, want ~4.54", got)
	}
}
