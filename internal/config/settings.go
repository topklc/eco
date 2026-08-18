package config

import (
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
)

// Settings is an immutable snapshot of every user preference the running
// app reads. It replaces v1's scattered package-level globals: readers on
// any goroutine call Current() and get a consistent view, and applying
// preferences publishes one new snapshot rather than mutating state that
// other goroutines are reading.
type Settings struct {
	// appearance
	Theme         string // "System", "Light", "Dark"
	FontPts       float64
	BGColorHex    string
	AccentHex     string
	SelfColorHex  string
	SelfColorName string

	// chat display
	ColorfulNicks  bool
	ShowTimestamps bool
	TimeLayout     string // derived from the 12/24-hour choice
	ScrollbackCap  int64  // 0 means unlimited
	HideJoinParts  bool

	// channels
	AutoScanChannels bool
	JoinAllChannels  bool

	// notifications
	NotifyEnabled bool
	NotifySound   bool
	NotifyAll     bool

	// behavior & security
	Tooltips bool
	TLSOnly  bool
}

var current atomic.Pointer[Settings]

func init() { current.Store(defaults()) }

func defaults() *Settings {
	return &Settings{
		Theme: "System", FontPts: 16, SelfColorName: "Green",
		ColorfulNicks: true, ShowTimestamps: true, TimeLayout: "15:04",
		ScrollbackCap: 2000, AutoScanChannels: true, JoinAllChannels: true,
		NotifyEnabled: true, NotifySound: true, Tooltips: true,
	}
}

// Current returns the live settings snapshot. Never mutate the result.
func Current() *Settings { return current.Load() }

// Publish installs a new snapshot atomically.
func Publish(s *Settings) { current.Store(s) }

// FromPreferences reads the stored preferences into a fresh snapshot.
func FromPreferences(p fyne.Preferences) *Settings {
	s := defaults()
	s.Theme = p.StringWithFallback("theme", "System")
	s.FontPts = p.FloatWithFallback("fontpts", 16)
	if s.FontPts < 10 {
		s.FontPts = 10
	} else if s.FontPts > 28 {
		s.FontPts = 28
	}
	s.BGColorHex = p.String("bgcolor")
	s.AccentHex = p.String("accent")
	s.SelfColorHex = p.String("selfcolorhex")
	s.SelfColorName = p.StringWithFallback("selfcolor", "Green")

	s.ColorfulNicks = p.BoolWithFallback("colorfulnicks", true)
	s.ShowTimestamps = p.BoolWithFallback("showtimestamps", true)
	s.TimeLayout = "15:04"
	if p.StringWithFallback("timefmt", "24 hour") == "12 hour" {
		s.TimeLayout = "3:04 PM"
	}
	s.HideJoinParts = p.BoolWithFallback("hidejoinparts", false)

	// scrollback: a number of lines, or All (stored as 0, meaning no cap)
	sbv := p.StringWithFallback("scrollbackv", "2000")
	switch {
	case strings.EqualFold(sbv, "All"):
		s.ScrollbackCap = 0
	default:
		if n, err := strconv.Atoi(strings.TrimSpace(sbv)); err == nil && n >= 100 {
			s.ScrollbackCap = int64(n)
		}
	}

	s.AutoScanChannels = p.BoolWithFallback("autoscan", true)
	s.JoinAllChannels = p.BoolWithFallback("joinall", true)
	s.NotifyEnabled = p.BoolWithFallback("notify", true)
	s.NotifySound = p.BoolWithFallback("notifysound", true)
	s.NotifyAll = p.BoolWithFallback("notifyall", false)
	s.Tooltips = p.BoolWithFallback("tooltips", true)
	s.TLSOnly = p.BoolWithFallback("tlsonly", false)
	return s
}
