package config

import (
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// TestSaveNeverWritesPlaintextSecrets is the security floor: on a machine
// with no keyring, eco must drop the password rather than persist it to
// disk (v1 wrote it into the preferences JSON), and must say so.
func TestSaveNeverWritesPlaintextSecrets(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := a.Preferences()

	set := keyringSet
	keyringSet = func(service, user, password string) error { return errors.New("no keyring") }
	defer func() { keyringSet = set }()

	warning := Save(p, []Network{{
		Name: "n", Server: "irc.example.org", Nick: "someone",
		SASLUser: "someone", SASLPass: "hunter2-sasl", ServerPass: "hunter2-pass",
	}})

	if warning == "" {
		t.Error("expected a warning when the keyring refuses the secret")
	}
	raw := p.String(networksPrefKey)
	for _, secret := range []string{"hunter2-sasl", "hunter2-pass"} {
		if strings.Contains(raw, secret) {
			t.Errorf("secret %q was written to preferences: %s", secret, raw)
		}
	}
}

// TestSaveWithKeyringReportsNoWarning covers the happy path: secrets go to
// the keyring, still never to disk, and the user is not warned.
func TestSaveWithKeyringReportsNoWarning(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := a.Preferences()

	stored := map[string]string{}
	set := keyringSet
	keyringSet = func(service, user, password string) error {
		stored[user] = password
		return nil
	}
	defer func() { keyringSet = set }()

	n := Network{Name: "n", Server: "irc.example.org", Nick: "someone",
		SASLUser: "someone", SASLPass: "hunter2-sasl"}
	if warning := Save(p, []Network{n}); warning != "" {
		t.Errorf("unexpected warning: %s", warning)
	}
	if stored[keyringKey(n)] != "hunter2-sasl" {
		t.Errorf("secret did not reach the keyring: %v", stored)
	}
	if strings.Contains(p.String(networksPrefKey), "hunter2-sasl") {
		t.Error("secret was also written to preferences")
	}
}
