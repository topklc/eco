package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/toprakkilic/eco/internal/render"
)

// selfNick is render's colour name for the user's own nick; like the ircN
// names it is answered by render, never here.
const selfNick fyne.ThemeColorName = "nickself"

// Theme is the app theme: the semantic tokens of this package wrapped
// around internal/render's engine theme.
//
// The division of labour is exact. render answers the IRC colour names
// (ircN, ircxN, nickself), ships the bold monospace faces Fyne lacks and
// pins line spacing to zero for art alignment - the frozen contract the
// golden suite renders through, inherited here untouched via Font and Size.
// Everything the app chrome paints - surfaces, text roles, the accent,
// state, pills, overlays - is answered from the token tables instead, so
// no app colour is a literal buried in a widget any more.
//
// The user's own choices, carried on the embedded render.Theme, still win:
// a custom background moves every surface with it (the palette's elevation
// steps are preserved), and a custom accent flows into links, focus,
// selection and the mention pill from one place.
type Theme struct {
	*render.Theme
}

// New wraps an engine theme. base carries the user's variant, font scale,
// background, accent and own-nick choices, exactly as before.
func New(base *render.Theme) *Theme { return &Theme{Theme: base} }

var _ fyne.Theme = (*Theme)(nil)

// Color answers every colour the app can ask for. Order matters: our own
// tokens first, then the frozen render contract, then Fyne's standard
// names mapped onto tokens, then - for anything a future Fyne adds - the
// wrapped theme's own answer.
func (t *Theme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.Force {
		v = t.Variant
	}

	if IsToken(n) {
		return t.token(n, v)
	}

	// the frozen contract: IRC palette entries and the own-nick colour are
	// render's answer, byte for byte. Nothing here may shadow them.
	if _, ok := render.LookupColor(n, v); ok {
		return t.Theme.Color(n, v)
	}
	if n == selfNick {
		return t.Theme.Color(n, v)
	}

	switch n {
	// surfaces
	case theme.ColorNameBackground:
		return t.token(Background, v)
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground,
		theme.ColorNameHeaderBackground, theme.ColorNameButton,
		theme.ColorNameDisabledButton, theme.ColorNameScrollBarBackground:
		return t.token(Surface, v)
	case theme.ColorNameInputBackground:
		return t.token(InputBackground, v)
	case theme.ColorNameSeparator, theme.ColorNameInputBorder,
		theme.ColorNameInnerWindowBorder, theme.ColorNameInnerWindowBorderInactive:
		return t.token(Border, v)

	// text
	case theme.ColorNameForeground:
		return t.token(TextPrimary, v)
	case theme.ColorNamePlaceHolder:
		return t.token(TextMuted, v)
	// the scroll bar is a faint mark on the page, not a control with a fill
	case theme.ColorNameDisabled, theme.ColorNameScrollBar:
		return t.token(TextFaint, v)

	// the accent
	case theme.ColorNamePrimary:
		return t.token(Accent, v)
	case theme.ColorNameHyperlink:
		return t.token(AccentText, v)
	case theme.ColorNameForegroundOnPrimary:
		return t.token(OnAccent, v)

	// state
	case theme.ColorNameSuccess:
		return t.token(Success, v)
	case theme.ColorNameWarning:
		return t.token(Warning, v)
	case theme.ColorNameError:
		return t.token(Danger, v)
	case theme.ColorNameForegroundOnSuccess:
		return Ink(t.token(Success, v))
	case theme.ColorNameForegroundOnWarning:
		return Ink(t.token(Warning, v))
	case theme.ColorNameForegroundOnError:
		return Ink(t.token(Danger, v))

	// state overlays
	case theme.ColorNameHover:
		return t.token(HoverOverlay, v)
	// a press is the selection wash arriving early: one accent tint for
	// "this is the thing you mean", rather than Fyne's separate grey flash
	case theme.ColorNameSelection, theme.ColorNamePressed:
		return t.token(SelectionOverlay, v)
	case theme.ColorNameFocus:
		return t.token(FocusOverlay, v)
	}

	// ColorNameShadow lands here on purpose: a drop shadow is not a surface
	// role and Fyne's variant-independent black-at-alpha is already right.
	return t.Theme.Color(n, v)
}

// token resolves one semantic token for this theme: the table value with
// the user's customisations folded in. Every colour the app paints comes
// through this one function.
func (t *Theme) token(tok fyne.ThemeColorName, v fyne.ThemeVariant) color.NRGBA {
	base := Color(tok, v)

	// a custom window background carries the whole surface family with it,
	// keeping the palette's own elevation steps
	if t.BG != nil {
		if step, ok := elevationStep(tok, v); ok {
			return shade(t.BG, step)
		}
	}

	// a custom accent re-tints everything the accent owns, at the alpha the
	// palette chose for each role
	if t.Accent != nil {
		switch tok {
		case Accent, AccentText, MentionPill, SelectionOverlay, FocusOverlay:
			return withAlpha(t.Accent, base.A)
		case OnAccent:
			return Ink(withAlpha(t.Accent, 0xFF))
		}
	}

	return base
}

// Variant reports the variant a widget should paint in under the given
// theme. A widget that resolves a token itself must use this rather than
// Settings().ThemeVariant(): the user can force light or dark in
// preferences against the system, and only the theme knows that.
func Variant(th fyne.Theme) fyne.ThemeVariant {
	switch t := th.(type) {
	case *Theme:
		if t.Force {
			return t.Variant
		}
	case *railTheme:
		return Variant(t.app())
	}
	return systemVariant()
}
