package app

import (
	_ "embed"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/render"
)

// These are read by the session goroutine, hence atomic.
var (
	notifyEnabled    atomic.Bool
	soundEnabled     atomic.Bool
	notifyAll        atomic.Bool // notify for every live message, not just highlights
	hideJoinParts    atomic.Bool
	autoScanChannels atomic.Bool
	joinAllChannels  atomic.Bool
)

// scrollbackCap is the per-buffer history depth, read by the session goroutine.
var scrollbackCap atomic.Int64

func applySettings(a fyne.App) {
	p := a.Preferences()
	t := &render.Theme{Theme: theme.DefaultTheme(), FontScale: 1}
	switch p.StringWithFallback("theme", "System") {
	case "Light":
		t.Force, t.Variant = true, theme.VariantLight
	case "Dark":
		t.Force, t.Variant = true, theme.VariantDark
	}
	// Fyne's base text size is 14; scale to the user's chosen point size.
	pts := p.FloatWithFallback("fontpts", 16)
	if pts < 10 {
		pts = 10
	} else if pts > 28 {
		pts = 28
	}
	t.FontScale = float32(pts) / 14.0
	if c, ok := render.ParseHexColor(p.String("bgcolor")); ok {
		t.BG = c
		// a custom background overrides the variant by its luminance:
		// light text on a light background must be impossible
		r, g, b, _ := c.RGBA()
		lum := (299*int(r>>8) + 587*int(g>>8) + 114*int(b>>8)) / 1000
		t.Force = true
		if lum > 128 {
			t.Variant = theme.VariantLight
		} else {
			t.Variant = theme.VariantDark
		}
	}
	if c, ok := render.ParseHexColor(p.String("accent")); ok {
		t.Accent = c
	}
	if c, ok := render.ParseHexColor(p.String("selfcolorhex")); ok {
		t.SelfNick = c
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
	if _, ok := render.ParseHexColor(p.String("selfcolorhex")); ok {
		selfNickColor = "nickself"
	} else if c, ok := render.NamedNickColors[p.StringWithFallback("selfcolor", "Green")]; ok {
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
