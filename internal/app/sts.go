package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

// IRCv3 strict transport security (the "sts" capability): once a server
// advertises a policy, we remember it and always connect over TLS, even if
// the profile says otherwise. Policies expire per the advertised duration.

type stsPolicy struct {
	Port    string `json:"port"`    // TLS port advertised during the upgrade
	Expires int64  `json:"expires"` // unix seconds
}

const stsPrefKey = "sts_policies_v1"

func loadSTS(p fyne.Preferences) map[string]stsPolicy {
	out := map[string]stsPolicy{}
	if raw := p.String(stsPrefKey); raw != "" {
		json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func saveSTS(p fyne.Preferences, m map[string]stsPolicy) {
	raw, _ := json.Marshal(m)
	p.SetString(stsPrefKey, string(raw))
}

// stsLookup returns the unexpired policy for a host, if any.
func stsLookup(p fyne.Preferences, host string) (stsPolicy, bool) {
	m := loadSTS(p)
	pol, ok := m[strings.ToLower(host)]
	if !ok || pol.Expires < time.Now().Unix() {
		return stsPolicy{}, false
	}
	return pol, true
}

// stsRecord stores or clears a policy. Per the spec, duration 0 revokes it.
func stsRecord(p fyne.Preferences, host, port string, durationSecs int64) {
	m := loadSTS(p)
	key := strings.ToLower(host)
	if durationSecs <= 0 {
		delete(m, key)
	} else {
		m[key] = stsPolicy{Port: port, Expires: time.Now().Unix() + durationSecs}
	}
	// prune expired entries while we are writing anyway; stsLookup already
	// treats them as absent, this just keeps the stored JSON from growing
	now := time.Now().Unix()
	for k, pol := range m {
		if pol.Expires < now {
			delete(m, k)
		}
	}
	saveSTS(p, m)
}

// parseSTSValue parses "duration=300,port=6697" into its parts.
func parseSTSValue(v string) (port string, duration int64) {
	for _, kv := range strings.Split(v, ",") {
		if s, ok := strings.CutPrefix(kv, "port="); ok {
			port = s
		}
		if s, ok := strings.CutPrefix(kv, "duration="); ok {
			duration, _ = strconv.ParseInt(s, 10, 64)
		}
	}
	return
}
