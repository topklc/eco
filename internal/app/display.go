package app

import (
	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/render"
)

// selfNickColor resolves the user's own-nick color from the settings
// snapshot: a custom hex wins, otherwise the named choice.
func selfNickColor(s *config.Settings) fyne.ThemeColorName {
	if _, ok := render.ParseHexColor(s.SelfColorHex); ok {
		return "nickself"
	}
	if c, ok := render.NamedNickColors[s.SelfColorName]; ok {
		return c
	}
	return "irc3"
}

// renderOptions builds the render layer's per-pass options from the current
// settings snapshot. The render engine holds no display state of its own.
func renderOptions(mono bool, mention func(string) func()) render.Options {
	s := config.Current()
	return render.Options{
		Mono:           mono,
		ShowTimestamps: s.ShowTimestamps,
		TimeLayout:     s.TimeLayout,
		ColorfulNicks:  s.ColorfulNicks,
		SelfColor:      selfNickColor(s),
		Mention:        mention,
	}
}
