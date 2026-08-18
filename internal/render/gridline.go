package render

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
)

// gridLineSegment renders one message line as a terminal-style character
// grid. RichText's proportional per-segment layout can never keep mIRC
// background-colored art column-aligned (fractional advances accumulate,
// and background rects only cover glyph height); TextGrid rounds every
// cell to a whole-pixel monospace advance and paints backgrounds over the
// full cell, exactly like irssi.
type gridLineSegment struct {
	cells []widget.TextGridCell
	text  string // plain text for Textual()
}

func (s *gridLineSegment) Inline() bool              { return false }
func (s *gridLineSegment) Textual() string           { return s.text }
func (s *gridLineSegment) Select(_, _ fyne.Position) {}
func (s *gridLineSegment) SelectedText() string      { return "" }
func (s *gridLineSegment) Unselect()                 {}

func (s *gridLineSegment) Visual() fyne.CanvasObject {
	return newArtLine(s.cells)
}

func (s *gridLineSegment) Update(o fyne.CanvasObject) {
	if a, ok := o.(*artLine); ok {
		a.cells = s.cells
		a.Refresh()
	}
}

// hasBackgroundArt reports whether the line should render as a grid.
// Art means painted backgrounds or colored block glyphs — a colored
// sentence is still prose and must keep word wrap, selection copy, and
// mention links, so color alone never qualifies.
func hasBackgroundArt(chunks []ircfmt.FormattedSubstring, mono bool) bool {
	total, painted := 0, 0
	blocks := false
	for _, chunk := range chunks {
		n := len([]rune(chunk.Content))
		total += n
		bg := chunk.BackgroundColor.IsSet && chunk.BackgroundColor.Value < 99
		fg := chunk.ForegroundColor.IsSet && chunk.ForegroundColor.Value < 99
		hasBlock := false
		for _, r := range chunk.Content {
			if r >= 0x2580 && r <= 0x259F {
				hasBlock = true
				blocks = true
				break
			}
		}
		if bg || (fg && hasBlock) {
			painted += n
		}
	}
	if painted == 0 || !blocksOrBG(chunks, blocks) {
		return false
	}
	return mono || painted*2 >= total
}

// blocksOrBG: art needs block glyphs somewhere, or real backgrounds.
func blocksOrBG(chunks []ircfmt.FormattedSubstring, blocks bool) bool {
	if blocks {
		return true
	}
	for _, chunk := range chunks {
		if chunk.BackgroundColor.IsSet && chunk.BackgroundColor.Value < 99 {
			return true
		}
	}
	return false
}

// gridCellsFor converts a prefix (timestamp, status dot, nick) and the
// IRC-formatted body into grid cells with resolved colors.
func gridCellsFor(prefix string, chunks []ircfmt.FormattedSubstring, base fyne.ThemeColorName) []widget.TextGridCell {
	settings := fyne.CurrentApp().Settings()
	th := settings.Theme()
	variant := settings.ThemeVariant()
	resolve := func(name fyne.ThemeColorName) *widget.CustomTextGridStyle {
		return &widget.CustomTextGridStyle{FGColor: th.Color(name, variant)}
	}

	cells := make([]widget.TextGridCell, 0, len(prefix)+len(chunks)*4)
	dim := resolve(theme.ColorNamePlaceHolder)
	for _, r := range prefix {
		cells = append(cells, widget.TextGridCell{Rune: r, Style: dim})
	}
	for _, chunk := range chunks {
		st := &widget.CustomTextGridStyle{
			TextStyle: fyne.TextStyle{Monospace: true, Bold: chunk.Bold,
				Italic: chunk.Italic, Underline: chunk.Underline},
		}
		if chunk.BackgroundColor.IsSet && chunk.BackgroundColor.Value < 99 {
			st.BGColor = ircBackgroundColor(chunk.BackgroundColor.Value)
		}
		if chunk.ForegroundColor.IsSet && chunk.ForegroundColor.Value < 99 {
			// grid lines are art: exact palette colors always. The
			// readability-tuned variants exist for prose on the theme
			// background and turn art's white into grey.
			st.FGColor = ircBackgroundColor(chunk.ForegroundColor.Value)
		}
		if st.FGColor == nil {
			st.FGColor = th.Color(base, variant)
		}
		for _, r := range chunk.Content {
			cells = append(cells, widget.TextGridCell{Rune: r, Style: st})
		}
	}
	return cells
}

// artLine draws grid cells the way a terminal does: block-element runes
// become exact rectangles (font glyph coverage never quite fills a cell,
// leaving hairline seams in art), everything else is text on the grid.
type artLine struct {
	widget.BaseWidget
	cells []widget.TextGridCell
}

func newArtLine(cells []widget.TextGridCell) *artLine {
	a := &artLine{cells: cells}
	a.ExtendBaseWidget(a)
	return a
}

func artCellSize() fyne.Size {
	th := fyne.CurrentApp().Settings().Theme()
	size := th.Size(theme.SizeNameText)
	m := fyne.MeasureText("M", size, fyne.TextStyle{Monospace: true})
	return fyne.NewSize(float32(math.Round(float64(m.Width))),
		float32(math.Round(float64(m.Height))))
}

func (a *artLine) MinSize() fyne.Size {
	cs := artCellSize()
	return fyne.NewSize(cs.Width*float32(len(a.cells)), cs.Height)
}

func (a *artLine) CreateRenderer() fyne.WidgetRenderer {
	r := &artLineRenderer{a: a}
	r.rebuild()
	return r
}

type artLineRenderer struct {
	a    *artLine
	objs []fyne.CanvasObject
}

func (r *artLineRenderer) rebuild() {
	cs := artCellSize()
	th := fyne.CurrentApp().Settings().Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	txtSize := th.Size(theme.SizeNameText)
	r.objs = r.objs[:0]
	x := float32(0)
	for _, c := range r.a.cells {
		var fg, bg color.Color
		ts := fyne.TextStyle{Monospace: true}
		if st, ok := c.Style.(*widget.CustomTextGridStyle); ok && st != nil {
			fg, bg = st.FGColor, st.BGColor
			ts = st.TextStyle
			ts.Monospace = true
		}
		if fg == nil {
			fg = th.Color(theme.ColorNameForeground, variant)
		}
		if bg != nil {
			rect := canvas.NewRectangle(bg)
			rect.Move(fyne.NewPos(x, 0))
			rect.Resize(cs)
			r.objs = append(r.objs, rect)
		}
		if geom, ok := blockGeom(c.Rune); ok {
			col := fg
			if geom.alpha != 0xFF {
				nr, ng, nb, _ := fg.RGBA()
				col = color.NRGBA{uint8(nr >> 8), uint8(ng >> 8), uint8(nb >> 8), geom.alpha}
			}
			rect := canvas.NewRectangle(col)
			rect.Move(fyne.NewPos(x+geom.x*cs.Width, geom.y*cs.Height))
			rect.Resize(fyne.NewSize(geom.w*cs.Width, geom.h*cs.Height))
			r.objs = append(r.objs, rect)
		} else if c.Rune != ' ' && c.Rune != 0 {
			t := canvas.NewText(string(c.Rune), fg)
			t.TextSize = txtSize
			t.TextStyle = ts
			t.Move(fyne.NewPos(x, 0))
			t.Resize(cs)
			r.objs = append(r.objs, t)
		}
		x += cs.Width
	}
}

func (r *artLineRenderer) Layout(fyne.Size)             {}
func (r *artLineRenderer) MinSize() fyne.Size           { return r.a.MinSize() }
func (r *artLineRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *artLineRenderer) Destroy()                     {}
func (r *artLineRenderer) Refresh() {
	r.rebuild()
	canvas.Refresh(r.a)
}

// blockGeom maps Unicode block elements to a rectangle inside the cell
// (fractions of cell size) plus an alpha for the shade characters.
type blockRect struct {
	x, y, w, h float32
	alpha      uint8
}

func blockGeom(r rune) (blockRect, bool) {
	full := blockRect{0, 0, 1, 1, 0xFF}
	switch r {
	case 0x2588: // █
		return full, true
	case 0x2580: // ▀
		return blockRect{0, 0, 1, 0.5, 0xFF}, true
	case 0x2584: // ▄
		return blockRect{0, 0.5, 1, 0.5, 0xFF}, true
	case 0x258C: // ▌
		return blockRect{0, 0, 0.5, 1, 0xFF}, true
	case 0x2590: // ▐
		return blockRect{0.5, 0, 0.5, 1, 0xFF}, true
	case 0x2594: // ▔
		return blockRect{0, 0, 1, 0.125, 0xFF}, true
	case 0x2595: // ▕
		return blockRect{0.875, 0, 0.125, 1, 0xFF}, true
	case 0x2591: // ░
		full.alpha = 0x40
		return full, true
	case 0x2592: // ▒
		full.alpha = 0x80
		return full, true
	case 0x2593: // ▓
		full.alpha = 0xC0
		return full, true
	}
	// ▁▂▃▅▆▇: lower blocks of increasing eighths
	if r >= 0x2581 && r <= 0x2587 {
		frac := float32(r-0x2580) / 8
		return blockRect{0, 1 - frac, 1, frac, 0xFF}, true
	}
	// ▏▎▍▋▊▉: left blocks (0x2589..0x258F, widths 7/8 down to 1/8)
	if r >= 0x2589 && r <= 0x258F {
		frac := float32(0x2590-r) / 8
		return blockRect{0, 0, frac, 1, 0xFF}, true
	}
	return blockRect{}, false
}
