package main

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
)

// The mIRC 16-color palette, in two variants: hue identity is shared, but
// lightness is tuned per background so every color stays readable. The
// classic values assume white paper; navy or maroon on a dark background
// are near invisible, and raw yellow or white vanish on light.
var mircPaletteLight = [16]color.Color{
	color.NRGBA{0x99, 0x99, 0x99, 0xFF}, // 0 white (rendered visible on light)
	color.NRGBA{0x00, 0x00, 0x00, 0xFF}, // 1 black
	color.NRGBA{0x00, 0x00, 0x7F, 0xFF}, // 2 navy
	color.NRGBA{0x00, 0x93, 0x00, 0xFF}, // 3 green
	color.NRGBA{0xFF, 0x00, 0x00, 0xFF}, // 4 red
	color.NRGBA{0x7F, 0x00, 0x00, 0xFF}, // 5 maroon
	color.NRGBA{0x9C, 0x00, 0x9C, 0xFF}, // 6 purple
	color.NRGBA{0xFC, 0x7F, 0x00, 0xFF}, // 7 orange
	color.NRGBA{0xB8, 0xA0, 0x00, 0xFF}, // 8 yellow (readable on light)
	color.NRGBA{0x00, 0xFC, 0x00, 0xFF}, // 9 light green
	color.NRGBA{0x00, 0x93, 0x93, 0xFF}, // 10 teal
	color.NRGBA{0x00, 0xFF, 0xFF, 0xFF}, // 11 cyan
	color.NRGBA{0x00, 0x00, 0xFC, 0xFF}, // 12 blue
	color.NRGBA{0xFF, 0x00, 0xFF, 0xFF}, // 13 pink
	color.NRGBA{0x7F, 0x7F, 0x7F, 0xFF}, // 14 grey
	color.NRGBA{0xD2, 0xD2, 0xD2, 0xFF}, // 15 light grey
}

var mircPaletteDark = [16]color.Color{
	color.NRGBA{0xF2, 0xF2, 0xF2, 0xFF}, // 0 white
	color.NRGBA{0x5C, 0x63, 0x70, 0xFF}, // 1 black (visible slate)
	color.NRGBA{0x5C, 0x7E, 0xCC, 0xFF}, // 2 navy
	color.NRGBA{0x5C, 0xCC, 0x5C, 0xFF}, // 3 green
	color.NRGBA{0xFF, 0x5C, 0x5C, 0xFF}, // 4 red
	color.NRGBA{0xCC, 0x66, 0x66, 0xFF}, // 5 maroon
	color.NRGBA{0xC6, 0x78, 0xDD, 0xFF}, // 6 purple
	color.NRGBA{0xFF, 0x94, 0x33, 0xFF}, // 7 orange
	color.NRGBA{0xE6, 0xD2, 0x4C, 0xFF}, // 8 yellow
	color.NRGBA{0x6A, 0xE8, 0x6A, 0xFF}, // 9 light green
	color.NRGBA{0x4D, 0xB8, 0xB8, 0xFF}, // 10 teal
	color.NRGBA{0x66, 0xE6, 0xE6, 0xFF}, // 11 cyan
	color.NRGBA{0x66, 0xB2, 0xFF, 0xFF}, // 12 blue
	color.NRGBA{0xE8, 0x73, 0xD3, 0xFF}, // 13 pink
	color.NRGBA{0x99, 0x99, 0x99, 0xFF}, // 14 grey
	color.NRGBA{0xCC, 0xCC, 0xCC, 0xFF}, // 15 light grey
}

var ircNames = [16]fyne.ThemeColorName{
	"irc0", "irc1", "irc2", "irc3", "irc4", "irc5", "irc6", "irc7",
	"irc8", "irc9", "irc10", "irc11", "irc12", "irc13", "irc14", "irc15",
}

var ircColorIndex = func() map[fyne.ThemeColorName]int {
	m := make(map[fyne.ThemeColorName]int, 16)
	for i, n := range ircNames {
		m[n] = i
	}
	return m
}()

func ircColorName(n uint8) fyne.ThemeColorName {
	return ircNames[n]
}

// lookupIRCColor resolves the synthetic "ircN" theme color names for the
// resolved theme variant.
func lookupIRCColor(name fyne.ThemeColorName, variant fyne.ThemeVariant) (color.Color, bool) {
	// one map hit: this runs for every color query of every paint
	n, ok := ircColorIndex[name]
	if !ok {
		return nil, false
	}
	if variant == theme.VariantDark {
		return mircPaletteDark[n], true
	}
	return mircPaletteLight[n], true
}

// Display options, written by applySettings and read on the UI thread.
var (
	showTimestamps = true
	colorfulNicks  = true
	selfNickColor  = fyne.ThemeColorName("irc3")
	timeLayout     = "15:04"
)

// Readable palette subset used for per-nick coloring.
var nickPaletteIdx = []uint8{2, 3, 4, 6, 7, 10, 12, 13}

func nickColorName(nick string) fyne.ThemeColorName {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(nick)))
	return ircColorName(nickPaletteIdx[h.Sum32()%uint32(len(nickPaletteIdx))])
}

var namedNickColors = map[string]fyne.ThemeColorName{
	"Green": "irc3", "Blue": "irc12", "Orange": "irc7",
	"Purple": "irc6", "Red": "irc4", "Teal": "irc10", "Pink": "irc13",
}

// richSegments renders IRC-formatted text (colors, bold, italic, underline,
// monospace) into RichText segments. All segments are inline.
func richSegments(text string, base fyne.ThemeColorName, italic bool) []widget.RichTextSegment {
	chunks := ircfmt.Split(text)
	segs := make([]widget.RichTextSegment, 0, len(chunks))
	for _, chunk := range chunks {
		st := widget.RichTextStyle{ColorName: base, Inline: true}
		st.TextStyle.Bold = chunk.Bold
		st.TextStyle.Italic = chunk.Italic || italic
		st.TextStyle.Underline = chunk.Underline
		st.TextStyle.Monospace = chunk.Monospace
		if chunk.ForegroundColor.IsSet && chunk.ForegroundColor.Value < 16 {
			st.ColorName = ircColorName(chunk.ForegroundColor.Value)
		}
		segs = append(segs, &widget.TextSegment{Text: chunk.Content, Style: st})
	}
	if len(segs) == 0 {
		segs = append(segs, &widget.TextSegment{Text: "", Style: widget.RichTextStyle{ColorName: base, Inline: true}})
	}
	return segs
}

func parseHexColor(s string) (color.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return nil, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return nil, false
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}, true
}

func colorToHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
