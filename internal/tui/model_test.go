package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestTransitionNoticeIncludesBreakReminder(t *testing.T) {
	schedule := []config.Segment{
		{Kind: config.SegmentLevel},
		{Kind: config.SegmentBreak, Reminder: "Chip up the 5-value chip."},
	}
	notice := transitionNotice([]domain.Transition{{Kind: domain.TransitionBreakStarted, ToIndex: 2}}, schedule)
	if !strings.Contains(notice, "Chip up! (5)") {
		t.Fatalf("break transition notice omitted reminder: %q", notice)
	}
}

func TestMinuteWarningOnlyFiresBeforeLevelChanges(t *testing.T) {
	schedule := []config.Segment{{Kind: config.SegmentLevel, SmallBlind: 5, BigBlind: 10}, {Kind: config.SegmentLevel, SmallBlind: 10, BigBlind: 20}, {Kind: config.SegmentBreak}}
	summary := tournament.Summary{TotalStack: 100, ActivePlayers: 2}
	checkpoint := store.Checkpoint{Running: true, SegmentIndex: 1, Remaining: 62 * time.Second}
	if !shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("did not detect crossing the one-minute threshold before a level")
	}
	checkpoint.Remaining = time.Minute + time.Second
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("repeated warning after crossing the threshold")
	}
	checkpoint.Remaining = 62 * time.Second
	checkpoint.SegmentIndex = 2
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("warned before a break instead of a level")
	}
	checkpoint.SegmentIndex = 3
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("warned before final schedule segment")
	}
	checkpoint.SegmentIndex = 1
	checkpoint.HoldReason = "waiting for chips"
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("warned during held clock")
	}
}

func TestLevelChangeChimeIsPrestartedForPlayerLatency(t *testing.T) {
	schedule := []config.Segment{{Kind: config.SegmentLevel, SmallBlind: 5, BigBlind: 10}, {Kind: config.SegmentLevel, SmallBlind: 10, BigBlind: 20}}
	summary := tournament.Summary{TotalStack: 100, ActivePlayers: 2}
	checkpoint := store.Checkpoint{Running: true, SegmentIndex: 1, Remaining: 2 * time.Second}
	if !shouldPlayLevelChangeSound(checkpoint, time.Second, schedule, summary) {
		t.Fatal("did not pre-start level chime one second before transition")
	}
	checkpoint.Remaining = time.Second
	if shouldPlayLevelChangeSound(checkpoint, time.Second, schedule, summary) {
		t.Fatal("level chime should not start at the transition itself")
	}
}

func TestAddOnClosedWarningAppearsBeforePlayerPicker(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.record.Config = template
	model.openPurchase(tournament.PurchaseAddOn)
	if model.modal == nil || model.modal.kind != dialogMessage || !strings.Contains(model.modal.message, "Add-on window is closed") {
		t.Fatalf("closed add-on window did not warn first: %+v", model.modal)
	}
}

func TestLevelSelectorStartsOnCurrentLevelAndSupportsHomeEnd(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.service = tournament.NewService(nil)
	model.record.Config = template
	model.record.Checkpoint.SegmentIndex = 5
	model.openLevelSelect()
	if model.modal == nil || model.modal.kind != dialogSelectSegment {
		t.Fatalf("level selector did not open: %+v", model.modal)
	}
	if model.modal.cursor != 4 || !strings.Contains(model.modal.choices[4], "CURRENT") {
		t.Fatalf("current level cursor/label = %d / %q", model.modal.cursor, model.modal.choices[4])
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyHome}))
	if model.modal.cursor != 0 {
		t.Fatalf("Home cursor = %d, want 0", model.modal.cursor)
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyEnd}))
	if model.modal.cursor != len(model.modal.choices)-1 {
		t.Fatalf("End cursor = %d, want %d", model.modal.cursor, len(model.modal.choices)-1)
	}
	model.record.Checkpoint.SegmentIndex = 4
	model.openLevelSelect()
	if model.modal.cursor != 3 || !strings.Contains(model.modal.choices[3], "CURRENT") || strings.Contains(model.modal.choices[2], "CURRENT") {
		t.Fatalf("break selection current marker/cursor = %d / %q / %q", model.modal.cursor, model.modal.choices[3], model.modal.choices[2])
	}
}

func TestSoundPickersStoreLevelAndMinuteWarningSeparately(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := NewModel()
	model.browser = &fileBrowser{purpose: browseLevelSound}
	model.browserSelected("level.wav")
	model.browser = &fileBrowser{purpose: browseMinuteSound}
	model.browserSelected("minute.mp3")
	if model.prefs.SoundFile != "level.wav" || model.prefs.MinuteSoundFile != "minute.mp3" {
		t.Fatalf("sound preferences conflated: level=%q minute=%q", model.prefs.SoundFile, model.prefs.MinuteSoundFile)
	}
}

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
	for _, name := range []string{"a.yaml", "b.yml", ".hidden.yaml", "ignore.txt", "nested"} {
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
		t.Fatalf("template browser should hide unselectable files by default = %v", names)
	}
	if strings.Contains(joined, ".hidden.yaml") {
		t.Fatalf("template browser should hide dot files by default: %v", names)
	}
	if err := browser.toggleUnsupported(); err != nil {
		t.Fatal(err)
	}
	foundUnsupported := false
	for _, entry := range browser.entries {
		if entry.name == "ignore.txt" {
			foundUnsupported = true
			if entry.selectable {
				t.Fatal("unsupported file became selectable")
			}
		}
	}
	if !foundUnsupported {
		t.Fatal("show-unselectable mode did not reveal unsupported files")
	}
	if err := browser.toggleHidden(); err != nil {
		t.Fatal(err)
	}
	foundHidden := false
	for _, entry := range browser.entries {
		if entry.name == ".hidden.yaml" {
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatal("show-hidden mode did not reveal dot files")
	}
	if err := browser.toggleHidden(); err != nil {
		t.Fatal(err)
	}
	if err := browser.toggleUnsupported(); err != nil {
		t.Fatal(err)
	}
	selectable := make(map[string]bool)
	for _, entry := range browser.entries {
		selectable[entry.name] = entry.selectable
	}
	if !selectable["a.yaml"] || !selectable["b.yml"] || selectable["ignore.txt"] {
		t.Fatalf("template file selection state = %+v", selectable)
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
	load, err := newFileBrowser(browseTournamentDB, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := load.toggleUnsupported(); err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, entry := range load.entries {
		if entry.name == "a.yaml" {
			visible = true
			if entry.selectable {
				t.Fatal("YAML template is selectable as a tournament database")
			}
		}
	}
	if !visible {
		t.Fatal("load browser hid the YAML file rather than displaying it as unsupported")
	}
}

func TestFileBrowserHomeAndEndKeysSelectBounds(t *testing.T) {
	browser, err := newFileBrowser(browseTournamentDB, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		browser.entries = append(browser.entries, browserEntry{name: fmt.Sprintf("%d.db", i), selectable: true})
	}
	model := NewModel()
	model.browser = browser
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyEnd}))
	if browser.cursor != len(browser.entries)-1 {
		t.Fatalf("End cursor = %d, want %d", browser.cursor, len(browser.entries)-1)
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyHome}))
	if browser.cursor != 0 {
		t.Fatalf("Home cursor = %d, want 0", browser.cursor)
	}
}

func TestSaveDatabaseBrowserNOpensFilenameDialog(t *testing.T) {
	browser, err := newFileBrowser(browseSaveDatabase, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.browser = browser
	model.pendingName = "Friday game"
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'n'}}))
	if model.modal == nil || model.modal.kind != dialogText || model.modal.textPurpose != textBrowserFilename {
		t.Fatalf("N did not open new filename prompt: %+v", model.modal)
	}
	if model.modal.input != "" || !strings.Contains(model.modal.prompt, "friday-game.sqlite") {
		t.Fatalf("filename prompt should be empty with a visible default: %+v", model.modal)
	}
	if view := model.View(); !strings.Contains(view, "New file") || !strings.Contains(view, "friday-game.sqlite") {
		t.Fatalf("filename prompt is hidden behind browser: %s", view)
	}
	model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'a', 'b', 'c'}}))
	if model.modal.input != "abc" {
		t.Fatalf("filename input = %q, want abc", model.modal.input)
	}
}

func TestClockPresentationUsesLargeCenteredTimerAndZeroAnte(t *testing.T) {
	if got := formatAnte(config.AnteConfig{Kind: config.AnteNone}); got != "Ante  0" {
		t.Fatalf("no-ante label = %q", got)
	}
	if got := lipgloss.Height(largeTimer("20:00")); got != 5 {
		t.Fatalf("large timer height = %d, want 5 rows", got)
	}
}

func TestTournamentPanelShowsPlacesInsteadOfPlayerNames(t *testing.T) {
	model := NewModel()
	model.record.Name = "Friday game"
	model.summary.PrizePoolCents = 1000
	model.players = []tournament.Player{{ID: "p1", Name: "Alice"}, {ID: "p2", Name: "Bob"}}
	model.placePrizes = []domain.PlacePrize{{Place: 1, AmountCents: 600}, {Place: 2, AmountCents: 400}}
	view := model.leftPanel()
	if !strings.Contains(view, "Friday game") || !strings.Contains(view, "Winner") || !strings.Contains(view, "2nd place") {
		t.Fatalf("tournament panel missing name or place labels:\n%s", view)
	}
	if strings.Contains(view, "Alice") || strings.Contains(view, "Bob") {
		t.Fatalf("tournament panel exposes player names in prizes:\n%s", view)
	}
}

func TestMainLayoutUsesTwoFiveTwoColumnsAndFillsHeight(t *testing.T) {
	left, center, right := columnWidths(180)
	if left != 40 || center != 100 || right != 40 {
		t.Fatalf("180-column layout = %d/%d/%d, want 40/100/40", left, center, right)
	}
	left, center, right = columnWidths(120)
	if left != 26 || center != 66 || right != 28 {
		t.Fatalf("minimum layout = %d/%d/%d, want 26/66/28", left, center, right)
	}
	model := NewModel()
	model.screen = screenMain
	model.width, model.height = 120, 36
	if got := lipgloss.Height(model.mainView()); got != 36 {
		t.Fatalf("main view height = %d, want full 36-row screen", got)
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
