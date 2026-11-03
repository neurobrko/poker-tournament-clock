package tournament

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"git.mpx.sk/elvis/poker_clock/internal/domain"
)

var ErrNoFinalResult = errors.New("no completed tournament result is stored")

// LatestFinalResult reads the most recent completed result without recomputing tie choices.
func (s *Service) LatestFinalResult(ctx context.Context) (EndResult, error) {
	var result EndResult
	err := s.store.ViewTx(ctx, func(tx *sql.Tx) error {
		var eventID int64
		var completed int
		if err := tx.QueryRowContext(ctx, `SELECT event_id, completed, prize_pool_cents FROM tournament_ends
			WHERE completed = 1 ORDER BY event_id DESC LIMIT 1`).Scan(&eventID, &completed, &result.PoolCents); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoFinalResult
			}
			return err
		}
		if completed == 0 {
			return ErrNoFinalResult
		}
		result.Completed = true
		rows, err := tx.QueryContext(ctx, `SELECT group_order, first_place, last_place, total_cents, pending_remainder_cents
			FROM end_prize_groups WHERE end_event_id = ? ORDER BY group_order`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		groupsByOrder := make(map[int]int)
		for rows.Next() {
			var order int
			var group domain.PrizeGroup
			if err := rows.Scan(&order, &group.FirstPlace, &group.LastPlace, &group.TotalCents, &group.PendingRemainderCents); err != nil {
				return err
			}
			groupsByOrder[order] = len(result.Prizes)
			result.Prizes = append(result.Prizes, group)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT group_order, player_id, amount_cents, gets_remainder
			FROM end_prize_players WHERE end_event_id = ? ORDER BY group_order, player_id`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var order, gets int
			var player domain.PlayerPrize
			if err := rows.Scan(&order, &player.PlayerID, &player.AmountCents, &gets); err != nil {
				return err
			}
			index, ok := groupsByOrder[order]
			if !ok {
				return fmt.Errorf("final prize player references missing group %d", order)
			}
			player.GetsRemainder = gets != 0
			result.Prizes[index].Players = append(result.Prizes[index].Players, player)
		}
		return rows.Err()
	})
	if err != nil {
		return EndResult{}, err
	}
	result.Standings, err = s.CurrentStandings(ctx)
	return result, err
}
