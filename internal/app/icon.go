package app

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/icon.png
var iconBytes []byte

var appIcon = fyne.NewStaticResource("eco.png", iconBytes)
