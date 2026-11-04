package main

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2/app"
	"git.mpx.sk/elvis/poker_clock/internal/tui"
)

func main() {
	application := app.NewWithID("sk.mpx.pokerclock")
	if err := tui.RunDesktop(application); err != nil {
		fmt.Fprintln(os.Stderr, "poker-clock:", err)
		os.Exit(1)
	}
}
