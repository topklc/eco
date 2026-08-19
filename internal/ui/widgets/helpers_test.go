package widgets

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/render"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// testApp is a headless app wearing the theme the shipped one wears: the
// semantic tokens over the render engine. The bare test theme knows neither
// the IRC colour names nor the bold monospace faces, and a missing face does
// not just look wrong — it measures wrong, which would make every height in
// this package a lie.
func testApp(t testing.TB) fyne.App {
	t.Helper()
	a := test.NewApp()
	a.Settings().SetTheme(ecotheme.New(&render.Theme{
		Theme:     theme.DefaultTheme(),
		Force:     true,
		Variant:   theme.VariantDark,
		FontScale: 1,
	}))
	t.Cleanup(a.Quit)
	return a
}

// at is a fixed clock the row tests build timestamps from.
var at = time.Date(2025, 6, 2, 14, 0, 0, 0, time.UTC)

// msg is a plain chat line n seconds into the fixed clock.
func msg(nick, text string, offset time.Duration) model.Line {
	return model.Line{Time: at.Add(offset), Nick: nick, Text: text}
}
