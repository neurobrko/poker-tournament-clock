package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/fyne/v2/app"
	"git.mpx.sk/elvis/poker_clock/internal/ui"
)

func main() {
	application := app.NewWithID("sk.mpx.pokerclock")
	if err := ui.RunDesktop(application); err != nil {
		reportError(err)
		os.Exit(1)
	}
}

func reportError(err error) {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "poker-clock:", err)
		return
	}
	configDir, configErr := os.UserConfigDir()
	if configErr != nil {
		return
	}
	dir := filepath.Join(configDir, "poker-clock")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	file, openErr := os.OpenFile(filepath.Join(dir, "errors.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	_, _ = fmt.Fprintf(file, "%s poker-clock: %v\n", time.Now().Format(time.RFC3339), err)
	_ = file.Close()
}
