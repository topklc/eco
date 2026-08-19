package widgets

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// How tall is a row before it exists?
//
// A variable-height widget.List needs every row's height up front, and the
// obvious way to get one - build the RichText and ask its MinSize - costs
// about 40µs a row. Over a 10,000-line buffer that is four seconds of
// measuring, which is the whole reason risk #1 in the blueprint is a risk.
//
// So heights are ARITHMETIC here, not layout. Fyne measures a string by
// summing its glyph advances, which means the width of "a b c" is exactly
// the width of "a" plus " " plus "b" plus " " plus "c" (measured, and
// asserted in wrap_test.go). Word widths are therefore reusable, and chat
// text repeats words relentlessly, so a small cache turns a measurement
// problem into map lookups: about 0.5µs a row cold, and near nothing warm.
//
// The wrap itself mirrors widget.RichText's: fill a row with whole words,
// break before the first one that does not fit, and carry the pen across
// inline segments so a colour change mid-sentence does not restart the line.
// wrap_test.go holds the two of them against each other over a corpus,
// because an under-count clips the last line of a message.
type measurer struct {
	size  float32
	cache map[measureKey]float32
	lineH map[fyne.TextStyle]float32
	toks  []token // scratch, so wrapping a row allocates nothing
}

type measureKey struct {
	text  string
	style fyne.TextStyle
}

func newMeasurer(size float32) *measurer {
	return &measurer{
		size:  size,
		cache: make(map[measureKey]float32, 512),
		lineH: make(map[fyne.TextStyle]float32, 8),
	}
}

// width is fyne.MeasureText with a memo in front of it.
func (m *measurer) width(text string, style fyne.TextStyle) float32 {
	if text == "" {
		return 0
	}
	k := measureKey{text, style}
	if w, ok := m.cache[k]; ok {
		return w
	}
	w := fyne.MeasureText(text, m.size, style).Width
	// the cache is per buffer render pass and words repeat; a runaway line
	// of unique tokens must not be allowed to grow it without end
	if len(m.cache) < 1<<16 {
		m.cache[k] = w
	}
	return w
}

// lineHeight is the height of one wrapped row of text in the given style -
// the same question RichText asks, and it asks it of the letter z. Cached
// because it is asked once per segment of every row of every rebuild, and an
// uncached measure costs several microseconds.
func (m *measurer) lineHeight(style fyne.TextStyle) float32 {
	if h, ok := m.lineH[style]; ok {
		return h
	}
	h := fyne.MeasureText("z", m.size, style).Height
	m.lineH[style] = h
	return h
}

// rows counts the wrapped rows a body's segments occupy at the given width,
// and reports the tallest line height among the styles used. The two answers
// travel together because they are one question: RichText stacks its rows at
// the height of the face they are set in, and monospace is not the same
// height as the proportional face beside it.
func (m *measurer) rows(segs []widget.RichTextSegment, width float32) (int, float32) {
	lineH := float32(0)
	if width <= 0 {
		return 1, m.lineHeight(fyne.TextStyle{})
	}
	lines, x := 1, float32(0)
	for i, seg := range segs {
		var (
			text  string
			style fyne.TextStyle
			atom  bool
		)
		switch s := seg.(type) {
		case *widget.TextSegment:
			text, style = s.Text, s.Style.TextStyle
		case *widget.HyperlinkSegment:
			text, style = s.Text, s.TextStyle
		default:
			// Anything that is not text to RichText - a background-painted
			// span, an image - is laid out as one object that never wraps;
			// it just eats width. Mirror that.
			text, atom = seg.Textual(), true
		}
		if h := m.lineHeight(style); h > lineH {
			lineH = h
		}
		if atom {
			x += m.width(text, style)
			if x > width && i < len(segs)-1 {
				lines, x = lines+1, 0
			}
			continue
		}
		lines, x = m.flow(text, style, width, lines, x)
		// a segment that does not continue the line ends the row - except
		// the last one, which is only ever the terminator render appends
		if !seg.Inline() && i < len(segs)-1 {
			lines, x = lines+1, 0
		}
	}
	if lineH == 0 {
		lineH = m.lineHeight(fyne.TextStyle{})
	}
	return lines, lineH
}

// flow wraps one run of text that starts at pen position x, returning the
// row count and where the pen ended.
func (m *measurer) flow(text string, style fyne.TextStyle, width float32, lines int, x float32) (int, float32) {
	for first := true; ; first = false {
		para := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			para, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		if !first {
			lines, x = lines+1, 0
		}
		m.toks = tokens(m.toks[:0], para)
		for _, tok := range m.toks {
			w := m.width(tok.word, style)
			if x > 0 && x+w > width {
				lines, x = lines+1, 0
			}
			if x+w > width {
				// a word longer than a whole row: RichText gives up on
				// the word boundary and breaks it between runes
				lines, x = m.breakWord(tok.word, style, width, lines, x)
			} else {
				x += w
			}
			// trailing spaces are not free - a run of them that does not
			// fit takes a row of its own, which is where a message ending
			// in whitespace gets its extra line
			if s := m.width(tok.space, style); s > 0 {
				if x > 0 && x+s > width {
					lines, x = lines+1, 0
				}
				x += s
			}
		}
		if text == "" {
			return lines, x
		}
	}
}

// breakWord fills row after row with as many runes of one over-long word as
// fit - an unbroken URL, or a CJK sentence, which has no spaces to break at.
// It is the only place that measures per rune, and it is reached only when a
// single token is wider than the whole row.
func (m *measurer) breakWord(word string, style fyne.TextStyle, width float32, lines int, x float32) (int, float32) {
	for _, r := range word {
		w := m.width(string(r), style)
		if x > 0 && x+w > width {
			lines, x = lines+1, 0
		}
		x += w
	}
	return lines, x
}

// token is a word and the whitespace that follows it.
type token struct{ word, space string }

// tokens splits a run of text into words and their trailing spaces. It is a
// hot path - every row of every buffer - so it slices rather than allocating
// through strings.Fields.
func tokens(out []token, s string) []token {
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && !isSpaceByte(s[j]) {
			j++
		}
		k := j
		for k < len(s) && isSpaceByte(s[k]) {
			k++
		}
		out = append(out, token{s[i:j], s[j:k]})
		i = k
	}
	return out
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' }
