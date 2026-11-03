package tournament

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestRegisterPlayerUsesTrimmedUnicodeCaseFoldedIdentity(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()

	first, err := service.RegisterPlayer(ctx, "  Jose\u0301  ", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "José" {
		t.Fatalf("normalized display name = %q", first.Name)
	}
	if _, err := service.RegisterPlayer(ctx, "JOSÉ", false, ActionContext{}); !errors.Is(err, ErrDuplicatePlayer) {
		t.Fatalf("duplicate player error = %v, want ErrDuplicatePlayer", err)
	}
	players, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].ID != first.ID {
		t.Fatalf("players = %+v", players)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != "player_created" || events[1].ClockElapsed != nil {
		t.Fatalf("pre-start player audit event = %+v", events)
	}
}

func TestRemovedPlayerRequiresExplicitRestoreAndKeepsIdentity(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()

	first, err := service.RegisterPlayer(ctx, "Sam", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePlayer(ctx, first.ID, "registered by mistake", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	candidate, err := service.RegisterPlayer(ctx, " SAM ", false, ActionContext{})
	if !errors.Is(err, ErrRestoreConfirmation) {
		t.Fatalf("re-add removed player error = %v, want confirmation", err)
	}
	if candidate.ID != first.ID || !candidate.Removed {
		t.Fatalf("restore candidate = %+v", candidate)
	}
	restored, err := service.RegisterPlayer(ctx, "sam", true, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != first.ID || restored.Removed || restored.Name != "sam" {
		t.Fatalf("restored identity = %+v", restored)
	}
	all, err := service.Players(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != first.ID || all[0].Removed {
		t.Fatalf("players after restore = %+v", all)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Kind != "player_removed" || events[3].Kind != "player_restored" {
		t.Fatalf("player lifecycle audit kinds = %v", eventKinds(events))
	}
	if events[2].Explanation != "registered by mistake" {
		t.Fatalf("removal explanation = %q", events[2].Explanation)
	}
}

func TestPlayerRemovalRequiresBoundedExplanation(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	player, err := service.RegisterPlayer(context.Background(), "Taylor", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, explanation := range []string{"", "  \n", strings.Repeat("x", 201)} {
		if err := service.RemovePlayer(context.Background(), player.ID, explanation, ActionContext{}); err == nil {
			t.Fatalf("accepted explanation of %d runes", len([]rune(explanation)))
		}
	}
	if err := service.RemovePlayer(context.Background(), player.ID, strings.Repeat("x", 200), ActionContext{}); err != nil {
		t.Fatalf("rejected 200-character explanation: %v", err)
	}
}

func TestRenamingPreservesPlayerIDAndRemovalReportsDependencies(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Before", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RenamePlayer(ctx, player.ID, "After", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	players, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].ID != player.ID || players[0].Name != "After" {
		t.Fatalf("renamed player = %+v", players)
	}
	err = service.RemovePlayer(ctx, player.ID, "remove", ActionContext{})
	var dependencies *DependentEventsError
	if !errors.As(err, &dependencies) || len(dependencies.Events) != 1 || !errors.Is(err, ErrPlayerHasHistory) {
		t.Fatalf("remove after rename error = %v, want dependent event details", err)
	}
}

func TestRestoreDoesNotCreateSecondIdentity(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Restored", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RemovePlayer(ctx, player.ID, "test", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterPlayer(ctx, "Restored", true, ActionContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterPlayer(ctx, "Restored", true, ActionContext{}); !errors.Is(err, ErrDuplicatePlayer) {
		t.Fatalf("duplicate restore error = %v", err)
	}
}

func TestActionClockContextMustMatchTournamentStartState(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	if _, err := service.RegisterPlayer(context.Background(), "Clockless", false, ActionContext{Checkpoint: &store.Checkpoint{}}); !errors.Is(err, ErrUnexpectedClockContext) {
		t.Fatalf("pre-start checkpoint error = %v", err)
	}
}

func eventKinds(events []store.EventRecord) []string {
	kinds := make([]string, len(events))
	for index, event := range events {
		kinds[index] = event.Kind
	}
	return kinds
}

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "tournament.db"), "Service test", template)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(database), database
}
