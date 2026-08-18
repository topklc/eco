package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

// version is stamped by the build (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	log.Printf("eco %s", version)
	a := app.NewWithID("dev.toprakkilic.eco")
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
