package tournament

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func TestConfiguredPurchasesAndDeadlineWindows(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Alex", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if initial.PriceCents != 200 || initial.Chips != 1000 {
		t.Fatalf("initial purchase = %+v", initial)
	}
	markStartedAt(t, database, 4, false)
	clock := store.Checkpoint{SegmentIndex: 4, Remaining: 10 * time.Minute}
	action := ActionContext{Checkpoint: &clock}

	second, err := service.RegisterPlayer(ctx, "Jordan", false, action)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Buy(ctx, second.ID, PurchaseInitialBuyIn, action); err != nil {
		t.Fatalf("initial buy-in during designated registration break: %v", err)
	}
	if _, err := service.Buy(ctx, player.ID, PurchaseRebuy, action); err != nil {
		t.Fatalf("rebuy during registration break: %v", err)
	}
	addon, err := service.Buy(ctx, player.ID, PurchaseAddOn, action)
	if err != nil {
		t.Fatal(err)
	}
	if addon.Chips != 5000 || addon.PriceCents != 200 {
		t.Fatalf("add-on = %+v", addon)
	}
	players, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]Player, len(players))
	for _, listed := range players {
		states[listed.ID] = listed
	}
	if !states[player.ID].HasAddOn || states[second.ID].HasAddOn {
		t.Fatalf("add-on eligibility flags = %+v", states)
	}
	if _, err := service.Buy(ctx, player.ID, PurchaseAddOn, action); !errors.Is(err, ErrAddOnAlreadyPaid) {
		t.Fatalf("second add-on error = %v", err)
	}

	markStartedAt(t, database, 4, true)
	finishedClock := store.Checkpoint{SegmentIndex: 4, Remaining: 0, SegmentFinished: true}
	finishedAction := ActionContext{Checkpoint: &finishedClock}
	third, err := service.RegisterPlayer(ctx, "Casey", false, finishedAction)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Buy(ctx, third.ID, PurchaseInitialBuyIn, finishedAction); !errors.Is(err, ErrInitialBuyInClosed) {
		t.Fatalf("initial buy-in after end-of-break deadline = %v", err)
	}
	if _, err := service.Buy(ctx, player.ID, PurchaseAddOn, finishedAction); !errors.Is(err, ErrAddOnWindowClosed) {
		t.Fatalf("add-on after break ended = %v", err)
	}
}

func TestBuyInAndSummaryUseIntegerMoneyAndChips(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	for _, name := range []string{"One", "Two"} {
		player, err := service.RegisterPlayer(ctx, name, false, ActionContext{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{}); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := service.CurrentSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.InitialBuyIns != 2 || summary.Entrants != 2 || summary.ActivePlayers != 2 || summary.TotalStack != 2000 || summary.AverageStack != 1000 {
		t.Fatalf("tournament summary = %+v", summary)
	}
	if summary.PrizePoolCents != 400 || summary.InitialBuyInCents != 400 {
		t.Fatalf("money totals = %+v", summary)
	}
}

func TestVoidPurchaseRetainsHistoryAndUnblocksConfigurationEdit(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Morgan", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Purchases.InitialBuyIn.Chips = 2000
	err = service.UpdateConfiguration(ctx, template, ActionContext{})
	var configError *PurchaseConfigError
	if !errors.As(err, &configError) || !errors.Is(err, ErrPurchaseConfigInUse) || configError.Kind != PurchaseInitialBuyIn || configError.Count != 1 {
		t.Fatalf("purchase configuration edit error = %v", err)
	}
	if err := service.VoidPurchase(ctx, purchase.ID, "entered in error", ActionContext{}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateConfiguration(ctx, template, ActionContext{}); err != nil {
		t.Fatalf("configuration edit after voiding purchase: %v", err)
	}
	purchases, err := service.Purchases(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(purchases) != 1 || purchases[0].VoidedByEventID == nil || purchases[0].ID != purchase.ID {
		t.Fatalf("purchase history = %+v", purchases)
	}
	players, err := service.Players(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].Active || players[0].HasInitialBuyIn {
		t.Fatalf("player after initial purchase void = %+v", players)
	}
	summary, err := service.CurrentSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Entrants != 0 || summary.TotalStack != 0 || summary.PrizePoolCents != 0 || summary.AverageStack != 0 {
		t.Fatalf("summary after void = %+v", summary)
	}
	events, err := database.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var voidEvent store.EventRecord
	for _, event := range events {
		if event.Kind == "purchase_voided" {
			voidEvent = event
		}
	}
	if voidEvent.ID == 0 || voidEvent.VoidEventID == nil || *voidEvent.VoidEventID != purchase.EventID || voidEvent.Explanation != "entered in error" {
		t.Fatalf("purchase void audit = %+v", voidEvent)
	}
}

func TestVoidInitialBuyInBlockedByLaterRebuy(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Dependent", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	markStartedAt(t, database, 4, false)
	clock := store.Checkpoint{SegmentIndex: 4, Remaining: 5 * time.Minute}
	if _, err := service.Buy(ctx, player.ID, PurchaseRebuy, ActionContext{Checkpoint: &clock}); err != nil {
		t.Fatal(err)
	}
	err = service.VoidPurchase(ctx, initial.ID, "wrong entry", ActionContext{Checkpoint: &clock})
	if !errors.Is(err, ErrPlayerHasHistory) {
		t.Fatalf("void dependent initial purchase error = %v", err)
	}
	purchases, err := service.Purchases(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(purchases) != 2 || purchases[0].VoidedByEventID != nil {
		t.Fatalf("failed correction modified purchase history: %+v", purchases)
	}
}

func TestConfiguredAddonAndRebuyFlagsAreEnforced(t *testing.T) {
	template, err := config.DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.Registration.RebuysEnabled = false
	template.Registration.AddOnsEnabled = false
	database, err := store.Create(context.Background(), filepath.Join(t.TempDir(), "disabled.db"), "Disabled flags", template)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := NewService(database)
	ctx := context.Background()
	player, err := service.RegisterPlayer(ctx, "Disabled", false, ActionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{}); err != nil {
		t.Fatal(err)
	}
	markStartedAt(t, database, 4, false)
	clock := store.Checkpoint{SegmentIndex: 4, Remaining: 5 * time.Minute}
	action := ActionContext{Checkpoint: &clock}
	if _, err := service.Buy(ctx, player.ID, PurchaseRebuy, action); !errors.Is(err, ErrRebuysDisabled) {
		t.Fatalf("disabled rebuy error = %v", err)
	}
	if _, err := service.Buy(ctx, player.ID, PurchaseAddOn, action); !errors.Is(err, ErrAddOnsDisabled) {
		t.Fatalf("disabled add-on error = %v", err)
	}
}

func TestVoidingEntryBelowTwoEntrantsPausesRunningClock(t *testing.T) {
	service, database := newTestService(t)
	defer database.Close()
	ctx := context.Background()
	purchases := make([]Purchase, 2)
	for index, name := range []string{"A", "B"} {
		player, err := service.RegisterPlayer(ctx, name, false, ActionContext{})
		if err != nil {
			t.Fatal(err)
		}
		purchase, err := service.Buy(ctx, player.ID, PurchaseInitialBuyIn, ActionContext{})
		if err != nil {
			t.Fatal(err)
		}
		purchases[index] = purchase
	}
	if err := service.StartTournament(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	record, err := database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := record.Checkpoint
	if err := service.VoidPurchase(ctx, purchases[1].ID, "mistaken entrant", ActionContext{Checkpoint: &checkpoint}); err != nil {
		t.Fatal(err)
	}
	record, err = database.Tournament(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != store.StatusPaused || record.Checkpoint.Running {
		t.Fatalf("clock did not pause after entrants fell below two: %+v", record)
	}
	if _, err := service.ResumeTournament(ctx, ActionContext{Checkpoint: &record.Checkpoint}); !errors.Is(err, ErrInsufficientEntrants) {
		t.Fatalf("resume with one valid entrant = %v", err)
	}
}

func markStartedAt(t *testing.T, database *store.Store, segment int, finished bool) {
	t.Helper()
	remaining := int64(5 * time.Minute)
	if finished {
		remaining = 0
	}
	_, err := database.CommitEvent(context.Background(), store.EventInput{Kind: "test_tournament_started"}, nil, func(tx *sql.Tx, _ int64) error {
		_, err := tx.Exec(`UPDATE tournament SET status = ?, started_at = ?, segment_index = ?,
			remaining_ns = ?, segment_finished = ?, clock_running = 0 WHERE id = 1`,
			store.StatusPaused, store.FormatWallTime(time.Now()), segment, remaining, boolInt(finished))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
