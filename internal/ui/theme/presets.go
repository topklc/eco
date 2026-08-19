package theme

import (
	"image/color"

	"fyne.io/fyne/v2/theme"
)

// The preset strips offered in Preferences. These are not tokens: they are
// the choices a user may pick from, and only one of each ever becomes a
// token value. They live here so that every colour the app can display is
// declared in this package, and so each default stays literally the same
// value as the token it overrides.
//
// The bands are the designer's: a matched-lightness hue wheel for accents,
// a both-backgrounds-safe band for nicks, and a black-to-white lightness
// ramp with two whisper tints for backgrounds.

// AccentPresets are the one-click accent choices; the first is the token
// default, the logo's leaf green.
func AccentPresets() []color.Color {
	return []color.Color{
		Color(Accent, theme.VariantLight),   // logo green (the default)
		color.NRGBA{0x2E, 0x7C, 0xD6, 0xFF}, // ocean blue
		color.NRGBA{0x14, 0x90, 0x7A, 0xFF}, // deep teal
		color.NRGBA{0x87, 0x57, 0xD1, 0xFF}, // iris purple
		color.NRGBA{0xC2, 0x4B, 0x8E, 0xFF}, // orchid pink
		color.NRGBA{0xD6, 0x45, 0x45, 0xFF}, // signal red
		color.NRGBA{0xC7, 0x74, 0x00, 0xFF}, // burnt amber
		color.NRGBA{0x6E, 0x7B, 0x85, 0xFF}, // steel grey
	}
}

// NickPresets are the own-nick colour choices: readable on both palettes.
func NickPresets() []color.Color {
	return []color.Color{
		color.NRGBA{0x3F, 0xA3, 0x3F, 0xFF}, // meadow green
		color.NRGBA{0x3D, 0x7D, 0xD6, 0xFF}, // clear blue
		color.NRGBA{0x2A, 0xA1, 0x98, 0xFF}, // lagoon teal
		color.NRGBA{0x9B, 0x59, 0xD0, 0xFF}, // soft violet
		color.NRGBA{0xD4, 0x56, 0x9B, 0xFF}, // rose pink
		color.NRGBA{0xD6, 0x45, 0x45, 0xFF}, // brick red
		color.NRGBA{0xC9, 0x7A, 0x16, 0xFF}, // copper orange
		color.NRGBA{0x6C, 0x7A, 0x89, 0xFF}, // slate grey
	}
}

// BackgroundPresets are the window background choices, darkest first. The
// dark default sits in the ramp; whichever is picked, the rest of the
// surface family steps off it (see elevation), so the rail stays quieter
// and dialogs stay raised no matter what the user chooses.
func BackgroundPresets() []color.Color {
	return []color.Color{
		color.NRGBA{0x0A, 0x0A, 0x0A, 0xFF},   // pitch black
		Color(Background, theme.VariantDark),  // soft black (the dark default)
		color.NRGBA{0x20, 0x20, 0x20, 0xFF},   // graphite
		color.NRGBA{0x10, 0x18, 0x20, 0xFF},   // midnight blue
		color.NRGBA{0x0F, 0x1A, 0x13, 0xFF},   // forest night
		color.NRGBA{0xF6, 0xF1, 0xE4, 0xFF},   // warm parchment
		Color(Background, theme.VariantLight), // paper (the light default)
		color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},   // pure white
	}
}
