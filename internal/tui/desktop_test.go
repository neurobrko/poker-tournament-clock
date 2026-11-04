package tui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestDesktopStartupAndNewTournamentFlow(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	model := NewModel()
	d := newTestDesktop(app, window, model)
	d.render(true)

	window.Canvas().Focused().TypedRune('n')
	if model.modal == nil || model.modal.kind != dialogText {
		t.Fatalf("new tournament did not open name entry: %+v", model.modal)
	}
	if d.entry == nil {
		t.Fatal("text dialog did not expose an editable field")
	}
	wasFullscreen := window.FullScreen()
	d.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF11})
	if window.FullScreen() == wasFullscreen {
		t.Fatal("F11 did not work while a text field had focus")
	}
	test.Type(d.entry, "Evening tournament")
	d.entry.OnSubmitted(d.entry.Text)
	if model.modal == nil || model.modal.kind != dialogMenu || model.modal.purpose != menuTemplate {
		t.Fatalf("name submission did not open template choices: %+v", model.modal)
	}
	d.clickChoice(0, model.modal.cursor)
	if model.modal != nil || model.browser == nil || model.browser.purpose != browseSaveDatabase {
		t.Fatalf("default template selection did not open save browser: modal=%+v browser=%+v", model.modal, model.browser)
	}
}

func TestDesktopWelcomeKeyboardNavigationAndShortcuts(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	model := NewModel()
	d := newTestDesktop(app, window, model)
	d.render(true)
	window.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	if model.startupCursor != 1 {
		t.Fatalf("welcome Tab cursor = %d, want 1", model.startupCursor)
	}
	window.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	if model.startupCursor != 0 {
		t.Fatalf("welcome left arrow cursor = %d, want 0", model.startupCursor)
	}
	window.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if model.startupCursor != 1 {
		t.Fatalf("welcome right arrow cursor = %d, want 1", model.startupCursor)
	}
	window.Canvas().Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if model.browser == nil || model.browser.purpose != browseTournamentDB {
		t.Fatalf("Enter did not activate highlighted Load button: %+v", model.browser)
	}
	d.closeModal()
}

func TestDesktopMainViewHasDisabledFutureActionsAndLargeClock(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	window.Resize(fyne.NewSize(1920, 1080))
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "gui-test.sqlite"), "Readable table", template)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	model.screen = screenMain
	model.database = database
	model.service = tournament.NewService(database)
	model.record, err = database.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model.summary, err = model.service.CurrentSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model.placePrizes, err = domain.CalculatePlacePrizes(int64(model.summary.PrizePoolCents), int(model.summary.Entrants), model.record.Config.PrizeTiers)
	if err != nil {
		t.Fatal(err)
	}
	model.summary.Entrants, model.summary.ActivePlayers = 10, 2
	model.summary.CurrentBusts, model.summary.TotalBusts = 2, 10
	model.summary.InitialBuyIns, model.summary.InitialBuyInCents = 10, 2000
	model.summary.Rebuys, model.summary.RebuyCents = 4, 800
	model.summary.AddOns, model.summary.AddOnCents = 2, 400
	model.summary.TotalStack, model.summary.AverageStack, model.summary.PrizePoolCents = 50000, 25000, 3200
	model.placePrizes = []domain.PlacePrize{{Place: 1, AmountCents: 1600}, {Place: 2, AmountCents: 960}, {Place: 3, AmountCents: 480}, {Place: 4, AmountCents: 160}}
	model.setStatus("Transition to next level", false)
	d := newTestDesktop(app, window, model)
	d.render(true)

	if d.home == nil || d.home.timer == nil {
		t.Fatal("main dashboard was not constructed")
	}
	if contentHeight, viewportHeight := d.home.leftScroll.Content.MinSize().Height, d.home.leftScroll.Size().Height; contentHeight > viewportHeight {
		t.Fatalf("four-payout fullscreen column should fit without scrolling: content %v > viewport %v", contentHeight, viewportHeight)
	}
	if !d.home.actions['h'].Disabled() || !d.home.actions['t'].Disabled() {
		t.Fatal("future-stage hand resolver and timeline actions must stay disabled")
	}
	if d.home.actions[' '].Disabled() {
		t.Fatal("clock start action unexpectedly disabled before tournament starts")
	}
	test.Tap(d.home.actions[' '])
	if model.modal == nil || model.modal.confirm != confirmStart {
		t.Fatal("mouse start action did not use the existing confirmation flow")
	}
	if d.home == nil {
		t.Fatal("main clock disappeared beneath modal overlay")
	}
	d.closeModal()
	wasFullscreen := window.FullScreen()
	test.Tap(d.home.fullscreen)
	if window.FullScreen() == wasFullscreen {
		t.Fatal("bottom F11 button did not toggle fullscreen state")
	}
	wasFullscreen = window.FullScreen()
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyF11})
	if window.FullScreen() == wasFullscreen {
		t.Fatal("F11 keyboard shortcut did not toggle fullscreen state")
	}
	if got := d.home.timer.text.TextSize; got < 300 {
		t.Fatalf("timer should scale up on fullscreen display; got font size %v", got)
	}
	if d.home.message.Text != "Transition to next level" {
		t.Fatalf("bottom notice = %q", d.home.message.Text)
	}
	if window.Content() == nil {
		t.Fatal("Fyne window has no content")
	}
}

func TestPokerClockIconIsEmbeddedSquarePNG(t *testing.T) {
	bounds, err := png.DecodeConfig(bytes.NewReader(pokerClockIconPNG))
	if err != nil {
		t.Fatal(err)
	}
	if bounds.Width != 512 || bounds.Height != 512 {
		t.Fatalf("icon size = %dx%d, want 512x512", bounds.Width, bounds.Height)
	}
	if name := pokerClockIcon().Name(); name != "poker-clock-icon.png" {
		t.Fatalf("icon resource name = %q", name)
	}
}

func TestRunningClockUsesUbuntuMonospaceFont(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	app.Settings().SetTheme(newSpacedustTheme())
	timer := newFitText("12:34", Spacedust.Foreground, 460, true)
	if !timer.text.TextStyle.Monospace {
		t.Fatal("timer text is not marked monospace")
	}
	if got := newSpacedustTheme().Font(timer.text.TextStyle).Name(); got != "UbuntuSansMono-Bold.ttf" {
		t.Fatalf("timer font = %q, want UbuntuSansMono-Bold.ttf", got)
	}
}

func TestSpacedustThemeUsesUbuntuFontFamilies(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	fontTheme := newSpacedustTheme()
	if got := fontTheme.Font(fyne.TextStyle{}).Name(); got != "UbuntuSans-Regular.ttf" {
		t.Fatalf("regular font = %q, want UbuntuSans-Regular.ttf", got)
	}
	if got := fontTheme.Font(fyne.TextStyle{Bold: true}).Name(); got != "UbuntuSans-Bold.ttf" {
		t.Fatalf("bold font = %q, want UbuntuSans-Bold.ttf", got)
	}
	if got := fontTheme.Font(fyne.TextStyle{Monospace: true}).Name(); got != "UbuntuSansMono-Regular.ttf" {
		t.Fatalf("clock font = %q, want UbuntuSansMono-Regular.ttf", got)
	}
	condensedTheme := newCondensedTheme()
	if got := condensedTheme.Font(fyne.TextStyle{}).Name(); got != "UbuntuSans-Condensed-Regular.ttf" {
		t.Fatalf("condensed font = %q, want UbuntuSans-Condensed-Regular.ttf", got)
	}
	if got := condensedTheme.Font(fyne.TextStyle{Bold: true}).Name(); got != "UbuntuSans-Condensed-Bold.ttf" {
		t.Fatalf("condensed bold font = %q, want UbuntuSans-Condensed-Bold.ttf", got)
	}
}

func TestPlayerHistoryShowsPurchaseAndBustActivity(t *testing.T) {
	ctx := context.Background()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(ctx, filepath.Join(t.TempDir(), "history.sqlite"), "History test", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := tournament.NewService(database)
	alice, err := service.RegisterPlayer(ctx, "Alice", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := service.RegisterPlayer(ctx, "Bob", false, tournament.ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, player := range []tournament.Player{alice, bob} {
		if _, err := service.Buy(ctx, player.ID, tournament.PurchaseInitialBuyIn, tournament.ActionContext{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record, err := database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordBust(ctx, tournament.BustRequest{
		Busters: []string{alice.ID}, Bustees: []tournament.Bustee{{PlayerID: bob.ID, Position: 1}},
	}, tournament.ActionContext{WallTime: time.Now(), Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	players, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.database, model.service, model.players = database, service, players
	model.showPlayerHistory(bob)
	if model.modal == nil || !strings.Contains(model.modal.message, "Buy-in") || !strings.Contains(model.modal.message, "Busted") {
		t.Fatalf("player history missing purchase or bust activity: %+v", model.modal)
	}
}

func TestDesktopEnterActivatesHighlightedChoice(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	model := NewModel()
	d := newTestDesktop(app, window, model)
	d.render(true)
	focused := window.Canvas().Focused()
	if _, ok := focused.(*desktopKeySink); !ok {
		t.Fatalf("global shortcut sink not focused: %T", focused)
	}
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if model.modal == nil || model.modal.kind != dialogText || model.modal.textPurpose != textTournamentName {
		t.Fatalf("Enter did not activate highlighted New tournament choice: %+v", model.modal)
	}
}

func TestDesktopBrowserScrollsSelectedEntryIntoView(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	window.Resize(fyne.NewSize(800, 600))
	directory := t.TempDir()
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("tournament-%02d.sqlite", i)
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	browser, err := newFileBrowser(browseTournamentDB, directory)
	if err != nil {
		t.Fatal(err)
	}
	browser.cursor = len(browser.entries) - 1
	model := NewModel()
	model.browser = browser
	d := newTestDesktop(app, window, model)
	d.render(true)
	if d.browserScroll == nil || d.browserScroll.Offset.Y <= 0 {
		t.Fatalf("selected row did not scroll into view: %+v", d.browserScroll)
	}
}

func TestDesktopLevelChangeRequiresConfirmation(t *testing.T) {
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model.record.Config = template
	model.selectSegment(1, nil)
	if model.modal == nil || model.modal.kind != dialogConfirm || model.modal.confirm != confirmSelectSegment {
		t.Fatalf("level selection skipped confirmation: %+v", model.modal)
	}
}

func TestPlayerAndSettingsMenusReflectCurrentActions(t *testing.T) {
	model := NewModel()
	model.openSettings()
	for _, choice := range model.modal.choices {
		if strings.Contains(strings.ToLower(choice), "toggle sound") {
			t.Fatal("settings still duplicates the sound toggle")
		}
	}
	model.modal = &dialog{kind: dialogMenu, purpose: menuPlayers, choices: []string{"Add player", "Player info / history", "Rename player", "Remove player", "Void purchase"}}
	for _, choice := range model.modal.choices {
		if strings.Contains(strings.ToLower(choice), "sit out") || strings.Contains(strings.ToLower(choice), "return") {
			t.Fatalf("player menu still duplicates sit-out control: %q", choice)
		}
	}
}

func TestDesktopChoiceListReservesSpaceForMultipleRows(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	d := newTestDesktop(app, app.NewWindow("test"), NewModel())
	list := d.choiceList([]string{"One", "Two", "Three", "Four"}, 0, func(int) {})
	if got := list.MinSize().Height; got < 4*48 {
		t.Fatalf("submenu list too short for all choices: %v", got)
	}
}

func TestPurchaseDisplayUsesEuroSuffix(t *testing.T) {
	if got := formatPurchaseCount(5, 1000, 200); got != "5 @ 2.00 €" {
		t.Fatalf("purchase price display = %q", got)
	}
}

func TestUserErrorFormattingRemovesInternalEventPrefix(t *testing.T) {
	got := formatUserError("apply purchase_recorded event: initial buy-in registration is closed")
	if got != "Initial buy-in registration is closed:" {
		t.Fatalf("formatted error = %q", got)
	}
}

func newTestDesktop(app fyne.App, window fyne.Window, model *Model) *desktop {
	app.Settings().SetTheme(newSpacedustTheme())
	d := &desktop{app: app, window: window, model: model}
	d.keys = newDesktopKeySink(d)
	window.Canvas().SetOnTypedRune(d.dispatchRune)
	window.Canvas().SetOnTypedKey(d.dispatchKeyEvent)
	return d
}
