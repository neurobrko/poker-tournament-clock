package tournament

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"git.mpx.sk/elvis/poker_clock/internal/domain"
)

var (
	ErrInvalidBust         = errors.New("invalid bust event")
	ErrBustNotFound        = errors.New("bust event not found")
	ErrBustAlreadyVoided   = errors.New("bust event is already voided")
	ErrParticipantNotFound = errors.New("bust participant not found")
)

type Bustee struct {
	PlayerID string
	Position int // Equal positions indicate a tie.
	Tied     bool
}

type BustRequest struct {
	Busters []string
	Bustees []Bustee
}

type BustParticipant struct {
	PlayerID string `json:"player_id"`
	Role     string `json:"role"`
	Position int    `json:"position"`
	Tied     bool   `json:"tied"`
}

// UnmarshalJSON accepts the original Go-field names as well as the stable snake_case audit keys.
func (p *BustParticipant) UnmarshalJSON(data []byte) error {
	var value struct {
		PlayerID       string `json:"player_id"`
		LegacyPlayerID string `json:"PlayerID"`
		Role           string `json:"role"`
		Position       int    `json:"position"`
		Tied           bool   `json:"tied"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.PlayerID = value.PlayerID
	if p.PlayerID == "" {
		p.PlayerID = value.LegacyPlayerID
	}
	p.Role, p.Position, p.Tied = value.Role, value.Position, value.Tied
	return nil
}

type Standing struct {
	Entrants int64
	Groups   []domain.FinishGroup
}

// RecordBust records all participants and ordering in one atomic action.
func (s *Service) RecordBust(ctx context.Context, request BustRequest, action ActionContext) (int64, error) {
	if err := validateBustRequest(request); err != nil {
		return 0, err
	}
	payload := map[string]any{"busters": request.Busters, "bustees": request.Bustees}
	event, checkpoint := action.event("bust_recorded", payload, "")
	var eventID int64
	eventID, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, id int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var started sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT started_at FROM tournament WHERE id = 1").Scan(&started); err != nil {
			return err
		}
		if !started.Valid {
			return ErrTournamentNotStarted
		}
		if err := validateCurrentBustParticipants(ctx, tx, request); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO busts (event_id) VALUES (?)", id); err != nil {
			return err
		}
		for index, playerID := range request.Busters {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bust_participants (event_id, player_id, role, finishing_order, tied)
				VALUES (?, ?, 'buster', ?, 0)`, id, playerID, index+1); err != nil {
				return err
			}
		}
		for _, bustee := range request.Bustees {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bust_participants (event_id, player_id, role, finishing_order, tied)
				VALUES (?, ?, 'bustee', ?, ?)`, id, bustee.PlayerID, bustee.Position, boolInt(bustee.Tied)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE players SET active = 0, sitting_out = 0 WHERE id = ?", bustee.PlayerID); err != nil {
				return err
			}
		}
		if err := reconcileClockAfterStateChange(ctx, tx, checkpoint, id, payload); err != nil {
			return err
		}
		return nil
	})
	return eventID, err
}

func validateBustRequest(request BustRequest) error {
	if len(request.Busters) == 0 || len(request.Bustees) == 0 {
		return fmt.Errorf("%w: at least one buster and one bustee are required", ErrInvalidBust)
	}
	bustees := make(map[string]struct{}, len(request.Bustees))
	all := make(map[string]struct{}, len(request.Busters)+len(request.Bustees))
	for _, buster := range request.Busters {
		if buster == "" {
			return fmt.Errorf("%w: player ID cannot be empty", ErrInvalidBust)
		}
		if _, exists := all[buster]; exists {
			return fmt.Errorf("%w: player %q appears more than once", ErrInvalidBust, buster)
		}
		all[buster] = struct{}{}
	}
	positions := make(map[int][]Bustee)
	maxPosition := 0
	for _, bustee := range request.Bustees {
		if bustee.PlayerID == "" || bustee.Position < 1 {
			return fmt.Errorf("%w: bustee requires a player ID and positive position", ErrInvalidBust)
		}
		if _, exists := all[bustee.PlayerID]; exists {
			return fmt.Errorf("%w: player %q appears more than once or both busts and survives", ErrInvalidBust, bustee.PlayerID)
		}
		all[bustee.PlayerID] = struct{}{}
		bustees[bustee.PlayerID] = struct{}{}
		positions[bustee.Position] = append(positions[bustee.Position], bustee)
		if bustee.Position > maxPosition {
			maxPosition = bustee.Position
		}
	}
	for position := 1; position <= maxPosition; position++ {
		group := positions[position]
		if len(group) == 0 {
			return fmt.Errorf("%w: bustee positions must be consecutive", ErrInvalidBust)
		}
		if len(group) > 1 {
			for _, bustee := range group {
				if !bustee.Tied {
					return fmt.Errorf("%w: all players at tied position %d must be marked tied", ErrInvalidBust, position)
				}
			}
		} else if group[0].Tied {
			return fmt.Errorf("%w: position %d is marked tied but has no co-finisher", ErrInvalidBust, position)
		}
	}
	validBuster := false
	for _, buster := range request.Busters {
		if _, busting := bustees[buster]; !busting {
			validBuster = true
		}
	}
	if !validBuster {
		return fmt.Errorf("%w: at least one buster must not be among the bustees", ErrInvalidBust)
	}
	return nil
}

func validateCurrentBustParticipants(ctx context.Context, tx *sql.Tx, request BustRequest) error {
	for _, playerID := range append(append([]string(nil), request.Busters...), busteeIDs(request.Bustees)...) {
		var active, removed int
		if err := tx.QueryRowContext(ctx, "SELECT active, removed FROM players WHERE id = ?", playerID).Scan(&active, &removed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: player %s does not exist", ErrInvalidBust, playerID)
			}
			return err
		}
		if removed != 0 || active == 0 {
			return fmt.Errorf("%w: player %s is not an active tournament player", ErrInvalidBust, playerID)
		}
	}
	return nil
}

func busteeIDs(bustees []Bustee) []string {
	ids := make([]string, len(bustees))
	for index, bustee := range bustees {
		ids[index] = bustee.PlayerID
	}
	return ids
}

// CorrectBustParticipant changes one participant without rewriting the original bust event.
// A nil replacement removes only that participant; event validity and later dependencies are rechecked.
func (s *Service) CorrectBustParticipant(ctx context.Context, bustEventID int64, originalPlayerID, originalRole string, replacement *BustParticipant, explanation string, action ActionContext) error {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" || len([]rune(explanation)) > 200 {
		return fmt.Errorf("correction explanation must contain 1 to 200 characters")
	}
	if originalPlayerID == "" || (originalRole != "buster" && originalRole != "bustee") {
		return ErrParticipantNotFound
	}
	var replacementPayload any
	if replacement != nil {
		replacementPayload = replacement
	}
	payload := map[string]any{
		"bust_event_id": bustEventID,
		"supersedes":    BustParticipant{PlayerID: originalPlayerID, Role: originalRole},
		"replacement":   replacementPayload,
	}
	event, checkpoint := action.event("bust_participant_corrected", payload, explanation)
	event.ParentEventID = &bustEventID
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, correctionEventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var voided sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT voided_by_event_id FROM busts WHERE event_id = ?", bustEventID).Scan(&voided); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrBustNotFound
			}
			return err
		}
		if voided.Valid {
			return ErrBustAlreadyVoided
		}
		participants, err := effectiveBustParticipants(ctx, tx, bustEventID)
		if err != nil {
			return err
		}
		updated, affected, err := substituteParticipant(participants, originalPlayerID, originalRole, replacement)
		if err != nil {
			return err
		}
		if err := validateEffectiveParticipants(updated); err != nil {
			return err
		}
		if err := validateHistoricalParticipants(ctx, tx, bustEventID, updated); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE bust_participants SET voided_by_event_id = ?
			WHERE event_id = ? AND player_id = ? AND role = ? AND voided_by_event_id IS NULL`, correctionEventID, bustEventID, originalPlayerID, originalRole)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			result, err = tx.ExecContext(ctx, `UPDATE bust_participant_corrections SET voided_by_event_id = ?
				WHERE bust_event_id = ? AND player_id = ? AND role = ? AND voided_by_event_id IS NULL`, correctionEventID, bustEventID, originalPlayerID, originalRole)
			if err != nil {
				return err
			}
			changed, err = result.RowsAffected()
			if err != nil {
				return err
			}
		}
		if changed == 0 {
			return ErrParticipantNotFound
		}
		var playerID, role any
		var position, tied any
		if replacement != nil {
			playerID, role = replacement.PlayerID, replacement.Role
			position, tied = replacement.Position, boolInt(replacement.Tied)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bust_participant_corrections
			(bust_event_id, correction_event_id, supersedes_player_id, supersedes_role, player_id, role, finishing_order, tied)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, bustEventID, correctionEventID, originalPlayerID, originalRole, playerID, role, position, tied); err != nil {
			return err
		}
		for _, player := range affected {
			active, sittingOut, dependencies, err := replayPlayerState(ctx, tx, player, 0)
			if err != nil {
				return err
			}
			if len(dependencies) != 0 {
				return &DependentEventsError{PlayerID: player, Events: dependencies}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE players SET active = ?, sitting_out = ? WHERE id = ?", boolInt(active), boolInt(sittingOut), player); err != nil {
				return err
			}
		}
		return reconcileClockAfterStateChange(ctx, tx, checkpoint, correctionEventID, payload)
	})
	return err
}

func substituteParticipant(current []BustParticipant, originalPlayerID, originalRole string, replacement *BustParticipant) ([]BustParticipant, []string, error) {
	updated := make([]BustParticipant, 0, len(current))
	found := false
	affected := []string{originalPlayerID}
	for _, participant := range current {
		if participant.PlayerID == originalPlayerID && participant.Role == originalRole {
			if found {
				return nil, nil, fmt.Errorf("%w: participant is ambiguous", ErrInvalidBust)
			}
			found = true
			if replacement != nil {
				updated = append(updated, *replacement)
				if replacement.PlayerID != originalPlayerID {
					affected = append(affected, replacement.PlayerID)
				}
			}
			continue
		}
		updated = append(updated, participant)
	}
	if !found {
		return nil, nil, ErrParticipantNotFound
	}
	return updated, affected, nil
}

func validateEffectiveParticipants(participants []BustParticipant) error {
	busters, bustees := 0, 0
	request := BustRequest{}
	for _, participant := range participants {
		switch participant.Role {
		case "buster":
			busters++
			request.Busters = append(request.Busters, participant.PlayerID)
		case "bustee":
			bustees++
			request.Bustees = append(request.Bustees, Bustee{PlayerID: participant.PlayerID, Position: participant.Position, Tied: participant.Tied})
		default:
			return fmt.Errorf("%w: unknown role %q", ErrInvalidBust, participant.Role)
		}
	}
	if busters == 0 || bustees == 0 {
		return fmt.Errorf("%w: correction must leave at least one buster and one bustee", ErrInvalidBust)
	}
	return validateBustRequest(request)
}

// EffectiveBustParticipants returns the current participants after all corrections.
func (s *Service) EffectiveBustParticipants(ctx context.Context, bustEventID int64) ([]BustParticipant, error) {
	var participants []BustParticipant
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		var err error
		participants, err = effectiveBustParticipants(ctx, tx, bustEventID)
		return err
	})
	return participants, err
}

const effectiveBustParticipantsQuery = `
	SELECT bp.event_id, bp.player_id, bp.role, bp.finishing_order, bp.tied
	FROM bust_participants bp JOIN busts b ON b.event_id = bp.event_id
	WHERE bp.voided_by_event_id IS NULL AND b.voided_by_event_id IS NULL
	UNION ALL
	SELECT pc.bust_event_id, pc.player_id, pc.role, pc.finishing_order, pc.tied
	FROM bust_participant_corrections pc JOIN busts b ON b.event_id = pc.bust_event_id
	WHERE pc.voided_by_event_id IS NULL AND pc.player_id IS NOT NULL AND b.voided_by_event_id IS NULL`

func effectiveBustParticipants(ctx context.Context, tx *sql.Tx, bustEventID int64) ([]BustParticipant, error) {
	rows, err := tx.QueryContext(ctx, `SELECT player_id, role, finishing_order, tied FROM (`+effectiveBustParticipantsQuery+`)
		WHERE event_id = ? ORDER BY role, finishing_order, player_id`, bustEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var participants []BustParticipant
	for rows.Next() {
		var participant BustParticipant
		var tied int
		if err := rows.Scan(&participant.PlayerID, &participant.Role, &participant.Position, &tied); err != nil {
			return nil, err
		}
		participant.Tied = tied != 0
		participants = append(participants, participant)
	}
	return participants, rows.Err()
}

func validateHistoricalParticipants(ctx context.Context, tx *sql.Tx, bustEventID int64, participants []BustParticipant) error {
	for _, participant := range participants {
		active, err := replayPlayerStateBefore(ctx, tx, participant.PlayerID, bustEventID)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: %s was not active when bust event %d occurred", ErrInvalidBust, participant.PlayerID, bustEventID)
		}
	}
	return nil
}

// VoidBust cancels a whole event only when later actions remain valid without it.
func (s *Service) VoidBust(ctx context.Context, bustEventID int64, explanation string, action ActionContext) error {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" || len([]rune(explanation)) > 200 {
		return fmt.Errorf("void explanation must contain 1 to 200 characters")
	}
	payload := map[string]any{"bust_event_id": bustEventID}
	event, checkpoint := action.event("bust_voided", payload, explanation)
	event.ParentEventID = &bustEventID
	event.VoidEventID = &bustEventID
	_, err := s.store.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, voidEventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var existing sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT voided_by_event_id FROM busts WHERE event_id = ?", bustEventID).Scan(&existing); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrBustNotFound
			}
			return err
		}
		if existing.Valid {
			return ErrBustAlreadyVoided
		}
		participants, err := effectiveBustParticipants(ctx, tx, bustEventID)
		if err != nil {
			return err
		}
		affected := uniqueParticipantIDs(participants)
		if _, err := tx.ExecContext(ctx, "UPDATE busts SET voided_by_event_id = ? WHERE event_id = ?", voidEventID, bustEventID); err != nil {
			return err
		}
		for _, player := range affected {
			active, sittingOut, dependencies, err := replayPlayerState(ctx, tx, player, 0)
			if err != nil {
				return err
			}
			if len(dependencies) != 0 {
				return &DependentEventsError{PlayerID: player, Events: dependencies}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE players SET active = ?, sitting_out = ? WHERE id = ?", boolInt(active), boolInt(sittingOut), player); err != nil {
				return err
			}
		}
		return reconcileClockAfterStateChange(ctx, tx, checkpoint, voidEventID, payload)
	})
	return err
}

func uniqueParticipantIDs(participants []BustParticipant) []string {
	seen := make(map[string]struct{}, len(participants))
	ids := make([]string, 0, len(participants))
	for _, participant := range participants {
		if _, exists := seen[participant.PlayerID]; exists {
			continue
		}
		seen[participant.PlayerID] = struct{}{}
		ids = append(ids, participant.PlayerID)
	}
	return ids
}

// CurrentStandings reconstructs provisional order: active entrants first, then final busts in reverse event order.
func (s *Service) CurrentStandings(ctx context.Context) (Standing, error) {
	var standings Standing
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchases WHERE kind = 'initial_buy_in'
			AND voided_by_event_id IS NULL`).Scan(&standings.Entrants); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM players WHERE active = 1 AND removed = 0 ORDER BY name_key, id")
		if err != nil {
			return err
		}
		active := make([]string, 0)
		activeSet := make(map[string]struct{})
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			active = append(active, id)
			activeSet[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(active) > 0 {
			standings.Groups = append(standings.Groups, domain.FinishGroup{PlayerIDs: active})
		}
		type busteeRecord struct {
			eventID  int64
			playerID string
			position int
			tied     bool
		}
		rows, err = tx.QueryContext(ctx, `SELECT event_id, player_id, finishing_order, tied FROM (`+effectiveBustParticipantsQuery+`)
			WHERE role = 'bustee' ORDER BY event_id DESC, finishing_order ASC, player_id ASC`)
		if err != nil {
			return err
		}
		records := make([]busteeRecord, 0)
		for rows.Next() {
			var record busteeRecord
			var tied int
			if err := rows.Scan(&record.eventID, &record.playerID, &record.position, &tied); err != nil {
				rows.Close()
				return err
			}
			record.tied = tied != 0
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		seen := make(map[string]struct{}, standings.Entrants)
		for id := range activeSet {
			seen[id] = struct{}{}
		}
		for index := 0; index < len(records); {
			record := records[index]
			end := index + 1
			for end < len(records) && records[end].eventID == record.eventID && records[end].position == record.position {
				end++
			}
			group := make([]string, 0, end-index)
			for _, candidate := range records[index:end] {
				if _, alreadyPlaced := seen[candidate.playerID]; alreadyPlaced {
					continue
				}
				var activeNow int
				if err := tx.QueryRowContext(ctx, "SELECT active FROM players WHERE id = ? AND removed = 0", candidate.playerID).Scan(&activeNow); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					return err
				}
				if activeNow != 0 {
					continue
				}
				group = append(group, candidate.playerID)
				seen[candidate.playerID] = struct{}{}
			}
			if len(group) > 0 {
				standings.Groups = append(standings.Groups, domain.FinishGroup{PlayerIDs: group})
			}
			index = end
		}
		if int64(len(seen)) != standings.Entrants {
			return fmt.Errorf("standings cover %d of %d valid entrants", len(seen), standings.Entrants)
		}
		return nil
	})
	return standings, err
}

func replayPlayerStateBefore(ctx context.Context, tx *sql.Tx, playerID string, beforeEventID int64) (bool, error) {
	active, _, dependencies, err := replayPlayerStateThrough(ctx, tx, playerID, 0, beforeEventID)
	if err != nil {
		return false, err
	}
	if len(dependencies) != 0 {
		return false, &DependentEventsError{PlayerID: playerID, Events: dependencies}
	}
	return active, nil
}
