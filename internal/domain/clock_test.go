package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
)

func TestClockAdvancesThroughLevelsAndBreaks(t *testing.T) {
	clock := testClock(t, []config.Segment{
		level(5, 10, time.Second),
		{Kind: config.SegmentBreak, Duration: config.Duration(time.Second)},
		level(10, 20, time.Second),
		level(20, 40, time.Second),
	})
	availability := Availability{TotalChips: 100, ActivePlayers: 4}
	transitions, err := clock.Start(availability)
	if err != nil || len(transitions) != 1 || transitions[0].Kind != TransitionLevelStarted {
		t.Fatalf("start = %v, %v", transitions, err)
	}
	transitions, err = clock.Advance(time.Second, availability)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionLevelEnded, TransitionBreakStarted}) {
		t.Fatalf("level-to-break transitions = %v", transitions)
	}
	transitions, err = clock.Advance(time.Second, availability)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionBreakEnded, TransitionLevelStarted}) {
		t.Fatalf("break-to-level transitions = %v", transitions)
	}
	if clock.SegmentIndex != 3 || clock.Remaining != time.Second || clock.Elapsed != 2*time.Second {
		t.Fatalf("clock after break = %+v", clock)
	}
}

func TestUnavailableNextLevelHoldsCurrentBlindsAndReleasesWhenRunning(t *testing.T) {
	clock := testClock(t, []config.Segment{level(5, 10, time.Second), level(50, 100, time.Second)})
	if _, err := clock.Start(Availability{TotalChips: 100, ActivePlayers: 2}); err != nil {
		t.Fatal(err)
	}
	transitions, err := clock.Advance(time.Second, Availability{TotalChips: 100, ActivePlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionLevelEnded, TransitionHoldStarted}) {
		t.Fatalf("blocked level transitions = %v", transitions)
	}
	if clock.SegmentIndex != 1 || !clock.SegmentFinished || clock.PendingIndex != 2 || clock.Remaining != 0 || !clock.Running {
		t.Fatalf("blocked clock state = %+v", clock)
	}
	if clock.HoldReason == "" {
		t.Fatal("blocked level has no hold reason")
	}
	transitions = clock.AvailabilityChanged(Availability{TotalChips: 150, ActivePlayers: 2})
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionLevelStarted}) || clock.SegmentIndex != 2 {
		t.Fatalf("release transitions = %v, clock %+v", transitions, clock)
	}
}

func TestBlockedBreakFinishesAndKeepsBreakEndState(t *testing.T) {
	clock := testClock(t, []config.Segment{
		level(5, 10, time.Second),
		{Kind: config.SegmentBreak, Duration: config.Duration(time.Second), Label: "Registration break"},
		level(50, 100, time.Second),
	})
	availability := Availability{TotalChips: 100, ActivePlayers: 2}
	if _, err := clock.Start(availability); err != nil {
		t.Fatal(err)
	}
	if _, err := clock.Advance(time.Second, availability); err != nil {
		t.Fatal(err)
	}
	transitions, err := clock.Advance(time.Second, availability)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionBreakEnded, TransitionHoldStarted}) {
		t.Fatalf("blocked break transitions = %v", transitions)
	}
	if clock.SegmentIndex != 2 || !clock.SegmentFinished || clock.PendingIndex != 3 {
		t.Fatalf("blocked break state = %+v", clock)
	}
	clock.Pause()
	if got := clock.AvailabilityChanged(Availability{TotalChips: 150, ActivePlayers: 2}); len(got) != 0 {
		t.Fatalf("paused clock advanced when level became available: %v", got)
	}
	transitions, err = clock.Resume(Availability{TotalChips: 150, ActivePlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionLevelStarted}) || clock.SegmentIndex != 3 {
		t.Fatalf("resume release = %v, state %+v", transitions, clock)
	}
}

func TestFinalLevelHoldsIndefinitelyAndElapsedTimeContinues(t *testing.T) {
	clock := testClock(t, []config.Segment{level(5, 10, time.Second)})
	availability := Availability{TotalChips: 100, ActivePlayers: 2}
	if _, err := clock.Start(availability); err != nil {
		t.Fatal(err)
	}
	transitions, err := clock.Advance(time.Second, availability)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds(transitions), []TransitionKind{TransitionLevelEnded, TransitionHoldStarted}) || !clock.SegmentFinished {
		t.Fatalf("final-level transitions = %v, state %+v", transitions, clock)
	}
	if _, err := clock.Advance(5*time.Second, availability); err != nil {
		t.Fatal(err)
	}
	if clock.Elapsed != 6*time.Second || clock.Remaining != 0 || clock.SegmentIndex != 1 {
		t.Fatalf("final hold clock = %+v", clock)
	}
}

func TestResumeRejectsUnavailableCurrentLevel(t *testing.T) {
	clock := testClock(t, []config.Segment{level(50, 100, time.Minute)})
	if _, err := clock.Start(Availability{TotalChips: 150, ActivePlayers: 2}); err != nil {
		t.Fatal(err)
	}
	if err := clock.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := clock.Resume(Availability{TotalChips: 149, ActivePlayers: 2}); !errors.Is(err, ErrCannotResumeLevel) {
		t.Fatalf("resume error = %v, want ErrCannotResumeLevel", err)
	}
}

func TestManualSelectionPreservesRunStateAndElapsedTime(t *testing.T) {
	clock := testClock(t, []config.Segment{level(5, 10, time.Minute), level(10, 20, time.Minute)})
	if _, err := clock.Start(Availability{TotalChips: 100, ActivePlayers: 2}); err != nil {
		t.Fatal(err)
	}
	clock.Elapsed = 17 * time.Second
	remaining := 12 * time.Second
	transitions, err := clock.Select(2, &remaining, Availability{TotalChips: 100, ActivePlayers: 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || clock.SegmentIndex != 2 || clock.Remaining != remaining || !clock.Running || clock.Elapsed != 17*time.Second {
		t.Fatalf("running manual selection state = %+v, transitions %v", clock, transitions)
	}
	if err := clock.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := clock.Select(1, nil, Availability{TotalChips: 100, ActivePlayers: 2}, false); err != nil {
		t.Fatal(err)
	}
	if clock.Running || clock.Elapsed != 17*time.Second {
		t.Fatalf("paused manual selection changed run state or elapsed time: %+v", clock)
	}
}

func TestRecoverySelectionOfUnavailableLevelStaysPaused(t *testing.T) {
	clock := testClock(t, []config.Segment{level(50, 100, time.Minute)})
	if _, err := clock.Select(1, nil, Availability{TotalChips: 50, ActivePlayers: 2}, true); err != nil {
		t.Fatal(err)
	}
	if clock.Running {
		t.Fatal("recovery selection of unavailable level must stay paused")
	}
	if _, err := clock.Resume(Availability{TotalChips: 50, ActivePlayers: 2}); !errors.Is(err, ErrCannotResumeLevel) {
		t.Fatalf("resume error = %v, want unavailable level error", err)
	}
}

func testClock(t *testing.T, schedule []config.Segment) Clock {
	t.Helper()
	clock, err := NewClock(schedule)
	if err != nil {
		t.Fatal(err)
	}
	return clock
}

func level(smallBlind, bigBlind int64, duration time.Duration) config.Segment {
	return config.Segment{
		Kind: config.SegmentLevel, SmallBlind: smallBlind, BigBlind: bigBlind,
		Duration: config.Duration(duration), Ante: config.AnteConfig{Kind: config.AnteNone},
	}
}

func kinds(transitions []Transition) []TransitionKind {
	result := make([]TransitionKind, len(transitions))
	for index, transition := range transitions {
		result[index] = transition.Kind
	}
	return result
}
