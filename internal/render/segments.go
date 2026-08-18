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
	Mono           bool   // server buffer: monospace so ASCII art lines up
	ShowTimestamps bool   //
	TimeLayout     string // Go time layout for the timestamp column
	ColorfulNicks  bool   // per-nick colors vs one accent color
	SelfColor      fyne.ThemeColorName
	// Mention resolves an @nick token to a tap action, or nil if the nick
	// is not someone we can open a conversation with.
	Mention func(nick string) func()
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
		return []widget.RichTextSegment{&gridLineSegment{
			cells: gridCellsFor(prefix, chunks, base),
			text:  prefix + ircfmt.Strip(l.Text),
		}}
	}
	segs := make([]widget.RichTextSegment, 0, 6)
	if opts.ShowTimestamps {
		segs = append(segs, &widget.TextSegment{
			Text:  l.Time.Format(opts.TimeLayout) + "  ",
			Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true},
		})
	}

	nickColor := func() fyne.ThemeColorName {
		if l.Self {
			return opts.SelfColor
		}
		if opts.ColorfulNicks {
			return NickColorName(l.Nick)
		}
		return theme.ColorNamePrimary
	}

	switch {
	case l.Status:
		segs = append(segs, &widget.TextSegment{
			Text:  "· ",
			Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true},
		})
		segs = append(segs, richSegments(chunks, theme.ColorNamePlaceHolder, false, nil)...)
	case l.Action:
		segs = append(segs, &widget.TextSegment{
			Text:  "* " + l.Nick + " ",
			Style: widget.RichTextStyle{ColorName: nickColor(), TextStyle: fyne.TextStyle{Italic: true}, Inline: true},
		})
		segs = append(segs, richSegments(chunks, theme.ColorNameForeground, true, mention)...)
	default:
		nick := l.Nick
		if l.Notice {
			nick = "-" + nick + "-"
		}
		segs = append(segs, &widget.TextSegment{
			Text:  nick + "  ",
			Style: widget.RichTextStyle{ColorName: nickColor(), TextStyle: fyne.TextStyle{Bold: true}, Inline: true},
		})
		textColor := theme.ColorNameForeground
		if l.Highlight {
			textColor = theme.ColorNameWarning
		}
		segs = append(segs, richSegments(chunks, textColor, false, mention)...)
	}

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
