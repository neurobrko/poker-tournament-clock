package tournament

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestVoidInitialBuyInRejectsDependentAddOn(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C")
	purchases, err := service.Purchases(ctx, players[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	if _, err := service.SelectSegment(ctx, 4, nil, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if _, err := service.Buy(ctx, players[0].ID, PurchaseAddOn, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	err = service.VoidPurchase(ctx, purchases[0].ID, "incorrect registration", ActionContext{Checkpoint: &record.Checkpoint})
	var dependent *DependentEventsError
	if !errors.As(err, &dependent) {
		t.Fatalf("void initial buy-in with dependent add-on = %v", err)
	}
	summary, err := service.CurrentSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Entrants != 3 || summary.AddOns != 1 {
		t.Fatalf("rejected void changed tournament totals: %+v", summary)
	}
}

func TestBustCorrectionCanReplaceCurrentParticipantAgain(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C", "D")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	bustID, err := service.RecordBust(ctx, BustRequest{Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}}}, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if err := service.CorrectBustParticipant(ctx, bustID, players[1].ID, "bustee", &BustParticipant{PlayerID: players[2].ID, Role: "bustee", Position: 1}, "first correction", ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if err := service.CorrectBustParticipant(ctx, bustID, players[2].ID, "bustee", &BustParticipant{PlayerID: players[3].ID, Role: "bustee", Position: 1}, "correct the correction", ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatalf("correct replacement participant: %v", err)
	}
	participants, err := service.EffectiveBustParticipants(ctx, bustID)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 2 {
		t.Fatalf("effective bust participants = %+v", participants)
	}
	foundReplacement, foundSuperseded := false, false
	for _, participant := range participants {
		foundReplacement = foundReplacement || participant.PlayerID == players[3].ID
		foundSuperseded = foundSuperseded || participant.PlayerID == players[2].ID
	}
	if !foundReplacement || foundSuperseded {
		t.Fatalf("latest participant correction not applied: %+v", participants)
	}
	standing, err := service.CurrentStandings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(standing.Groups) != 2 || len(standing.Groups[0].PlayerIDs) != 3 || standing.Groups[1].PlayerIDs[0] != players[3].ID {
		t.Fatalf("standings did not apply latest correction: %+v", standing)
	}
}

func TestConfigurationImportResetsPrestartRemainingTime(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	record := mustTournament(t, database)
	next := record.Config
	next.Schedule[0].Duration = config.Duration(10 * time.Minute)
	if err := service.UpdateConfiguration(ctx, next, ActionContext{}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if record.Checkpoint.Remaining != 10*time.Minute {
		t.Fatalf("prestart remaining=%s, configured opening duration=%s", record.Checkpoint.Remaining, next.Schedule[0].Duration.Value())
	}
}

func TestLatestFinalResultKeepsItsHistoricalStandings(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "Winner", "Runner")
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record := mustTournament(t, database)
	bustID, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, ActionContext{Checkpoint: &record.Checkpoint})
	if err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if _, err := service.EndTournament(ctx, "", nil, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	if err := service.ReopenTournament(ctx, "operator correction", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	record = mustTournament(t, database)
	if _, err := service.Buy(ctx, players[1].ID, PurchaseRebuy, ActionContext{Checkpoint: &record.Checkpoint}); err != nil {
		t.Fatal(err)
	}
	current, err := service.CurrentStandings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Groups) != 1 || len(current.Groups[0].PlayerIDs) != 2 {
		t.Fatalf("reopened current standings = %+v", current)
	}
	final, err := service.LatestFinalResult(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if final.Standings.Entrants != 2 || len(final.Standings.Groups) != 2 || final.Standings.Groups[0].PlayerIDs[0] != players[0].ID || final.Standings.Groups[1].PlayerIDs[0] != players[1].ID {
		t.Fatalf("stored final standings changed after reopen: %+v (bust %d)", final.Standings, bustID)
	}
}

func TestBustParticipantUnmarshalsLegacyAuditFields(t *testing.T) {
	var participant BustParticipant
	if err := json.Unmarshal([]byte(`{"PlayerID":"legacy-player","Role":"bustee","Position":2,"Tied":true}`), &participant); err != nil {
		t.Fatal(err)
	}
	if participant.PlayerID != "legacy-player" || participant.Role != "bustee" || participant.Position != 2 || !participant.Tied {
		t.Fatalf("legacy audit participant = %+v", participant)
	}
}

func TestTournamentAndPlayerNamesEnforceDisplayedLimit(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	if _, err := service.RegisterPlayer(ctx, strings.Repeat("x", store.MaxNameLength+1), false, ActionContext{}); err == nil {
		t.Fatal("player name exceeding UI limit accepted")
	}
	if err := database.RenameTournament(ctx, strings.Repeat("x", store.MaxNameLength+1), time.Now()); err == nil {
		t.Fatal("tournament name exceeding UI limit accepted")
	}
}
