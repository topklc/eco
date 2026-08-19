package theme

import (
	"fmt"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/toprakkilic/eco/internal/render"
)

func engine() *render.Theme {
	return &render.Theme{Theme: theme.DefaultTheme(), FontScale: 1}
}

// fyneColorNames is every colour name Fyne 2.8 asks a theme for. The app
// must answer all of them from a token; ColorNameShadow is the one
// deliberate exception (see Theme.Color).
var fyneColorNames = []fyne.ThemeColorName{
	theme.ColorNameBackground, theme.ColorNameButton, theme.ColorNameDisabledButton,
	theme.ColorNameDisabled, theme.ColorNameError, theme.ColorNameFocus,
	theme.ColorNameForeground, theme.ColorNameForegroundOnError,
	theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess,
	theme.ColorNameForegroundOnWarning, theme.ColorNameHeaderBackground,
	theme.ColorNameHover, theme.ColorNameHyperlink, theme.ColorNameInnerWindowBorder,
	theme.ColorNameInnerWindowBorderInactive, theme.ColorNameInputBackground,
	theme.ColorNameInputBorder, theme.ColorNameMenuBackground,
	theme.ColorNameOverlayBackground, theme.ColorNamePlaceHolder, theme.ColorNamePressed,
	theme.ColorNamePrimary, theme.ColorNameScrollBar, theme.ColorNameScrollBarBackground,
	theme.ColorNameSelection, theme.ColorNameSeparator, theme.ColorNameSuccess,
	theme.ColorNameWarning,
}

// TestEveryColorComesFromAToken is the M11(a) promise in test form: no
// colour the app paints is a literal any more. Every standard Fyne colour
// name resolves to a value that is in the token tables (or an Ink of one).
func TestEveryColorComesFromAToken(t *testing.T) {
	th := New(engine())
	for _, v := range variants {
		known := map[color.NRGBA]bool{}
		for _, tok := range Tokens {
			c := Color(tok, v.v)
			known[c] = true
			known[Ink(c)] = true
		}
		for _, n := range fyneColorNames {
			got := nrgba(th.Color(n, v.v))
			if !known[got] {
				t.Errorf("%s/%s: %v is not a token value", n, v.name, got)
			}
		}
	}
}

// TestShadowFallsThroughDeliberately documents the one exception: a drop
// shadow is not a surface role, and Fyne's black-at-alpha is already right.
func TestShadowFallsThroughDeliberately(t *testing.T) {
	base := engine()
	th := New(base)
	for _, v := range variants {
		want := base.Color(theme.ColorNameShadow, v.v)
		if got := th.Color(theme.ColorNameShadow, v.v); got != want {
			t.Errorf("%s: shadow %v, want the wrapped theme's %v", v.name, got, want)
		}
	}
}

// TestFrozenContractDelegates is the golden guard. The IRC palettes are
// render's answer and must reach the app through the wrapper untouched: if
// this drifts, the 44 golden PNGs and the running app disagree about what
// colour 4 is.
func TestFrozenContractDelegates(t *testing.T) {
	base := engine()
	th := New(base)
	names := []fyne.ThemeColorName{selfNick}
	for i := 0; i <= 98; i++ {
		names = append(names, fyne.ThemeColorName(fmt.Sprintf("irc%d", i)))
		names = append(names, fyne.ThemeColorName(fmt.Sprintf("ircx%d", i)))
	}
	for _, v := range variants {
		for _, n := range names {
			want := base.Color(n, v.v)
			if got := th.Color(n, v.v); got != want {
				t.Fatalf("%s/%s: wrapper %v, render %v", n, v.name, got, want)
			}
		}
	}
}

// TestEngineRulesSurvive: the bold monospace faces and the zero art line
// spacing are render's, inherited rather than reimplemented.
func TestEngineRulesSurvive(t *testing.T) {
	base := engine()
	base.FontScale = 2
	th := New(base)

	mono := fyne.TextStyle{Monospace: true, Bold: true}
	if got, want := th.Font(mono), base.Font(mono); got != want {
		t.Errorf("bold mono face: %v, want render's %v", got, want)
	}
	if got := th.Size(theme.SizeNameLineSpacing); got != 0 {
		t.Errorf("line spacing: %v, want 0 (art alignment)", got)
	}
	if got, want := th.Size(theme.SizeNameText), base.Size(theme.SizeNameText); got != want {
		t.Errorf("text size: %v, want render's scaled %v", got, want)
	}
}

// TestForcedVariantWins: preferences can pin light or dark against the OS,
// and the tokens must obey the pin, not the query.
func TestForcedVariantWins(t *testing.T) {
	base := engine()
	base.Force, base.Variant = true, theme.VariantLight
	th := New(base)
	if got, want := nrgba(th.Color(theme.ColorNameBackground, theme.VariantDark)),
		Color(Background, theme.VariantLight); got != want {
		t.Errorf("forced light asked as dark: %v, want %v", got, want)
	}
	if got, want := nrgba(th.Color(TextPrimary, theme.VariantDark)),
		Color(TextPrimary, theme.VariantLight); got != want {
		t.Errorf("forced light token: %v, want %v", got, want)
	}
	if got, want := Variant(th), theme.VariantLight; got != want {
		t.Errorf("Variant: %v, want the forced %v", got, want)
	}
}

// TestCustomBackgroundMovesTheFamily: pick a background and the dialogs,
// composer and rail move with it - the whole reason elevation is a step
// rather than a fixed colour.
func TestCustomBackgroundMovesTheFamily(t *testing.T) {
	parchment := color.NRGBA{0xF6, 0xF1, 0xE4, 0xFF}
	base := engine()
	base.BG = parchment
	base.Force, base.Variant = true, theme.VariantLight
	th := New(base)

	if got := nrgba(th.Color(theme.ColorNameBackground, theme.VariantLight)); got != parchment {
		t.Errorf("background: %v, want the chosen %v", got, parchment)
	}
	bg := relativeLuminance(parchment)
	rail := relativeLuminance(th.Color(RailSurface, theme.VariantLight))
	surface := relativeLuminance(th.Color(Surface, theme.VariantLight))
	if !(rail < bg && bg < surface) {
		t.Errorf("family did not follow: rail %.4f bg %.4f surface %.4f", rail, bg, surface)
	}
	// and the text roles stay off the custom background, unchanged
	if got, want := nrgba(th.Color(theme.ColorNameForeground, theme.VariantLight)),
		Color(TextPrimary, theme.VariantLight); got != want {
		t.Errorf("foreground: %v, want %v", got, want)
	}
}

// TestCustomAccentFlows: one choice reaches links, focus, selection and the
// mention pill, each at the alpha its role was designed with.
func TestCustomAccentFlows(t *testing.T) {
	ocean := color.NRGBA{0x2E, 0x7C, 0xD6, 0xFF}
	base := engine()
	base.Accent = ocean
	th := New(base)

	for _, c := range []struct {
		name fyne.ThemeColorName
		tok  fyne.ThemeColorName
	}{
		{theme.ColorNamePrimary, Accent},
		{theme.ColorNameHyperlink, AccentText},
		{theme.ColorNameFocus, FocusOverlay},
		{theme.ColorNameSelection, SelectionOverlay},
	} {
		want := withAlpha(ocean, Color(c.tok, theme.VariantDark).A)
		if got := nrgba(th.Color(c.name, theme.VariantDark)); got != want {
			t.Errorf("%s: %v, want %v", c.name, got, want)
		}
	}
	if got, want := nrgba(th.Color(MentionPill, theme.VariantDark)), ocean; got != want {
		t.Errorf("mention pill: %v, want the chosen accent %v", got, want)
	}
	if got, want := nrgba(th.Color(theme.ColorNameForegroundOnPrimary, theme.VariantDark)),
		Ink(ocean); got != want {
		t.Errorf("ink on the accent: %v, want %v", got, want)
	}
	if got := Contrast(th.Color(theme.ColorNameForegroundOnPrimary, theme.VariantDark), ocean); got < objectFloor {
		t.Errorf("ink on a custom accent: %.2f:1, want >= %.2f:1", got, objectFloor)
	}
}

// TestRailIsQuieter: the ThemeOverride's theme answers the ordinary
// background query with the rail surface, and passes everything else
// through - that substitution is the whole mechanism.
func TestRailIsQuieter(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	app := New(engine())
	a.Settings().SetTheme(app)

	rail := &railTheme{}
	for _, v := range variants {
		got := nrgba(rail.Color(theme.ColorNameBackground, v.v))
		want := Color(RailSurface, v.v)
		if got != want {
			t.Errorf("%s: rail background %v, want %v", v.name, got, want)
		}
		if relativeLuminance(got) >= relativeLuminance(Color(Background, v.v)) {
			t.Errorf("%s: rail is not quieter than the chat pane", v.name)
		}
		if fg, want := rail.Color(theme.ColorNameForeground, v.v),
			app.Color(theme.ColorNameForeground, v.v); fg != want {
			t.Errorf("%s: rail foreground %v, want the app's %v", v.name, fg, want)
		}
		if got, want := rail.Color("irc4", v.v), app.Color("irc4", v.v); got != want {
			t.Errorf("%s: rail irc4 %v, want the app's %v", v.name, got, want)
		}
	}
	if got, want := rail.Size(theme.SizeNameLineSpacing), app.Size(theme.SizeNameLineSpacing); got != want {
		t.Errorf("rail line spacing %v, want %v", got, want)
	}
}

// TestRailFollowsALaterSetTheme: preferences installs a brand new theme
// object on every change. The override must not have captured the old one.
func TestRailFollowsALaterSetTheme(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(New(engine()))
	rail := &railTheme{}
	before := nrgba(rail.Color(theme.ColorNameForeground, theme.VariantDark))

	custom := engine()
	custom.BG = color.NRGBA{0x0F, 0x1A, 0x13, 0xFF} // forest night
	custom.Force, custom.Variant = true, theme.VariantDark
	a.Settings().SetTheme(New(custom))

	if got := nrgba(rail.Color(theme.ColorNameBackground, theme.VariantDark)); got == Color(RailSurface, theme.VariantDark) {
		t.Error("rail kept the old theme's surface after SetTheme")
	}
	if got := nrgba(rail.Color(theme.ColorNameForeground, theme.VariantDark)); got != before {
		t.Errorf("rail foreground drifted: %v, want %v", got, before)
	}
}

// TestPlatePaintsItsScope: the plate is the only surface paint in the app,
// and it reads the colour from whichever theme its scope resolves to.
func TestPlatePaintsItsScope(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	app := New(engine())
	a.Settings().SetTheme(app)

	plain := NewPlate()
	if got, want := nrgba(plain.fill()), nrgba(app.Color(theme.ColorNameBackground, Variant(app))); got != want {
		t.Errorf("bare plate: %v, want the window background %v", got, want)
	}

	inner := NewPlate()
	Rail(inner)
	if got, want := nrgba(inner.fill()), Color(RailSurface, Variant(app)); got != want {
		t.Errorf("plate inside a Rail: %v, want the rail surface %v", got, want)
	}
}
