package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartupScreenOffersNewLoadQuit(t *testing.T) {
	model := NewModel()
	view := model.View()
	for _, text := range []string{"New tournament", "Load tournament", "Quit", "Enter open"} {
		if !strings.Contains(view, text) {
			t.Fatalf("startup screen missing %q:\n%s", text, view)
		}
	}
}

func TestQuitAlwaysAsksForConfirmation(t *testing.T) {
	model := NewModel()
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'q'}}))
	if model.modal == nil || model.modal.kind != dialogConfirm || model.modal.confirm != confirmQuit {
		t.Fatalf("Q did not open exit confirmation: %+v", model.modal)
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if model.modal != nil {
		t.Fatal("Escape did not cancel exit confirmation")
	}
}

func TestSmallTerminalExitConfirmationCanBeCompleted(t *testing.T) {
	model := NewModel()
	model.tooSmall = true
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'q'}}))
	if model.modal == nil || model.modal.confirm != confirmQuit {
		t.Fatalf("small-screen Q did not ask for confirmation: %+v", model.modal)
	}
	if view := model.View(); !strings.Contains(view, "Exit Poker Clock") || !strings.Contains(view, "Y/Enter confirm") {
		t.Fatalf("small-screen confirmation is not visible/operable:\n%s", view)
	}
	_, command := model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'y'}}))
	if command == nil {
		t.Fatal("confirmed small-screen exit did not return a quit command")
	}
}

func TestTextDialogTreatsSpaceAndQAsText(t *testing.T) {
	model := NewModel()
	model.modal = &dialog{kind: dialogText, input: "name"}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeySpace}))
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'q'}}))
	if model.modal.input != "name q" {
		t.Fatalf("text input = %q, want space and q appended", model.modal.input)
	}
	if model.suspendedModal != nil {
		t.Fatal("typed q unexpectedly opened global quit confirmation")
	}
}

func TestSmallTerminalWarningPreservesDialogAndKeepsGlobalShortcuts(t *testing.T) {
	model := NewModel()
	model.screen = screenMain
	model.tooSmall = true
	modal := &dialog{kind: dialogText, title: "Preserve me", input: "draft"}
	model.modal = modal
	if view := model.View(); !strings.Contains(view, "120 × 36") || !strings.Contains(view, "Q to quit") {
		t.Fatalf("small-terminal warning missing required guidance:\n%s", view)
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeySpace}))
	if model.modal != nil || model.suspendedModal != modal {
		t.Fatal("Space did not preserve/hide the open dialog")
	}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	if model.modal != modal || model.suspendedModal != nil {
		t.Fatal("resizing did not restore the preserved dialog")
	}
}

func TestFileBrowserFiltersTemplatesAndNeverChoosesExistingSavePath(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yml", "ignore.txt", "nested"} {
		path := filepath.Join(directory, name)
		if name == "nested" {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	browser, err := newFileBrowser(browseTemplate, directory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range browser.entries {
		names = append(names, entry.name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "a.yaml") || !strings.Contains(joined, "b.yml") || strings.Contains(joined, "ignore.txt") {
		t.Fatalf("template browser entries = %v", names)
	}
	save, err := newFileBrowser(browseSaveTemplate, directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := save.newPath("a.yaml"); err == nil {
		t.Fatal("save browser allowed an existing template path")
	}
	path, err := save.newPath("new-rules")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".yaml" {
		t.Fatalf("template extension = %q", filepath.Ext(path))
	}
}

func TestMainScreenIncludesRequiredKeyboardActions(t *testing.T) {
	model := NewModel()
	model.screen = screenMain
	view := model.mainView()
	for _, shortcut := range []string{"[B] Buy-in", "[P] Players", "[X] Bust", "[Space] Start/pause", "[Q] Exit"} {
		if !strings.Contains(view, shortcut) {
			t.Errorf("main screen missing %q", shortcut)
		}
	}
}
