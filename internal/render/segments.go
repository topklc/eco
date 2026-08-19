// Package render turns IRC-formatted lines into Fyne widgets. It owns the
// mIRC color palettes, the terminal-grid renderer for colored art, and the
// segment builder; it is verified by the golden-image suite in this package
// and must stay free of application state (everything arrives via Options).
package render

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/model"
)

// Options carries every display decision the render layer needs. The app
// builds one per render pass from its settings snapshot.
type Options struct {
	// Mono sets the message body in the monospace face, and lets a line of
	// painted colour count as art on its own. It is what the network log
	// asks for — MOTD, LIST and WHOIS are columns the server formatted —
	// and, since the M11(c) typography gate, what a conversation does not:
	// chat bodies are proportional, and the Timeline draws its timestamp and
	// nick columns itself, in monospace, at positions it computes.
	Mono bool

	ShowTimestamps bool   //
	TimeLayout     string // Go time layout for the timestamp column
	ColorfulNicks  bool   // per-nick colors vs one accent color
	SelfColor      fyne.ThemeColorName
	// Mention resolves an @nick token to a tap action, or nil if the nick
	// is not someone we can open a conversation with.
	Mention func(nick string) func()
	// URL resolves a bare http(s):// link to a tap action, or nil to leave
	// links as ordinary text. Nil is what every pre-M11 caller passes and
	// what the golden corpus renders with, so the frozen output is the
	// no-links output (blueprint §5.5).
	URL func(raw string) func()
}

// Segments renders one line into RichText segments. It is the render
// package's entry point: everything display-related arrives through opts,
// so this layer holds no global state.
func Segments(l model.Line, opts Options) []widget.RichTextSegment {
	mono, mention := opts.Mono, opts.Mention
	chunks := ircfmt.Split(l.Text)
	// background-colored art gets terminal-grid rendering; RichText's
	// proportional layout cannot keep the columns aligned
	if hasBackgroundArt(chunks, mono) {
		cells, text := artGrid(l, chunks, opts)
		return []widget.RichTextSegment{&gridLineSegment{cells: cells, text: text}}
	}
	segs := make([]widget.RichTextSegment, 0, 6)
	if opts.ShowTimestamps {
		segs = append(segs, &widget.TextSegment{
			Text:  l.Time.Format(opts.TimeLayout) + "  ",
			Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true},
		})
	}

	switch {
	case l.Status:
		segs = append(segs, &widget.TextSegment{
			Text:  "· ",
			Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true},
		})
		segs = append(segs, richSegments(chunks, theme.ColorNamePlaceHolder, false, nil, opts.URL)...)
	case l.Action:
		segs = append(segs, &widget.TextSegment{
			Text:  "* " + l.Nick + " ",
			Style: widget.RichTextStyle{ColorName: NickColor(l, opts), TextStyle: fyne.TextStyle{Italic: true}, Inline: true},
		})
		segs = append(segs, richSegments(chunks, theme.ColorNameForeground, true, mention, opts.URL)...)
	default:
		nick := l.Nick
		if l.Notice {
			nick = "-" + nick + "-"
		}
		segs = append(segs, &widget.TextSegment{
			Text:  nick + "  ",
			Style: widget.RichTextStyle{ColorName: NickColor(l, opts), TextStyle: fyne.TextStyle{Bold: true}, Inline: true},
		})
		segs = append(segs, richSegments(chunks, bodyColor(l), false, mention, opts.URL)...)
	}
	return finish(segs, mono)
}

// Body renders only the message text of a line: no timestamp gutter, no nick
// column, no status bullet. It is what the Timeline's rows draw, because a
// row lays those columns out itself — a fixed gutter, a soft-aligned nick,
// and a body that wraps under itself instead of back to the left edge
// (blueprint §4.2). Everything about how the text itself reads — the mIRC
// palette, the highlight color, mentions, links, the two workarounds that
// terminate a row — is still this engine's, and is the same code Segments
// runs.
//
// Art lines have no body: they are one grid, and Art draws them.
func Body(l model.Line, opts Options) []widget.RichTextSegment {
	chunks := ircfmt.Split(l.Text)
	if hasBackgroundArt(chunks, opts.Mono) {
		return nil
	}
	base, italic, mention := bodyColor(l), false, opts.Mention
	switch {
	case l.Status:
		base, mention = theme.ColorNamePlaceHolder, nil
	case l.Action:
		base, italic = theme.ColorNameForeground, true
	}
	return finish(richSegments(chunks, base, italic, mention, opts.URL), opts.Mono)
}

// Art reports whether a line must be drawn as a terminal character grid —
// mIRC background art, which no proportional layout can keep aligned — and
// returns the widget that draws it. The grid carries its own timestamp and
// nick prefix, so an art row is the whole line, columns included; that is
// why the Timeline gives it a row type of its own.
func Art(l model.Line, opts Options) (fyne.CanvasObject, bool) {
	chunks := ircfmt.Split(l.Text)
	if !hasBackgroundArt(chunks, opts.Mono) {
		return nil, false
	}
	cells, _ := artGrid(l, chunks, opts)
	return newArtLine(cells), true
}

// PlainText is a line's text with the IRC formatting removed — what "Copy
// message" puts on the clipboard, and the only thing outside this package
// that should ever need to know the codes are in there.
func PlainText(l model.Line) string { return ircfmt.Strip(l.Text) }

// NickColor is the color a nick is written in: the user's own color for
// their own messages, a per-nick color when colorful nicks are on, and the
// one accent otherwise.
func NickColor(l model.Line, opts Options) fyne.ThemeColorName {
	if l.Self {
		return opts.SelfColor
	}
	if opts.ColorfulNicks {
		return NickColorName(l.Nick)
	}
	return theme.ColorNamePrimary
}

// bodyColor is the message text's own color: highlighted lines are warned.
func bodyColor(l model.Line) fyne.ThemeColorName {
	if l.Highlight {
		return theme.ColorNameWarning
	}
	return theme.ColorNameForeground
}

// artGrid builds the character-grid cells for one art line, prefix included.
func artGrid(l model.Line, chunks []ircfmt.FormattedSubstring, opts Options) ([]widget.TextGridCell, string) {
	prefix := ""
	if opts.ShowTimestamps {
		prefix = l.Time.Format(opts.TimeLayout) + "  "
	}
	base := theme.ColorNameForeground
	switch {
	case l.Status:
		prefix += "· "
		base = theme.ColorNamePlaceHolder
	case l.Action:
		prefix += "* " + l.Nick + " "
	default:
		nick := l.Nick
		if l.Notice {
			nick = "-" + nick + "-"
		}
		prefix += nick + "  "
	}
	return gridCellsFor(prefix, chunks, base), prefix + ircfmt.Strip(l.Text)
}

// finish applies the server buffer's monospace pass and terminates the row.
func finish(segs []widget.RichTextSegment, mono bool) []widget.RichTextSegment {
	// the server buffer renders monospace so MOTD ASCII art lines up
	if mono {
		for _, s := range segs {
			switch ts := s.(type) {
			case *widget.TextSegment:
				ts.Style.TextStyle.Monospace = true
			case *bgTextSegment:
				ts.Style.TextStyle.Monospace = true
			}
		}
	}

	// terminate the line
	switch last := segs[len(segs)-1].(type) {
	case *widget.TextSegment:
		last.Style.Inline = false
	case *bgTextSegment:
		last.Style.Inline = false
	default:
		// hyperlink (and any other always-inline) segments cannot end a
		// row; add an empty terminator so lines do not merge
		segs = append(segs, &widget.TextSegment{Text: ""})
	}
	return segs
}
