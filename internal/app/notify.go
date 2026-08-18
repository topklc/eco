package app

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
)

// Notification behavior, set from applySettings.
var (
	notifyEnabled atomic.Bool
	soundEnabled  atomic.Bool
	notifyAll     atomic.Bool // notify for every live message, not just highlights
)

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
func (u *UI) notify(title, body string) {
	if !notifyEnabled.Load() {
		return
	}
	go func() {
		if p := resolveNotifySend(); p != "" {
			// "--" stops option parsing: a message starting with "-" must
			// never be interpreted as a notify-send flag
			exec.Command(p, "-a", "eco", "-i", "internet-chat", "--", title, body).Run()
		} else {
			u.app.SendNotification(fyne.NewNotification(title, body))
		}
	}()
	if soundEnabled.Load() {
		go playPing()
	}
}

func playPing() {
	if runtime.GOOS == "windows" {
		exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"[System.Media.SystemSounds]::Notification.Play()").Run()
		return
	}
	if p, ok := resolvePlayer(); ok && p != "" {
		exec.Command(p, pingSound).Run()
	}
}
