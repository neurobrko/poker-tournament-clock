package tournament

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestRecordBustSupportsMultipleBustersAndTiedBustees(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "Alice", "Boris", "Chris", "Drew")
	markStartedAt(t, database, 1, false)
	clock := store.Checkpoint{SegmentIndex: 1, Remaining: 20 * time.Minute}
	request := BustRequest{
		Busters: []string{players[0].ID, players[1].ID},
		Bustees: []Bustee{
			{PlayerID: players[2].ID, Position: 1, Tied: true},
			{PlayerID: players[3].ID, Position: 1, Tied: true},
		},
	}
	eventID, err := service.RecordBust(ctx, request, ActionContext{Checkpoint: &clock})
	if err != nil {
		t.Fatal(err)
	}
	if eventID <= 0 {
		t.Fatalf("bust event ID = %d", eventID)
	}
	current, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, player := range current {
		if player.Active {
			active++
		}
	}
	if active != 2 {
		t.Fatalf("active players after multi-bust = %d, want 2", active)
	}
	standing, err := service.CurrentStandings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if standing.Entrants != 4 || len(standing.Groups) != 2 || len(standing.Groups[0].PlayerIDs) != 2 || len(standing.Groups[1].PlayerIDs) != 2 {
		t.Fatalf("provisional standings = %+v", standing)
	}
}

func TestBustRebuyReactivatesPlayerAndReordersProvisionalStandings(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C")
	markStartedAt(t, database, 1, false)
	clock := store.Checkpoint{SegmentIndex: 1, Remaining: 20 * time.Minute}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Buy(ctx, players[1].ID, PurchaseRebuy, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[1].ID}, Bustees: []Bustee{{PlayerID: players[2].ID, Position: 1}},
	}, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	standing, err := service.CurrentStandings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if standing.Entrants != 3 || len(standing.Groups) != 2 || len(standing.Groups[0].PlayerIDs) != 2 || standing.Groups[1].PlayerIDs[0] != players[2].ID {
		t.Fatalf("standings after rebuy and bust = %+v", standing)
	}
}

func TestBustRejectsInvalidParticipantsAndTieOrdering(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C")
	markStartedAt(t, database, 1, false)
	clock := ActionContext{Checkpoint: &store.Checkpoint{SegmentIndex: 1, Remaining: 20 * time.Minute}}
	invalid := []BustRequest{
		{Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[0].ID, Position: 1}}},
		{Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 2}}},
		{Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1, Tied: true}}},
	}
	for _, request := range invalid {
		if _, err := service.RecordBust(ctx, request, clock); !errors.Is(err, ErrInvalidBust) {
			t.Fatalf("invalid bust error = %v", err)
		}
	}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, ActionContext{}); !errors.Is(err, ErrClockContextRequired) {
		t.Fatalf("missing running-tournament clock context error = %v", err)
	}
}

func TestCorrectBustParticipantRetainsAuditAndRecomputesState(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "Alice", "Boris", "Chris", "Drew")
	markStartedAt(t, database, 1, false)
	clock := ActionContext{Checkpoint: &store.Checkpoint{SegmentIndex: 1, Remaining: 20 * time.Minute}}
	bustEventID, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CorrectBustParticipant(ctx, bustEventID, players[1].ID, "bustee", &BustParticipant{
		PlayerID: players[3].ID, Role: "bustee", Position: 1,
	}, "operator selected wrong player", clock); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[2].ID, Position: 1}},
	}, clock); err != nil {
		t.Fatal(err)
	}
	current, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]bool)
	for _, player := range current {
		states[player.ID] = player.Active
	}
	if !states[players[1].ID] || states[players[2].ID] || states[players[3].ID] {
		t.Fatalf("corrected player statuses = %+v", states)
	}
	standing, err := service.CurrentStandings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(standing.Groups) != 3 || standing.Groups[1].PlayerIDs[0] != players[2].ID || standing.Groups[2].PlayerIDs[0] != players[3].ID {
		t.Fatalf("standings after participant correction = %+v", standing)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var correction store.EventRecord
	for _, event := range events {
		if event.Kind == "bust_participant_corrected" {
			correction = event
		}
	}
	if correction.ID == 0 || correction.ParentEventID == nil || *correction.ParentEventID != bustEventID || correction.Explanation != "operator selected wrong player" {
		t.Fatalf("correction audit = %+v", correction)
	}
}

func TestBustCorrectionBlockedIfItInvalidatesLaterBuster(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	players := buyInPlayers(t, service, "A", "B", "C", "D")
	markStartedAt(t, database, 1, false)
	clock := ActionContext{Checkpoint: &store.Checkpoint{SegmentIndex: 1, Remaining: 20 * time.Minute}}
	firstEvent, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID, players[2].ID},
		Bustees: []Bustee{{PlayerID: players[1].ID, Position: 1}},
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordBust(ctx, BustRequest{
		Busters: []string{players[0].ID}, Bustees: []Bustee{{PlayerID: players[3].ID, Position: 1}},
	}, clock); err != nil {
		t.Fatal(err)
	}
	err = service.CorrectBustParticipant(ctx, firstEvent, players[0].ID, "buster", &BustParticipant{
		PlayerID: players[0].ID, Role: "bustee", Position: 2,
	}, "correct role", clock)
	if !errors.Is(err, ErrPlayerHasHistory) {
		t.Fatalf("dependent correction error = %v, want dependency block", err)
	}
}

func buyInPlayers(t *testing.T, service *Service, names ...string) []Player {
	t.Helper()
	players := make([]Player, len(names))
	for index, name := range names {
		player, err := service.RegisterPlayer(context.Background(), name, false, ActionContext{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Buy(context.Background(), player.ID, PurchaseInitialBuyIn, ActionContext{}); err != nil {
			t.Fatal(err)
		}
		players[index] = player
	}
	return players
}
