// Package tournament coordinates tournament actions across the domain and SQLite store.
package tournament

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/store"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrDuplicatePlayer        = errors.New("a player with this name already exists")
	ErrRestoreConfirmation    = errors.New("a removed player with this name exists; explicit restoration is required")
	ErrPlayerNotFound         = errors.New("player not found")
	ErrPlayerRemoved          = errors.New("player has been removed")
	ErrPlayerNotActive        = errors.New("player is not active")
	ErrPlayerHasHistory       = errors.New("player has valid dependent history and cannot be removed")
	ErrTournamentEnded        = errors.New("ended tournament is read-only")
	ErrClockContextRequired   = errors.New("started tournament action requires a clock checkpoint")
	ErrUnexpectedClockContext = errors.New("pre-start action must not include a running clock checkpoint")
	ErrNoStateChange          = errors.New("requested player state is already set")
)

type Player struct {
	ID              string
	Name            string
	Active          bool
	SittingOut      bool
	Removed         bool
	HasInitialBuyIn bool
}

type ActionContext struct {
	WallTime   time.Time
	Checkpoint *store.Checkpoint // Nil for records created before the tournament starts.
}

type Service struct {
	store *store.Store
}

func NewService(database *store.Store) *Service { return &Service{store: database} }

// NormalizePlayerName trims whitespace, applies Unicode NFC, then Unicode case folding.
func NormalizePlayerName(name string) (display, key string, err error) {
	display = norm.NFC.String(strings.TrimSpace(name))
	if display == "" {
		return "", "", fmt.Errorf("player name is required")
	}
	key = cases.Fold().String(display)
	return display, key, nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate player ID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func (a ActionContext) event(kind string, payload any, explanation string) (store.EventInput, *store.Checkpoint) {
	wall := a.WallTime
	if wall.IsZero() {
		wall = time.Now()
	}
	event := store.EventInput{Kind: kind, Payload: payload, Explanation: explanation, WallTime: wall}
	if a.Checkpoint == nil {
		return event, nil
	}
	checkpoint := *a.Checkpoint
	event.ClockElapsed = &checkpoint.ClockElapsed
	event.RealElapsed = &checkpoint.RealElapsed
	return event, &checkpoint
}

func (s *Service) ensureActionClock(ctx context.Context, tx *sql.Tx, action ActionContext) error {
	var started sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT started_at FROM tournament WHERE id = 1").Scan(&started); err != nil {
		return err
	}
	if started.Valid && action.Checkpoint == nil {
		return ErrClockContextRequired
	}
	if !started.Valid && action.Checkpoint != nil {
		return ErrUnexpectedClockContext
	}
	return nil
}

func ensureNotEnded(ctx context.Context, tx *sql.Tx) error {
	var status store.TournamentStatus
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
		return err
	}
	if status == store.StatusEnded {
		return ErrTournamentEnded
	}
	return nil
}

func insertPlayerEvent(ctx context.Context, tx *sql.Tx, eventID int64, playerID, kind string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO player_events (event_id, player_id, kind) VALUES (?, ?, ?)", eventID, playerID, kind)
	return err
}

// Players lists known players, including removed identities when includeRemoved is true.
func (s *Service) Players(ctx context.Context, includeRemoved bool) ([]Player, error) {
	players := make([]Player, 0)
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		query := `SELECT p.id, p.name, p.active, p.sitting_out, p.removed,
			EXISTS (SELECT 1 FROM purchases b WHERE b.player_id = p.id AND b.kind = 'initial_buy_in' AND b.voided_by_event_id IS NULL)
			FROM players p`
		if !includeRemoved {
			query += " WHERE p.removed = 0"
		}
		query += " ORDER BY p.name_key, p.id"
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var player Player
			var active, sittingOut, removed, entrant int
			if err := rows.Scan(&player.ID, &player.Name, &active, &sittingOut, &removed, &entrant); err != nil {
				return err
			}
			player.Active = active != 0
			player.SittingOut = sittingOut != 0
			player.Removed = removed != 0
			player.HasInitialBuyIn = entrant != 0
			players = append(players, player)
		}
		return rows.Err()
	})
	return players, err
}

// RegisterPlayer creates a player, or restores an existing removed identity when explicitly confirmed.
func (s *Service) RegisterPlayer(ctx context.Context, name string, restoreRemoved bool, action ActionContext) (Player, error) {
	display, key, err := NormalizePlayerName(name)
	if err != nil {
		return Player{}, err
	}
	var existing Player
	lookupErr := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		var active, sittingOut, removed int
		err := tx.QueryRowContext(ctx, `SELECT id, name, active, sitting_out, removed FROM players WHERE name_key = ?`, key).
			Scan(&existing.ID, &existing.Name, &active, &sittingOut, &removed)
		if err != nil {
			return err
		}
		existing.Active, existing.SittingOut, existing.Removed = active != 0, sittingOut != 0, removed != 0
		return nil
	})
	if lookupErr == nil {
		if !existing.Removed {
			return Player{}, ErrDuplicatePlayer
		}
		if !restoreRemoved {
			return existing, ErrRestoreConfirmation
		}
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return Player{}, lookupErr
	}
	if lookupErr == nil {
		event, checkpoint := action.event("player_restored", map[string]string{"player_id": existing.ID, "name": display}, "")
		_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
			if err := s.ensureActionClock(ctx, tx, action); err != nil {
				return err
			}
			if err := ensureNotEnded(ctx, tx); err != nil {
				return err
			}
			var removed int
			if err := tx.QueryRowContext(ctx, "SELECT removed FROM players WHERE id = ? AND name_key = ?", existing.ID, key).Scan(&removed); err != nil {
				return err
			}
			if removed == 0 {
				return ErrDuplicatePlayer
			}
			if _, err := tx.ExecContext(ctx, "UPDATE players SET name = ?, removed = 0, removed_event_id = NULL WHERE id = ?", display, existing.ID); err != nil {
				return err
			}
			return insertPlayerEvent(ctx, tx, eventID, existing.ID, "player_restored")
		})
		if err != nil {
			return Player{}, err
		}
		existing.Name, existing.Removed = display, false
		return existing, nil
	}

	id, err := newID()
	if err != nil {
		return Player{}, err
	}
	event, checkpoint := action.event("player_created", map[string]string{"player_id": id, "name": display}, "")
	_, err = s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO players (id, name, name_key, created_event_id)
			VALUES (?, ?, ?, ?)`, id, display, key, eventID)
		if err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, id, "player_created")
	})
	if err != nil {
		return Player{}, err
	}
	return Player{ID: id, Name: display}, nil
}

// RenamePlayer changes the display name without changing its stable identity.
func (s *Service) RenamePlayer(ctx context.Context, playerID, newName string, action ActionContext) error {
	display, key, err := NormalizePlayerName(newName)
	if err != nil {
		return err
	}
	event, checkpoint := action.event("player_renamed", map[string]string{"player_id": playerID, "name": display}, "")
	_, err = s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var removed int
		if err := tx.QueryRowContext(ctx, "SELECT removed FROM players WHERE id = ?", playerID).Scan(&removed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlayerNotFound
			}
			return err
		}
		if removed != 0 {
			return ErrPlayerRemoved
		}
		if _, err := tx.ExecContext(ctx, "UPDATE players SET name = ?, name_key = ? WHERE id = ?", display, key, playerID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, playerID, "player_renamed")
	})
	return err
}

// SetSittingOut changes only sitting-out status. Sit-outs remain active entrants and count toward antes.
func (s *Service) SetSittingOut(ctx context.Context, playerID string, sittingOut bool, action ActionContext) error {
	kind := "player_returned"
	if sittingOut {
		kind = "player_sitting_out"
	}
	event, checkpoint := action.event(kind, map[string]any{"player_id": playerID, "sitting_out": sittingOut}, "")
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var active, current, removed int
		if err := tx.QueryRowContext(ctx, "SELECT active, sitting_out, removed FROM players WHERE id = ?", playerID).Scan(&active, &current, &removed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlayerNotFound
			}
			return err
		}
		if removed != 0 {
			return ErrPlayerRemoved
		}
		if active == 0 {
			return ErrPlayerNotActive
		}
		if (current != 0) == sittingOut {
			return ErrNoStateChange
		}
		if _, err := tx.ExecContext(ctx, "UPDATE players SET sitting_out = ? WHERE id = ?", boolInt(sittingOut), playerID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, playerID, kind)
	})
	return err
}

// RemovePlayer preserves the player identity but refuses removal while valid history depends on it.
func (s *Service) RemovePlayer(ctx context.Context, playerID, explanation string, action ActionContext) error {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" || len([]rune(explanation)) > 200 {
		return fmt.Errorf("removal explanation must contain 1 to 200 characters")
	}
	event, checkpoint := action.event("player_removed", map[string]string{"player_id": playerID}, explanation)
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var removed int
		if err := tx.QueryRowContext(ctx, "SELECT removed FROM players WHERE id = ?", playerID).Scan(&removed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlayerNotFound
			}
			return err
		}
		if removed != 0 {
			return ErrPlayerRemoved
		}
		dependencies, err := playerDependencies(ctx, tx, playerID)
		if err != nil {
			return err
		}
		if len(dependencies) != 0 {
			return &DependentEventsError{PlayerID: playerID, Events: dependencies}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE players SET removed = 1, removed_event_id = ?, active = 0, sitting_out = 0 WHERE id = ?", eventID, playerID); err != nil {
			return err
		}
		return insertPlayerEvent(ctx, tx, eventID, playerID, "player_removed")
	})
	return err
}

type DependentEventsError struct {
	PlayerID string
	Events   []string
}

func (e *DependentEventsError) Error() string {
	return fmt.Sprintf("%v: %s depends on %s", ErrPlayerHasHistory, strings.Join(e.Events, ", "), e.PlayerID)
}

func (e *DependentEventsError) Unwrap() error { return ErrPlayerHasHistory }

func playerDependencies(ctx context.Context, tx *sql.Tx, playerID string) ([]string, error) {
	dependencies := make([]string, 0)
	queries := []struct {
		query string
		args  []any
	}{
		{
			query: `SELECT 'purchase #' || p.id FROM purchases p WHERE p.player_id = ? AND p.voided_by_event_id IS NULL`,
			args:  []any{playerID},
		},
		{
			query: `SELECT 'bust #' || b.event_id FROM bust_participants bp JOIN busts b ON b.event_id = bp.event_id
				WHERE bp.player_id = ? AND bp.voided_by_event_id IS NULL AND b.voided_by_event_id IS NULL`,
			args: []any{playerID},
		},
		{
			query: `SELECT 'bust correction #' || pc.correction_event_id FROM bust_participant_corrections pc
				JOIN busts b ON b.event_id = pc.bust_event_id
				WHERE pc.player_id = ? AND pc.voided_by_event_id IS NULL AND b.voided_by_event_id IS NULL`,
			args: []any{playerID},
		},
		{
			query: `SELECT pe.kind || ' event #' || pe.event_id FROM player_events pe
				WHERE pe.player_id = ? AND pe.kind NOT IN ('player_created', 'player_restored', 'player_removed')
				AND NOT EXISTS (SELECT 1 FROM audit_events v WHERE v.voids_event_id = pe.event_id)`,
			args: []any{playerID},
		},
	}
	for _, item := range queries {
		rows, err := tx.QueryContext(ctx, item.query, item.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var description string
			if err := rows.Scan(&description); err != nil {
				rows.Close()
				return nil, err
			}
			dependencies = append(dependencies, description)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return dependencies, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
