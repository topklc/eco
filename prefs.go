package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"
)

// swatch is a small tappable color square for one-click standard choices.
type swatch struct {
	widget.BaseWidget
	c     color.Color
	onTap func()
}

func newSwatch(c color.Color, onTap func()) *swatch {
	s := &swatch{c: c, onTap: onTap}
	s.ExtendBaseWidget(s)
	return s
}

func (s *swatch) CreateRenderer() fyne.WidgetRenderer {
	r := newRect(s.c)
	return widget.NewSimpleRenderer(container.NewCenter(
		container.NewGridWrap(fyne.NewSize(26, 26), r)))
}

func (s *swatch) Tapped(_ *fyne.PointEvent) { s.onTap() }

// colorRow builds a swatch strip plus a wheel picker and a default reset.
// Changes persist to prefKey immediately and call apply.
func (u *UI) colorRow(prefKey, defaultLabel string, standards []color.Color, apply func()) fyne.CanvasObject {
	p := u.app.Preferences()
	current := widget.NewLabel(p.StringWithFallback(prefKey, defaultLabel))
	set := func(hex string) {
		p.SetString(prefKey, hex)
		if hex == "" {
			current.SetText(defaultLabel)
		} else {
			current.SetText(hex)
		}
		apply()
	}
	items := []fyne.CanvasObject{}
	for _, c := range standards {
		c := c
		items = append(items, newSwatch(c, func() { set(colorToHex(c)) }))
	}
	pick := widget.NewButton("Custom", func() {
		// the native OS color dialog: fyne's built-in wheel renders badly
		go func() {
			start := color.Color(color.NRGBA{0x42, 0x9B, 0x00, 0xFF})
			if c, ok := parseHexColor(p.String(prefKey)); ok {
				start = c
			}
			picked, err := zenity.SelectColor(zenity.Color(start))
			if err == nil && picked != nil {
				fyne.Do(func() { set(colorToHex(picked)) })
			}
		}()
	})
	reset := widget.NewButton("Default", func() { set("") })
	items = append(items, pick, reset, current)
	return container.NewHBox(items...)
}

func (u *UI) showPreferences() {
	p := u.app.Preferences()
	apply := func() {
		applySettings(u.app)
		u.applyBackground()
		u.applyTips()
		u.showBuffer(u.current)
	}

	// helpers: widgets that persist and apply instantly
	// set initial values BEFORE attaching callbacks: otherwise building
	// the dialog fires a full re-theme per control and opening lags
	check := func(label, key string, def bool) *widget.Check {
		c := widget.NewCheck(label, nil)
		c.SetChecked(p.BoolWithFallback(key, def))
		c.OnChanged = func(v bool) { p.SetBool(key, v); apply() }
		return c
	}
	sel := func(key, def string, opts []string) *widget.Select {
		s := widget.NewSelect(opts, nil)
		s.SetSelected(p.StringWithFallback(key, def))
		s.OnChanged = func(v string) { p.SetString(key, v); apply() }
		return s
	}
	slider := func(key string, def, min, max float64, format string) fyne.CanvasObject {
		val := p.FloatWithFallback(key, def)
		lbl := widget.NewLabel(fmt.Sprintf(format, val))
		s := widget.NewSlider(min, max)
		s.Step = 1
		s.SetValue(val)
		s.OnChanged = func(v float64) { lbl.SetText(fmt.Sprintf(format, v)) }
		s.OnChangeEnded = func(v float64) { p.SetFloat(key, v); apply() }
		return container.NewBorder(nil, nil, nil, lbl, s)
	}

	// preset palettes per the designer's spec: matched-lightness hue wheel
	// for accents, both-backgrounds-safe band for nicks, and a black to
	// white lightness ramp with two whisper tints for backgrounds
	accentStd := []color.Color{
		color.NRGBA{0x42, 0x9B, 0x00, 0xFF}, // logo green
		color.NRGBA{0x2E, 0x7C, 0xD6, 0xFF}, // ocean blue
		color.NRGBA{0x14, 0x90, 0x7A, 0xFF}, // deep teal
		color.NRGBA{0x87, 0x57, 0xD1, 0xFF}, // iris purple
		color.NRGBA{0xC2, 0x4B, 0x8E, 0xFF}, // orchid pink
		color.NRGBA{0xD6, 0x45, 0x45, 0xFF}, // signal red
		color.NRGBA{0xC7, 0x74, 0x00, 0xFF}, // burnt amber
		color.NRGBA{0x6E, 0x7B, 0x85, 0xFF}, // steel grey
	}
	nickStd := []color.Color{
		color.NRGBA{0x3F, 0xA3, 0x3F, 0xFF}, // meadow green
		color.NRGBA{0x3D, 0x7D, 0xD6, 0xFF}, // clear blue
		color.NRGBA{0x2A, 0xA1, 0x98, 0xFF}, // lagoon teal
		color.NRGBA{0x9B, 0x59, 0xD0, 0xFF}, // soft violet
		color.NRGBA{0xD4, 0x56, 0x9B, 0xFF}, // rose pink
		color.NRGBA{0xD6, 0x45, 0x45, 0xFF}, // brick red
		color.NRGBA{0xC9, 0x7A, 0x16, 0xFF}, // copper orange
		color.NRGBA{0x6C, 0x7A, 0x89, 0xFF}, // slate grey
	}
	bgStd := []color.Color{
		color.NRGBA{0x0A, 0x0A, 0x0A, 0xFF}, // pitch black
		color.NRGBA{0x14, 0x14, 0x14, 0xFF}, // soft black
		color.NRGBA{0x20, 0x20, 0x20, 0xFF}, // graphite
		color.NRGBA{0x10, 0x18, 0x20, 0xFF}, // midnight blue
		color.NRGBA{0x0F, 0x1A, 0x13, 0xFF}, // forest night
		color.NRGBA{0xF6, 0xF1, 0xE4, 0xFF}, // warm parchment
		color.NRGBA{0xF4, 0xF4, 0xF2, 0xFF}, // paper white
		color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}, // pure white
	}

	bgImgLabel := widget.NewLabel(p.StringWithFallback("bgimage", "none"))
	bgImgLabel.Truncation = fyne.TextTruncateEllipsis
	pickImg := widget.NewButton("Browse", func() {
		go func() {
			path, err := zenity.SelectFile(
				zenity.Title("Choose a background image"),
				zenity.FileFilters{{Name: "Images",
					Patterns: []string{"*.png", "*.jpg", "*.jpeg", "*.webp", "*.gif", "*.bmp"},
					CaseFold: true}})
			if err == nil && path != "" {
				fyne.Do(func() {
					p.SetString("bgimage", path)
					bgImgLabel.SetText(path)
					apply()
				})
			}
		}()
	})
	clearImg := widget.NewButton("None", func() {
		p.SetString("bgimage", "")
		bgImgLabel.SetText("none")
		apply()
	})

	appearance := widget.NewForm(
		widget.NewFormItem("Theme", sel("theme", "System", []string{"System", "Light", "Dark"})),
		widget.NewFormItem("Font size", slider("fontpts", 16, 10, 28, "%.0f pt")),
		widget.NewFormItem("Accent color", u.colorRow("accent", "logo green", accentStd, apply)),
		widget.NewFormItem("Your nick color", u.colorRow("selfcolorhex", "theme green", nickStd, apply)),
		widget.NewFormItem("Background", u.colorRow("bgcolor", "theme default", bgStd, apply)),
		widget.NewFormItem("Background image", container.NewBorder(nil, nil, nil,
			container.NewHBox(pickImg, clearImg), bgImgLabel)),
		widget.NewFormItem("Image opacity", slider("bgopacity", 18, 5, 60, "%.0f%%")),
	)

	chat := container.NewVBox(
		check("Give every person their own nick color", "colorfulnicks", true),
		check("Show timestamps", "showtimestamps", true),
		widget.NewForm(
			widget.NewFormItem("Time format", sel("timefmt", "24 hour", []string{"24 hour", "12 hour"})),
			widget.NewFormItem("Scrollback lines", scrollbackEntry(p, apply)),
		),
		check("Hide join/part/quit messages in channels", "hidejoinparts", false),
		widget.NewButton("Ignored users…", func() { u.showIgnoredUsers() }),
	)

	channels := container.NewVBox(
		check("Scan the channel list when connecting", "autoscan", true),
		check("Join all channels on connect (small networks)", "joinall", true),
	)

	notifications := container.NewVBox(
		check("Desktop notifications for mentions and PMs", "notify", true),
		check("Play a ping sound with notifications", "notifysound", true),
		check("Notify for every message, not just mentions", "notifyall", false),
	)

	behavior := container.NewVBox(
		check("Show tooltips on toolbar icons", "tooltips", true),
	)

	security := container.NewVBox(
		check("Allow encrypted connections only (TLS)", "tlsonly", false),
	)

	pad := func(o fyne.CanvasObject) fyne.CanvasObject {
		return container.NewPadded(container.NewVScroll(o))
	}
	// lazy tabs: only the visible page is laid out
	names := []string{"Appearance", "Chat", "Channels", "Notifications", "Behavior", "Shortcuts", "Security"}
	pages := []fyne.CanvasObject{appearance, chat, channels, notifications, behavior, u.keybindsPane(), security}
	items := make([]*container.TabItem, len(names))
	for i, n := range names {
		items[i] = container.NewTabItem(n, container.NewStack())
	}
	tabs := container.NewAppTabs(items...)
	rendered := make([]bool, len(names))
	render := func(i int) {
		if !rendered[i] {
			rendered[i] = true
			items[i].Content = pad(pages[i])
			tabs.Refresh()
		}
	}
	tabs.OnSelected = func(t *container.TabItem) {
		for i, it := range items {
			if it == t {
				render(i)
			}
		}
	}
	render(0)

	var d dialog.Dialog
	closeBtn := widget.NewButton("Close", func() { d.Hide() })
	body := container.NewBorder(nil,
		container.NewBorder(nil, nil, nil, closeBtn), nil, nil, tabs)
	d = dialog.NewCustomWithoutButtons("Preferences",
		container.NewGridWrap(u.dialogSize(640, 480), body), u.win)
	u.trackDialog(d)
	d.Show()
}

// scrollbackEntry is a precise editable field with preset choices and All.
func scrollbackEntry(p fyne.Preferences, apply func()) fyne.CanvasObject {
	e := widget.NewSelectEntry([]string{"500", "1000", "2000", "5000", "10000", "All"})
	e.SetText(p.StringWithFallback("scrollbackv", "2000"))
	e.OnChanged = func(v string) {
		if v == "" {
			return
		}
		if !strings.EqualFold(v, "All") {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err != nil || n < 100 {
				return // incomplete or invalid input; keep the last good value
			}
		}
		p.SetString("scrollbackv", v)
		apply()
	}
	return e
}
