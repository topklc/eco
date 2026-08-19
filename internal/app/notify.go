package app

import (
	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/notify"
)

// notify posts a desktop notification for a mention or message, honoring
// the notification preferences.
//
// The indirection is a seam, not decoration: a notification ends in an exec of
// notify-send, so a test that wants to know WHETHER one was raised has to be
// able to answer that without one actually happening. Since M9 the decision
// itself is upstream - the store evaluates §4.4 and says so on the change - and
// this is only the doing of it.
func (u *UI) notify(title, body string) {
	if u.notifier != nil {
		u.notifier(title, body)
		return
	}
	u.desktopNotify(title, body)
}

func (u *UI) desktopNotify(title, body string) {
	if !config.Current().NotifyEnabled {
		return
	}
	n := notify.Notifier{Fallback: func(t, b string) {
		u.app.SendNotification(fyne.NewNotification(t, b))
	}}
	n.Send(title, body, config.Current().NotifySound)
}
