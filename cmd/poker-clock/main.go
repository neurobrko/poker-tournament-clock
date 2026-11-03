package main

import (
	"fmt"
	"os"

	"git.mpx.sk/elvis/poker_clock/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	program := tea.NewProgram(tui.NewModel(), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "poker-clock:", err)
		os.Exit(1)
	}
}
