package ui

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestAddOnLabelRemainsOpenAfterRegistrationCloses(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	window.Resize(fyne.NewSize(1280, 800))
	model, database, _ := newRunningModel(t)
	defer database.Close()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	model.screen = screenMain
	model.record.Config = template
	model.record.StartedAt = &started
	model.record.Status = store.StatusRunning
	model.record.RegistrationClosed = true
	model.record.Checkpoint.SegmentIndex = 4
	d := newTestDesktop(app, window, model)
	d.render(true)
	addon := d.home.actions['a']
	if addon == nil || addon.Disabled() || strings.Contains(addon.Text, "CLOSED") {
		t.Fatalf("independent add-on window shown closed: button=%+v", addon)
	}
}

func TestDesktopNameValidationBlocksSubmission(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	model := NewModel()
	desktop := newTestDesktop(app, window, model)
	desktop.render(true)
	desktop.activate('n')
	desktop.entry.SetText(strings.Repeat("x", store.MaxNameLength+1))
	if err := desktop.entry.Validate(); err == nil {
		t.Fatal("overlong tournament name passed validator")
	}
	desktop.entry.OnSubmitted(desktop.entry.Text)
	if model.pendingName != "" || model.modal == nil || model.modal.textPurpose != textTournamentName {
		t.Fatalf("invalid name submitted: pending=%q modal=%+v", model.pendingName, model.modal)
	}
}

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

func TestFooterPlacesVersionBetweenTimeAndFullscreenButton(t *testing.T) {
	objects := []fyne.CanvasObject{widget.NewLabel(""), widget.NewLabel("12:34:56"), widget.NewLabel("v0.0.1"), widget.NewButton("⛶  F11", nil)}
	bottomFooterLayout{}.Layout(objects, fyne.NewSize(500, 40))
	message, localTime, version, fullscreen := objects[0], objects[1], objects[2], objects[3]
	if localTime.Position().X+localTime.Size().Width > version.Position().X || version.Position().X+version.Size().Width > fullscreen.Position().X {
		t.Fatal("footer version must appear after the time and before F11")
	}
	if message.Size().Width == 0 {
		t.Fatal("footer notice lost all available width")
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
	if d.home.blinds.text.TextSize <= 64 {
		t.Fatalf("current blinds font size = %v, want larger than 64", d.home.blinds.text.TextSize)
	}
	if d.home.nextBreak.maxTextSize != 21 || d.home.nextLevel.maxTextSize != 21 {
		t.Fatalf("next-line font caps = %v/%v, want same 21px as blinds heading", d.home.nextBreak.maxTextSize, d.home.nextLevel.maxTextSize)
	}
	if contentHeight, viewportHeight := d.home.leftScroll.Content.MinSize().Height, d.home.leftScroll.Size().Height; contentHeight > viewportHeight {
		t.Fatalf("four-payout fullscreen column should fit without scrolling: content %v > viewport %v", contentHeight, viewportHeight)
	}
	if d.home.actions['h'] != nil || d.home.actions['t'] != nil {
		t.Fatal("unimplemented hand resolver and timeline actions should be hidden")
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
	renderer := d.home.nextBreak.CreateRenderer().(*coloredLineRenderer)
	renderer.Layout(fyne.NewSize(700, 64))
	if got := renderer.texts[0].TextSize; got < 20 {
		t.Fatalf("next-break labels should remain readable; got font size %v", got)
	}
	if renderer.texts[1].TextSize != d.home.status.text.TextSize || !renderer.texts[1].TextStyle.Monospace || renderer.texts[4].TextSize != d.home.status.text.TextSize {
		t.Fatalf("next-break value sizing/font = %v/%v, want status size %v and monospace timer", renderer.texts[1].TextSize, renderer.texts[4].TextSize, d.home.status.text.TextSize)
	}
	for _, pair := range [][2]int{{0, 1}, {3, 4}} {
		left, right := renderer.texts[pair[0]], renderer.texts[pair[1]]
		_, leftBaseline := app.Driver().RenderedTextSize(left.Text, left.TextSize, left.TextStyle, nil)
		_, rightBaseline := app.Driver().RenderedTextSize(right.Text, right.TextSize, right.TextStyle, nil)
		leftBaseline += left.Position().Y
		rightBaseline += right.Position().Y
		if leftBaseline-rightBaseline > 0.1 || rightBaseline-leftBaseline > 0.1 {
			t.Fatalf("property/value baselines differ: %v vs %v", leftBaseline, rightBaseline)
		}
	}
	firstGap := renderer.texts[1].Position().X - (renderer.texts[0].Position().X + renderer.texts[0].Size().Width)
	secondGap := renderer.texts[4].Position().X - (renderer.texts[3].Position().X + renderer.texts[3].Size().Width)
	if firstGap-secondGap > 0.1 || secondGap-firstGap > 0.1 {
		t.Fatalf("property/value gaps differ: %v vs %v", firstGap, secondGap)
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
	playerList := &dialog{kind: dialogSelectPlayer, purpose: selectInfo, title: "Select player", players: players, cursor: 1}
	model.modal = playerList
	model.choosePlayer(playerList, bob)
	if model.modal == nil || !strings.Contains(model.modal.message, "PURCHASES · newest first") || !strings.Contains(model.modal.message, "BUSTS · newest first") || !strings.Contains(model.modal.message, "Buy-in") || !strings.Contains(model.modal.message, "Busted in bust #1") {
		t.Fatalf("player history missing structured purchase or bust activity: %+v", model.modal)
	}
	model.onKey(keyEvent{Type: keyEsc})
	if model.modal != playerList {
		t.Fatalf("Esc returned to %T instead of player list", model.modal)
	}
	model.choosePlayer(playerList, alice)
	if model.modal == nil || model.modal.kind != dialogMessage || !strings.Contains(model.modal.title, alice.Name) {
		t.Fatalf("could not open another player's history immediately: %+v", model.modal)
	}
}

func TestConfiguredChipUpReminderAppearsDuringBreak(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	model := NewModel()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Schedule[0].Reminder = "Level reminder"
	template.Schedule[7].Reminder = "Registration reminder"
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "break-reminder.sqlite"), "Break reminder", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	model.screen = screenMain
	model.database = database
	model.service = tournament.NewService(database)
	model.record, err = database.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model.record.Checkpoint.SegmentIndex = 8
	model.summary, err = model.service.CurrentSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDesktop(app, window, model)
	d.render(true)
	if d.home.chipUpReminder.Text != "Chip up! (5)" || d.home.chipUpReminder.Color != Spacedust.Red {
		t.Fatalf("chip-up warning = %q with color %+v", d.home.chipUpReminder.Text, d.home.chipUpReminder.Color)
	}
	if d.home.reminder.Text != "Registration reminder" || d.home.reminder.Color != Spacedust.Yellow {
		t.Fatalf("reminder line = %q with color %+v", d.home.reminder.Text, d.home.reminder.Color)
	}
	model.record.Checkpoint.SegmentIndex = 1
	d.home.refresh(model)
	if d.home.chipUpReminder.Text != "" || d.home.reminder.Text != "Level reminder" {
		t.Fatalf("level reminder display = chip-up %q / reminder %q", d.home.chipUpReminder.Text, d.home.reminder.Text)
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
	if len(model.modal.choices) != 4 || model.modal.choices[3] != "Sound settings" {
		t.Fatalf("main settings menu did not group sound options: %v", model.modal.choices)
	}
	model.chooseSettings(3)
	if model.modal == nil || model.modal.purpose != menuSoundSettings || len(model.modal.choices) != 4 || model.modal.choices[0] != "Choose end-of-level sound" || model.modal.choices[1] != "Choose one-minute warning sound" {
		t.Fatalf("sound submenu choices = %+v", model.modal)
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

func TestDesktopPlayerOverlayKeyboardNavigationScrollsAndSupportsHomeEnd(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	window.Resize(fyne.NewSize(800, 600))
	model := NewModel()
	players := make([]tournament.Player, 20)
	for i := range players {
		players[i] = tournament.Player{ID: fmt.Sprintf("player-%02d", i), Name: fmt.Sprintf("Player %02d", i)}
	}
	model.modal = &dialog{kind: dialogSelectPlayer, title: "Choose player", players: players}
	d := newTestDesktop(app, window, model)
	d.render(true)

	focused := window.Canvas().Focused()
	if focused == nil {
		t.Fatal("player overlay has no keyboard focus")
	}
	for i := 0; i < 10; i++ {
		focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
		focused = window.Canvas().Focused()
	}
	if model.modal.cursor != 10 {
		t.Fatalf("Down cursor = %d, want 10", model.modal.cursor)
	}
	if d.modalScroll == nil || d.modalScroll.Offset.Y <= 0 {
		t.Fatalf("selected player did not scroll into view: %+v", d.modalScroll)
	}
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	if model.modal.cursor != len(players)-1 {
		t.Fatalf("End cursor = %d, want %d", model.modal.cursor, len(players)-1)
	}
	focused = window.Canvas().Focused()
	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyHome})
	if model.modal.cursor != 0 {
		t.Fatalf("Home cursor = %d, want 0", model.modal.cursor)
	}
}

func TestDesktopRulesKeyboardScrollingAndWidthLimit(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("test")
	window.Resize(fyne.NewSize(1600, 800))
	model := NewModel()
	model.screen = screenRules
	model.rulesText = strings.Repeat("A readable rules paragraph.\n\n", 100)
	d := newTestDesktop(app, window, model)
	d.render(true)
	if d.rulesScroll == nil {
		t.Fatal("rules page has no scroll container")
	}
	maxWidth := fyne.MeasureText(strings.Repeat("0", 120), 16, fyne.TextStyle{}).Width
	if d.rulesScroll.Size().Width > maxWidth {
		t.Fatalf("rules content width %v exceeds 120-character limit %v", d.rulesScroll.Size().Width, maxWidth)
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyDown})
	if d.rulesScroll.Offset.Y <= 0 {
		t.Fatal("Down did not scroll rules")
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyLeft})
	if d.rulesScroll.Offset.Y != 0 {
		t.Fatal("Left did not scroll rules up")
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyRight})
	if d.rulesScroll.Offset.Y <= 0 {
		t.Fatal("Right did not scroll rules down")
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	if d.rulesScroll.Offset.Y <= 24 {
		t.Fatal("PgDn did not advance rules page")
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyEnd})
	if d.rulesScroll.Offset.Y < d.rulesScroll.Content.MinSize().Height-d.rulesScroll.Size().Height-1 {
		t.Fatal("End did not reach end of rules")
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyHome})
	if d.rulesScroll.Offset.Y != 0 {
		t.Fatal("Home did not return to top of rules")
	}
}

func TestDesktopWindowContentHasSideInsets(t *testing.T) {
	content := widget.NewLabel("content")
	(windowSideInsetLayout{inset: 16}).Layout([]fyne.CanvasObject{content}, fyne.NewSize(500, 300))
	if content.Position().X != 16 || content.Size().Width != 468 {
		t.Fatalf("window content geometry = position %v size %v, want 16px side insets", content.Position(), content.Size())
	}
}

func TestAverageStackRoundsDownToSmallestConfiguredChip(t *testing.T) {
	chips := []config.ChipValue{{Value: 25}, {Value: 5}, {Value: 100}}
	if got := roundedAverageStack(1667, chips); got != 1665 {
		t.Fatalf("average stack = %d, want 1665", got)
	}
	if got := roundedAverageStack(1667, nil); got != 1667 {
		t.Fatalf("average stack without denominations = %d, want 1667", got)
	}
}

func TestDesktopChipLegendUsesConfiguredValuesAndColors(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	legend := chipLegend(template.ChipValues, nil)
	if len(legend.Objects) != 2 {
		t.Fatalf("chip legend rows = %#v", legend)
	}
	wantCounts := []int{3, 2}
	var rowWidths []float32
	for index, count := range wantCounts {
		row, ok := legend.Objects[index].(*fyne.Container)
		if !ok || len(row.Objects) != 1 {
			t.Fatalf("chip row %d = %#v", index, legend.Objects[index])
		}
		chips, ok := row.Objects[0].(*fyne.Container)
		if !ok || len(chips.Objects) != count {
			t.Fatalf("chip row %d token count = %#v", index, chips)
		}
		rowWidths = append(rowWidths, chips.MinSize().Width)
	}
	if rowWidths[1] >= rowWidths[0] {
		t.Fatalf("two-chip row width %v is not narrower than first row %v", rowWidths[1], rowWidths[0])
	}
	first := legend.Objects[0].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*chipToken)
	if first.value != "5" || first.fill != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("first chip token = %#v", first)
	}
	if got := first.CreateRenderer().(*chipTokenRenderer).label.TextSize; got != 15 {
		t.Fatalf("chip value font size = %v, want 15", got)
	}
}

func TestChipUpRemovesDenominationFromAverageAndGraysLegend(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	remaining, removed := chipsAfterChipUps(template.ChipValues, template.Schedule, 12)
	if len(remaining) != 3 || remaining[0].Value != 100 || !removed[5] || !removed[25] {
		t.Fatalf("chips after third break = %+v, removed = %+v", remaining, removed)
	}
	if got := roundedAverageStack(1667, remaining); got != 1600 {
		t.Fatalf("average stack after 25 chip-up = %d, want 1600", got)
	}
	legend := chipLegend(template.ChipValues, removed)
	var grayed int
	for _, row := range legend.Objects {
		for _, token := range row.(*fyne.Container).Objects[0].(*fyne.Container).Objects {
			chip := token.(*chipToken)
			if chip.value == "5" || chip.value == "25" {
				if chip.fill != (color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff}) {
					t.Errorf("removed chip %s fill = %+v, want gray", chip.value, chip.fill)
				}
				grayed++
			}
		}
	}
	if grayed != 2 {
		t.Fatalf("grayed chip tokens = %d, want 2", grayed)
	}
}

func TestDesktopRefreshKeepsAverageVisibleAndUpdatesChipLegendAtThirdBreak(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.record.Config = template
	model.record.Checkpoint.SegmentIndex = 12
	model.summary.AverageStack = 1667
	d := newTestDesktop(app, app.NewWindow("test"), model)
	_, home := d.mainView()
	if home.averageStack.Text != "1600" || len(home.nextBreak.parts) != 2 || home.nextBreak.parts[0].text != "AVG. STACK" || home.nextBreak.parts[1].text != "1600" || !home.nextBreak.parts[1].spaceBefore {
		t.Fatalf("third-break average/next-break display = %q / %+v", home.averageStack.Text, home.nextBreak.parts)
	}
	var grayed int
	for _, row := range home.chipLegendView.Objects {
		for _, token := range row.(*fyne.Container).Objects[0].(*fyne.Container).Objects {
			chip := token.(*chipToken)
			if chip.value == "5" || chip.value == "25" {
				if chip.fill != (color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff}) {
					t.Errorf("removed chip %s fill = %+v, want gray", chip.value, chip.fill)
				}
				grayed++
			}
		}
	}
	if grayed != 2 {
		t.Fatalf("third-break grayed chips = %d, want 2", grayed)
	}
}

func TestDesktopStatusAndNextBreakTypography(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.record.Config = template
	model.record.Checkpoint.SegmentIndex = 1
	model.record.Status = store.StatusRunning
	if got := desktopStatus(model, &template.Schedule[0]); got != "LEVEL 1 RUNNING" {
		t.Fatalf("level status = %q, want uppercase level and state", got)
	}
	white := Spacedust.Foreground
	parts := desktopNextBreakParts(model, 12000, nil)
	if len(parts) != 5 || parts[0].color != Spacedust.Blue || parts[1].color != white || parts[3].color != Spacedust.Blue || parts[4].color != white {
		t.Fatalf("next-break property/value colors = %+v", parts)
	}
	blindsParts := desktopNextBlindsParts(model)
	if len(blindsParts) != 2 || blindsParts[0].color != Spacedust.Blue || blindsParts[1].color != white {
		t.Fatalf("next-blinds property/value colors = %+v", blindsParts)
	}
	model.record.Config.Schedule = []config.Segment{{Kind: config.SegmentLevel}}
	model.record.Checkpoint.SegmentIndex = 0
	if parts := desktopNextBreakParts(model, 12000, nil); len(parts) != 2 || parts[0].text != "AVG. STACK" || parts[1].text != "12k" || !parts[1].spaceBefore {
		t.Fatalf("next-break/average-stack parts without configured break = %+v", parts)
	}
	model.record.Config = template
	model.record.Checkpoint.SegmentIndex = 12 // Break 3; next break is absent.
	if parts := desktopNextBreakParts(model, 1667, nil); len(parts) != 2 || parts[0].text != "AVG. STACK" || parts[1].text != "1600" || !parts[1].spaceBefore {
		t.Fatalf("third-break next-break/average-stack parts = %+v", parts)
	}
}

func TestDesktopBreakStatusUsesConfiguredLabel(t *testing.T) {
	model := NewModel()
	model.record.Config.Schedule = []config.Segment{
		{Kind: config.SegmentLevel},
		{Kind: config.SegmentBreak, Label: "Late registration", Duration: config.Duration(time.Minute)},
	}
	model.record.Checkpoint.SegmentIndex = 2
	model.record.Status = store.StatusRunning
	segment := &model.record.Config.Schedule[1]
	if got := desktopStatus(model, segment); got != "LATE REGISTRATION" {
		t.Fatalf("custom break status = %q", got)
	}
	model.record.Status = store.StatusPaused
	if got := desktopStatus(model, segment); got != "LATE REGISTRATION PAUSED" {
		t.Fatalf("paused custom break status = %q", got)
	}
	segment.Label = ""
	if got := desktopStatus(model, segment); got != "BREAK 1 PAUSED" {
		t.Fatalf("unnamed break status = %q", got)
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
