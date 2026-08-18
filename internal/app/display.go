package app

import (
	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/render"
)

// Display options, written by applySettings and read on the UI thread.
// These are the last of the display globals; M4 replaces them with an
// immutable settings snapshot.
var (
	showTimestamps = true
	colorfulNicks  = true
	selfNickColor  = fyne.ThemeColorName("irc3")
	timeLayout     = "15:04"
)

// renderOptions builds the render layer's per-pass options from the current
// display settings.
func renderOptions(mono bool, mention func(string) func()) render.Options {
	return render.Options{
		Mono:           mono,
		ShowTimestamps: showTimestamps,
		TimeLayout:     timeLayout,
		ColorfulNicks:  colorfulNicks,
		SelfColor:      selfNickColor,
		Mention:        mention,
	}
}
