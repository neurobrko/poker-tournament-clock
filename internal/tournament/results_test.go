package tournament

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestIncompleteEndingRequiresExplanationAndReopeningPreservesHistory(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	buyInPlayers(t, service, "A", "B")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if _, err := service.EndTournament(ctx, "", nil, ActionContext{Checkpoint: &record.Checkpoint}); !errors.Is(err, ErrIncompleteExplanationRequired) {
		t.Fatalf("incomplete end without reason = %v", err)
	}
	result, err := service.EndTournament(ctx, "players agreed to stop", nil, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed || len(result.Standings.Groups) != 0 || len(result.Prizes) != 0 {
		t.Fatalf("incomplete end result declared standings: %+v", result)
	}
	ended := mustTournament(t, database)
	if ended.Status != store.StatusEnded || ended.EndedAt == nil {
		t.Fatalf("ended tournament state = %+v", ended)
	}
	var completed, prizeGroups int
	if err := database.ViewTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT completed FROM tournament_ends ORDER BY event_id DESC LIMIT 1").Scan(&completed); err != nil {
			return err
		}
		return tx.QueryRow("SELECT COUNT(*) FROM end_prize_groups").Scan(&prizeGroups)
	}); err != nil {
		t.Fatal(err)
	}
	if completed != 0 || prizeGroups != 0 {
		t.Fatalf("incomplete ending persisted completed=%d prize groups=%d", completed, prizeGroups)
	}
	if err := service.ReopenTournament(ctx, "", ActionContext{}); err == nil {
		t.Fatal("reopened without required explanation")
	}
	if err := service.ReopenTournament(ctx, "operator correction", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	reopened := mustTournament(t, database)
	if reopened.Status != store.StatusPaused || reopened.EndedAt != nil || reopened.StartedAt == nil {
		t.Fatalf("reopened tournament state = %+v", reopened)
	}
	if err := service.UpdateConfiguration(ctx, reopened.Config, ActionContext{}); !errors.Is(err, store.ErrConfigurationLocked) {
		t.Fatalf("settings unlocked after reopen: %v", err)
	}
	if err := database.ViewTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT COUNT(*) FROM tournament_ends").Scan(&prizeGroups)
	}); err != nil {
		t.Fatal(err)
	}
	if prizeGroups != 1 {
		t.Fatalf("reopen erased prior ending history: %d rows", prizeGroups)
	}
}

func TestSingleRemainingPlayerCanFinalizeWhileRebuysAreOpen(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "Winner", "Runner up")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	clock := ActionContext{Checkpoint: &record.Checkpoint}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, clock); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	result, err := service.EndTournament(ctx, "", nil, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || len(result.Prizes) != 2 || result.Prizes[0].Players[0].PlayerID != players[0].ID || result.Prizes[0].TotalCents+result.Prizes[1].TotalCents != 400 {
		t.Fatalf("completed result = %+v", result)
	}
	if result.Prizes[0].TotalCents != 280 || result.Prizes[1].TotalCents != 120 {
		t.Fatalf("short-field normalized prizes = %+v", result.Prizes)
	}
}

func TestTiedPrizeRemainderRequiresOperatorChoiceAndConservesPool(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Purchases.InitialBuyIn.Price = 10
	service, database := newServiceWithTemplate(t, template)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "Winner", "Tie A", "Tie B")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	clock := ActionContext{Checkpoint: &record.Checkpoint}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID},
		Bustees: []Bustee{
			{PlayerID: players[1].ID, Position: 1, Tied: true},
			{PlayerID: players[2].ID, Position: 1, Tied: true},
		},
	}, clock); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if _, err := service.EndTournament(ctx, "", nil, ActionContext{Checkpoint: &record.Checkpoint}); !errors.Is(err, ErrPendingPrizeChoices) {
		t.Fatalf("tied payout without choice = %v", err)
	}
	choices := map[int][]string{2: {players[2].ID}}
	result, err := service.EndTournament(ctx, "", choices, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.PoolCents != 30 || len(result.Prizes) != 2 {
		t.Fatalf("tied final result = %+v", result)
	}
	var total int64
	for _, group := range result.Prizes {
		if group.PendingRemainderCents != 0 {
			t.Fatalf("final prize has pending remainder: %+v", group)
		}
		for _, player := range group.Players {
			total += player.AmountCents
		}
	}
	if total != result.PoolCents {
		t.Fatalf("awards total %d cents, pool %d cents", total, result.PoolCents)
	}
	amounts := make(map[string]int64)
	for _, player := range result.Prizes[1].Players {
		amounts[player.PlayerID] = player.AmountCents
	}
	if amounts[players[2].ID] != 10 || amounts[players[1].ID] != 0 {
		t.Fatalf("operator remainder choice not applied: %+v", result.Prizes[1])
	}
	var persistedPlayers int
	if err := database.ViewTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT COUNT(*) FROM end_prize_players").Scan(&persistedPlayers)
	}); err != nil {
		t.Fatal(err)
	}
	if persistedPlayers != 3 {
		t.Fatalf("persisted final prize players = %d, want 3", persistedPlayers)
	}
	stored, err := service.LatestFinalResult(ctx)
	if err != nil {
		t.Fatal(err)
	}
	storedAmounts := make(map[string]int64)
	for _, group := range stored.Prizes {
		for _, player := range group.Players {
			storedAmounts[player.PlayerID] = player.AmountCents
		}
	}
	if storedAmounts[players[2].ID] != 10 || storedAmounts[players[1].ID] != 0 || stored.PoolCents != 30 {
		t.Fatalf("stored final result lost remainder choice: %+v", stored)
	}
}

func TestCurrentPrizesRemainProvisionalBeforeEnd(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	result, err := service.CurrentPrizes(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed || len(result.Prizes) == 0 || mustTournament(t, database).Status == store.StatusEnded {
		t.Fatalf("provisional prizes changed tournament finalization state: %+v", result)
	}
}
