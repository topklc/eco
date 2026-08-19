package app

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
)

// TestAppWearsTheTokenTheme: applySettings must install the semantic layer,
// not the bare engine theme. Everything else in M11(a) hangs off this - the
// rail override and the status dot both resolve tokens through whatever
// theme the app is wearing.
func TestAppWearsTheTokenTheme(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	applySettings(a)

	th, ok := a.Settings().Theme().(*ecotheme.Theme)
	if !ok {
		t.Fatalf("app theme is %T, want *ecotheme.Theme", a.Settings().Theme())
	}
	v := ecotheme.Variant(th)
	if got, want := th.Color(theme.ColorNameBackground, v), ecotheme.Color(ecotheme.Background, v); got != want {
		t.Errorf("window background %v, want the token %v", got, want)
	}
	// and the frozen contract still arrives through it
	if got := th.Color(fyne.ThemeColorName("irc3"), v); got == nil {
		t.Error("IRC colour names no longer resolve through the app theme")
	}
	if got := th.Size(theme.SizeNameLineSpacing); got != 0 {
		t.Errorf("line spacing %v, want render's 0", got)
	}
}

// TestRailRendersQuieter is the ThemeOverride wired up for real: inside the
// rail, the ordinary background query yields the rail surface, so the
// buffer list sits on a quieter plane than the chat without anything
// painting a colour of its own.
func TestRailRendersQuieter(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	applySettings(a)
	w := test.NewWindow(nil)
	defer w.Close()
	u := NewUI(a, w)

	railTh := u.sidePane.Theme
	appTh := a.Settings().Theme()
	v := ecotheme.Variant(appTh)

	rail := railTh.Color(theme.ColorNameBackground, v)
	if want := ecotheme.Color(ecotheme.RailSurface, v); rail != want {
		t.Errorf("rail background %v, want %v", rail, want)
	}
	if rail == appTh.Color(theme.ColorNameBackground, v) {
		t.Error("rail is painted with the chat pane's background")
	}
	// text and the IRC palette are the app's, only the surface differs
	if got, want := railTh.Color(theme.ColorNameForeground, v),
		appTh.Color(theme.ColorNameForeground, v); got != want {
		t.Errorf("rail foreground %v, want the app's %v", got, want)
	}
}

// TestStatusDotUsesTokens: the connection indicator was three NRGBA
// literals; it is now three named states resolved through the theme.
func TestStatusDotUsesTokens(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	applySettings(a)

	d := newStatusDot()
	v := ecotheme.Variant(a.Settings().Theme())
	for _, c := range []struct {
		name  string
		state fyne.ThemeColorName
	}{
		{"connected", dotOnline},
		{"connecting", dotConnecting},
		{"offline", dotOffline},
	} {
		d.set(c.state, c.name)
		if got, want := d.circle.FillColor, ecotheme.Color(c.state, v); got != want {
			t.Errorf("%s dot %v, want the token %v", c.name, got, want)
		}
	}
}
