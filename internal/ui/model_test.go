package ui

import (
	"context"
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
)

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
	if !strings.Contains(model.modal.message, "Corrected out") || strings.Contains(model.modal.message, "Busted ·") {
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
	if _, err := service2.RecordBust(ctx, tournament.BustRequest{Busters: []string{contestants[0].ID}, Bustees: []tournament.Bustee{{PlayerID: contestants[1].ID, Position: 1}}}, tournament.ActionContext{Checkpoint: &checkpoint}); err != nil {
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
	if model.modal == nil || len(model.modal.choices) != 1 || model.modal.choices[0] != "Player info / history" {
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
