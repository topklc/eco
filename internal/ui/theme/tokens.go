// Package theme is eco's semantic colour layer: one named token per
// surface and text role the app paints, defined once per variant.
//
// The split from internal/render is deliberate and load-bearing. render
// owns the IRC palettes (ircN / ircxN), the shipped bold-monospace faces
// and the zero line spacing that ASCII-art alignment depends on — the
// frozen render contract the 44 golden PNGs are byte-compared against.
// This package owns everything else: window and pane surfaces, text roles,
// the single accent, state colours, pills and overlays. Nothing declared
// here can move a golden pixel, because nothing here sits on the render
// path: Theme wraps render.Theme and hands every IRC colour name straight
// back to it (blueprint M11(a), R-13).
//
// Tokens are plain fyne.ThemeColorName values, so any widget — and any
// widget.RichTextStyle.ColorName, which is how the Timeline will paint its
// faint timestamp gutter — can name one and let the theme resolve it:
//
//	th := w.Theme()
//	c := th.Color(ecotheme.TextFaint, ecotheme.Variant(th))
//
// Asking the theme (rather than this package's Color) is what makes a
// per-container ThemeOverride work: inside the Rail the very same query
// yields the rail's quieter surface. See rail.go. Variant, not
// Settings().ThemeVariant(), because the user can pin light or dark.
package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The semantic tokens. Names are prefixed so they can never collide with
// Fyne's own colour names or with render's "ircN" / "nickself".
const (
	// Surfaces, quietest to most raised.
	RailSurface     fyne.ThemeColorName = "ecoRailSurface"     // the buffer rail: one step DOWN from the chat
	Background      fyne.ThemeColorName = "ecoBackground"      // the window and the chat pane
	InputBackground fyne.ThemeColorName = "ecoInputBackground" // the composer and every entry field
	Surface         fyne.ThemeColorName = "ecoSurface"         // one step UP: dialogs, menus, popups
	Border          fyne.ThemeColorName = "ecoBorder"          // dividers, separators, entry outlines

	// Text roles.
	TextPrimary fyne.ThemeColorName = "ecoTextPrimary" // body text
	TextMuted   fyne.ThemeColorName = "ecoTextMuted"   // timestamps, placeholders, secondary labels
	TextFaint   fyne.ThemeColorName = "ecoTextFaint"   // disabled text, the timestamp gutter, hints

	// The one accent.
	Accent     fyne.ThemeColorName = "ecoAccent"     // accent as a fill: primary buttons, pills, dots
	AccentText fyne.ThemeColorName = "ecoAccentText" // accent as text: links, accent labels
	OnAccent   fyne.ThemeColorName = "ecoOnAccent"   // ink drawn ON an accent fill

	// State.
	Success fyne.ThemeColorName = "ecoSuccess" // connected
	Warning fyne.ThemeColorName = "ecoWarning" // connecting, highlighted rows
	Danger  fyne.ThemeColorName = "ecoDanger"  // disconnected, errors

	// Attention pills (blueprint §4.1: grey = unread, accent = mentions).
	UnreadPill  fyne.ThemeColorName = "ecoUnreadPill"
	MentionPill fyne.ThemeColorName = "ecoMentionPill"

	// Translucent state overlays, painted over whatever is beneath.
	HoverOverlay     fyne.ThemeColorName = "ecoHoverOverlay"
	SelectionOverlay fyne.ThemeColorName = "ecoSelectionOverlay"
	FocusOverlay     fyne.ThemeColorName = "ecoFocusOverlay"
)

// Tokens lists every token, in declaration order. The table test walks it.
var Tokens = []fyne.ThemeColorName{
	RailSurface, Background, InputBackground, Surface, Border,
	TextPrimary, TextMuted, TextFaint,
	Accent, AccentText, OnAccent,
	Success, Warning, Danger,
	UnreadPill, MentionPill,
	HoverOverlay, SelectionOverlay, FocusOverlay,
}

// darkTokens is the dark palette: pure neutrals, no blue tint, on a soft
// black window — never #000, which turns OLED text into halos and loses
// every elevation step below the background.
//
// Background, Accent, SelectionOverlay and FocusOverlay repeat the values
// render.Theme already answers, so promoting the surfaces out of render
// leaves the shipped dark app pixel-identical where it mattered.
var darkTokens = map[fyne.ThemeColorName]color.NRGBA{
	RailSurface:     {0x0F, 0x0F, 0x0F, 0xFF},
	Background:      {0x14, 0x14, 0x14, 0xFF},
	InputBackground: {0x1B, 0x1B, 0x1B, 0xFF},
	Surface:         {0x23, 0x23, 0x23, 0xFF},
	Border:          {0x33, 0x33, 0x33, 0xFF},

	TextPrimary: {0xE8, 0xE8, 0xE4, 0xFF},
	TextMuted:   {0x9C, 0x9C, 0x96, 0xFF},
	TextFaint:   {0x6E, 0x6E, 0x69, 0xFF},

	Accent:     {0x7C, 0xC9, 0x40, 0xFF},
	AccentText: {0x8C, 0xD1, 0x52, 0xFF},
	OnAccent:   {0x10, 0x20, 0x0A, 0xFF},

	Success: {0x4F, 0xC2, 0x4F, 0xFF},
	Warning: {0xE8, 0xA3, 0x3A, 0xFF},
	Danger:  {0xE8, 0x77, 0x6B, 0xFF},

	UnreadPill:  {0x30, 0x30, 0x30, 0xFF},
	MentionPill: {0x7C, 0xC9, 0x40, 0xFF},

	HoverOverlay:     {0xFF, 0xFF, 0xFF, 0x14},
	SelectionOverlay: {0x7C, 0xC9, 0x40, 0x38},
	FocusOverlay:     {0x7C, 0xC9, 0x40, 0xA8},
}

// lightTokens is the light palette: warm-neutral paper, never #FFF — a
// pure-white window has nowhere left to raise a dialog to, and glares.
var lightTokens = map[fyne.ThemeColorName]color.NRGBA{
	RailSurface:     {0xED, 0xED, 0xE8, 0xFF},
	Background:      {0xF6, 0xF6, 0xF2, 0xFF},
	InputBackground: {0xFA, 0xFA, 0xF6, 0xFF},
	Surface:         {0xFC, 0xFC, 0xF9, 0xFF},
	Border:          {0xD9, 0xD9, 0xD2, 0xFF},

	TextPrimary: {0x1C, 0x1C, 0x1A, 0xFF},
	TextMuted:   {0x5F, 0x5F, 0x5A, 0xFF},
	TextFaint:   {0x85, 0x85, 0x7E, 0xFF},

	Accent:     {0x42, 0x9B, 0x00, 0xFF},
	AccentText: {0x33, 0x7A, 0x00, 0xFF},
	OnAccent:   {0x17, 0x17, 0x12, 0xFF},

	Success: {0x23, 0x7A, 0x23, 0xFF},
	Warning: {0x96, 0x59, 0x00, 0xFF},
	Danger:  {0xB0, 0x2A, 0x1C, 0xFF},

	UnreadPill:  {0xD8, 0xD8, 0xD0, 0xFF},
	MentionPill: {0x42, 0x9B, 0x00, 0xFF},

	HoverOverlay:     {0x00, 0x00, 0x00, 0x0F},
	SelectionOverlay: {0x40, 0xA0, 0x00, 0x33},
	FocusOverlay:     {0x42, 0x9B, 0x00, 0xA8},
}

// Color is the default value of one token in one variant, before any user
// customisation. Widgets should ask their theme instead — Theme.Color folds
// in the user's chosen background and accent, and a ThemeOverride can
// answer differently — but tests, previews and the token tables want the
// unadorned value.
func Color(tok fyne.ThemeColorName, v fyne.ThemeVariant) color.NRGBA {
	if v == theme.VariantLight {
		return lightTokens[tok]
	}
	return darkTokens[tok]
}

// IsToken reports whether a colour name is one of ours.
func IsToken(n fyne.ThemeColorName) bool {
	_, ok := darkTokens[n]
	return ok
}

// elevation is how far each surface sits from the window background, as a
// per-channel step: {light, dark}. It exists so a user-chosen background
// keeps its family — pick warm parchment and the dialogs, the composer and
// the rail all move with it instead of reverting to grey.
var elevation = map[fyne.ThemeColorName][2]int{
	Background:      {0, 0},
	Surface:         {+6, +15},
	InputBackground: {+4, +7},
	RailSurface:     {-9, -5},
}

// elevationStep is how far a surface token sits from the window background
// in the given variant, and whether it is a surface at all.
func elevationStep(tok fyne.ThemeColorName, v fyne.ThemeVariant) (int, bool) {
	step, ok := elevation[tok]
	if !ok {
		return 0, false
	}
	if v == theme.VariantLight {
		return step[0], true
	}
	return step[1], true
}

// nrgba is the un-premultiplied form of any colour. color.Color.RGBA()
// premultiplies by alpha, which silently darkens every translucent colour
// it round-trips; the model conversion is the correct way back.
func nrgba(c color.Color) color.NRGBA {
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// shade lightens (delta > 0) or darkens a colour by a flat per-channel
// step, clamped. Alpha is preserved.
func shade(c color.Color, delta int) color.NRGBA {
	n := nrgba(c)
	clamp := func(v uint8) uint8 {
		s := int(v) + delta
		if s < 0 {
			return 0
		}
		if s > 0xFF {
			return 0xFF
		}
		return uint8(s)
	}
	return color.NRGBA{clamp(n.R), clamp(n.G), clamp(n.B), n.A}
}

// withAlpha re-tints: the given colour at the given opacity. Focus rings,
// selection washes and pills are the accent at a fixed alpha, so a custom
// accent flows into all of them from one place.
func withAlpha(c color.Color, a uint8) color.NRGBA {
	n := nrgba(c)
	n.A = a
	return n
}
