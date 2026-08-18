package main

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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

// extendedPalette is the modern-IRC extended color table for codes 16-98
// (https://modern.ircdocs.horse/formatting.html); these are exact colors
// shared by both theme variants.
var extendedPalette = [83]color.Color{
	rgb(0x470000), rgb(0x472100), rgb(0x474700), rgb(0x324700), rgb(0x004700), rgb(0x00472C),
	rgb(0x004747), rgb(0x002747), rgb(0x000047), rgb(0x2E0047), rgb(0x470047), rgb(0x47002A),
	rgb(0x740000), rgb(0x743A00), rgb(0x747400), rgb(0x517400), rgb(0x007400), rgb(0x007449),
	rgb(0x007474), rgb(0x004074), rgb(0x000074), rgb(0x4B0074), rgb(0x740074), rgb(0x740045),
	rgb(0xB50000), rgb(0xB56300), rgb(0xB5B500), rgb(0x7DB500), rgb(0x00B500), rgb(0x00B571),
	rgb(0x00B5B5), rgb(0x0063B5), rgb(0x0000B5), rgb(0x7500B5), rgb(0xB500B5), rgb(0xB5006B),
	rgb(0xFF0000), rgb(0xFF8C00), rgb(0xFFFF00), rgb(0xB2FF00), rgb(0x00FF00), rgb(0x00FFA0),
	rgb(0x00FFFF), rgb(0x008CFF), rgb(0x0000FF), rgb(0xA500FF), rgb(0xFF00FF), rgb(0xFF0098),
	rgb(0xFF5959), rgb(0xFFB459), rgb(0xFFFF71), rgb(0xCFFF60), rgb(0x6FFF6F), rgb(0x65FFC9),
	rgb(0x6DFFFF), rgb(0x59B4FF), rgb(0x5959FF), rgb(0xC459FF), rgb(0xFF66FF), rgb(0xFF59BC),
	rgb(0xFF9C9C), rgb(0xFFD39C), rgb(0xFFFF9C), rgb(0xE2FF9C), rgb(0x9CFF9C), rgb(0x9CFFDB),
	rgb(0x9CFFFF), rgb(0x9CD3FF), rgb(0x9C9CFF), rgb(0xDC9CFF), rgb(0xFF9CFF), rgb(0xFF94D3),
	rgb(0x000000), rgb(0x131313), rgb(0x282828), rgb(0x363636), rgb(0x4D4D4D), rgb(0x656565),
	rgb(0x818181), rgb(0x9F9F9F), rgb(0xBCBCBC), rgb(0xE2E2E2), rgb(0xFFFFFF),
}

func rgb(v uint32) color.Color {
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}
}

// classicPalette is the untuned mIRC 16 palette, used for backgrounds
// where the true color matters more than text readability.
var classicPalette = [16]color.Color{
	rgb(0xFFFFFF), rgb(0x000000), rgb(0x00007F), rgb(0x009300),
	rgb(0xFF0000), rgb(0x7F0000), rgb(0x9C009C), rgb(0xFC7F00),
	rgb(0xFFFF00), rgb(0x00FC00), rgb(0x009393), rgb(0x00FFFF),
	rgb(0x0000FC), rgb(0xFF00FF), rgb(0x7F7F7F), rgb(0xD2D2D2),
}

var ircNames = func() [99]fyne.ThemeColorName {
	var names [99]fyne.ThemeColorName
	for i := range names {
		names[i] = fyne.ThemeColorName("irc" + strconv.Itoa(i))
	}
	return names
}()

// ircColorIndex also carries "ircxN" names (index+100): the exact palette
// colors, used for text drawn over a painted background where the
// readability-tuned variants would look wrong.
var ircColorIndex = func() map[fyne.ThemeColorName]int {
	m := make(map[fyne.ThemeColorName]int, 2*len(ircNames))
	for i, n := range ircNames {
		m[n] = i
		m[fyne.ThemeColorName("ircx"+strconv.Itoa(i))] = i + 100
	}
	return m
}()

func ircExactColorName(n uint8) fyne.ThemeColorName {
	return fyne.ThemeColorName("ircx" + strconv.Itoa(int(n)))
}

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
	if n >= 100 {
		return ircBackgroundColor(uint8(n - 100)), true
	}
	if n >= 16 {
		return extendedPalette[n-16], true
	}
	if variant == theme.VariantDark {
		return mircPaletteDark[n], true
	}
	return mircPaletteLight[n], true
}

// ircBackgroundColor is the exact color for a background code, where
// fidelity beats readability tuning.
func ircBackgroundColor(n uint8) color.Color {
	if n < 16 {
		return classicPalette[n]
	}
	if int(n)-16 < len(extendedPalette) {
		return extendedPalette[n-16]
	}
	return color.Transparent
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
// monospace) into RichText segments. All segments are inline. When mention
// is non-nil, "@nick" tokens it recognizes become tappable links.
func richSegments(chunks []ircfmt.FormattedSubstring, base fyne.ThemeColorName, italic bool, mention func(string) func()) []widget.RichTextSegment {
	segs := make([]widget.RichTextSegment, 0, len(chunks))
	for _, chunk := range chunks {
		st := widget.RichTextStyle{ColorName: base, Inline: true}
		st.TextStyle.Bold = chunk.Bold
		st.TextStyle.Italic = chunk.Italic || italic
		st.TextStyle.Underline = chunk.Underline
		st.TextStyle.Monospace = chunk.Monospace
		fgSet := chunk.ForegroundColor.IsSet && chunk.ForegroundColor.Value < 99
		if fgSet {
			st.ColorName = ircColorName(chunk.ForegroundColor.Value)
		}
		if chunk.BackgroundColor.IsSet && chunk.BackgroundColor.Value < 99 {
			// over a real background the exact palette color is right;
			// the readability-tuned variant is calibrated for the theme's
			// own background, not this one
			if fgSet {
				st.ColorName = ircExactColorName(chunk.ForegroundColor.Value)
			}
			segs = append(segs, &bgTextSegment{
				TextSegment: widget.TextSegment{Text: chunk.Content, Style: st},
				bg:          ircBackgroundColor(chunk.BackgroundColor.Value),
			})
			continue
		}
		segs = append(segs, mentionSegments(chunk.Content, st, mention)...)
	}
	// a background segment must never end the line: RichText resizes the
	// final non-text object of a row to the full line width, which made
	// the last colored cell smear to the right edge
	if len(segs) == 0 || func() bool { _, bg := segs[len(segs)-1].(*bgTextSegment); return bg }() {
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

// bgTextSegment is a text segment with a painted background: RichText has
// no native background support, so the visual is a rectangle stacked
// behind the plain text visual.
type bgTextSegment struct {
	widget.TextSegment
	bg color.Color
}

func (s *bgTextSegment) Visual() fyne.CanvasObject {
	txt := s.TextSegment.Visual()
	rect := canvas.NewRectangle(s.bg)
	return container.NewStack(rect, txt)
}

func (s *bgTextSegment) Update(o fyne.CanvasObject) {
	if c, ok := o.(*fyne.Container); ok && len(c.Objects) == 2 {
		if r, ok := c.Objects[0].(*canvas.Rectangle); ok {
			r.FillColor = s.bg
			r.Refresh()
		}
		s.TextSegment.Update(c.Objects[1])
		return
	}
	s.TextSegment.Update(o)
}

// mentionSegments splits a plain chunk around "@nick" tokens that the
// mention resolver recognizes, turning each into a tappable link that
// opens the DM, hyperlink-style.
func mentionSegments(content string, st widget.RichTextStyle, mention func(string) func()) []widget.RichTextSegment {
	if mention == nil || !strings.Contains(content, "@") {
		return []widget.RichTextSegment{&widget.TextSegment{Text: content, Style: st}}
	}
	var segs []widget.RichTextSegment
	emit := func(txt string) {
		if txt != "" {
			segs = append(segs, &widget.TextSegment{Text: txt, Style: st})
		}
	}
	i := 0
	for i < len(content) {
		j := strings.IndexByte(content[i:], '@')
		if j < 0 {
			emit(content[i:])
			break
		}
		j += i
		k := j + 1
		for k < len(content) && isNickByte(content[k]) {
			k++
		}
		nick := content[j+1 : k]
		var act func()
		if nick != "" && (j == 0 || !isNickByte(content[j-1])) {
			act = mention(nick)
		}
		if act == nil {
			emit(content[i : j+1])
			i = j + 1
			continue
		}
		emit(content[i:j])
		segs = append(segs, &widget.HyperlinkSegment{
			Text: "@" + nick, OnTapped: act, TextStyle: st.TextStyle,
		})
		i = k
	}
	if len(segs) == 0 {
		segs = append(segs, &widget.TextSegment{Text: content, Style: st})
	}
	return segs
}

// isNickByte is isNickChar plus uppercase: mention scanning runs over the
// original text, not a lowercased copy.
func isNickByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || isNickChar(c)
}
