package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icons/poker-clock.png
var pokerClockIconPNG []byte

func pokerClockIcon() fyne.Resource {
	return fyne.NewStaticResource("poker-clock-icon.png", pokerClockIconPNG)
}
