package app

import (
	"fyne.io/fyne/v2"

	"github.com/toprakkilic/eco/internal/notify"
	"github.com/toprakkilic/eco/internal/config"
)

// notify posts a desktop notification for a mention or message, honoring
// the notification preferences.
func (u *UI) notify(title, body string) {
	if !config.Current().NotifyEnabled {
		return
	}
	n := notify.Notifier{Fallback: func(t, b string) {
		u.app.SendNotification(fyne.NewNotification(t, b))
	}}
	n.Send(title, body, config.Current().NotifySound)
}
