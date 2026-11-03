package tournament

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

var (
	ErrIncompleteExplanationRequired = errors.New("incomplete ending requires an explanation of 1 to 200 characters")
	ErrPendingPrizeChoices           = errors.New("tied prize remainders must be assigned before finalization")
)

type PendingPrizeChoicesError struct {
	Groups []domain.PrizeGroup
}

func (e *PendingPrizeChoicesError) Error() string { return ErrPendingPrizeChoices.Error() }
func (e *PendingPrizeChoicesError) Unwrap() error { return ErrPendingPrizeChoices }

type EndResult struct {
	Completed bool
	PoolCents int64
	Standings Standing
	Prizes    []domain.PrizeGroup
}

// CurrentPrizes calculates provisional prizes without persisting them. Tie remainders remain pending
// until the operator chooses which tied players receive each spare €0.10.
func (s *Service) CurrentPrizes(ctx context.Context, extraChoices map[int][]string) (EndResult, error) {
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return EndResult{}, err
	}
	summary, err := s.CurrentSummary(ctx)
	if err != nil {
		return EndResult{}, err
	}
	standings, err := s.CurrentStandings(ctx)
	if err != nil {
		return EndResult{}, err
	}
	if summary.Entrants > int64(maxInt()) {
		return EndResult{}, fmt.Errorf("entrant count exceeds supported range")
	}
	prizes, err := domain.CalculatePrizes(int64(summary.PrizePoolCents), int(summary.Entrants), record.Config.PrizeTiers, standings.Groups, extraChoices)
	if err != nil {
		return EndResult{}, err
	}
	return EndResult{PoolCents: int64(summary.PrizePoolCents), Standings: standings, Prizes: prizes}, nil
}

// EndTournament stops play. Multiple active players produce an incomplete ending with no final
// standings; one remaining player can be finalized even while rebuys are open.
func (s *Service) EndTournament(ctx context.Context, explanation string, extraChoices map[int][]string, action ActionContext) (EndResult, error) {
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return EndResult{}, err
	}
	if record.StartedAt == nil || record.Status == store.StatusNotStarted {
		return EndResult{}, ErrTournamentNotStarted
	}
	if record.Status == store.StatusEnded {
		return EndResult{}, ErrTournamentEnded
	}
	if action.Checkpoint == nil {
		return EndResult{}, ErrClockContextRequired
	}
	summary, err := s.CurrentSummary(ctx)
	if err != nil {
		return EndResult{}, err
	}
	standings, err := s.CurrentStandings(ctx)
	if err != nil {
		return EndResult{}, err
	}
	completed := summary.Entrants == 1 || summary.ActivePlayers == 1
	if !completed {
		explanation = strings.TrimSpace(explanation)
		if explanation == "" || len([]rune(explanation)) > 200 {
			return EndResult{}, ErrIncompleteExplanationRequired
		}
	} else {
		explanation = strings.TrimSpace(explanation)
	}
	resultStandings := standings
	if !completed {
		resultStandings.Groups = nil
	}
	var prizes []domain.PrizeGroup
	if completed {
		if summary.Entrants > int64(maxInt()) {
			return EndResult{}, fmt.Errorf("entrant count exceeds supported range")
		}
		prizes, err = domain.CalculatePrizes(int64(summary.PrizePoolCents), int(summary.Entrants), record.Config.PrizeTiers, standings.Groups, extraChoices)
		if err != nil {
			return EndResult{}, err
		}
		pending := false
		for _, group := range prizes {
			pending = pending || group.PendingRemainderCents != 0
		}
		if pending {
			return EndResult{}, &PendingPrizeChoicesError{Groups: prizes}
		}
	}
	checkpoint := *action.Checkpoint
	checkpoint.Running = false
	checkpoint.RealElapsed = maxDuration(checkpoint.RealElapsed, realElapsedAt(record, action.WallTime))
	action.Checkpoint = &checkpoint
	payload := map[string]any{
		"completed":        completed,
		"prize_pool_cents": int64(summary.PrizePoolCents),
		"standings":        resultStandings.Groups,
		"prizes":           prizes,
	}
	event, persisted := action.event("tournament_ended", payload, explanation)
	_, err = s.store.CommitEvent(ctx, event, persisted, func(tx *sql.Tx, eventID int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		var status store.TournamentStatus
		var entrants, active int64
		if err := tx.QueryRowContext(ctx, `SELECT status,
			(SELECT COUNT(*) FROM purchases WHERE kind = 'initial_buy_in' AND voided_by_event_id IS NULL),
			(SELECT COUNT(*) FROM players WHERE active = 1 AND removed = 0) FROM tournament WHERE id = 1`).
			Scan(&status, &entrants, &active); err != nil {
			return err
		}
		if status == store.StatusEnded {
			return ErrTournamentEnded
		}
		if status == store.StatusNotStarted {
			return ErrTournamentNotStarted
		}
		if entrants != summary.Entrants || active != summary.ActivePlayers {
			return fmt.Errorf("tournament changed while ending; recalculate standings and retry")
		}
		wall := event.WallTime
		if wall.IsZero() {
			wall = time.Now()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tournament SET status = ?, ended_at = ?, clock_running = 0,
			registration_closed = 1, result_completed = ? WHERE id = 1`, store.StatusEnded, store.FormatWallTime(wall), nullableBool(completed)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tournament_ends (event_id, completed, prize_pool_cents, explanation)
			VALUES (?, ?, ?, ?)`, eventID, boolInt(completed), int64(summary.PrizePoolCents), nullableText(explanation)); err != nil {
			return err
		}
		if completed {
			for groupIndex, group := range prizes {
				groupOrder := groupIndex + 1
				if _, err := tx.ExecContext(ctx, `INSERT INTO end_prize_groups
					(end_event_id, group_order, first_place, last_place, total_cents, pending_remainder_cents)
					VALUES (?, ?, ?, ?, ?, ?)`, eventID, groupOrder, group.FirstPlace, group.LastPlace, group.TotalCents, group.PendingRemainderCents); err != nil {
					return err
				}
				for _, player := range group.Players {
					if _, err := tx.ExecContext(ctx, `INSERT INTO end_prize_players
						(end_event_id, group_order, player_id, amount_cents, gets_remainder)
						VALUES (?, ?, ?, ?, ?)`, eventID, groupOrder, player.PlayerID, player.AmountCents, boolInt(player.GetsRemainder)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return EndResult{}, err
	}
	return EndResult{Completed: completed, PoolCents: int64(summary.PrizePoolCents), Standings: resultStandings, Prizes: prizes}, nil
}

// ReopenTournament restores an ended tournament paused, preserving prior final results as history.
func (s *Service) ReopenTournament(ctx context.Context, explanation string, action ActionContext) error {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" || len([]rune(explanation)) > 200 {
		return fmt.Errorf("reopen explanation must contain 1 to 200 characters")
	}
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return err
	}
	if record.Status != store.StatusEnded {
		return fmt.Errorf("only an ended tournament can be reopened")
	}
	checkpoint := record.Checkpoint
	checkpoint.Running = false
	checkpoint.RealElapsed = maxDuration(checkpoint.RealElapsed, realElapsedAt(record, action.WallTime))
	action.Checkpoint = &checkpoint
	payload := map[string]any{
		"explanation":    explanation,
		"prior_end_time": record.EndedAt,
	}
	event, persisted := action.event("tournament_reopened", payload, explanation)
	_, err = s.store.CommitEvent(ctx, event, persisted, func(tx *sql.Tx, _ int64) error {
		var status store.TournamentStatus
		if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
			return err
		}
		if status != store.StatusEnded {
			return fmt.Errorf("only an ended tournament can be reopened")
		}
		registrationClosed := !registrationOpen(record.Config, checkpoint.SegmentIndex, checkpoint.SegmentFinished, true)
		_, err := tx.ExecContext(ctx, `UPDATE tournament SET status = ?, ended_at = NULL, clock_running = 0,
			result_completed = NULL, registration_closed = ? WHERE id = 1`, store.StatusPaused, boolInt(registrationClosed))
		return err
	})
	return err
}

func nullableBool(value bool) any {
	if value {
		return 1
	}
	return 0
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
