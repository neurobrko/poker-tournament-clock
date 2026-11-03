package tournament

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

var (
	ErrInsufficientActivePlayers = errors.New("at least two active players are required to start")
	ErrInsufficientEntrants      = errors.New("at least two valid entrants are required to continue")
	ErrTournamentAlreadyStarted  = errors.New("tournament has already started")
)

// StartTournament locks configuration and begins the opening level. UI must obtain the
// operator's first-start confirmation before calling this method.
func (s *Service) StartTournament(ctx context.Context, at time.Time) error {
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return err
	}
	if record.Status != store.StatusNotStarted || record.StartedAt != nil {
		return ErrTournamentAlreadyStarted
	}
	if at.IsZero() {
		at = time.Now()
	}
	availability, summary, err := s.currentAvailability(ctx)
	if err != nil {
		return err
	}
	if summary.ActivePlayers < 2 {
		return ErrInsufficientActivePlayers
	}
	clock, err := domain.NewClock(record.Config.Schedule)
	if err != nil {
		return err
	}
	transitions, err := clock.Start(availability)
	if err != nil {
		return err
	}
	checkpoint := checkpointFromClock(clock, 0)
	payload := map[string]any{"transitions": transitions}
	event := store.EventInput{
		Kind: "tournament_started", Payload: payload, WallTime: at,
		ClockElapsed: durationPointer(checkpoint.ClockElapsed), RealElapsed: durationPointer(checkpoint.RealElapsed),
	}
	_, err = s.store.CommitEvent(ctx, event, &checkpoint, func(tx *sql.Tx, _ int64) error {
		var status store.TournamentStatus
		var started sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT status, started_at FROM tournament WHERE id = 1").Scan(&status, &started); err != nil {
			return err
		}
		if status != store.StatusNotStarted || started.Valid {
			return ErrTournamentAlreadyStarted
		}
		total, active, err := tournamentTotalsTx(ctx, tx)
		if err != nil {
			return err
		}
		if active < 2 {
			return ErrInsufficientActivePlayers
		}
		available, _, reason, err := domain.LevelAvailability(record.Config.Schedule[0], total, active)
		if err != nil {
			return err
		}
		if !available {
			return fmt.Errorf("%w: opening level %s", domain.ErrUnavailableLevel, reason)
		}
		_, err = tx.ExecContext(ctx, `UPDATE tournament SET status = ?, started_at = ?, registration_closed = 0
			WHERE id = 1`, store.StatusRunning, store.FormatWallTime(at))
		return err
	})
	return err
}

// PauseTournament persists an operator pause and its elapsed-time checkpoint atomically.
func (s *Service) PauseTournament(ctx context.Context, action ActionContext) error {
	if action.Checkpoint == nil {
		return ErrClockContextRequired
	}
	checkpoint := *action.Checkpoint
	if !checkpoint.Running {
		return domain.ErrClockNotRunning
	}
	checkpoint.Running = false
	action.Checkpoint = &checkpoint
	event, persisted := action.event("clock_paused", map[string]string{"reason": "operator"}, "")
	_, err := s.store.CommitEvent(ctx, event, persisted, func(tx *sql.Tx, _ int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		var status store.TournamentStatus
		if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
			return err
		}
		if status != store.StatusRunning {
			return domain.ErrClockNotRunning
		}
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = 0 WHERE id = 1", store.StatusPaused)
		return err
	})
	return err
}

// ResumeTournament validates entrant and blind availability before resuming. A blocked-next-level
// hold is released here if its level became playable while paused.
func (s *Service) ResumeTournament(ctx context.Context, action ActionContext) ([]domain.Transition, error) {
	if action.Checkpoint == nil {
		return nil, ErrClockContextRequired
	}
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return nil, err
	}
	if record.Status != store.StatusPaused {
		return nil, domain.ErrClockNotRunning
	}
	availability, summary, err := s.currentAvailability(ctx)
	if err != nil {
		return nil, err
	}
	if summary.Entrants < 2 {
		return nil, ErrInsufficientEntrants
	}
	clock, err := clockFromCheckpoint(record.Config.Schedule, *action.Checkpoint)
	if err != nil {
		return nil, err
	}
	transitions, err := clock.Resume(availability)
	if err != nil {
		return nil, err
	}
	realElapsed := realElapsedAt(record, action.WallTime)
	checkpoint := checkpointFromClock(clock, realElapsed)
	action.Checkpoint = &checkpoint
	payload := map[string]any{"transitions": transitions}
	event, persisted := action.event("clock_resumed", payload, "")
	_, err = s.store.CommitEvent(ctx, event, persisted, func(tx *sql.Tx, _ int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		var status store.TournamentStatus
		var entrants int64
		if err := tx.QueryRowContext(ctx, `SELECT status, (SELECT COUNT(*) FROM purchases
			WHERE kind = 'initial_buy_in' AND voided_by_event_id IS NULL) FROM tournament WHERE id = 1`).Scan(&status, &entrants); err != nil {
			return err
		}
		if status != store.StatusPaused {
			return fmt.Errorf("clock is not paused")
		}
		if entrants < 2 {
			return ErrInsufficientEntrants
		}
		registrationClosed := !registrationOpen(record.Config, checkpoint.SegmentIndex, checkpoint.SegmentFinished, true)
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = 1, registration_closed = ? WHERE id = 1", store.StatusRunning, boolInt(registrationClosed))
		return err
	})
	if err != nil {
		return nil, err
	}
	return transitions, nil
}

// AdvanceClock applies one deterministic timer delta and checkpoints every call.
// It emits an audit event when automatic progression or an indefinite hold begins.
func (s *Service) AdvanceClock(ctx context.Context, delta time.Duration, at time.Time) ([]domain.Transition, error) {
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return nil, err
	}
	if record.Status != store.StatusRunning || !record.Checkpoint.Running {
		return nil, nil
	}
	clock, err := clockFromCheckpoint(record.Config.Schedule, record.Checkpoint)
	if err != nil {
		return nil, err
	}
	availability, _, err := s.currentAvailability(ctx)
	if err != nil {
		return nil, err
	}
	transitions, err := clock.Advance(delta, availability)
	if err != nil {
		return nil, err
	}
	realElapsed := realElapsedAt(record, at)
	checkpoint := checkpointFromClock(clock, realElapsed)
	if at.IsZero() {
		at = time.Now()
	}
	if len(transitions) == 0 {
		if err := s.store.SaveCheckpoint(ctx, checkpoint, at); err != nil {
			return nil, err
		}
		return nil, nil
	}
	payload := map[string]any{"transitions": transitions}
	event := store.EventInput{
		Kind: "clock_progressed", Payload: payload, WallTime: at,
		ClockElapsed: durationPointer(checkpoint.ClockElapsed), RealElapsed: durationPointer(checkpoint.RealElapsed),
	}
	_, err = s.store.CommitEvent(ctx, event, &checkpoint, func(tx *sql.Tx, _ int64) error {
		var status store.TournamentStatus
		if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
			return err
		}
		if status != store.StatusRunning {
			return fmt.Errorf("clock stopped while progression was being saved")
		}
		if !checkpoint.Running {
			_, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = 0, registration_closed = ? WHERE id = 1", store.StatusPaused, boolInt(!registrationOpen(record.Config, checkpoint.SegmentIndex, checkpoint.SegmentFinished, true)))
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET registration_closed = ? WHERE id = 1", boolInt(!registrationOpen(record.Config, checkpoint.SegmentIndex, checkpoint.SegmentFinished, true)))
		return err
	})
	if err != nil {
		return nil, err
	}
	return transitions, nil
}

// PauseAfterSleep discards the unsaved interval, restores the last durable clock checkpoint,
// and records why the operator must resume manually.
func (s *Service) PauseAfterSleep(ctx context.Context, at time.Time) error {
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return err
	}
	if record.Status != store.StatusRunning {
		return nil
	}
	checkpoint := record.Checkpoint
	checkpoint.Running = false
	checkpoint.RealElapsed = maxDuration(checkpoint.RealElapsed, realElapsedAt(record, at))
	if at.IsZero() {
		at = time.Now()
	}
	event := store.EventInput{
		Kind: "clock_paused_after_sleep", Payload: map[string]string{"reason": "computer_sleep_detected"}, WallTime: at,
		ClockElapsed: durationPointer(checkpoint.ClockElapsed), RealElapsed: durationPointer(checkpoint.RealElapsed),
	}
	_, err = s.store.CommitEvent(ctx, event, &checkpoint, func(tx *sql.Tx, _ int64) error {
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = 0 WHERE id = 1 AND status = ?", store.StatusPaused, store.StatusRunning)
		return err
	})
	return err
}

// SelectSegment performs an auditable manual level/break jump without changing elapsed tournament time.
func (s *Service) SelectSegment(ctx context.Context, index int, remaining *time.Duration, action ActionContext) ([]domain.Transition, error) {
	if action.Checkpoint == nil {
		return nil, ErrClockContextRequired
	}
	record, err := s.store.Tournament(ctx)
	if err != nil {
		return nil, err
	}
	if record.StartedAt == nil || record.Status == store.StatusEnded {
		return nil, ErrTournamentNotStarted
	}
	availability, _, err := s.currentAvailability(ctx)
	if err != nil {
		return nil, err
	}
	clock, err := clockFromCheckpoint(record.Config.Schedule, *action.Checkpoint)
	if err != nil {
		return nil, err
	}
	recovery := false
	if index >= 1 && index <= len(record.Config.Schedule) && record.Config.Schedule[index-1].Kind == config.SegmentLevel {
		available, _, reason, err := domain.LevelAvailability(record.Config.Schedule[index-1], availability.TotalChips, availability.ActivePlayers)
		if err != nil {
			return nil, err
		}
		if !available {
			if record.Status != store.StatusPaused || registrationOpen(record.Config, action.Checkpoint.SegmentIndex, action.Checkpoint.SegmentFinished, true) {
				return nil, fmt.Errorf("%w: level %d %s", domain.ErrUnavailableLevel, index, reason)
			}
			playable := false
			for _, segment := range record.Config.Schedule {
				if segment.Kind != config.SegmentLevel {
					continue
				}
				ok, _, _, err := domain.LevelAvailability(segment, availability.TotalChips, availability.ActivePlayers)
				if err != nil {
					return nil, err
				}
				playable = playable || ok
			}
			if playable {
				return nil, fmt.Errorf("%w: no level is unavailable for registration recovery while another level is playable; level %d %s", domain.ErrUnavailableLevel, index, reason)
			}
			recovery = true
		}
	}
	transitions, err := clock.Select(index, remaining, availability, recovery)
	if err != nil {
		return nil, err
	}
	realElapsed := realElapsedAt(record, action.WallTime)
	checkpoint := checkpointFromClock(clock, realElapsed)
	action.Checkpoint = &checkpoint
	payload := map[string]any{"from_segment": record.Checkpoint.SegmentIndex, "to_segment": index, "transitions": transitions, "recovery": recovery}
	event, persisted := action.event("segment_selected", payload, "")
	_, err = s.store.CommitEvent(ctx, event, persisted, func(tx *sql.Tx, _ int64) error {
		if err := s.ensureActionClock(ctx, tx, action); err != nil {
			return err
		}
		if err := ensureNotEnded(ctx, tx); err != nil {
			return err
		}
		var status store.TournamentStatus
		if err := tx.QueryRowContext(ctx, "SELECT status FROM tournament WHERE id = 1").Scan(&status); err != nil {
			return err
		}
		if status == store.StatusEnded || status == store.StatusNotStarted {
			return ErrTournamentNotStarted
		}
		newStatus := status
		if recovery {
			newStatus = store.StatusPaused
		}
		registrationClosed := !registrationOpen(record.Config, checkpoint.SegmentIndex, checkpoint.SegmentFinished, true)
		_, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = ?, registration_closed = ? WHERE id = 1", newStatus, boolInt(newStatus == store.StatusRunning), boolInt(registrationClosed))
		return err
	})
	if err != nil {
		return nil, err
	}
	return transitions, nil
}

func reconcileClockAfterStateChange(ctx context.Context, tx *sql.Tx, checkpoint *store.Checkpoint, eventID int64, payload map[string]any) error {
	if checkpoint == nil {
		return nil
	}
	var status store.TournamentStatus
	var configJSON string
	if err := tx.QueryRowContext(ctx, "SELECT status, configuration_json FROM tournament WHERE id = 1").Scan(&status, &configJSON); err != nil {
		return err
	}
	var template config.Template
	if err := json.Unmarshal([]byte(configJSON), &template); err != nil {
		return err
	}
	if err := template.Validate(); err != nil {
		return err
	}
	clock, err := clockFromCheckpoint(template.Schedule, *checkpoint)
	if err != nil {
		return err
	}
	if status == store.StatusRunning && !clock.Running {
		return fmt.Errorf("running tournament received a paused clock checkpoint")
	}
	if status == store.StatusPaused && clock.Running {
		return fmt.Errorf("paused tournament received a running clock checkpoint")
	}
	if status != store.StatusRunning {
		return nil
	}
	total, active, err := tournamentTotalsTx(ctx, tx)
	if err != nil {
		return err
	}
	var entrants int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchases WHERE kind = 'initial_buy_in'
		AND voided_by_event_id IS NULL`).Scan(&entrants); err != nil {
		return err
	}
	availability := domain.Availability{TotalChips: total, ActivePlayers: active}
	var transitions []domain.Transition
	pauseReason := ""
	if entrants < 2 {
		pauseReason = "fewer than two valid entrants remain"
		if err := clock.Pause(); err != nil {
			return err
		}
	} else {
		transitions = clock.AvailabilityChanged(availability)
	}
	if pauseReason == "" && !clock.SegmentFinished && clock.SegmentIndex > 0 && clock.SegmentIndex <= len(template.Schedule) {
		segment := template.Schedule[clock.SegmentIndex-1]
		if segment.Kind == config.SegmentLevel {
			available, _, reason, err := domain.LevelAvailability(segment, total, active)
			if err != nil {
				return err
			}
			if !available {
				pauseReason = reason
				if err := clock.Pause(); err != nil {
					return err
				}
			}
		}
	}
	updated := checkpointFromClock(clock, checkpoint.RealElapsed)
	*checkpoint = updated
	if pauseReason != "" {
		if _, err := tx.ExecContext(ctx, "UPDATE tournament SET status = ?, clock_running = 0 WHERE id = 1", store.StatusPaused); err != nil {
			return err
		}
		payload["clock_paused_reason"] = pauseReason
	}
	if len(transitions) > 0 {
		payload["clock_transitions"] = transitions
	}
	registrationClosed := !registrationOpen(template, clock.SegmentIndex, clock.SegmentFinished, true)
	if _, err := tx.ExecContext(ctx, "UPDATE tournament SET registration_closed = ? WHERE id = 1", boolInt(registrationClosed)); err != nil {
		return err
	}
	if pauseReason != "" || len(transitions) > 0 {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE audit_events SET payload_json = ? WHERE id = ?", string(encoded), eventID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) currentAvailability(ctx context.Context) (domain.Availability, Summary, error) {
	summary, err := s.CurrentSummary(ctx)
	if err != nil {
		return domain.Availability{}, Summary{}, err
	}
	return domain.Availability{TotalChips: summary.TotalStack, ActivePlayers: summary.ActivePlayers}, summary, nil
}

func clockFromCheckpoint(schedule []config.Segment, checkpoint store.Checkpoint) (domain.Clock, error) {
	clock, err := domain.NewClock(schedule)
	if err != nil {
		return domain.Clock{}, err
	}
	clock.SegmentIndex = checkpoint.SegmentIndex
	clock.Remaining = checkpoint.Remaining
	clock.Elapsed = checkpoint.ClockElapsed
	clock.Running = checkpoint.Running
	clock.SegmentFinished = checkpoint.SegmentFinished
	clock.HoldReason = checkpoint.HoldReason
	clock.PendingIndex = checkpoint.PendingIndex
	if clock.SegmentIndex < 0 || clock.SegmentIndex > len(schedule) {
		return domain.Clock{}, fmt.Errorf("stored clock segment %d is outside schedule", clock.SegmentIndex)
	}
	return clock, nil
}

func checkpointFromClock(clock domain.Clock, realElapsed time.Duration) store.Checkpoint {
	return store.Checkpoint{
		ClockElapsed: clock.Elapsed, RealElapsed: realElapsed, SegmentIndex: clock.SegmentIndex,
		Remaining: clock.Remaining, Running: clock.Running, SegmentFinished: clock.SegmentFinished,
		HoldReason: clock.HoldReason, PendingIndex: clock.PendingIndex,
	}
}

func realElapsedAt(record store.TournamentRecord, at time.Time) time.Duration {
	if record.StartedAt == nil {
		return record.Checkpoint.RealElapsed
	}
	if at.IsZero() {
		at = time.Now()
	}
	elapsed := at.Sub(*record.StartedAt)
	return maxDuration(record.Checkpoint.RealElapsed, elapsed)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func durationPointer(value time.Duration) *time.Duration { return &value }
