package app

import (
	"log"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
)

// version is set by Main from cmd/eco, where the build stamps it
// (-ldflags "-X main.version=...").
var version = "dev"

// Main runs the eco application. It is the single seam exported to
// cmd/eco (blueprint M2: mechanical split, narrowest possible surface).
func Main(v string) {
	version = v
	log.Printf("eco %s", version)
	a := fyneapp.NewWithID("dev.toprakkilic.eco")
	a.SetIcon(appIcon)
	w := a.NewWindow("eco")
	w.SetIcon(appIcon)
	w.Resize(fyne.NewSize(1000, 640))

	applySettings(a)
	ui := NewUI(a, w)

	// start idle: no dialog, no auto connect; the user decides when to
	// connect via the settings menu or /server
	ui.showWelcome()

	w.ShowAndRun()
}
