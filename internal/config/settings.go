package config

import (
	"encoding/json"
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
	// SmartJoinParts shows join/part/quit only for people who have spoken
	// recently (blueprint §4.3). It is ON by default: it is what makes a busy
	// channel readable, and the alternative default - every stranger's
	// connection churn - is the thing new users complain about first.
	SmartJoinParts bool
	// SpeakerMinutes is the smart filter's recency window, in minutes.
	SpeakerMinutes int

	// channels
	AutoScanChannels bool
	JoinAllChannels  bool

	// notifications
	NotifyEnabled bool
	NotifySound   bool
	NotifyAll     bool
	// HighlightWords are the extra words that count as a mention, beyond the
	// user's own nick (blueprint §4.4). Matching is whole-word and folded; the
	// rule lives in internal/state, which is the only place that evaluates it.
	HighlightWords []string
	// NetworkNotify is §4.4's per-network default notification level, by
	// profile name: "all", "mentions" or "mute". A profile with no entry
	// follows NotifyAll, the global preference. It is here rather than in the
	// archive because it is a fact about the PROFILE, which exists before any
	// connection does and still exists when the network is offline.
	NetworkNotify map[string]string

	// behavior & security
	Tooltips bool
	TLSOnly  bool

	// history & privacy (blueprint §5.4's privacy contract). Logging is ON by
	// default - that is the expectation every chat application has taught -
	// with a per-buffer opt-out that lives in the archive itself, these two
	// global switches, and a retention horizon.
	//
	// Retention is an archive.Retention value ("forever", "365", "90", "30",
	// "session"); it is a plain string here so that internal/config keeps
	// depending on nothing.
	Retention     string
	ExcludeDMLogs bool // never write private messages to disk
	DMLogAnswered bool // the first-DM logging question has been answered
}

var current atomic.Pointer[Settings]

func init() { current.Store(defaults()) }

func defaults() *Settings {
	return &Settings{
		Theme: "System", FontPts: 16, SelfColorName: "Green",
		ColorfulNicks: true, ShowTimestamps: true, TimeLayout: "15:04",
		ScrollbackCap: 2000, AutoScanChannels: true, JoinAllChannels: true,
		NotifyEnabled: true, NotifySound: true, Tooltips: true,
		SmartJoinParts: true, SpeakerMinutes: 10,
		Retention: "forever",
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
	s.SmartJoinParts = p.BoolWithFallback("smartjoinparts", true)
	s.SpeakerMinutes = int(p.FloatWithFallback("speakerminutes", 10))
	if s.SpeakerMinutes < 1 {
		s.SpeakerMinutes = 1
	} else if s.SpeakerMinutes > 120 {
		s.SpeakerMinutes = 120
	}

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
	s.HighlightWords = ParseHighlightWords(p.String("highlightwords"))
	s.NetworkNotify = loadNetworkNotify(p)
	s.Tooltips = p.BoolWithFallback("tooltips", true)
	s.TLSOnly = p.BoolWithFallback("tlsonly", false)

	s.Retention = p.StringWithFallback("retention", "forever")
	s.ExcludeDMLogs = p.BoolWithFallback("logexcludedms", false)
	s.DMLogAnswered = p.BoolWithFallback("dmloganswered", false)
	return s
}

// ParseHighlightWords splits the stored highlight-word list. The stored form is
// what the user typed into one field: words separated by commas or newlines,
// because asking someone to learn a syntax for a list of words is asking for a
// syntax error instead of a highlight.
//
// Blanks and duplicates are dropped, and the order the user wrote is kept -
// nothing here folds case, because folding belongs to the network's CASEMAPPING
// and is done where the match is (internal/state/highlight.go).
func ParseHighlightWords(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		w := strings.TrimSpace(f)
		if w == "" || seen[strings.ToLower(w)] {
			continue
		}
		seen[strings.ToLower(w)] = true
		out = append(out, w)
	}
	return out
}

// FormatHighlightWords writes a word list back into the field's form.
func FormatHighlightWords(words []string) string { return strings.Join(words, ", ") }

// networkNotifyKey is where the per-network levels are stored, as one JSON
// object: profile name -> level.
const networkNotifyKey = "notifylevels"

// loadNetworkNotify reads the per-network default levels.
func loadNetworkNotify(p fyne.Preferences) map[string]string {
	raw := p.String(networkNotifyKey)
	if raw == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// SetNetworkNotify records one network's default notification level. An empty
// level removes the entry, putting that network back on the global preference.
// The caller republishes the snapshot (applySettings) as it does for every
// other preference.
func SetNetworkNotify(p fyne.Preferences, profile, level string) {
	if p == nil || profile == "" {
		return
	}
	m := loadNetworkNotify(p)
	if m == nil {
		m = map[string]string{}
	}
	if level == "" {
		delete(m, profile)
	} else {
		m[profile] = level
	}
	if len(m) == 0 {
		p.RemoveValue(networkNotifyKey)
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return // unreachable: map[string]string always marshals
	}
	p.SetString(networkNotifyKey, string(raw))
}
