// Package config owns everything eco persists: server/identity profiles,
// their secrets (system keyring only - never plaintext on disk), STS
// policies and the preference-backed stores the UI reads.
package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"github.com/zalando/go-keyring"
)

// randSuffix makes a default nickname unlikely to collide.
func randSuffix() string {
	b := make([]byte, 2)
	rand.Read(b)
	return fmt.Sprintf("%d", int(b[0])%90+10)
}

// Keyring access goes through these seams so tests can simulate a machine
// with no Secret Service running.
var (
	keyringSet    = keyring.Set
	keyringGetFn  = keyring.Get
	keyringDelete = keyring.Delete
)

const keyringService = "eco"

func keyringKey(n Network) string     { return n.Server + "|" + n.SASLUser }
func keyringPassKey(n Network) string { return n.Server + "|PASS" }

func keyringGet(key string) (string, bool) {
	if pw, err := keyringGetFn(keyringService, key); err == nil {
		return pw, true
	}
	return "", false
}

// Network is a saved server + identity profile.
type Network struct {
	Name       string `json:"name"`
	Server     string `json:"server"`
	Port       string `json:"port"`
	TLS        bool   `json:"tls"`
	ServerPass string `json:"server_pass"`
	Nick       string `json:"nick"`
	SASLUser   string `json:"sasl_user"`
	SASLPass   string `json:"sasl_pass"`
	SASLMech   string `json:"sasl_mech"`
	Channels   string `json:"channels"`
	Pinned     bool   `json:"pinned"`
}

const networksPrefKey = "networks_v1"

// loadNetworkMeta parses the profile list WITHOUT touching the keyring.
// Hot paths (sidebar refresh, row menus) need only names and pin flags;
// keyring roundtrips are synchronous DBus and belong at connect time only.
func LoadMeta(p fyne.Preferences) []Network {
	var nets []Network
	if raw := p.String(networksPrefKey); raw != "" {
		if json.Unmarshal([]byte(raw), &nets) == nil && len(nets) > 0 {
			return nets
		}
	}
	return []Network{
		{Name: "Ergo Testnet", Server: "testnet.ergo.chat", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix()},
		{Name: "Libera.Chat", Server: "irc.libera.chat", Port: "6697", TLS: true,
			Nick: "newbie" + randSuffix(), Channels: ""},
	}
}

func Load(p fyne.Preferences) []Network {
	nets := LoadMeta(p)
	for i := range nets {
		if nets[i].SASLPass == "" && nets[i].SASLUser != "" {
			if pw, ok := keyringGet(keyringKey(nets[i])); ok {
				nets[i].SASLPass = pw
			}
		}
		if nets[i].ServerPass == "" {
			if pw, ok := keyringGet(keyringPassKey(nets[i])); ok {
				nets[i].ServerPass = pw
			}
		}
	}
	return nets
}

// dropKeyringFor removes a profile's stored secrets, used when the profile
// is deleted or its identity (server or account) changes so the entries
// would otherwise be orphaned forever.
func DropSecrets(n Network) {
	if n.SASLUser != "" {
		keyringDelete(keyringService, keyringKey(n))
	}
	keyringDelete(keyringService, keyringPassKey(n))
}

// Save persists profiles to preferences. Secrets go to the system keyring
// and NEVER to disk: if no keyring is available the password is dropped from
// the saved profile and Save reports it, so the app can tell the user their
// password was not remembered. v1 silently wrote the secret into the
// preferences JSON in that case.
func Save(p fyne.Preferences, nets []Network) (warning string) {
	clean := make([]Network, len(nets))
	copy(clean, nets)
	refused := false
	for i := range clean {
		if clean[i].SASLUser != "" && clean[i].SASLPass != "" {
			if err := keyringSet(keyringService, keyringKey(clean[i]), clean[i].SASLPass); err != nil {
				refused = true
			}
			clean[i].SASLPass = "" // never persisted to disk either way
		}
		if clean[i].ServerPass != "" {
			if err := keyringSet(keyringService, keyringPassKey(clean[i]), clean[i].ServerPass); err != nil {
				refused = true
			}
			clean[i].ServerPass = ""
		}
	}
	raw, _ := json.Marshal(clean)
	p.SetString(networksPrefKey, string(raw))
	if refused {
		return "No system keyring is available, so your password was not saved. " +
			"eco never writes passwords to disk in plain text; you will be asked again next time."
	}
	return ""
}

// autojoinHas reports whether ch is in the profile's autojoin list.
func AutojoinHas(p fyne.Preferences, profile, ch string) bool {
	for _, n := range LoadMeta(p) {
		if n.Name != profile {
			continue
		}
		for _, c := range strings.Split(n.Channels, ",") {
			if strings.EqualFold(strings.TrimSpace(c), ch) {
				return true
			}
		}
	}
	return false
}

// autojoinToggle adds or removes ch from the profile's autojoin list and
// returns the new membership state.
func AutojoinToggle(p fyne.Preferences, profile, ch string) bool {
	nets := LoadMeta(p)
	added := false
	for i := range nets {
		if nets[i].Name != profile {
			continue
		}
		var keep []string
		found := false
		for _, c := range strings.Split(nets[i].Channels, ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if strings.EqualFold(c, ch) {
				found = true
				continue
			}
			keep = append(keep, c)
		}
		if !found {
			keep = append(keep, ch)
			added = true
		}
		nets[i].Channels = strings.Join(keep, ", ")
		Save(p, nets)
		break
	}
	return added
}

// setPinned flips a profile's sidebar pin.
func SetPinned(p fyne.Preferences, profile string, pinned bool) {
	nets := LoadMeta(p)
	for i := range nets {
		if nets[i].Name == profile {
			nets[i].Pinned = pinned
			Save(p, nets)
			return
		}
	}
}
