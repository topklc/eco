package app

import (
	_ "embed"
	"image/color"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// These are read by the session goroutine, hence atomic.
var (
	hideJoinParts    atomic.Bool
	autoScanChannels atomic.Bool
	joinAllChannels  atomic.Bool
)

// forcedTheme pins the theme variant (light/dark) and scales font sizes.
type forcedTheme struct {
	fyne.Theme
	variant   fyne.ThemeVariant
	force     bool
	fontScale float32
	bg        color.Color // custom background, nil = theme default
	accent    color.Color // custom accent, nil = the logo green
	selfNick  color.Color // custom own-nick color, nil = named choice
}

// scrollbackCap is the per-buffer history depth, read by the session goroutine.
var scrollbackCap atomic.Int64

func (t *forcedTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if t.force {
		v = t.variant
	}
	if c, ok := lookupIRCColor(n, v); ok {
		return c
	}
	dark := v == theme.VariantDark

	// the user's own nick color when customized past the named choices
	if n == fyne.ThemeColorName("nickself") && t.selfNick != nil {
		return t.selfNick
	}

	// accent: the logo's leaf green unless the user picked their own
	if t.accent != nil {
		r, g, b, _ := t.accent.RGBA()
		base := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xFF}
		switch n {
		case theme.ColorNamePrimary, theme.ColorNameHyperlink:
			return base
		case theme.ColorNameFocus:
			base.A = 0xA8
			return base
		case theme.ColorNameSelection:
			base.A = 0x38
			return base
		}
	} else {
		switch n {
		case theme.ColorNamePrimary:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0xFF}
			}
			return color.NRGBA{0x42, 0x9B, 0x00, 0xFF}
		case theme.ColorNameHyperlink:
			if dark {
				return color.NRGBA{0x8C, 0xD1, 0x52, 0xFF}
			}
			return color.NRGBA{0x33, 0x7A, 0x00, 0xFF}
		case theme.ColorNameFocus:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0xA8}
			}
			return color.NRGBA{0x42, 0x9B, 0x00, 0xA8}
		case theme.ColorNameSelection:
			if dark {
				return color.NRGBA{0x7C, 0xC9, 0x40, 0x38}
			}
			return color.NRGBA{0x40, 0xA0, 0x00, 0x33}
		}
	}

	// dark surfaces: pure neutrals, no blue tint; near-black window with
	// one lighter elevation step for dialogs and menus
	if dark {
		switch n {
		case theme.ColorNameBackground:
			if t.bg != nil {
				return t.bg
			}
			return color.NRGBA{0x14, 0x14, 0x14, 0xFF}
		case theme.ColorNameOverlayBackground:
			return color.NRGBA{0x1F, 0x1F, 0x1F, 0xFF}
		case theme.ColorNameMenuBackground:
			return color.NRGBA{0x23, 0x23, 0x23, 0xFF}
		case theme.ColorNameInputBackground:
			return color.NRGBA{0x1B, 0x1B, 0x1B, 0xFF}
		}
	}

	if t.bg != nil && n == theme.ColorNameBackground {
		return t.bg
	}
	return t.Theme.Color(n, v)
}

func (t *forcedTheme) Size(n fyne.ThemeSizeName) float32 {
	// tight message spacing: the default 4px gap between paragraphs made
	// chat airy and broke multi-line ASCII art (irssi uses none)
	if n == theme.SizeNameLineSpacing {
		return 0
	}
	s := t.Theme.Size(n)
	if n == theme.SizeNameText || n == theme.SizeNameSubHeadingText || n == theme.SizeNameHeadingText {
		return s * t.fontScale
	}
	return s
}

func applySettings(a fyne.App) {
	p := a.Preferences()
	t := &forcedTheme{Theme: theme.DefaultTheme(), fontScale: 1}
	switch p.StringWithFallback("theme", "System") {
	case "Light":
		t.force, t.variant = true, theme.VariantLight
	case "Dark":
		t.force, t.variant = true, theme.VariantDark
	}
	// Fyne's base text size is 14; scale to the user's chosen point size.
	pts := p.FloatWithFallback("fontpts", 16)
	if pts < 10 {
		pts = 10
	} else if pts > 28 {
		pts = 28
	}
	t.fontScale = float32(pts) / 14.0
	if c, ok := parseHexColor(p.String("bgcolor")); ok {
		t.bg = c
		// a custom background overrides the variant by its luminance:
		// light text on a light background must be impossible
		r, g, b, _ := c.RGBA()
		lum := (299*int(r>>8) + 587*int(g>>8) + 114*int(b>>8)) / 1000
		t.force = true
		if lum > 128 {
			t.variant = theme.VariantLight
		} else {
			t.variant = theme.VariantDark
		}
	}
	if c, ok := parseHexColor(p.String("accent")); ok {
		t.accent = c
	}
	if c, ok := parseHexColor(p.String("selfcolorhex")); ok {
		t.selfNick = c
	}
	a.Settings().SetTheme(t)
	hideJoinParts.Store(p.BoolWithFallback("hidejoinparts", false))
	loadIgnorePrefs(p)
	autoScanChannels.Store(p.BoolWithFallback("autoscan", true))
	joinAllChannels.Store(p.BoolWithFallback("joinall", true))
	notifyEnabled.Store(p.BoolWithFallback("notify", true))
	soundEnabled.Store(p.BoolWithFallback("notifysound", true))
	notifyAll.Store(p.BoolWithFallback("notifyall", false))

	showTimestamps = p.BoolWithFallback("showtimestamps", true)
	colorfulNicks = p.BoolWithFallback("colorfulnicks", true)
	if _, ok := parseHexColor(p.String("selfcolorhex")); ok {
		selfNickColor = "nickself"
	} else if c, ok := namedNickColors[p.StringWithFallback("selfcolor", "Green")]; ok {
		selfNickColor = c
	}
	if p.StringWithFallback("timefmt", "24 hour") == "12 hour" {
		timeLayout = "3:04 PM"
	} else {
		timeLayout = "15:04"
	}
	// scrollback: a number of lines, or All (stored as 0, meaning no cap)
	sbv := p.StringWithFallback("scrollbackv", "2000")
	if strings.EqualFold(sbv, "All") {
		scrollbackCap.Store(0)
	} else if n, err := strconv.Atoi(strings.TrimSpace(sbv)); err == nil && n >= 100 {
		scrollbackCap.Store(int64(n))
	} else {
		scrollbackCap.Store(2000)
	}
}

// showMainMenu drops the app menu down from the gear button: with only
// three global items left, a dialog hub earned its retirement.
func (u *UI) showMainMenu(anchor fyne.CanvasObject) {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Servers & Identities", func() { u.ShowNetworkManager() }),
		fyne.NewMenuItem("Preferences", func() { u.showPreferences() }),
		fyne.NewMenuItem("Documentation", func() { u.showHelp() }),
	}
	widget.ShowPopUpMenuAtRelativePosition(fyne.NewMenu("", items...), u.win.Canvas(),
		fyne.NewPos(0, anchor.Size().Height), anchor)
}

// Fyne ships only a regular monospace face and ignores the Bold flag for
// monospace text entirely, so IRC bold in the server buffer silently
// rendered regular. Ship the bold faces ourselves.
//
//go:embed assets/DejaVuSansMono-Bold.ttf
var monoBoldTTF []byte

//go:embed assets/DejaVuSansMono-BoldOblique.ttf
var monoBoldObliqueTTF []byte

var (
	monoBold        = fyne.NewStaticResource("DejaVuSansMono-Bold.ttf", monoBoldTTF)
	monoBoldOblique = fyne.NewStaticResource("DejaVuSansMono-BoldOblique.ttf", monoBoldObliqueTTF)
)

func (t *forcedTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Monospace && style.Bold {
		if style.Italic {
			return monoBoldOblique
		}
		return monoBold
	}
	return t.Theme.Font(style)
}
