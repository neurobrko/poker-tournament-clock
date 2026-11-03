package tournament

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestStartPauseResumeAndElapsedClockPersistence(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	if err := service.StartTournament(ctx, time.Now()); !errors.Is(err, ErrInsufficientActivePlayers) {
		t.Fatalf("start without entrants = %v", err)
	}
	buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	record, err := database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != store.StatusRunning || !record.Checkpoint.Running || record.Checkpoint.SegmentIndex != 1 || record.Checkpoint.Remaining != 20*time.Minute {
		t.Fatalf("start state = %+v", record)
	}
	clock := record.Checkpoint
	if _, err := service.AdvanceClock(ctx, 30*time.Second, startedAt.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	record, err = database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Checkpoint.ClockElapsed != 30*time.Second || record.Checkpoint.Remaining != 19*time.Minute+30*time.Second {
		t.Fatalf("clock after tick = %+v", record.Checkpoint)
	}
	clock = record.Checkpoint
	if err := service.PauseTournament(ctx, ActionContext{Checkpoint: &clock, WallTime: startedAt.Add(31 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	record, err = database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != store.StatusPaused || record.Checkpoint.Running || record.Checkpoint.ClockElapsed != 30*time.Second {
		t.Fatalf("paused state = %+v", record)
	}
	clock = record.Checkpoint
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &clock, WallTime: startedAt.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	record, err = database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != store.StatusRunning || !record.Checkpoint.Running || record.Checkpoint.ClockElapsed != 30*time.Second {
		t.Fatalf("resumed state = %+v", record)
	}
}

func TestResumeAllowsOneActivePlayerButRequiresTwoEntrants(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record, _ := database.Tournament(ctx)
	clock := record.Checkpoint
	if err := service.PauseTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	// One active player is sufficient to continue; entrants count remains two.
	if _, err := database.CommitEvent(ctx, store.EventInput{Kind: "test_bust_state"}, nil, func(tx *sql.Tx, _ int64) error {
		_, err := tx.ExecContext(ctx, "UPDATE players SET active = 0 WHERE id = ?", players[1].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	record, _ = database.Tournament(ctx)
	clock = record.Checkpoint
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatalf("resume with one active player: %v", err)
	}
	if _, err := database.CommitEvent(ctx, store.EventInput{Kind: "test_void_entry"}, nil, func(tx *sql.Tx, eventID int64) error {
		_, err := tx.ExecContext(ctx, `UPDATE purchases SET voided_by_event_id = ?
			WHERE player_id = ? AND kind = 'initial_buy_in'`, eventID, players[1].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	clock = mustTournament(t, database).Checkpoint
	if err := service.PauseTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	clock = mustTournament(t, database).Checkpoint
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &clock}); !errors.Is(err, ErrInsufficientEntrants) {
		t.Fatalf("resume below two entrants = %v", err)
	}
}

func TestAutomaticProgressionAndEndOfBreakDeadline(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	for step := 1; step <= 3; step++ {
		if _, err := service.AdvanceClock(ctx, 20*time.Minute, startedAt.Add(time.Duration(step)*20*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	record := mustTournament(t, database)
	if record.Checkpoint.SegmentIndex != 4 || record.Config.Schedule[3].Kind != config.SegmentBreak || record.RegistrationClosed {
		t.Fatalf("first break state = %+v", record)
	}
	if _, err := service.AdvanceClock(ctx, 10*time.Minute, startedAt.Add(70*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if record.Checkpoint.SegmentIndex != 5 || !record.RegistrationClosed {
		t.Fatalf("deadline should remain open at first blind of following level: %+v", record)
	}
}

func TestBreakEndDeadlineClosesEvenWhenNextLevelIsBlocked(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Schedule = []config.Segment{
		template.Schedule[0],
		{Kind: config.SegmentBreak, Label: "Registration", Duration: config.Duration(10 * time.Minute)},
		{Kind: config.SegmentLevel, Duration: config.Duration(20 * time.Minute), SmallBlind: 1000, BigBlind: 2000, Ante: config.AnteConfig{Kind: config.AnteNone}},
	}
	template.Registration.RegistrationDeadline = config.Deadline{At: config.DeadlineAtBreakEnd, Target: 1}
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, 20*time.Minute, startedAt.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, 10*time.Minute, startedAt.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if record.Checkpoint.SegmentIndex != 2 || !record.Checkpoint.SegmentFinished || record.Checkpoint.PendingIndex != 3 || !record.RegistrationClosed {
		t.Fatalf("blocked break end state = %+v", record)
	}
	if _, err := service.Buy(ctx, "missing-player", PurchaseInitialBuyIn, ActionContext{Checkpoint: &record.Checkpoint}); !errors.Is(err, ErrInitialBuyInClosed) {
		t.Fatalf("buy-in after blocked break deadline = %v", err)
	}
}

func TestBlockedLevelReleasesOnPurchaseOnlyWhenRunning(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Schedule = []config.Segment{
		template.Schedule[0],
		{Kind: config.SegmentLevel, Duration: config.Duration(20 * time.Minute), SmallBlind: 5000, BigBlind: 10000, Ante: config.AnteConfig{Kind: config.AnteNone}},
	}
	template.Registration.RegistrationDeadline = config.Deadline{At: config.DeadlineAtLevelStart, Target: 2}
	template.Registration.AddOnsEnabled = false
	template.Registration.AddOnBreak = 0
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, 20*time.Minute, startedAt.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if !record.Checkpoint.SegmentFinished || record.Checkpoint.PendingIndex != 2 || record.Checkpoint.SegmentIndex != 1 {
		t.Fatalf("clock did not hold blocked next level: %+v", record.Checkpoint)
	}
	for count := 0; count < 13; count++ {
		record = mustTournament(t, database)
		clock := record.Checkpoint
		if _, err := service.Buy(ctx, players[count%2].ID, PurchaseRebuy, ActionContext{Checkpoint: &clock}); err != nil {
			t.Fatal(err)
		}
	}
	record = mustTournament(t, database)
	if record.Checkpoint.SegmentIndex != 2 || record.Checkpoint.SegmentFinished || record.Checkpoint.PendingIndex != 0 || !record.Checkpoint.Running {
		t.Fatalf("level did not release after chips added: %+v", record.Checkpoint)
	}
}

func TestManualSelectionPreservesRunStateAndLogsChange(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	clock := record.Checkpoint
	clock.ClockElapsed = 40 * time.Second
	clock.Remaining -= 40 * time.Second
	if _, err := service.SelectSegment(ctx, 2, nil, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if record.Checkpoint.SegmentIndex != 2 || record.Checkpoint.ClockElapsed != 40*time.Second || !record.Checkpoint.Running {
		t.Fatalf("manual jump changed run state or elapsed time: %+v", record.Checkpoint)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Kind != "segment_selected" {
		t.Fatalf("manual selection not audited: %v", eventKinds(events))
	}
}

func TestStartRejectsUnavailableOpeningLevelAndLocksSettings(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Purchases.InitialBuyIn.Chips = 5
	template.Purchases.Rebuy.Chips = 5
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	if err := service.StartTournament(ctx, time.Now()); !errors.Is(err, domain.ErrUnavailableLevel) {
		t.Fatalf("start with unavailable opening blinds = %v", err)
	}
	record := mustTournament(t, database)
	if record.Status != store.StatusNotStarted {
		t.Fatalf("failed start changed status: %+v", record)
	}
	template.Purchases.InitialBuyIn.Chips = 1000
	// Restore enough chips by editing only after voiding the recorded purchases.
	purchases, err := service.Purchases(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, purchase := range purchases {
		if err := service.VoidPurchase(ctx, purchase.ID, "test correction", ActionContext{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.UpdateConfiguration(ctx, template, ActionContext{}); err != nil {
		t.Fatal(err)
	}
	buyInPlayers(t, service, "C", "D")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateConfiguration(ctx, template, ActionContext{}); !errors.Is(err, store.ErrConfigurationLocked) {
		t.Fatalf("settings edit after start = %v", err)
	}
}

func newServiceWithTemplate(t *testing.T, template config.Template) (*Service, *store.Store) {
	t.Helper()
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "clock.db"), "Clock test", template)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(database), database
}

func mustTournament(t *testing.T, database *store.Store) store.TournamentRecord {
	t.Helper()
	record, err := database.Tournament(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return record
}
