// Package notify sends desktop notifications and the mention ping. The
// backends are feature-detected at first use; every argv keeps the "--"
// guard so a message starting with "-" can never be read as a flag.
package notify

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
)

// Notification behavior, set from applySettings.

const pingSound = "/usr/share/sounds/freedesktop/stereo/message-new-instant.oga"

// binary paths resolve once: LookPath walks PATH per call otherwise
var (
	notifyOnce     sync.Once
	notifySendPath string
	playerOnce     sync.Once
	playerPath     string
	pingSoundOK    bool
)

func resolveNotifySend() string {
	notifyOnce.Do(func() {
		if p, err := exec.LookPath("notify-send"); err == nil {
			notifySendPath = p
		}
	})
	return notifySendPath
}

func resolvePlayer() (string, bool) {
	playerOnce.Do(func() {
		if _, err := os.Stat(pingSound); err == nil {
			pingSoundOK = true
		}
		for _, player := range []string{"paplay", "pw-play"} {
			if p, err := exec.LookPath(player); err == nil {
				playerPath = p
				break
			}
		}
	})
	return playerPath, pingSoundOK
}

// notify raises a desktop notification and optionally a ping sound for a
// highlight (mention or private message). notify-send is preferred because
// Fyne's own dbus path fails silently on some desktops.
// Notifier delivers desktop notifications, falling back to the toolkit's
// own notification when no system notifier is available.
type Notifier struct {
	Fallback func(title, body string)
}

// Send posts a notification; sound controls the mention ping.
func (n *Notifier) Send(title, body string, sound bool) {
	go func() {
		if p := resolveNotifySend(); p != "" {
			// "--" stops option parsing: a message starting with "-" must
			// never be interpreted as a notify-send flag
			exec.Command(p, "-a", "eco", "-i", "internet-chat", "--", title, body).Run()
		} else if n.Fallback != nil {
			n.Fallback(title, body)
		}
	}()
	if sound {
		go PlayPing()
	}
}

// PlayPing plays the mention sound through whatever player exists.
func PlayPing() {
	if runtime.GOOS == "windows" {
		exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"[System.Media.SystemSounds]::Notification.Play()").Run()
		return
	}
	if p, ok := resolvePlayer(); ok && p != "" {
		exec.Command(p, pingSound).Run()
	}
}
