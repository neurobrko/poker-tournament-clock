package tournament

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

// UpdateConfiguration edits tournament structure only before play and refuses to rewrite
// purchase history under changed price or chip values.
func (s *Service) UpdateConfiguration(ctx context.Context, next config.Template, action ActionContext) error {
	if err := next.Validate(); err != nil {
		return fmt.Errorf("invalid tournament configuration: %w", err)
	}
	configJSON, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode tournament configuration: %w", err)
	}
	payload := map[string]any{"configuration": json.RawMessage(configJSON)}
	event, _ := action.event("configuration_changed", payload, "")
	_, err = s.store.CommitEvent(ctx, event, nil, func(tx *sql.Tx, _ int64) error {
		var status string
		var previousJSON string
		if err := tx.QueryRowContext(ctx, "SELECT status, configuration_json FROM tournament WHERE id = 1").Scan(&status, &previousJSON); err != nil {
			return err
		}
		if status == string(store.StatusEnded) {
			return ErrTournamentEnded
		}
		if status != string(store.StatusNotStarted) {
			return store.ErrConfigurationLocked
		}
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		var previous config.Template
		if err := json.Unmarshal([]byte(previousJSON), &previous); err != nil {
			return err
		}
		if err := previous.Validate(); err != nil {
			return err
		}
		for _, kind := range changedPurchaseTypes(previous.Purchases, next.Purchases) {
			var count int64
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM purchases WHERE kind = ? AND voided_by_event_id IS NULL", kind).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return &PurchaseConfigError{Kind: kind, Count: count}
			}
		}
		wall := event.WallTime
		if wall.IsZero() {
			wall = time.Now()
		}
		_, err := tx.ExecContext(ctx, `UPDATE tournament SET configuration_json = ?, updated_at = ?,
			remaining_ns = CASE WHEN segment_index = 0 THEN ? ELSE remaining_ns END WHERE id = 1`,
			string(configJSON), store.FormatWallTime(wall), int64(next.Schedule[0].Duration.Value()))
		return err
	})
	return err
}
