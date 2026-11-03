package tournament

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestPauseAfterSleepRestoresLastCheckpoint(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, 11*time.Second, startedAt.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	lastSaved := mustTournament(t, database).Checkpoint
	if err := service.PauseAfterSleep(ctx, startedAt.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if record.Status != store.StatusPaused || record.Checkpoint.Running || record.Checkpoint.ClockElapsed != lastSaved.ClockElapsed || record.Checkpoint.Remaining != lastSaved.Remaining {
		t.Fatalf("sleep recovery state = %+v", record)
	}
	if record.Checkpoint.RealElapsed < 5*time.Minute {
		t.Fatalf("real elapsed did not include sleep/pause time: %s", record.Checkpoint.RealElapsed)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Kind != "clock_paused_after_sleep" {
		t.Fatalf("sleep recovery not audited: %v", eventKinds(events))
	}
}

func TestBackwardJumpReopensRegistrationWindow(t *testing.T) {
	service, database := newTestService(t)
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
	if _, err := service.AdvanceClock(ctx, 10*time.Minute, startedAt.Add(70*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if !record.RegistrationClosed {
		t.Fatal("registration should close after first break")
	}
	clock := record.Checkpoint
	if err := service.PauseTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	clock = record.Checkpoint
	if _, err := service.SelectSegment(ctx, 4, nil, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if record.RegistrationClosed || record.Checkpoint.SegmentIndex != 4 || record.Checkpoint.Running {
		t.Fatalf("backward jump did not reopen break window: %+v", record)
	}
	newPlayer, err := service.RegisterPlayer(ctx, "Late entry", false, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Buy(ctx, newPlayer.ID, PurchaseInitialBuyIn, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatalf("initial purchase after backward jump: %v", err)
	}
}

func TestUnavailableLevelRecoveryJumpRequiresClosedRegistrationAndPausedClock(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Schedule = []config.Segment{
		{Kind: config.SegmentLevel, Duration: config.Duration(time.Minute), SmallBlind: 5, BigBlind: 10, Ante: config.AnteConfig{Kind: config.AnteNone}},
		{Kind: config.SegmentBreak, Label: "Registration", Duration: config.Duration(time.Minute)},
		{Kind: config.SegmentLevel, Duration: config.Duration(time.Minute), SmallBlind: 1000, BigBlind: 2000, Ante: config.AnteConfig{Kind: config.AnteNone}},
	}
	template.Registration.RegistrationDeadline = config.Deadline{At: config.DeadlineAtBreakEnd, Target: 1}
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B")
	startedAt := time.Now()
	if err := service.StartTournament(ctx, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, time.Minute, startedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceClock(ctx, time.Minute, startedAt.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	for _, player := range players {
		purchases, err := service.Purchases(ctx, player.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.VoidPurchase(ctx, purchases[0].ID, "test void", ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
			t.Fatal(err)
		}
		record = mustTournament(t, database)
	}
	if !record.RegistrationClosed || record.Checkpoint.PendingIndex != 3 || record.Status != store.StatusRunning {
		t.Fatalf("pre-recovery blocked state = %+v", record)
	}
	clock := record.Checkpoint
	if _, err := service.SelectSegment(ctx, 1, nil, ActionContext{Checkpoint: &clock}); !errors.Is(err, domain.ErrUnavailableLevel) {
		t.Fatalf("recovery jump while running = %v", err)
	}
	if err := service.PauseTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	clock = record.Checkpoint
	if _, err := service.SelectSegment(ctx, 1, nil, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatalf("paused registration recovery selection: %v", err)
	}
	record = mustTournament(t, database)
	if record.Status != store.StatusPaused || record.Checkpoint.Running || record.Checkpoint.SegmentIndex != 1 || record.Checkpoint.SegmentFinished {
		t.Fatalf("recovery selection state = %+v", record)
	}
	clock = record.Checkpoint
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &clock}); !errors.Is(err, ErrInsufficientEntrants) {
		t.Fatalf("resume below two restored entrants = %v", err)
	}
	for _, player := range players {
		record = mustTournament(t, database)
		clock = record.Checkpoint
		if _, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{Checkpoint: &clock}); err != nil {
			t.Fatal(err)
		}
	}
	clock = mustTournament(t, database).Checkpoint
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatalf("resume after level availability and entrant count restored: %v", err)
	}
}
