package main

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
)

// Client-side ignore list and per-channel join/part hiding. Both are plain
// preference-backed sets consulted from the session goroutines, so they
// live behind a mutex rather than fyne.Do.
var (
	ignoreMu sync.Mutex
	ignored  = map[string]bool{} // lowercase nicks
	jpHidden = map[string]bool{} // network + "\x00" + lowercase channel
)

func loadIgnorePrefs(p fyne.Preferences) {
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	loadSet(p.String("ignored_v1"), ignored)
	loadSet(p.String("hidejp_v1"), jpHidden)
}

func loadSet(raw string, dst map[string]bool) {
	var list []string
	if raw == "" || json.Unmarshal([]byte(raw), &list) != nil {
		return
	}
	for _, v := range list {
		dst[v] = true
	}
}

func saveSet(p fyne.Preferences, key string, src map[string]bool) {
	list := make([]string, 0, len(src))
	for v := range src {
		list = append(list, v)
	}
	raw, _ := json.Marshal(list)
	p.SetString(key, string(raw))
}

func isIgnored(nick string) bool {
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	return ignored[strings.ToLower(nick)]
}

// toggleIgnore flips a nick's ignore state and returns the new state.
func toggleIgnore(p fyne.Preferences, nick string) bool {
	key := strings.ToLower(nick)
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	if ignored[key] {
		delete(ignored, key)
	} else {
		ignored[key] = true
	}
	saveSet(p, "ignored_v1", ignored)
	return ignored[key]
}

func jpKey(network, ch string) string { return network + "\x00" + strings.ToLower(ch) }

// joinPartsHiddenFor combines the global preference with the per-channel
// override.
func joinPartsHiddenFor(network, ch string) bool {
	if hideJoinParts.Load() {
		return true
	}
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	return jpHidden[jpKey(network, ch)]
}

// toggleJPHidden flips a channel's join/part hiding and returns the new state.
func toggleJPHidden(p fyne.Preferences, network, ch string) bool {
	key := jpKey(network, ch)
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	if jpHidden[key] {
		delete(jpHidden, key)
	} else {
		jpHidden[key] = true
	}
	saveSet(p, "hidejp_v1", jpHidden)
	return jpHidden[key]
}

// ignoredList returns the ignored nicks, sorted.
func ignoredList() []string {
	ignoreMu.Lock()
	defer ignoreMu.Unlock()
	out := make([]string, 0, len(ignored))
	for n := range ignored {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
