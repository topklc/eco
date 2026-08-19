package theme

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2/theme"
)

// Contrast is the WCAG 2.1 contrast ratio between two colours, from 1
// (identical) to 21 (black on white). The palettes in tokens.go were chosen
// by it, the contrast test asserts the floors, and Ink uses it to keep a
// label readable on a fill the user picked themselves.
//
// Alpha is ignored: a ratio over a translucent colour is undefined without
// knowing what lies beneath it, and every pair we assert is opaque.
func Contrast(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// relativeLuminance is WCAG's Y: sRGB channels linearised, then weighted.
func relativeLuminance(c color.Color) float64 {
	n := nrgba(c)
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(n.R) + 0.7152*lin(n.G) + 0.0722*lin(n.B)
}

// Ink is the readable text colour over a given fill: whichever of the two
// body inks - the light palette's near-black or the dark palette's
// near-white - contrasts better. It keeps a label legible on an accent,
// error or warning fill, including one the user chose in preferences.
func Ink(over color.Color) color.NRGBA {
	dark := Color(TextPrimary, theme.VariantLight) // near-black
	light := Color(TextPrimary, theme.VariantDark) // near-white
	if Contrast(dark, over) >= Contrast(light, over) {
		return dark
	}
	return light
}
