package app

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
	"github.com/toprakkilic/eco/internal/render"
	ecotheme "github.com/toprakkilic/eco/internal/ui/theme"
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
		items = append(items, newSwatch(c, func() { set(render.ColorToHex(c)) }))
	}
	pick := widget.NewButton("Custom", func() {
		// the native OS color dialog: fyne's built-in wheel renders badly
		go func() {
			// the wheel opens on the current choice, or on the accent token
			start := color.Color(ecotheme.Color(ecotheme.Accent,
				u.app.Settings().ThemeVariant()))
			if c, ok := render.ParseHexColor(p.String(prefKey)); ok {
				start = c
			}
			picked, err := zenity.SelectColor(zenity.Color(start))
			if err == nil && picked != nil {
				fyne.Do(func() { set(render.ColorToHex(picked)) })
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
		u.renderBuffer()
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

	// the preset strips live in the theme package beside the tokens they
	// override, so no colour the app can display is declared in a widget
	accentStd := ecotheme.AccentPresets()
	nickStd := ecotheme.NickPresets()
	bgStd := ecotheme.BackgroundPresets()

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

	history := u.historyPane(apply)

	pad := func(o fyne.CanvasObject) fyne.CanvasObject {
		return container.NewPadded(container.NewVScroll(o))
	}
	// lazy tabs: only the visible page is laid out
	names := []string{"Appearance", "Chat", "Channels", "Notifications", "Behavior", "Shortcuts", "History", "Security"}
	pages := []fyne.CanvasObject{appearance, chat, channels, notifications, behavior, u.keybindsPane(), history, security}
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

// retentionChoices are the retention horizons, in the order they are offered.
// The labels are the user's vocabulary; the values are archive.Retention's.
var retentionChoices = []struct{ label, value string }{
	{"Keep forever", "forever"},
	{"Keep for a year", "365"},
	{"Keep for 90 days", "90"},
	{"Keep for 30 days", "30"},
	{"Only while eco is open", "session"},
}

// historyPane is the privacy contract as a person meets it (blueprint §5.4 and
// §5.6): what is kept, for how long, whether private messages are included, and
// the one sentence about encryption that eco owes anyone who trusts it with a
// year of their conversations.
//
// The per-buffer opt-out is deliberately NOT here. It belongs on the buffer, in
// its right-click menu, where the decision is actually made.
func (u *UI) historyPane(apply func()) fyne.CanvasObject {
	p := u.app.Preferences()

	labels := make([]string, 0, len(retentionChoices))
	for _, c := range retentionChoices {
		labels = append(labels, c.label)
	}
	cur := p.StringWithFallback("retention", "forever")
	retention := widget.NewSelect(labels, nil)
	retention.SetSelected(labels[0])
	for _, c := range retentionChoices {
		if c.value == cur {
			retention.SetSelected(c.label)
		}
	}
	retention.OnChanged = func(picked string) {
		for _, c := range retentionChoices {
			if c.label == picked {
				p.SetString("retention", c.value)
			}
		}
		apply()
		for _, s := range u.sessions {
			s.applyRetention()
		}
	}

	excludeDMs := widget.NewCheck("Never save private messages", nil)
	excludeDMs.SetChecked(p.BoolWithFallback("logexcludedms", false))
	excludeDMs.OnChanged = func(v bool) {
		p.SetBool("logexcludedms", v)
		p.SetBool("dmloganswered", true) // an explicit answer; do not ask again
		apply()
		for _, s := range u.sessions {
			s.ApplyDMLogging(v)
		}
	}

	note := widget.NewRichTextFromMarkdown(
		"eco keeps your conversations in a database on **this computer**, one file per " +
			"network, so history and unread counts survive a restart and can be searched.\n\n" +
			"**The file is not encrypted.** Anyone who can read your files — another " +
			"account on a shared machine, a backup, someone with the disk — can read your " +
			"messages. eco does not encrypt it and does not pretend to: use full-disk or " +
			"home-directory encryption if that matters to you.\n\n" +
			"To stop saving one channel or one person, right-click it in the sidebar. " +
			"“Erase saved history” there deletes what is already stored, including its " +
			"search index.")
	note.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		widget.NewForm(widget.NewFormItem("Keep history", retention)),
		excludeDMs,
		widget.NewSeparator(),
		note,
	)
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
