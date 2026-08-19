package theme

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/toprakkilic/eco/internal/render"
)

var variants = []struct {
	name string
	v    fyne.ThemeVariant
}{
	{"light", theme.VariantLight},
	{"dark", theme.VariantDark},
}

// TestTokenTable is the completeness gate: every token must have a real
// value in both variants. A missing map entry reads back as the zero NRGBA
// — transparent black — which is exactly the silent failure this catches.
func TestTokenTable(t *testing.T) {
	for _, tok := range Tokens {
		for _, v := range variants {
			c := Color(tok, v.v)
			if c == (color.NRGBA{}) {
				t.Errorf("%s/%s: undefined (zero value)", tok, v.name)
			}
			if c.A == 0 {
				t.Errorf("%s/%s: fully transparent %v", tok, v.name, c)
			}
		}
	}
}

// TestTokenTablesAgree guards the two tables against drifting apart: a
// token defined for one variant only is a half-finished palette.
func TestTokenTablesAgree(t *testing.T) {
	if len(darkTokens) != len(Tokens) || len(lightTokens) != len(Tokens) {
		t.Fatalf("table sizes differ from Tokens: dark %d light %d tokens %d",
			len(darkTokens), len(lightTokens), len(Tokens))
	}
	for tok := range darkTokens {
		if _, ok := lightTokens[tok]; !ok {
			t.Errorf("%s: defined dark, missing light", tok)
		}
		if !IsToken(tok) {
			t.Errorf("%s: not reported by IsToken", tok)
		}
	}
	seen := map[fyne.ThemeColorName]bool{}
	for _, tok := range Tokens {
		if seen[tok] {
			t.Errorf("%s: listed twice in Tokens", tok)
		}
		seen[tok] = true
		if _, ok := darkTokens[tok]; !ok {
			t.Errorf("%s: listed in Tokens, missing from the tables", tok)
		}
	}
}

// TestNeitherExtremeSurface: a pure-black dark theme has nowhere to put a
// surface below the background and haloes text on OLED; a pure-white light
// theme glares and has nowhere to raise a dialog to.
func TestNeitherExtremeSurface(t *testing.T) {
	dark := Color(Background, theme.VariantDark)
	if dark.R == 0 && dark.G == 0 && dark.B == 0 {
		t.Error("dark background is pure black")
	}
	light := Color(Background, theme.VariantLight)
	if light.R == 0xFF && light.G == 0xFF && light.B == 0xFF {
		t.Error("light background is pure white")
	}
}

// TestElevationOrder: the rail is quieter than the chat, and dialogs are
// raised above it, in both variants. The whole point of railSurface is that
// it steps the other way from surface.
func TestElevationOrder(t *testing.T) {
	for _, v := range variants {
		rail := relativeLuminance(Color(RailSurface, v.v))
		bg := relativeLuminance(Color(Background, v.v))
		input := relativeLuminance(Color(InputBackground, v.v))
		surface := relativeLuminance(Color(Surface, v.v))
		if !(rail < bg && bg < input && input < surface) {
			t.Errorf("%s: elevation out of order rail %.4f bg %.4f input %.4f surface %.4f",
				v.name, rail, bg, input, surface)
		}
	}
}

// TestIsTokenLeavesForeignNamesAlone: the token namespace must not swallow
// Fyne's own colour names or render's IRC ones, or the wrapper would answer
// for the frozen contract.
func TestIsTokenLeavesForeignNamesAlone(t *testing.T) {
	foreign := []fyne.ThemeColorName{
		theme.ColorNameBackground, theme.ColorNameForeground, theme.ColorNamePrimary,
		theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameHover,
		"irc0", "irc3", "irc15", "ircx16", "ircx4", selfNick,
	}
	for _, n := range foreign {
		if IsToken(n) {
			t.Errorf("%s: claimed as an eco token", n)
		}
	}
	for _, tok := range Tokens {
		if _, ok := render.LookupColor(tok, theme.VariantDark); ok {
			t.Errorf("%s: collides with a render IRC colour name", tok)
		}
	}
}

func TestShadeClamps(t *testing.T) {
	black := color.NRGBA{0, 0, 0, 0xFF}
	if got := shade(black, -20); got != black {
		t.Errorf("darkening black: got %v want %v", got, black)
	}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	if got := shade(white, +20); got != white {
		t.Errorf("lightening white: got %v want %v", got, white)
	}
	if got := shade(color.NRGBA{0x10, 0x20, 0x30, 0x80}, +5); got != (color.NRGBA{0x15, 0x25, 0x35, 0x80}) {
		t.Errorf("shade kept alpha/step wrong: %v", got)
	}
}

func TestWithAlpha(t *testing.T) {
	got := withAlpha(color.NRGBA{0x11, 0x22, 0x33, 0xFF}, 0x40)
	if got != (color.NRGBA{0x11, 0x22, 0x33, 0x40}) {
		t.Errorf("withAlpha: %v", got)
	}
}
