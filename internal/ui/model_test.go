package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestTournamentEditorBuildsStructuredTemplateDraft(t *testing.T) {
	model := NewModel()
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'t'}})
	if model.modal == nil || model.modal.purpose != menuEditorSource || len(model.modal.choices) != 4 {
		t.Fatalf("editor source choices = %+v", model.modal)
	}
	model.modal.cursor = 2
	model.chooseMenu(model.modal)
	if model.screen != screenEditor || len(model.editorTemplate.Schedule) != 0 || model.editorName != "" {
		t.Fatalf("scratch editor = screen %v, template %+v", model.screen, model.editorTemplate)
	}
	model.openEditorForm(editorFormLevel, -1)
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormBreak, -1)
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormLevel, -1)
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormChip, -1)
	model.modal.formValues["value"], model.modal.formValues["color"] = "5", "red"
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormLevel, 0)
	model.modal.formValues["ante_kind"], model.modal.formValues["ante_amount"] = string(config.AntePerPlayer), "1"
	model.modal.formValues["reminder"] = "Ante begins next level"
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormBreak, 1)
	model.modal.formValues["chipup"], model.modal.formValues["reminder"] = "5", "Remove smallest chips"
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormPlayer, -1)
	model.modal.formValues["name"], model.modal.formValues["alias"] = "Alice", "Ace"
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormPrize, 0)
	model.modal.formValues["percentages"] = "70, 30"
	model.saveEditorForm(model.modal)
	model.openEditorForm(editorFormPurchases, -1)
	model.modal.formValues["initial_price"] = "3.00"
	model.modal.formValues["rebuys_enabled"] = "false"
	model.modal.formValues["deadline_at"] = string(config.DeadlineAtBreakEnd)
	model.modal.formValues["addons_enabled"] = "true"
	model.modal.formValues["addon_break"] = "1"
	model.saveEditorForm(model.modal)
	if err := model.editorTemplate.Validate(); err != nil {
		t.Fatalf("completed structured draft is invalid: %v", err)
	}
	if len(model.editorTemplate.Schedule) != 3 || model.editorTemplate.Players[0] != (config.Player{Name: "Alice", Alias: "Ace"}) || model.editorTemplate.Purchases.InitialBuyIn.Price != 300 {
		t.Fatalf("structured draft fields were not applied: %+v", model.editorTemplate)
	}
	model.moveEditorSchedule(1, 1)
	if model.editorTemplate.Schedule[1].Kind != config.SegmentLevel || model.editorTemplate.Schedule[2].Kind != config.SegmentBreak {
		t.Fatal("schedule reorder did not move selected segment")
	}
	model.moveEditorSchedule(2, -1)
	if err := model.editorTemplate.Validate(); err != nil {
		t.Fatalf("restored schedule order is invalid: %v", err)
	}
}

func TestTournamentEditorSavesNamelessTemplateAndCanCreateAfterSave(t *testing.T) {
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model.startEditor(template)
	model.editorName = "Sunday game"
	model.editorAfterSave = true
	model.saveEditorTemplate()
	if model.browser == nil || model.browser.purpose != browseEditorSaveTemplate {
		t.Fatalf("save template browser = %+v", model.browser)
	}
	path := filepath.Join(t.TempDir(), "saved.yaml")
	model.saveEditorTemplateFile(path)
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ParseTemplate(saved); err != nil {
		t.Fatal(err)
	}
	if model.browser == nil || model.browser.purpose != browseSaveDatabase || model.pendingName != "Sunday game" {
		t.Fatalf("create-after-save flow: browser=%+v name=%q", model.browser, model.pendingName)
	}
	model.onKey(keyEvent{Type: keyEsc})
	if model.screen != screenEditor {
		t.Fatalf("cancelled post-save creation returned to %v", model.screen)
	}
	model.editorAfterSave = false
	model.createEditorTournament()
	if model.browser == nil || model.browser.purpose != browseSaveDatabase {
		t.Fatalf("direct create browser = %+v", model.browser)
	}
}

func TestTemplateImportExportPlayerCheckboxDefaultsOff(t *testing.T) {
	model := NewModel()
	model.screen = screenMain
	model.chooseSettings(0)
	if model.modal == nil || model.modal.kind != dialogTemplateOptions || model.modal.includePlayers || !strings.Contains(model.modal.message, "Import will change the current roster") || !strings.Contains(model.modal.message, "export will include the current roster") {
		t.Fatalf("template export options = %+v", model.modal)
	}
	model.onKey(keyEvent{Type: keySpace})
	if !model.modal.includePlayers {
		t.Fatal("Space did not check the player roster option")
	}
	model.onKey(keyEvent{Type: keyEnter})
	if model.browser == nil || model.browser.purpose != browseSaveTemplate || !model.templatePlayersChecked {
		t.Fatalf("checked export options = browser %+v checked=%t", model.browser, model.templatePlayersChecked)
	}
	model.onKey(keyEvent{Type: keyEsc})
	if model.browser != nil || model.templatePlayersChecked {
		t.Fatal("cancelled template export kept the roster checkbox set")
	}
	model.chooseSettings(1)
	if model.modal == nil || model.modal.includePlayers {
		t.Fatalf("template import checkbox was not unchecked: %+v", model.modal)
	}
	model.onKey(keyEvent{Type: keyEnter})
	if model.browser == nil || model.browser.purpose != browseTemplate || model.templatePlayersChecked {
		t.Fatalf("default import options = browser %+v checked=%t", model.browser, model.templatePlayersChecked)
	}
	model.onKey(keyEvent{Type: keyEsc})
}

func TestTemplateImportWithoutCheckboxIgnoresTemplateRoster(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "template-roster-skip.sqlite"), "Import", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	model := NewModel()
	model.database, model.service = database, tournament.NewService(database)
	imported := template
	imported.Players = []config.Player{{Name: "Not imported"}}
	data, err := config.MarshalTemplate(imported)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "template.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	model.saveTemplateImport(path)
	players, err := model.service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 0 || len(model.record.Config.Players) != 0 {
		t.Fatalf("unchecked template import included players: roster=%+v config=%+v", players, model.record.Config.Players)
	}
}

func TestTemplateImportCheckboxMergesRosterAfterConflictChoices(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "template-roster-import.sqlite"), "Import", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	alice, err := service.RegisterPlayer(ctx, "Alice", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPlayerAlias(ctx, alice.ID, "Old", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.screen, model.database, model.service = screenMain, database, service
	model.refresh()
	imported := template
	imported.Purchases.AddOn.Chips = 6000
	imported.Players = []config.Player{{Name: "Alice", Alias: "Ace"}, {Name: "Bob", Alias: "Bee"}}
	data, err := config.MarshalTemplate(imported)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "template.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	model.templatePlayersChecked = true
	model.saveTemplateImport(path)
	if model.modal == nil || model.modal.purpose != menuPlayerImportConflict || model.record.Config.Purchases.AddOn.Chips != 5000 {
		t.Fatalf("import applied before roster conflict choice: modal=%+v chips=%d", model.modal, model.record.Config.Purchases.AddOn.Chips)
	}
	model.modal.cursor = 1
	model.chooseMenu(model.modal)
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if model.record.Config.Purchases.AddOn.Chips != 6000 || len(players) != 2 || players[0].Alias != "Ace" || players[1].Name != "Bob" {
		t.Fatalf("template/roster import result: chips=%d players=%+v", model.record.Config.Purchases.AddOn.Chips, players)
	}
}

func TestTemplateExportCheckboxUsesCurrentNonRemovedRoster(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Players = []config.Player{{Name: "Seed"}}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "template-roster-export.sqlite"), "Export", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	seeded, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePlayer(ctx, seeded[0].ID, "not attending", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	current, err := service.RegisterPlayer(ctx, "Current", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPlayerAlias(ctx, current.ID, "C", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.database, model.service = database, service
	uncheckedPath := filepath.Join(t.TempDir(), "export-without-players.yaml")
	model.saveBrowserDestination(uncheckedPath, browseSaveTemplate)
	uncheckedData, err := os.ReadFile(uncheckedPath)
	if err != nil {
		t.Fatal(err)
	}
	unchecked, err := config.ParseTemplate(uncheckedData)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchecked.Players) != 0 {
		t.Fatalf("unchecked export included players: %+v", unchecked.Players)
	}
	path := filepath.Join(t.TempDir(), "export.yaml")
	model.templatePlayersChecked = true
	model.saveBrowserDestination(path, browseSaveTemplate)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := config.ParseTemplate(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exported.Players, []config.Player{{Name: "Current", Alias: "C"}}) {
		t.Fatalf("template exported roster = %+v", exported.Players)
	}
}

func TestPlayerAliasesAppearOnlyInPlayersMenuAndCanBeEdited(t *testing.T) {
	model := NewModel()
	model.players = []tournament.Player{{ID: "alice", Name: "Alice", Alias: "Ace", Active: true}}
	labels := playerDialogLabels(model, &dialog{purpose: selectInfo, players: model.players})
	if len(labels) != 1 || labels[0] != "Alice (Ace) · active" {
		t.Fatalf("players menu label = %v", labels)
	}
	labels = playerDialogLabels(model, &dialog{purpose: selectPurchase, players: model.players})
	if len(labels) != 1 || labels[0] != "Alice · active" {
		t.Fatalf("non-players menu unexpectedly showed alias: %v", labels)
	}
	model.screen = screenMain
	model.record.Status = store.StatusNotStarted
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'p'}})
	menu := model.modal
	if len(menu.choices) != 8 || menu.choices[5] != "Edit player alias" || menu.choices[6] != "Import players from YAML" || menu.choices[7] != "Export players to YAML" {
		t.Fatalf("players menu choices = %v", menu.choices)
	}
	menu.cursor = 5
	model.chooseMenu(menu)
	if model.modal.purpose != selectAlias || len(model.modal.players) != 1 {
		t.Fatalf("alias editor selection = %+v", model.modal)
	}
	model.choosePlayer(model.modal, model.players[0])
	if model.modal.textPurpose != textPlayerAlias || model.modal.input != "Ace" {
		t.Fatalf("alias editor dialog = %+v", model.modal)
	}
}

func TestPlayerYAMLImportResolvesConflictsBeforeApplyingRosterMerge(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "player-import.sqlite"), "Import", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	alice, err := service.RegisterPlayer(ctx, "Alice", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPlayerAlias(ctx, alice.ID, "Old", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.screen, model.database, model.service = screenMain, database, service
	model.refresh()
	path := filepath.Join(t.TempDir(), "players.yaml")
	if err := os.WriteFile(path, []byte("- name: Alice\n  alias: Ace\n- name: Bob\n  alias: Bee\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model.importPlayersFrom(path)
	if model.modal == nil || model.modal.purpose != menuPlayerImportConflict {
		t.Fatalf("import did not prompt for existing player: %+v", model.modal)
	}
	before, err := service.Players(ctx, true)
	if err != nil || len(before) != 1 {
		t.Fatalf("roster changed before conflicts were resolved: players=%+v err=%v", before, err)
	}
	model.modal.cursor = 1
	model.chooseMenu(model.modal)
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 2 || players[0].Alias != "Ace" || players[1].Name != "Bob" || players[1].Alias != "Bee" || players[1].HasInitialBuyIn {
		t.Fatalf("merged roster = %+v", players)
	}

	if err := os.WriteFile(path, []byte("- name: Alice\n  alias: Not applied\n- name: Carol\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model.importPlayersFrom(path)
	model.modal.cursor = 2
	model.chooseMenu(model.modal)
	players, err = service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 2 || players[0].Alias != "Ace" {
		t.Fatalf("cancelled import changed roster: %+v", players)
	}
}

func TestPlayerYAMLImportCanRestoreRemovedPlayer(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "player-restore.sqlite"), "Restore", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	removed, err := service.RegisterPlayer(ctx, "Sam", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePlayer(ctx, removed.ID, "mistake", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.screen, model.database, model.service = screenMain, database, service
	model.refresh()
	path := filepath.Join(t.TempDir(), "restore.yaml")
	if err := os.WriteFile(path, []byte("- name: Sam\n  alias: Sammy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model.importPlayersFrom(path)
	if model.modal == nil || len(model.modal.choices) != 3 || model.modal.choices[0] != "Restore and update alias" {
		t.Fatalf("removed player choices = %+v", model.modal)
	}
	model.chooseMenu(model.modal)
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].Removed || players[0].ID != removed.ID || players[0].Alias != "Sammy" {
		t.Fatalf("restored player = %+v", players)
	}
}

func TestPlayerYAMLExportOmitsRemovedPlayers(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "player-export.sqlite"), "Export", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	kept, err := service.RegisterPlayer(ctx, "Keep", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPlayerAlias(ctx, kept.ID, "K", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	removed, err := service.RegisterPlayer(ctx, "Removed", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePlayer(ctx, removed.ID, "mistake", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.database, model.service = database, service
	model.refresh()
	path := filepath.Join(t.TempDir(), "export.yaml")
	model.exportPlayers(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	players, err := config.ParsePlayersYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(players, []config.Player{{Name: "Keep", Alias: "K"}}) {
		t.Fatalf("exported players = %+v", players)
	}
}

func TestPlayerYAMLImportIsBlockedAfterTournamentStarts(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	model.service = service
	path := filepath.Join(t.TempDir(), "players.yaml")
	if err := os.WriteFile(path, []byte("- name: Late player\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model.importPlayersFrom(path)
	if model.modal == nil || model.modal.title != "Import unavailable" {
		t.Fatalf("started tournament import result = %+v", model.modal)
	}
	players, err := service.Players(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, player := range players {
		if player.Name == "Late player" {
			t.Fatal("player was imported after tournament start")
		}
	}
}

func TestChipUpAndReminderAreKeptOnSeparateLines(t *testing.T) {
	segment := config.Segment{Kind: config.SegmentBreak, ChipUp: 25, Reminder: "Registration closes soon"}
	chipUp, reminder := segmentReminderLines(segment)
	if chipUp != "Chip up! (25)" || reminder != "Registration closes soon" {
		t.Fatalf("break message lines = %q / %q", chipUp, reminder)
	}
	transition := transitionNotice([]domain.Transition{{Kind: domain.TransitionBreakStarted, ToIndex: 1}}, []config.Segment{segment})
	if !strings.Contains(transition, "Chip up! (25)\nRegistration closes soon") {
		t.Fatalf("transition notice did not put reminder on next line: %q", transition)
	}
}

func TestTransitionNoticeIncludesBreakReminder(t *testing.T) {
	schedule := []config.Segment{{Kind: config.SegmentLevel}, {Kind: config.SegmentBreak, Reminder: "Chip up the 5-value chip."}}
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
		t.Fatal("did not detect crossing the one-minute threshold")
	}
	checkpoint.SegmentIndex = 2
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("warned before a break")
	}
	checkpoint.SegmentIndex, checkpoint.HoldReason = 1, "waiting for chips"
	if shouldPlayMinuteWarning(checkpoint, time.Second, schedule, summary) {
		t.Fatal("warned during held clock")
	}
}

func TestLevelChangeChimeIsPrestartedForPlayerLatency(t *testing.T) {
	schedule := []config.Segment{{Kind: config.SegmentLevel, SmallBlind: 5, BigBlind: 10}, {Kind: config.SegmentLevel, SmallBlind: 10, BigBlind: 20}}
	summary := tournament.Summary{TotalStack: 100, ActivePlayers: 2}
	checkpoint := store.Checkpoint{Running: true, SegmentIndex: 1, Remaining: 2 * time.Second}
	if !shouldPlayLevelChangeSound(checkpoint, time.Second, schedule, summary) {
		t.Fatal("did not pre-start level chime")
	}
	checkpoint.Remaining = time.Second
	if shouldPlayLevelChangeSound(checkpoint, time.Second, schedule, summary) {
		t.Fatal("chime should not start at transition")
	}
}

func TestAddOnClosedWarningAndEligiblePlayers(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.record.Config = template
	model.openPurchase(tournament.PurchaseAddOn)
	if model.modal == nil || model.modal.kind != dialogMessage || !strings.Contains(model.modal.message, "Add-on window is closed") {
		t.Fatalf("closed add-on window did not warn: %+v", model.modal)
	}
	started := time.Now()
	model.record.StartedAt = &started
	model.record.Checkpoint.SegmentIndex = 4
	model.players = []tournament.Player{
		{ID: "used", Name: "Used", Active: true, HasInitialBuyIn: true, HasAddOn: true},
		{ID: "eligible", Name: "Eligible", Active: true, HasInitialBuyIn: true},
	}
	model.openPurchase(tournament.PurchaseAddOn)
	if model.modal == nil || model.modal.kind != dialogSelectPlayer || len(model.modal.players) != 1 || model.modal.players[0].ID != "eligible" {
		t.Fatalf("add-on picker players = %+v", model.modal)
	}
}

func TestBustParticipantNamesPluralizeOnlyForMultiplePlayers(t *testing.T) {
	if got := namedParticipants("Buster", []string{"Alice"}); got != "Buster: Alice" {
		t.Fatalf("singular participant label = %q", got)
	}
	if got := namedParticipants("Bustee", []string{"Bob", "Carol"}); got != "Bustees: Bob, Carol" {
		t.Fatalf("plural participant label = %q", got)
	}
}

func TestClosedRebuyWarnsBeforeOpeningPlayerPicker(t *testing.T) {
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model.record.Config = template
	model.record.RegistrationClosed = true
	model.players = []tournament.Player{{ID: "busted", Name: "Busted", HasInitialBuyIn: true}}
	model.openPurchase(tournament.PurchaseRebuy)
	if model.modal == nil || model.modal.kind != dialogMessage || model.modal.title != "Rebuy unavailable" || !strings.Contains(model.modal.message, "Rebuys are closed") {
		t.Fatalf("closed rebuy did not warn immediately: %+v", model.modal)
	}
}

func TestTournamentCanBeRenamedWhileRunning(t *testing.T) {
	model, database, _ := newRunningModel(t)
	defer database.Close()
	model.openSettings()
	model.chooseSettings(2)
	if model.modal == nil || model.modal.textPurpose != textTournamentNameRename {
		t.Fatalf("running settings did not allow tournament rename: %+v", model.modal)
	}
	model.modal.input = "Saturday final table"
	model.submitText(model.modal)
	if model.record.Name != "Saturday final table" {
		t.Fatalf("running tournament name = %q", model.record.Name)
	}
	if model.modal != nil {
		t.Fatalf("rename unexpectedly showed an error: %+v", model.modal)
	}
	events, err := database.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || event.Kind == "tournament_renamed"
	}
	if !found {
		t.Fatal("running rename was not audited")
	}
}

func TestYamlImportAfterStartShowsConfigurationLockNotCheckpointError(t *testing.T) {
	model, database, _ := newRunningModel(t)
	defer database.Close()
	data, err := config.DefaultYAML()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "template.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	model.saveTemplateImport(path)
	if model.modal == nil || model.modal.kind != dialogMessage || !strings.Contains(strings.ToLower(model.modal.message), "configuration is locked") || strings.Contains(strings.ToLower(model.modal.message), "atomic clock checkpoint") {
		t.Fatalf("started YAML import returned misleading error: %+v", model.modal)
	}
}

func TestLevelSelectorHomeAndEndKeys(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.service = tournament.NewService(nil)
	model.record.Config = template
	model.record.Checkpoint.SegmentIndex = 5
	model.openLevelSelect()
	if model.modal.cursor != 4 || !strings.Contains(model.modal.choices[4], "CURRENT") {
		t.Fatalf("current level cursor/label = %d / %q", model.modal.cursor, model.modal.choices[4])
	}
	model.onKey(keyEvent{Type: keyHome})
	if model.modal.cursor != 0 {
		t.Fatalf("Home cursor = %d", model.modal.cursor)
	}
	model.onKey(keyEvent{Type: keyEnd})
	if model.modal.cursor != len(model.modal.choices)-1 {
		t.Fatalf("End cursor = %d", model.modal.cursor)
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

func TestPrestartBreakCountdownDoesNotDoubleCountOpeningLevel(t *testing.T) {
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model.record.Config = template
	model.record.Checkpoint.Remaining = template.Schedule[0].Duration.Value()
	var want time.Duration
	for _, segment := range template.Schedule {
		if segment.Kind == config.SegmentBreak {
			break
		}
		want += segment.Duration.Value()
	}
	if got := model.nextBreakLabel(); !strings.HasSuffix(got, " in "+formatDuration(want)) {
		t.Fatalf("next break = %q, want duration %s", got, formatDuration(want))
	}
}

func TestPlayerHistoryRanksAfterRebuyClosureAndIncludesFinalPrize(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	ctx := context.Background()
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	busted := players[1]
	record := mustModelTournament(t, database)
	request := tournament.BustRequest{Busters: []string{players[0].ID}, Bustees: []tournament.Bustee{{PlayerID: busted.ID, Position: 1}}}
	if _, err := service.RecordBust(ctx, request, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	if _, err := service.Buy(ctx, busted.ID, tournament.PurchaseRebuy, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	if _, err := service.RecordBust(ctx, request, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	model.refresh()
	busted, _ = model.playerByID(busted.ID)
	model.showPlayerHistory(busted)
	if strings.Contains(model.modal.message, "RANKING") || strings.Contains(model.modal.message, "2nd place") || !strings.Contains(model.modal.message, "Busted in bust #2") {
		t.Fatalf("history assigned a premature rank while rebuys were open: %s", model.modal.message)
	}
	if model.record.Status != store.StatusRunning {
		t.Fatalf("tournament paused before rebuys closed: %s", model.record.Status)
	}
	model.modal = nil

	record = mustModelTournament(t, database)
	if _, err := service.SelectSegment(ctx, 5, nil, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	model.refresh()
	if model.record.Status != store.StatusPaused || model.modal == nil || model.modal.confirm != confirmCompletedEnd {
		t.Fatalf("settled winner did not auto-pause and request finalization: status=%s modal=%+v", model.record.Status, model.modal)
	}
	model.onKey(keyEvent{Type: keyEsc}) // Operator can postpone finalization; the clock stays paused.
	busted, _ = model.playerByID(busted.ID)
	model.showPlayerHistory(busted)
	if !strings.Contains(model.modal.message, "RANKING\n  • 2nd place") || strings.Contains(model.modal.message, "Provisional") {
		t.Fatalf("post-closure bust ranking missing: %s", model.modal.message)
	}

	record = mustModelTournament(t, database)
	if _, err := service.EndTournament(ctx, "", nil, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	model.refresh()
	busted, _ = model.playerByID(busted.ID)
	model.showPlayerHistory(busted)
	if !strings.Contains(model.modal.message, "Final result · 2nd place · prize 1.80 €") {
		t.Fatalf("final ranking/prize missing from history: %s", model.modal.message)
	}
}

func TestDelayedClockUpdateDoesNotLookLikeSleep(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	now := time.Now()
	model.record = mustModelTournament(t, database)
	model.resetClockAnchor(now.Add(-4 * time.Second))
	model.syncClock()
	if model.record.Status != store.StatusRunning {
		t.Fatalf("ordinary delayed update paused tournament: status=%s notice=%s", model.record.Status, model.notice)
	}
	if model.record.Checkpoint.ClockElapsed < 4*time.Second {
		t.Fatalf("delayed update lost elapsed time: %s", model.record.Checkpoint.ClockElapsed)
	}
	_ = service
}

func TestFailedClockCheckpointRetainsIntervalAndBlocksTournamentActions(t *testing.T) {
	model, database, _ := newRunningModel(t)
	anchor := time.Now().Add(-3 * time.Second)
	model.resetClockAnchor(anchor)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	model.syncClock()
	if !model.clockPersistenceFailed || !model.lastTick.Equal(anchor) {
		t.Fatalf("failed checkpoint state: failed=%t anchor=%s want=%s", model.clockPersistenceFailed, model.lastTick, anchor)
	}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'b'}})
	if model.modal != nil || !strings.Contains(model.statusMessage, "retrying") {
		t.Fatalf("action was accepted without a durable clock checkpoint: modal=%+v status=%q", model.modal, model.statusMessage)
	}
}

func TestStaleTickerTimestampCannotRewindClockAnchor(t *testing.T) {
	model, database, _ := newRunningModel(t)
	defer database.Close()
	now := time.Now()
	model.record = mustModelTournament(t, database)
	model.resetClockAnchor(now)
	model.syncClock()
	if model.lastTick.Before(now) {
		t.Fatalf("clock anchor moved backwards: %s before %s", model.lastTick, now)
	}
}

func TestFinalResultsAndCorrectedPlayerHistoryUseStoredParticipants(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	ctx := context.Background()
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	record := mustModelTournament(t, database)
	third, err := service.RegisterPlayer(ctx, "C", false, tournament.ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	if _, err := service.Buy(ctx, third.ID, tournament.PurchaseInitialBuyIn, tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	bustID, err := service.RecordBust(ctx, tournament.BustRequest{
		Busters: []string{players[0].ID}, Bustees: []tournament.Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, tournament.ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	if err := service.CorrectBustParticipant(ctx, bustID, players[1].ID, "bustee", &tournament.BustParticipant{PlayerID: third.ID, Role: "bustee", Position: 1}, "correct participant", tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	model.refresh()
	model.showParticipantPicker(bustID)
	foundReplacement := false
	for _, participant := range model.modal.participants {
		foundReplacement = foundReplacement || participant.PlayerID == third.ID
	}
	if model.modal == nil || len(model.modal.participants) != 2 || !foundReplacement {
		t.Fatalf("correction picker did not show effective participants: %+v", model.modal)
	}
	model.showPlayerHistory(third)
	if !strings.Contains(model.modal.message, "Busted") || !strings.Contains(model.modal.message, "correction") {
		t.Fatalf("replacement player history lacks current bust/correction: %s", model.modal.message)
	}
	model.showPlayerHistory(players[1])
	if !strings.Contains(model.modal.message, "Corrected out") || strings.Contains(model.modal.message, "Busted in bust") {
		t.Fatalf("superseded player history still reports bust: %s", model.modal.message)
	}

	// A separate completed event proves the GUI exposes durable, named payouts.
	model2, db2, service2 := newRunningModel(t)
	defer db2.Close()
	contestants, err := service2.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := mustModelTournament(t, db2).Checkpoint
	finalBustID, err := service2.RecordBust(ctx, tournament.BustRequest{Busters: []string{contestants[0].ID}, Bustees: []tournament.Bustee{{PlayerID: contestants[1].ID, Position: 1}}}, tournament.ActionContext{Checkpoint: &checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint = mustModelTournament(t, db2).Checkpoint
	if _, err := service2.EndTournament(ctx, "", nil, tournament.ActionContext{Checkpoint: &checkpoint}); err != nil {
		t.Fatal(err)
	}
	model2.refresh()
	model2.showFinalResults()
	if !strings.Contains(model2.modal.message, contestants[0].Name) || !strings.Contains(model2.modal.message, "Payouts") || !strings.Contains(model2.modal.message, "2.80 €") {
		t.Fatalf("final results view missing stored player payout: %s", model2.modal.message)
	}
	winnerLabels := playerDialogLabels(model2, &dialog{players: model2.players})
	if !strings.Contains(strings.Join(winnerLabels, "\n"), contestants[0].Name+" · winner") {
		t.Fatalf("completed tournament player list did not identify winner: %v", winnerLabels)
	}
	model2.showPlayerHistory(contestants[0])
	if !strings.Contains(model2.modal.message, "Final result · 1st place · prize 2.80 €") {
		t.Fatalf("completed winner history missing final result/payout: %s", model2.modal.message)
	}

	if err := service2.ReopenTournament(ctx, "correct final bust", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	checkpoint = mustModelTournament(t, db2).Checkpoint
	if err := service2.VoidBust(ctx, finalBustID, "final bust was recorded incorrectly", tournament.ActionContext{Checkpoint: &checkpoint}); err != nil {
		t.Fatal(err)
	}
	model2.refresh()
	model2.showPlayerHistory(contestants[0])
	if strings.Contains(model2.modal.message, "RANKING") || strings.Contains(model2.modal.message, "Final result") || strings.Contains(model2.modal.message, "prize 2.80 €") {
		t.Fatalf("voided final bust left stale winner/rank/prize in history: %s", model2.modal.message)
	}
}

func TestVoidBustPickerShowsSequentialBustNumberNotAuditID(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	players, err := service.Players(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	record := mustModelTournament(t, database)
	bustID, err := service.RecordBust(context.Background(), tournament.BustRequest{
		Busters: []string{players[0].ID}, Bustees: []tournament.Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, tournament.ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	model.refresh()
	model.openBustEvents(selectVoidBust)
	if model.modal == nil || len(model.modal.bustLabels) != 1 || !strings.HasPrefix(model.modal.bustLabels[0], "Bust #1 ") || strings.Contains(model.modal.bustLabels[0], fmt.Sprintf("#%d", bustID)) || !strings.Contains(model.modal.bustLabels[0], "Buster: "+players[0].Name) || !strings.Contains(model.modal.bustLabels[0], "Bustee: "+players[1].Name) {
		t.Fatalf("void bust label uses audit ID instead of sequence number: %+v", model.modal)
	}
	model.showParticipantPicker(bustID)
	if model.modal == nil || len(model.modal.participants) == 0 || model.modal.participants[0].BustNumber != 1 {
		t.Fatalf("participant correction did not retain bust sequence number: %+v", model.modal)
	}
}

func TestVoidedBustRemainsVisibleInPlayerHistory(t *testing.T) {
	model, database, service := newRunningModel(t)
	defer database.Close()
	ctx := context.Background()
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	record := mustModelTournament(t, database)
	bustID, err := service.RecordBust(ctx, tournament.BustRequest{
		Busters: []string{players[0].ID}, Bustees: []tournament.Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, tournament.ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	record = mustModelTournament(t, database)
	if err := service.VoidBust(ctx, bustID, "incorrect event", tournament.ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	model.refresh()
	model.showPlayerHistory(players[1])
	if !strings.Contains(model.modal.message, "Busted") || !strings.Contains(model.modal.message, "voided") {
		t.Fatalf("voided bust history missing original event: %s", model.modal.message)
	}
}

func TestEndedTournamentKeepsMouseAccessibleReadOnlyPlayerHistory(t *testing.T) {
	model := NewModel()
	model.screen = screenMain
	model.record.Status = store.StatusEnded
	model.players = []tournament.Player{{ID: "p", Name: "Player"}}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'p'}})
	if model.modal == nil || len(model.modal.choices) != 2 || model.modal.choices[0] != "Player info / history" || model.modal.choices[1] != "Export players to YAML" {
		t.Fatalf("ended players menu = %+v", model.modal)
	}
	model.chooseMenu(model.modal)
	if model.modal == nil || model.modal.purpose != selectInfo {
		t.Fatalf("read-only history choice did not open: %+v", model.modal)
	}
}

func TestFileBrowserSymlinkDirectoryIsNavigable(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("symlink test uses Unix permissions")
	}
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	browser, err := newFileBrowser(browseTemplate, root)
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range browser.entries {
		if entry.name == "linked/" {
			browser.cursor = i
			if _, done, err := browser.enter(); err != nil || done || browser.directory != link {
				t.Fatalf("linked directory navigation: done=%t directory=%s err=%v", done, browser.directory, err)
			}
			return
		}
	}
	t.Fatal("symlink directory omitted from file browser")
}

func TestFileBrowserFilteringSaveAndBounds(t *testing.T) {
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
	if joined := strings.Join(names, ","); !strings.Contains(joined, "a.yaml") || strings.Contains(joined, "ignore.txt") || strings.Contains(joined, ".hidden.yaml") {
		t.Fatalf("filtered template files = %v", names)
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
		t.Fatal("unsupported file not shown")
	}
	if err := browser.toggleHidden(); err != nil {
		t.Fatal(err)
	}
	foundHidden := false
	for _, entry := range browser.entries {
		foundHidden = foundHidden || entry.name == ".hidden.yaml"
	}
	if !foundHidden {
		t.Fatal("hidden file not shown")
	}
	saver, err := newFileBrowser(browseSaveTemplate, directory)
	if err != nil {
		t.Fatal(err)
	}
	if path, err := saver.newPath("new-rules"); err != nil || filepath.Ext(path) != ".yaml" {
		t.Fatalf("new template path=%q err=%v", path, err)
	}
	playerSaver, err := newFileBrowser(browseSavePlayers, directory)
	if err != nil {
		t.Fatal(err)
	}
	if path, err := playerSaver.newPath("players"); err != nil || filepath.Ext(path) != ".yaml" {
		t.Fatalf("new player list path=%q err=%v", path, err)
	}
	editorSaver, err := newFileBrowser(browseEditorSaveTemplate, directory)
	if err != nil {
		t.Fatal(err)
	}
	if path, err := editorSaver.newPath("tournament-template"); err != nil || filepath.Ext(path) != ".yaml" {
		t.Fatalf("new editor template path=%q err=%v", path, err)
	}
	for i := 0; i < 5; i++ {
		browser.entries = append(browser.entries, browserEntry{name: fmt.Sprintf("%d.yaml", i), selectable: true})
	}
	model := NewModel()
	model.browser = browser
	model.onKey(keyEvent{Type: keyEnd})
	if browser.cursor != len(browser.entries)-1 {
		t.Fatalf("End cursor=%d", browser.cursor)
	}
	model.onKey(keyEvent{Type: keyHome})
	if browser.cursor != 0 {
		t.Fatalf("Home cursor=%d", browser.cursor)
	}
}

func newRunningModel(t *testing.T) (*Model, *store.Store, *tournament.Service) {
	t.Helper()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "running.sqlite"), "Test", template)
	if err != nil {
		t.Fatal(err)
	}
	service := tournament.NewService(database)
	for _, name := range []string{"A", "B"} {
		player, err := service.RegisterPlayer(context.Background(), name, false, tournament.ActionContext{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Buy(context.Background(), player.ID, tournament.PurchaseInitialBuyIn, tournament.ActionContext{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.StartTournament(context.Background(), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.database, model.service = database, service
	model.resetClockAnchor(time.Now())
	model.refresh()
	return model, database, service
}

func mustModelTournament(t *testing.T, database *store.Store) store.TournamentRecord {
	t.Helper()
	record, err := database.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return record
}
