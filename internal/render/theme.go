package render

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Theme carries everything the render engine's output depends on: the IRC
// palettes, the bold monospace faces Fyne does not ship, the pinned
// light/dark variant, font scaling and the tight line spacing that art
// alignment needs. The golden suite renders through it, so a change here
// is a change to the frozen render contract.
//
// It is not the app's theme. internal/ui/theme wraps it with the semantic
// token layer every window surface and text role is painted from, and hands
// the IRC colour names, the faces and the spacing rule straight back here.
type Theme struct {
	fyne.Theme
	Variant   fyne.ThemeVariant
	Force     bool
	FontScale float32
	BG        color.Color // custom background, nil = theme default
	Accent    color.Color // custom accent, nil = the logo green
	SelfNick  color.Color // custom own-nick color, nil = named choice
}

func (t *Theme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.Force {
		v = t.Variant
	}
	if c, ok := LookupColor(n, v); ok {
		return c
	}
	dark := v == theme.VariantDark

	// the user's own nick color when customized past the named choices
	if n == fyne.ThemeColorName("nickself") && t.SelfNick != nil {
		return t.SelfNick
	}

	// accent: the logo's leaf green unless the user picked their own
	if t.Accent != nil {
		r, g, b, _ := t.Accent.RGBA()
		base := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xFF}
		switch n {
		case theme.ColorNamePrimary, theme.ColorNameHyperlink:
			return base
		case theme.ColorNameFocus:
			base.A = 0xA8
			return base
		case theme.ColorNameSelection:
			base.A = 0x38
			return base
		}
	} else {
		switch n {
		case theme.ColorNamePrimary:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0xFF}
			}
			return color.NRGBA{0x42, 0x9B, 0x00, 0xFF}
		case theme.ColorNameHyperlink:
			if dark {
				return color.NRGBA{0x8C, 0xD1, 0x52, 0xFF}
			}
			return color.NRGBA{0x33, 0x7A, 0x00, 0xFF}
		case theme.ColorNameFocus:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0xA8}
			}
			return color.NRGBA{0x42, 0x9B, 0x00, 0xA8}
		case theme.ColorNameSelection:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0x38}
			}
			return color.NRGBA{0x40, 0xA0, 0x00, 0x33}
		}
	}

	// The window background is the paper the corpus is rendered over, so it
	// is part of what the goldens capture and stays pinned here: a soft
	// black, pure neutral, no blue tint. Every other app surface — dialogs,
	// menus, the composer, the rail — was promoted to a semantic token in
	// internal/ui/theme at M11(a), and the app theme answers those long
	// before this method sees them.
	if n == theme.ColorNameBackground {
		if t.BG != nil {
			return t.BG
		}
		if dark {
			return color.NRGBA{0x14, 0x14, 0x14, 0xFF}
		}
	}
	return t.Theme.Color(n, v)
}

func (t *Theme) Size(n fyne.ThemeSizeName) float32 {
	// tight message spacing: the default 4px gap between paragraphs made
	// chat airy and broke multi-line ASCII art (irssi uses none)
	if n == theme.SizeNameLineSpacing {
		return 0
	}
	s := t.Theme.Size(n)
	if n == theme.SizeNameText || n == theme.SizeNameSubHeadingText || n == theme.SizeNameHeadingText {
		return s * t.FontScale
	}
	return s
}

// Fyne ships only a regular monospace face and ignores the Bold flag for
// monospace text entirely, so IRC bold in the server buffer silently
// rendered regular. Ship the bold faces ourselves.
//
//go:embed assets/DejaVuSansMono-Bold.ttf
var monoBoldTTF []byte

//go:embed assets/DejaVuSansMono-BoldOblique.ttf
var monoBoldObliqueTTF []byte

var (
	monoBold        = fyne.NewStaticResource("DejaVuSansMono-Bold.ttf", monoBoldTTF)
	monoBoldOblique = fyne.NewStaticResource("DejaVuSansMono-BoldOblique.ttf", monoBoldObliqueTTF)
)

func (t *Theme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Monospace && style.Bold {
		if style.Italic {
			return monoBoldOblique
		}
		return monoBold
	}
	return t.Theme.Font(style)
}
