package tournament

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type PlayerImportAction string

const (
	PlayerImportAdd         PlayerImportAction = "add"
	PlayerImportRestore     PlayerImportAction = "restore"
	PlayerImportUpdateAlias PlayerImportAction = "update_alias"
)

type PlayerImportChange struct {
	Action   PlayerImportAction
	PlayerID string
	Name     string
	Alias    string
}

// ApplyPlayerImport atomically applies roster changes and optional tournament settings.
func (s *Service) ApplyPlayerImport(ctx context.Context, next *config.Template, changes []PlayerImportChange, action ActionContext) error {
	if next == nil && len(changes) == 0 {
		return nil
	}
	var configJSON []byte
	if next != nil {
		if err := next.Validate(); err != nil {
			return fmt.Errorf("invalid tournament configuration: %w", err)
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("encode tournament configuration: %w", err)
		}
		configJSON = encoded
	}
	seen := make(map[string]bool, len(changes))
	for index := range changes {
		change := &changes[index]
		display, key, err := NormalizePlayerName(change.Name)
		if err != nil {
			return fmt.Errorf("player import item %d: %w", index+1, err)
		}
		change.Name = display
		if seen[key] {
			return fmt.Errorf("player import contains duplicate name %q", display)
		}
		seen[key] = true
		change.Alias = strings.TrimSpace(change.Alias)
		if strings.ContainsRune(change.Alias, '\x00') || len([]rune(change.Alias)) > store.MaxNameLength {
			return fmt.Errorf("player import item %d alias cannot exceed %d characters or contain NUL", index+1, store.MaxNameLength)
		}
		switch change.Action {
		case PlayerImportAdd:
			if change.PlayerID != "" {
				return fmt.Errorf("player import item %d add must not specify an existing player", index+1)
			}
		case PlayerImportRestore, PlayerImportUpdateAlias:
			if change.PlayerID == "" {
				return fmt.Errorf("player import item %d requires an existing player", index+1)
			}
		default:
			return fmt.Errorf("player import item %d has unknown action %q", index+1, change.Action)
		}
	}

	payload := map[string]any{"players": changes}
	if configJSON != nil {
		payload["configuration"] = json.RawMessage(configJSON)
	}
	event, checkpoint := action.event("player_roster_imported", payload, "")
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, importEventID int64) error {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
			return err
		}
		if status == string(store.StatusEnded) {
			return ErrTournamentEnded
		}
		if status != string(store.StatusNotStarted) {
			return store.ErrConfigurationLocked
		}
		if next == nil {
			if err := s.ensureActionClock(ctx, tx, action); err != nil {
				return err
			}
		}
		wall := event.WallTime
		if wall.IsZero() {
			wall = time.Now()
		}
		if next != nil {
			if err := s.updateConfigurationTx(ctx, tx, *next, string(configJSON), wall, action); err != nil {
				return err
			}
		}
		for _, change := range changes {
			if err := applyPlayerImportChange(ctx, tx, importEventID, wall, change); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func applyPlayerImportChange(ctx context.Context, tx *sql.Tx, parentEventID int64, wall time.Time, change PlayerImportChange) error {
	nameKey := norm.NFC.String(cases.Fold().String(change.Name))
	switch change.Action {
	case PlayerImportAdd:
		var existing string
		err := tx.QueryRowContext(ctx, "SELECT id FROM players WHERE name_key = ?", nameKey).Scan(&existing)
		if err == nil {
			return ErrDuplicatePlayer
		}
		if err != sql.ErrNoRows {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		eventID, err := insertRosterPlayerEvent(ctx, tx, parentEventID, wall, "player_created", map[string]string{"player_id": id, "name": change.Name, "alias": change.Alias})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO players (id, name, name_key, alias, created_event_id)
			VALUES (?, ?, ?, ?, ?)`, id, change.Name, nameKey, change.Alias, eventID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, id, "player_created")
	case PlayerImportRestore:
		var removed int
		var existingKey string
		if err := tx.QueryRowContext(ctx, "SELECT removed, name_key FROM players WHERE id = ?", change.PlayerID).Scan(&removed, &existingKey); err != nil {
			if err == sql.ErrNoRows {
				return ErrPlayerNotFound
			}
			return err
		}
		if existingKey != nameKey {
			return fmt.Errorf("player %q changed during import", change.Name)
		}
		if removed == 0 {
			return ErrDuplicatePlayer
		}
		eventID, err := insertRosterPlayerEvent(ctx, tx, parentEventID, wall, "player_restored", map[string]string{"player_id": change.PlayerID, "name": change.Name, "alias": change.Alias})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE players SET name = ?, alias = ?, removed = 0, removed_event_id = NULL
			WHERE id = ?`, change.Name, change.Alias, change.PlayerID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, change.PlayerID, "player_restored")
	case PlayerImportUpdateAlias:
		var removed int
		var existingKey, existingAlias string
		if err := tx.QueryRowContext(ctx, "SELECT removed, name_key, alias FROM players WHERE id = ?", change.PlayerID).Scan(&removed, &existingKey, &existingAlias); err != nil {
			if err == sql.ErrNoRows {
				return ErrPlayerNotFound
			}
			return err
		}
		if existingKey != nameKey {
			return fmt.Errorf("player %q changed during import", change.Name)
		}
		if removed != 0 {
			return ErrPlayerRemoved
		}
		if existingAlias == change.Alias {
			return nil
		}
		eventID, err := insertRosterPlayerEvent(ctx, tx, parentEventID, wall, "player_alias_changed", map[string]string{"player_id": change.PlayerID, "alias": change.Alias})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE players SET alias = ? WHERE id = ?", change.Alias, change.PlayerID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, change.PlayerID, "player_alias_changed")
	default:
		return fmt.Errorf("unknown player import action %q", change.Action)
	}
}

func insertRosterPlayerEvent(ctx context.Context, tx *sql.Tx, parentEventID int64, wall time.Time, kind string, payload any) (int64, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode %s event: %w", kind, err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO audit_events (kind, wall_timestamp, payload_json, parent_event_id)
		VALUES (?, ?, ?, ?)`, kind, store.FormatWallTime(wall), string(encoded), parentEventID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
