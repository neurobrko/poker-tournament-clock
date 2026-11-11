package ui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func newPrestartModelForTest(t *testing.T) *Model {
	t.Helper()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "review.sqlite"), "Review", template)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	m := NewModel()
	m.screen, m.database, m.service = screenMain, database, tournament.NewService(database)
	m.refresh()
	return m
}

func TestEditorRejectsNullPrizePercentages(t *testing.T) {
	for _, input := range []string{"null", "~", "# comment"} {
		t.Run(input, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("prize input %q panicked: %v", input, p)
				}
			}()
			if _, err := parseEditorPercentages(input); err == nil {
				t.Errorf("invalid input accepted")
			}
		})
	}
}

func TestDualDisplayKeysCannotOperateTournament(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	m, db, _ := newRunningModel(t)
	defer db.Close()
	m.screen = screenMain
	d := newTestDesktop(app, app.NewWindow("review"), m)
	d.render(true)
	d.toggleDualDisplay()
	d.dualWindow.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	record, err := db.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != store.StatusRunning {
		t.Errorf("read-only dual window changed durable tournament status to %s", record.Status)
	}
}

func TestDualCloseDoesNotTypeIntoMainDialog(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	m := newPrestartModelForTest(t)
	d := newTestDesktop(app, app.NewWindow("review"), m)
	d.render(true)
	d.toggleDualDisplay()
	m.modal = &dialog{kind: dialogText, textPurpose: textAddPlayer, title: "Add player"}
	d.render(true)
	d.dualWindow.Canvas().Focused().TypedRune('c')
	if d.dualVisible {
		t.Errorf("C did not close dual window; main dialog input now %q", m.modal.input)
	}
}

func TestAliasOnlyPlayersCanBeRemoved(t *testing.T) {
	ctx := context.Background()
	m := newPrestartModelForTest(t)
	m.beginPlayerImport([]config.Player{{Name: "Alice", Alias: "Ace"}}, nil)
	if len(m.players) != 1 {
		t.Fatal("import failed")
	}
	removeWithoutPurchase(t, m.service, m.players[0].ID, "imported")

	alice, err := m.service.RegisterPlayer(ctx, "Alice", true, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.service.SetPlayerAlias(ctx, alice.ID, "Ace 2", tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	removeWithoutPurchase(t, m.service, alice.ID, "restored/manual alias")

	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Players = []config.Player{{Name: "Seeded", Alias: "Seed"}}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "seeded.sqlite"), "Seeded", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	seeded, err := tournament.NewService(database).Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	removeWithoutPurchase(t, tournament.NewService(database), seeded[0].ID, "seeded")
}

func removeWithoutPurchase(t *testing.T, service *tournament.Service, playerID, source string) {
	t.Helper()
	if err := service.RemovePlayer(context.Background(), playerID, "not attending", tournament.ActionContext{}); err != nil {
		t.Errorf("%s player without purchases cannot be removed: %v (history error: %t)", source, err, errors.Is(err, tournament.ErrPlayerHasHistory))
	}
}

func TestPlayerImportIsAtomic(t *testing.T) {
	m := newPrestartModelForTest(t)
	before := m.record.Config.Purchases.AddOn.Chips
	template := m.record.Config
	template.Purchases.AddOn.Chips = 6000
	changes := []tournament.PlayerImportChange{
		{Action: tournament.PlayerImportAdd, Name: "Alice", Alias: "Ace"},
		{Action: tournament.PlayerImportRestore, PlayerID: "missing", Name: "Bob"},
	}
	if err := m.service.ApplyPlayerImport(context.Background(), &template, changes, tournament.ActionContext{}); err == nil {
		t.Fatal("invalid stale restore should fail")
	}
	eventsBefore, err := m.database.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	players, err := m.service.Players(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	record, err := m.database.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 0 || record.Config.Purchases.AddOn.Chips != before {
		t.Errorf("failed import left %d players and add-on chips=%d (was %d)", len(players), record.Config.Purchases.AddOn.Chips, before)
	}
	eventsAfter, err := m.database.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Errorf("failed import wrote %d audit events, want none", len(eventsAfter)-len(eventsBefore))
	}
	if _, _, err := tournament.NormalizePlayerName("\x00"); err == nil {
		t.Fatal("NUL player name passed storage-boundary validation")
	}
	if _, err := config.ParsePlayersYAML([]byte("- name: \"\\0\"\n")); err == nil {
		t.Fatal("NUL player name passed YAML validation")
	}
}

func TestDualLegendRefreshesAfterConfigChanges(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	m := newPrestartModelForTest(t)
	d := newTestDesktop(app, app.NewWindow("review"), m)
	d.render(true)
	d.toggleDualDisplay()
	next := m.record.Config
	next.ChipValues = append([]config.ChipValue(nil), next.ChipValues...)
	next.ChipValues[0].Color = "magenta"
	if err := m.service.UpdateConfiguration(context.Background(), next, tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	d.refreshDualDisplay()
	token := d.dualHome.chipLegendView.Objects[0].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*chipToken)
	want, err := config.ParseChipColor("magenta")
	if err != nil {
		t.Fatal(err)
	}
	if token.fill != want {
		t.Errorf("dual legend retained old color %v, want %v", token.fill, want)
	}

	for index := range next.Schedule {
		next.Schedule[index].ChipUp = 0
	}
	next.ChipValues = nil
	if err := m.service.UpdateConfiguration(context.Background(), next, tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	d.refreshDualDisplay()
	if d.dualHome.chipLegendView != nil {
		t.Fatal("dual display kept chip legend after chip values were removed")
	}

	next.ChipValues = []config.ChipValue{{Value: 10, Color: "blue"}}
	if err := m.service.UpdateConfiguration(context.Background(), next, tournament.ActionContext{}); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	d.refreshDualDisplay()
	if d.dualHome.chipLegendView == nil {
		t.Fatal("dual display did not add legend after chip values were imported")
	}
}

func TestEditorConfirmDoesNotEditBackground(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	m := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	m.startEditor(template)
	window := app.NewWindow("review")
	d := newTestDesktop(app, window, m)
	d.render(true)
	m.confirmEditorRemoval(0, editorFormLevel, "Level 1")
	d.render(true)
	window.Canvas().Focused().TypedRune('y')
	if m.editorName != "" {
		t.Errorf("typing Y in removal confirmation changed background tournament name to %q", m.editorName)
	}
	if m.modal != nil && m.modal.confirm == confirmRemoveEditorItem {
		t.Errorf("Y failed to confirm removal")
	}
	if len(m.editorTemplate.Schedule) != len(template.Schedule)-1 {
		t.Errorf("Y left schedule length %d, want %d", len(m.editorTemplate.Schedule), len(template.Schedule)-1)
	}

	m.confirmEditorRemoval(0, editorFormLevel, "Level 2")
	d.render(true)
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyEnter})
	if len(m.editorTemplate.Schedule) != len(template.Schedule)-2 {
		t.Errorf("Enter left schedule length %d, want %d", len(m.editorTemplate.Schedule), len(template.Schedule)-2)
	}

	m.showMessage("Editor error", "invalid draft")
	d.render(true)
	window.Canvas().Focused().TypedRune('z')
	if m.editorName != "" {
		t.Errorf("typing under editor message changed background name to %q", m.editorName)
	}
	m.modal = nil
	m.editorDirty = true
	m.leaveEditor()
	d.render(true)
	window.Canvas().Focused().TypedRune('n')
	if m.screen != screenEditor || m.editorName != "" {
		t.Errorf("N should dismiss discard confirmation without changing editor: screen=%v name=%q", m.screen, m.editorName)
	}
}

func TestDualClockUpdatesAwayFromDashboard(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	m, db, _ := newRunningModel(t)
	defer db.Close()
	m.screen = screenMain
	d := newTestDesktop(app, app.NewWindow("review"), m)
	d.render(true)
	d.toggleDualDisplay()
	d.activate('?')
	shown := d.dualHome.timer.text.Text
	m.resetClockAnchor(time.Now().Add(-3 * time.Second))
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { defer close(done); d.clockLoop(stop) }()
	time.AfterFunc(1200*time.Millisecond, func() { close(stop) })
	<-done
	want := formatDuration(m.record.Checkpoint.Remaining)
	if shown == want {
		t.Fatal("test did not advance clock")
	}
	if got := d.dualHome.timer.text.Text; got != want {
		t.Errorf("model clock advanced to %s, dual still shows %s while main is rules screen", want, got)
	}
}
