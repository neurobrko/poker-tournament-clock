package tournament

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

var (
	ErrTournamentNotStarted  = errors.New("tournament has not started")
	ErrInitialBuyInClosed    = errors.New("initial buy-in registration is closed")
	ErrRebuysDisabled        = errors.New("rebuys are disabled")
	ErrAddOnsDisabled        = errors.New("add-ons are disabled")
	ErrAddOnWindowClosed     = errors.New("add-on window is closed")
	ErrPlayerNeedsBuyIn      = errors.New("player needs a valid initial buy-in first")
	ErrInitialAlreadyPaid    = errors.New("player already has a valid initial buy-in")
	ErrAddOnAlreadyPaid      = errors.New("player already used their add-on")
	ErrPurchaseNotFound      = errors.New("purchase not found")
	ErrPurchaseAlreadyVoided = errors.New("purchase is already voided")
	ErrPurchaseConfigInUse   = errors.New("purchase configuration cannot change while valid purchases exist")
)

type PurchaseKind string

const (
	PurchaseInitialBuyIn PurchaseKind = "initial_buy_in"
	PurchaseRebuy        PurchaseKind = "rebuy"
	PurchaseAddOn        PurchaseKind = "add_on"
)

type Purchase struct {
	ID              int64
	PlayerID        string
	Kind            PurchaseKind
	PriceCents      config.EuroCents
	Chips           int64
	EventID         int64
	VoidedByEventID *int64
}

type Summary struct {
	InitialBuyIns     int64
	InitialBuyInCents config.EuroCents
	Rebuys            int64
	RebuyCents        config.EuroCents
	AddOns            int64
	AddOnCents        config.EuroCents
	Entrants          int64
	ActivePlayers     int64
	TotalStack        int64
	AverageStack      int64
	PrizePoolCents    config.EuroCents
}

type PurchaseConfigError struct {
	Kind  PurchaseKind
	Count int64
}

func (e *PurchaseConfigError) Error() string {
	return fmt.Sprintf("%v: %d non-void %s purchase(s) must be voided first", ErrPurchaseConfigInUse, e.Count, e.Kind)
}
func (e *PurchaseConfigError) Unwrap() error { return ErrPurchaseConfigInUse }

// Buy records a configured purchase without accepting per-purchase price or chip overrides.
func (s *Service) Buy(ctx context.Context, playerID string, kind PurchaseKind, action ActionContext) (Purchase, error) {
	if playerID == "" {
		return Purchase{}, ErrPlayerNotFound
	}
	if kind != PurchaseInitialBuyIn && kind != PurchaseRebuy && kind != PurchaseAddOn {
		return Purchase{}, fmt.Errorf("unknown purchase kind %q", kind)
	}
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return Purchase{}, err
	}
	configured := purchaseConfiguration(record.Config, kind)
	if configured == nil {
		return Purchase{}, fmt.Errorf("purchase type %q is not configured", kind)
	}
	payload := map[string]any{
		"player_id": playerID, "kind": kind,
		"price_cents": int64(configured.Price), "chips": configured.Chips,
	}
	event, checkpoint := action.event("purchase_recorded", payload, "")
	var purchase Purchase
	_, err = s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var status store.TournamentStatus
		var configJSON string
		var startedAt sql.NullString
		var segmentIndex, segmentFinished int
		if err := tx.QueryRowContext(ctx, `SELECT status, configuration_json, started_at, segment_index, segment_finished
			FROM tournament WHERE id = 1`).Scan(&status, &configJSON, &startedAt, &segmentIndex, &segmentFinished); err != nil {
			return err
		}
		var current config.Template
		if err := json.Unmarshal([]byte(configJSON), &current); err != nil {
			return err
		}
		if err := current.Validate(); err != nil {
			return err
		}
		currentPurchase := purchaseConfiguration(current, kind)
		if currentPurchase == nil || *currentPurchase != *configured {
			return fmt.Errorf("purchase configuration changed while action was open; retry")
		}
		if (kind == PurchaseRebuy || kind == PurchaseAddOn) && !startedAt.Valid {
			return ErrTournamentNotStarted
		}
		if kind != PurchaseAddOn && !registrationOpen(current, segmentIndex, segmentFinished != 0, startedAt.Valid) {
			return ErrInitialBuyInClosed
		}
		var active, removed int
		if err := tx.QueryRowContext(ctx, "SELECT active, removed FROM players WHERE id = ?", playerID).Scan(&active, &removed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlayerNotFound
			}
			return err
		}
		if removed != 0 {
			return ErrPlayerRemoved
		}
		var initialCount int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchases WHERE player_id = ?
			AND kind = 'initial_buy_in' AND voided_by_event_id IS NULL`, playerID).Scan(&initialCount); err != nil {
			return err
		}
		switch kind {
		case PurchaseInitialBuyIn:
			if initialCount != 0 {
				return ErrInitialAlreadyPaid
			}
			if active != 0 {
				return fmt.Errorf("player state is active without a valid initial buy-in")
			}
		case PurchaseRebuy:
			if !current.Registration.RebuysEnabled {
				return ErrRebuysDisabled
			}
			if initialCount == 0 {
				return ErrPlayerNeedsBuyIn
			}
		case PurchaseAddOn:
			if !current.Registration.AddOnsEnabled {
				return ErrAddOnsDisabled
			}
			if initialCount == 0 {
				return ErrPlayerNeedsBuyIn
			}
			if active == 0 {
				return ErrPlayerNotActive
			}
			if !inAddOnBreak(current, segmentIndex, segmentFinished != 0) {
				return ErrAddOnWindowClosed
			}
			var existing int64
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchases WHERE player_id = ?
				AND kind = 'add_on' AND voided_by_event_id IS NULL`, playerID).Scan(&existing); err != nil {
				return err
			}
			if existing != 0 {
				return ErrAddOnAlreadyPaid
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO purchases (player_id, kind, price_cents, chips, event_id)
			VALUES (?, ?, ?, ?, ?)`, playerID, kind, int64(configured.Price), configured.Chips, eventID)
		if err != nil {
			return err
		}
		purchaseID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if kind == PurchaseInitialBuyIn || kind == PurchaseRebuy {
			if _, err := tx.ExecContext(ctx, "UPDATE players SET active = 1 WHERE id = ?", playerID); err != nil {
				return err
			}
		}
		purchase = Purchase{ID: purchaseID, PlayerID: playerID, Kind: kind, PriceCents: configured.Price, Chips: configured.Chips, EventID: eventID}
		if err := reconcileClockAfterStateChange(ctx, tx, checkpoint, eventID, payload); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Purchase{}, err
	}
	return purchase, nil
}

func purchaseConfiguration(template config.Template, kind PurchaseKind) *config.PurchaseType {
	switch kind {
	case PurchaseInitialBuyIn:
		return &template.Purchases.InitialBuyIn
	case PurchaseRebuy:
		return &template.Purchases.Rebuy
	case PurchaseAddOn:
		return &template.Purchases.AddOn
	default:
		return nil
	}
}

func registrationOpen(template config.Template, segmentIndex int, segmentFinished, started bool) bool {
	if !started {
		return true
	}
	deadline := template.Registration.RegistrationDeadline
	wantedKind := config.SegmentLevel
	if deadline.At == config.DeadlineAtBreakStart || deadline.At == config.DeadlineAtBreakEnd {
		wantedKind = config.SegmentBreak
	}
	targetIndex, ordinal := 0, 0
	for index, segment := range template.Schedule {
		if segment.Kind == wantedKind {
			ordinal++
			if ordinal == deadline.Target {
				targetIndex = index + 1
				break
			}
		}
	}
	if targetIndex == 0 {
		return false
	}
	if deadline.At == config.DeadlineAtBreakEnd {
		return segmentIndex < targetIndex || segmentIndex == targetIndex && !segmentFinished
	}
	return segmentIndex < targetIndex
}

func inAddOnBreak(template config.Template, segmentIndex int, segmentFinished bool) bool {
	if segmentIndex < 1 || segmentFinished {
		return false
	}
	ordinal := 0
	for index, segment := range template.Schedule {
		if segment.Kind != config.SegmentBreak {
			continue
		}
		ordinal++
		if ordinal == template.Registration.AddOnBreak {
			return index+1 == segmentIndex
		}
	}
	return false
}

func tournamentTotalsTx(ctx context.Context, tx *sql.Tx) (totalChips, activePlayers int64, err error) {
	rows, err := tx.QueryContext(ctx, "SELECT chips FROM purchases WHERE voided_by_event_id IS NULL")
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var chips int64
		if err := rows.Scan(&chips); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if chips > 0 && totalChips > math.MaxInt64-chips {
			rows.Close()
			return 0, 0, fmt.Errorf("total chips overflow")
		}
		totalChips += chips
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM players WHERE active = 1 AND removed = 0").Scan(&activePlayers); err != nil {
		return 0, 0, err
	}
	return totalChips, activePlayers, nil
}

// VoidPurchase retains the original purchase, records a reason, and blocks dependent corrections.
func (s *Service) VoidPurchase(ctx context.Context, purchaseID int64, explanation string, action ActionContext) error {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" || len([]rune(explanation)) > 200 {
		return fmt.Errorf("void explanation must contain 1 to 200 characters")
	}
	var purchase Purchase
	if err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		var voided sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT id, player_id, kind, price_cents, chips, event_id, voided_by_event_id
			FROM purchases WHERE id = ?`, purchaseID).Scan(&purchase.ID, &purchase.PlayerID, &purchase.Kind,
			&purchase.PriceCents, &purchase.Chips, &purchase.EventID, &voided)
		if err != nil {
			return err
		}
		if voided.Valid {
			return ErrPurchaseAlreadyVoided
		}
		return nil
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPurchaseNotFound
		}
		return err
	}
	voidedEventID := purchase.EventID
	payload := map[string]any{"purchase_id": purchase.ID, "player_id": purchase.PlayerID, "kind": purchase.Kind}
	event, checkpoint := action.event("purchase_voided", payload, explanation)
	event.ParentEventID = &voidedEventID
	event.VoidEventID = &voidedEventID
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var currentVoided sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT voided_by_event_id FROM purchases WHERE id = ?", purchaseID).Scan(&currentVoided); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPurchaseNotFound
			}
			return err
		}
		if currentVoided.Valid {
			return ErrPurchaseAlreadyVoided
		}
		if _, err := tx.ExecContext(ctx, "UPDATE purchases SET voided_by_event_id = ? WHERE id = ?", eventID, purchaseID); err != nil {
			return err
		}
		if purchase.Kind == PurchaseInitialBuyIn || purchase.Kind == PurchaseRebuy {
			active, sittingOut, dependencies, err := replayPlayerState(ctx, tx, purchase.PlayerID, purchaseID)
			if err != nil {
				return err
			}
			if len(dependencies) != 0 {
				return &DependentEventsError{PlayerID: purchase.PlayerID, Events: dependencies}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE players SET active = ?, sitting_out = ? WHERE id = ?", boolInt(active), boolInt(sittingOut), purchase.PlayerID); err != nil {
				return err
			}
		}
		return reconcileClockAfterStateChange(ctx, tx, checkpoint, eventID, payload)
	})
	return err
}

// Purchases returns all transactions, including voided history.
func (s *Service) Purchases(ctx context.Context, playerID string) ([]Purchase, error) {
	purchases := make([]Purchase, 0)
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		query := `SELECT id, player_id, kind, price_cents, chips, event_id, voided_by_event_id FROM purchases`
		args := []any{}
		if playerID != "" {
			query += " WHERE player_id = ?"
			args = append(args, playerID)
		}
		query += " ORDER BY id"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var purchase Purchase
			var voided sql.NullInt64
			if err := rows.Scan(&purchase.ID, &purchase.PlayerID, &purchase.Kind, &purchase.PriceCents, &purchase.Chips, &purchase.EventID, &voided); err != nil {
				return err
			}
			if voided.Valid {
				value := voided.Int64
				purchase.VoidedByEventID = &value
			}
			purchases = append(purchases, purchase)
		}
		return rows.Err()
	})
	return purchases, err
}

// CurrentSummary derives all money and chip totals from non-void purchases and current active players.
func (s *Service) CurrentSummary(ctx context.Context) (Summary, error) {
	var summary Summary
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT kind, price_cents, chips FROM purchases WHERE voided_by_event_id IS NULL`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var kind PurchaseKind
			var cents, chips int64
			if err := rows.Scan(&kind, &cents, &chips); err != nil {
				rows.Close()
				return err
			}
			totalCents, ok := addNonnegative(int64(summary.PrizePoolCents), cents)
			if !ok {
				rows.Close()
				return fmt.Errorf("prize pool overflow")
			}
			summary.PrizePoolCents = config.EuroCents(totalCents)
			totalChips, ok := addNonnegative(summary.TotalStack, chips)
			if !ok {
				rows.Close()
				return fmt.Errorf("total stack overflow")
			}
			summary.TotalStack = totalChips
			switch kind {
			case PurchaseInitialBuyIn:
				summary.InitialBuyIns++
				summary.InitialBuyInCents += config.EuroCents(cents)
				summary.Entrants++
			case PurchaseRebuy:
				summary.Rebuys++
				summary.RebuyCents += config.EuroCents(cents)
			case PurchaseAddOn:
				summary.AddOns++
				summary.AddOnCents += config.EuroCents(cents)
			default:
				rows.Close()
				return fmt.Errorf("unknown stored purchase kind %q", kind)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM players WHERE active = 1 AND removed = 0").Scan(&summary.ActivePlayers); err != nil {
			return err
		}
		if summary.ActivePlayers > 0 {
			quotient := summary.TotalStack / summary.ActivePlayers
			remainder := summary.TotalStack % summary.ActivePlayers
			if remainder >= summary.ActivePlayers/2+summary.ActivePlayers%2 {
				quotient++
			}
			summary.AverageStack = quotient
		}
		return nil
	})
	return summary, err
}

func replayPlayerState(ctx context.Context, tx *sql.Tx, playerID string, excludedPurchaseID int64) (bool, bool, []string, error) {
	return replayPlayerStateThrough(ctx, tx, playerID, excludedPurchaseID, math.MaxInt64)
}

func replayPlayerStateThrough(ctx context.Context, tx *sql.Tx, playerID string, excludedPurchaseID, beforeEventID int64) (active, sittingOut bool, dependencies []string, err error) {
	type action struct {
		eventID int64
		kind    string
		name    string
	}
	actions := make([]action, 0)
	rows, err := tx.QueryContext(ctx, `SELECT p.event_id, p.kind, p.id FROM purchases p
			WHERE p.player_id = ? AND p.voided_by_event_id IS NULL AND p.id != ? AND p.event_id < ?`, playerID, excludedPurchaseID, beforeEventID)
	if err != nil {
		return false, false, nil, err
	}
	for rows.Next() {
		var item action
		var purchaseID int64
		if err := rows.Scan(&item.eventID, &item.kind, &purchaseID); err != nil {
			rows.Close()
			return false, false, nil, err
		}
		item.name = fmt.Sprintf("purchase #%d", purchaseID)
		actions = append(actions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, nil, err
	}
	if err := rows.Close(); err != nil {
		return false, false, nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT bp.event_id, bp.role, 'bust #' || bp.event_id FROM bust_participants bp
		JOIN busts b ON b.event_id = bp.event_id
		WHERE bp.player_id = ? AND bp.voided_by_event_id IS NULL AND b.voided_by_event_id IS NULL AND bp.event_id < ?
		UNION ALL
		SELECT pc.bust_event_id, pc.role, 'bust #' || pc.bust_event_id FROM bust_participant_corrections pc
		JOIN busts b ON b.event_id = pc.bust_event_id
		WHERE pc.player_id = ? AND pc.voided_by_event_id IS NULL AND pc.player_id IS NOT NULL AND b.voided_by_event_id IS NULL AND pc.bust_event_id < ?`, playerID, beforeEventID, playerID, beforeEventID)
	if err != nil {
		return false, false, nil, err
	}
	for rows.Next() {
		var item action
		if err := rows.Scan(&item.eventID, &item.kind, &item.name); err != nil {
			rows.Close()
			return false, false, nil, err
		}
		actions = append(actions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, nil, err
	}
	if err := rows.Close(); err != nil {
		return false, false, nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT pe.event_id, pe.kind, pe.kind || ' event #' || pe.event_id
		FROM player_events pe WHERE pe.player_id = ? AND pe.kind IN ('player_sitting_out', 'player_returned') AND pe.event_id < ?
		AND NOT EXISTS (SELECT 1 FROM audit_events v WHERE v.voids_event_id = pe.event_id)`, playerID, beforeEventID)
	if err != nil {
		return false, false, nil, err
	}
	for rows.Next() {
		var item action
		if err := rows.Scan(&item.eventID, &item.kind, &item.name); err != nil {
			rows.Close()
			return false, false, nil, err
		}
		actions = append(actions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, false, nil, err
	}
	if err := rows.Close(); err != nil {
		return false, false, nil, err
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].eventID < actions[j].eventID })
	hasInitialBuyIn := false
	for _, item := range actions {
		switch item.kind {
		case string(PurchaseInitialBuyIn):
			hasInitialBuyIn = true
			active = true
		case string(PurchaseRebuy):
			if !hasInitialBuyIn {
				dependencies = append(dependencies, item.name)
				continue
			}
			if !active {
				active = true
				sittingOut = false
			}
		case "buster":
			if !active {
				dependencies = append(dependencies, item.name)
			}
		case "bustee":
			if !active {
				dependencies = append(dependencies, item.name)
			}
			active = false
			sittingOut = false
		case "player_sitting_out":
			if !active {
				dependencies = append(dependencies, item.name)
			} else {
				sittingOut = true
			}
		case "player_returned":
			if !active {
				dependencies = append(dependencies, item.name)
			} else {
				sittingOut = false
			}
		}
	}
	return active, sittingOut, dependencies, nil
}

func changedPurchaseTypes(old, next config.PurchaseConfig) []PurchaseKind {
	var changed []PurchaseKind
	for _, item := range []struct {
		kind PurchaseKind
		old  config.PurchaseType
		new  config.PurchaseType
	}{
		{PurchaseInitialBuyIn, old.InitialBuyIn, next.InitialBuyIn},
		{PurchaseRebuy, old.Rebuy, next.Rebuy},
		{PurchaseAddOn, old.AddOn, next.AddOn},
	} {
		if item.old != item.new {
			changed = append(changed, item.kind)
		}
	}
	return changed
}

func addNonnegative(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}
