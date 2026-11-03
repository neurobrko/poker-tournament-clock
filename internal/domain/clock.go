package domain

import (
	"errors"
	"fmt"
	"math"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
)

var (
	ErrClockNotStarted     = errors.New("tournament clock has not started")
	ErrClockNotRunning     = errors.New("tournament clock is not running")
	ErrClockAlreadyRunning = errors.New("tournament clock is already running")
	ErrClockAlreadyStarted = errors.New("tournament clock has already started")
	ErrUnavailableLevel    = errors.New("blind level is unavailable")
	ErrCannotResumeLevel   = errors.New("current blind level is unavailable; select an available level or add chips")
	ErrInvalidSegment      = errors.New("schedule segment does not exist")
)

type Availability struct {
	TotalChips    int64
	ActivePlayers int64
}

type Clock struct {
	Schedule        []config.Segment
	SegmentIndex    int // One-based; zero before the first start.
	Remaining       time.Duration
	Elapsed         time.Duration
	Running         bool
	SegmentFinished bool
	HoldReason      string
	PendingIndex    int // Next level being awaited while current blinds remain in force.
}

type TransitionKind string

const (
	TransitionLevelStarted TransitionKind = "level_started"
	TransitionBreakStarted TransitionKind = "break_started"
	TransitionBreakEnded   TransitionKind = "break_ended"
	TransitionLevelEnded   TransitionKind = "level_ended"
	TransitionHoldStarted  TransitionKind = "hold_started"
)

type Transition struct {
	Kind      TransitionKind
	FromIndex int
	ToIndex   int
	Reason    string
}

func NewClock(schedule []config.Segment) (Clock, error) {
	if len(schedule) == 0 {
		return Clock{}, fmt.Errorf("clock requires a nonempty schedule")
	}
	if schedule[0].Kind != config.SegmentLevel {
		return Clock{}, fmt.Errorf("clock schedule must begin with a blind level")
	}
	previous := config.SegmentKind("")
	for index, segment := range schedule {
		if segment.Duration.Value() <= 0 {
			return Clock{}, fmt.Errorf("schedule segment %d must have positive duration", index+1)
		}
		switch segment.Kind {
		case config.SegmentLevel:
			if _, err := RequiredChips(segment, 0); err != nil {
				return Clock{}, fmt.Errorf("invalid blind level %d: %w", index+1, err)
			}
		case config.SegmentBreak:
			if previous != config.SegmentLevel || segment.SmallBlind != 0 || segment.BigBlind != 0 || segment.Ante.Amount != 0 || (segment.Ante.Kind != "" && segment.Ante.Kind != config.AnteNone) {
				return Clock{}, fmt.Errorf("invalid break at schedule segment %d", index+1)
			}
		default:
			return Clock{}, fmt.Errorf("unknown schedule segment kind %q", segment.Kind)
		}
		previous = segment.Kind
	}
	if previous == config.SegmentBreak {
		return Clock{}, fmt.Errorf("clock schedule cannot end with a break")
	}
	return Clock{Schedule: append([]config.Segment(nil), schedule...)}, nil
}

func (c Clock) Snapshot() (segmentIndex int, remaining, elapsed time.Duration, running, finished bool, holdReason string) {
	return c.SegmentIndex, c.Remaining, c.Elapsed, c.Running, c.SegmentFinished, c.HoldReason
}

func (c *Clock) Start(availability Availability) ([]Transition, error) {
	if c.SegmentIndex != 0 {
		return nil, ErrClockAlreadyStarted
	}
	if len(c.Schedule) == 0 {
		return nil, fmt.Errorf("clock has no schedule")
	}
	if c.Schedule[0].Kind != config.SegmentLevel {
		return nil, fmt.Errorf("opening schedule segment must be a blind level")
	}
	if ok, required, reason, err := c.levelAvailable(1, availability); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: opening level requires %d chips (%s)", ErrUnavailableLevel, required, reason)
	}
	c.SegmentIndex = 1
	c.Remaining = c.Schedule[0].Duration.Value()
	c.Running = true
	c.SegmentFinished = false
	c.HoldReason = ""
	c.PendingIndex = 0
	return []Transition{{Kind: TransitionLevelStarted, ToIndex: 1}}, nil
}

func (c *Clock) Pause() error {
	if c.SegmentIndex == 0 {
		return ErrClockNotStarted
	}
	if !c.Running {
		return ErrClockNotRunning
	}
	c.Running = false
	return nil
}

func (c *Clock) Resume(availability Availability) ([]Transition, error) {
	if c.SegmentIndex == 0 {
		return nil, ErrClockNotStarted
	}
	if c.Running {
		return nil, ErrClockAlreadyRunning
	}
	if !c.SegmentFinished && c.Schedule[c.SegmentIndex-1].Kind == config.SegmentLevel {
		if ok, required, reason, err := c.levelAvailable(c.SegmentIndex, availability); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("%w: requires %d chips (%s)", ErrCannotResumeLevel, required, reason)
		}
	}
	c.Running = true
	return c.tryReleaseHold(availability), nil
}

// Advance moves the clock by delta. The caller supplies a monotonic elapsed duration,
// normally from a one-second ticker, so this function remains deterministic in tests.
func (c *Clock) Advance(delta time.Duration, availability Availability) ([]Transition, error) {
	if delta < 0 {
		return nil, fmt.Errorf("clock delta cannot be negative")
	}
	if !c.Running {
		return nil, nil
	}
	if c.SegmentIndex == 0 {
		return nil, ErrClockNotStarted
	}
	if delta > time.Duration(math.MaxInt64)-c.Elapsed {
		return nil, fmt.Errorf("elapsed clock time overflow")
	}
	c.Elapsed += delta
	transitions := c.tryReleaseHold(availability)
	if c.SegmentFinished {
		return transitions, nil
	}
	remainingDelta := delta
	for remainingDelta > 0 && c.Running && !c.SegmentFinished {
		if remainingDelta < c.Remaining {
			c.Remaining -= remainingDelta
			break
		}
		remainingDelta -= c.Remaining
		c.Remaining = 0
		newTransitions := c.expireCurrent(availability)
		transitions = append(transitions, newTransitions...)
		if c.SegmentFinished {
			break
		}
	}
	return transitions, nil
}

// AvailabilityChanged advances a waiting hold immediately only when clock is running.
func (c *Clock) AvailabilityChanged(availability Availability) []Transition {
	if !c.Running {
		return nil
	}
	return c.tryReleaseHold(availability)
}

func (c *Clock) expireCurrent(availability Availability) []Transition {
	current := c.SegmentIndex
	segment := c.Schedule[current-1]
	transitions := make([]Transition, 0, 2)
	if segment.Kind == config.SegmentBreak {
		transitions = append(transitions, Transition{Kind: TransitionBreakEnded, FromIndex: current, ToIndex: current})
	} else {
		transitions = append(transitions, Transition{Kind: TransitionLevelEnded, FromIndex: current, ToIndex: current})
	}
	next := current + 1
	if next > len(c.Schedule) {
		c.SegmentFinished = true
		c.Remaining = 0
		c.PendingIndex = 0
		c.HoldReason = "last configured level reached"
		transitions = append(transitions, Transition{Kind: TransitionHoldStarted, FromIndex: current, ToIndex: current, Reason: c.HoldReason})
		return transitions
	}
	if c.Schedule[next-1].Kind == config.SegmentLevel {
		if ok, _, reason, err := c.levelAvailable(next, availability); err != nil {
			c.Running = false
			c.SegmentFinished = true
			c.Remaining = 0
			c.HoldReason = err.Error()
			transitions = append(transitions, Transition{Kind: TransitionHoldStarted, FromIndex: current, ToIndex: next, Reason: c.HoldReason})
			return transitions
		} else if !ok {
			c.SegmentFinished = true
			c.Remaining = 0
			c.PendingIndex = next
			c.HoldReason = reason
			transitions = append(transitions, Transition{Kind: TransitionHoldStarted, FromIndex: current, ToIndex: next, Reason: reason})
			return transitions
		}
	}
	return append(transitions, c.enterSegment(next)...)
}

func (c *Clock) enterSegment(index int) []Transition {
	from := c.SegmentIndex
	segment := c.Schedule[index-1]
	c.SegmentIndex = index
	c.Remaining = segment.Duration.Value()
	c.SegmentFinished = false
	c.HoldReason = ""
	c.PendingIndex = 0
	kind := TransitionLevelStarted
	if segment.Kind == config.SegmentBreak {
		kind = TransitionBreakStarted
	}
	return []Transition{{Kind: kind, FromIndex: from, ToIndex: index}}
}

func (c *Clock) tryReleaseHold(availability Availability) []Transition {
	if !c.Running || !c.SegmentFinished || c.PendingIndex == 0 {
		return nil
	}
	available, _, _, err := c.levelAvailable(c.PendingIndex, availability)
	if err != nil || !available {
		return nil
	}
	return c.enterSegment(c.PendingIndex)
}

func (c *Clock) levelAvailable(index int, availability Availability) (bool, int64, string, error) {
	if index < 1 || index > len(c.Schedule) {
		return false, 0, "", ErrInvalidSegment
	}
	segment := c.Schedule[index-1]
	if segment.Kind != config.SegmentLevel {
		return true, 0, "", nil
	}
	return LevelAvailability(segment, availability.TotalChips, availability.ActivePlayers)
}

// Select jumps to a configured segment without changing elapsed tournament time or run state.
// An unavailable level can be selected only for the explicit paused registration-recovery path.
func (c *Clock) Select(index int, remaining *time.Duration, availability Availability, recovery bool) ([]Transition, error) {
	if index < 1 || index > len(c.Schedule) {
		return nil, ErrInvalidSegment
	}
	segment := c.Schedule[index-1]
	if segment.Kind == config.SegmentLevel {
		if ok, required, reason, err := c.levelAvailable(index, availability); err != nil {
			return nil, err
		} else if !ok && !recovery {
			return nil, fmt.Errorf("%w: level %d requires %d chips (%s)", ErrUnavailableLevel, index, required, reason)
		}
	}
	if remaining != nil && *remaining < 0 {
		return nil, fmt.Errorf("remaining time cannot be negative")
	}
	previous := c.SegmentIndex
	c.SegmentIndex = index
	c.Remaining = segment.Duration.Value()
	if remaining != nil {
		c.Remaining = *remaining
	}
	c.SegmentFinished = false
	c.HoldReason = ""
	c.PendingIndex = 0
	if segment.Kind == config.SegmentLevel && recovery {
		if ok, _, _, err := c.levelAvailable(index, availability); err != nil {
			return nil, err
		} else if !ok {
			c.Running = false
		}
	}
	kind := TransitionLevelStarted
	if segment.Kind == config.SegmentBreak {
		kind = TransitionBreakStarted
	}
	return []Transition{{Kind: kind, FromIndex: previous, ToIndex: index}}, nil
}
