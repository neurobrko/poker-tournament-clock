// Package domain contains deterministic tournament rules independent of storage and UI.
package domain

import (
	"fmt"
	"math"

	"git.mpx.sk/elvis/poker_clock/internal/config"
)

// RequiredChips computes the minimum total stack needed to play a blind level.
// ActivePlayers includes players sitting out; the app does not model individual stacks.
func RequiredChips(level config.Segment, activePlayers int64) (int64, error) {
	if level.Kind != config.SegmentLevel {
		return 0, fmt.Errorf("segment %q is not a blind level", level.Kind)
	}
	if level.SmallBlind <= 0 || level.BigBlind <= level.SmallBlind {
		return 0, fmt.Errorf("blind level requires positive small blind and larger big blind")
	}
	if activePlayers < 0 || level.Ante.Amount < 0 {
		return 0, fmt.Errorf("ante and player count cannot be negative")
	}
	total, err := checkedAdd(level.SmallBlind, level.BigBlind)
	if err != nil {
		return 0, fmt.Errorf("blind requirement overflows: %w", err)
	}
	switch level.Ante.Kind {
	case "", config.AnteNone:
		if level.Ante.Amount != 0 {
			return 0, fmt.Errorf("no-ante level must have amount zero")
		}
	case config.AnteBigBlind:
		if level.Ante.Amount <= 0 {
			return 0, fmt.Errorf("big-blind ante must be positive")
		}
		total, err = checkedAdd(total, level.Ante.Amount)
	case config.AntePerPlayer:
		if level.Ante.Amount <= 0 {
			return 0, fmt.Errorf("per-player ante must be positive")
		}
		anteTotal, mulErr := checkedMultiply(level.Ante.Amount, activePlayers)
		if mulErr != nil {
			return 0, fmt.Errorf("per-player ante requirement overflows: %w", mulErr)
		}
		total, err = checkedAdd(total, anteTotal)
	default:
		return 0, fmt.Errorf("unknown ante kind %q", level.Ante.Kind)
	}
	if err != nil {
		return 0, fmt.Errorf("blind and ante requirement overflows: %w", err)
	}
	return total, nil
}

// LevelAvailability reports whether the total chips in play can cover one payment round.
func LevelAvailability(level config.Segment, totalChips, activePlayers int64) (available bool, required int64, reason string, err error) {
	if totalChips < 0 {
		return false, 0, "total chips cannot be negative", nil
	}
	required, err = RequiredChips(level, activePlayers)
	if err != nil {
		return false, 0, "", err
	}
	if totalChips >= required {
		return true, required, "", nil
	}
	return false, required, fmt.Sprintf("requires %d chips; only %d in play", required, totalChips), nil
}

func checkedAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("integer overflow")
	}
	return a + b, nil
}

func checkedMultiply(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a > 0 && b > 0 && a > math.MaxInt64/b || a < 0 && b < 0 && a < math.MaxInt64/b || a > 0 && b < 0 && b < math.MinInt64/a || a < 0 && b > 0 && a < math.MinInt64/b {
		return 0, fmt.Errorf("integer overflow")
	}
	return a * b, nil
}
