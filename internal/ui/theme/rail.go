package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Rail wraps content so everything inside renders one step quieter than
// the chat pane: the buffer sidebar reads as background, not as another
// competing panel (blueprint §4.1).
//
// The mechanism is Fyne's per-container theme override (container.Theme-
// Override, since 2.5), not a hand-painted rectangle. Two things make that
// work. Widgets resolve colours through BaseWidget.Theme(), which returns
// the overridden theme for anything inside the scope; and widget.List
// re-applies the scope to rows it creates later
// (fyne/widget/list.go, createItemAndApplyThemeScope), so rail rows built
// long after this call still theme correctly. The only paint is Plate, and
// even that asks for the ordinary background colour — the override is what
// makes the answer different.
//
// Hide and Show the returned widget, not the content: a Border layout gives
// space to whatever is visible, and the wrapper is what it can see.
func Rail(content fyne.CanvasObject) *container.ThemeOverride {
	return container.NewThemeOverride(container.NewStack(NewPlate(), content), &railTheme{})
}

// railTheme is the app theme with exactly one substitution: a request for
// the window background is answered with the rail surface.
//
// It looks the current app theme up per query rather than holding one, so
// that changing theme in preferences — which installs a whole new theme
// object — reaches inside the override too.
type railTheme struct{}

var _ fyne.Theme = (*railTheme)(nil)

func (r *railTheme) app() fyne.Theme {
	if a := fyne.CurrentApp(); a != nil {
		if th := a.Settings().Theme(); th != nil {
			return th
		}
	}
	return theme.DefaultTheme()
}

func (r *railTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	th := r.app()
	if n == theme.ColorNameBackground {
		n = RailSurface
	}
	if _, ours := th.(*Theme); !ours && IsToken(n) {
		// no app theme of ours (a bare test app) knows our token names
		return Color(n, v)
	}
	return th.Color(n, v)
}

func (r *railTheme) Font(s fyne.TextStyle) fyne.Resource     { return r.app().Font(s) }
func (r *railTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return r.app().Icon(n) }
func (r *railTheme) Size(n fyne.ThemeSizeName) float32       { return r.app().Size(n) }

// Plate is an opaque fill of its own scope's background colour, and the
// only thing in the app that paints a pane's surface. Inside a Rail it
// comes out as the rail surface; anywhere else it is the window background,
// which is invisible — exactly the intent.
type Plate struct {
	widget.BaseWidget
}

func NewPlate() *Plate {
	p := &Plate{}
	p.ExtendBaseWidget(p)
	return p
}

func (p *Plate) CreateRenderer() fyne.WidgetRenderer {
	r := &plateRenderer{plate: p, rect: canvas.NewRectangle(p.fill())}
	return r
}

// fill asks this widget's theme — which is the override's when it has one —
// for the ordinary background colour.
func (p *Plate) fill() color.Color {
	th := p.Theme()
	return th.Color(theme.ColorNameBackground, Variant(th))
}

func systemVariant() fyne.ThemeVariant {
	if a := fyne.CurrentApp(); a != nil {
		return a.Settings().ThemeVariant()
	}
	return theme.VariantDark
}

type plateRenderer struct {
	plate *Plate
	rect  *canvas.Rectangle
}

func (r *plateRenderer) Destroy() {}

func (r *plateRenderer) Layout(s fyne.Size) { r.rect.Resize(s) }

// MinSize is zero: a plate never asks for room, it only fills what it is
// given.
func (r *plateRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *plateRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect} }

func (r *plateRenderer) Refresh() {
	r.rect.FillColor = r.plate.fill()
	r.rect.Refresh()
}
