package ui

import (
	"fmt"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func browserTitle(purpose browserPurpose) string {
	switch purpose {
	case browseTournamentDB:
		return "LOAD TOURNAMENT DATABASE (.db / .sqlite)"
	case browseTemplate:
		return "SELECT YAML TEMPLATE (.yaml / .yml)"
	case browseSaveDatabase:
		return "NEW TOURNAMENT DATABASE"
	case browseSaveTemplate:
		return "EXPORT TEMPLATE"
	case browseLevelSound:
		return "SELECT END-OF-LEVEL SOUND (.wav / .mp3)"
	case browseMinuteSound:
		return "SELECT ONE-MINUTE WARNING SOUND (.wav / .mp3)"
	default:
		return "FILE BROWSER"
	}
}

func (m *Model) currentSegment() *config.Segment {
	index := m.record.Checkpoint.SegmentIndex
	if index < 1 || index > len(m.record.Config.Schedule) {
		if m.record.Status == store.StatusNotStarted && len(m.record.Config.Schedule) > 0 {
			return &m.record.Config.Schedule[0]
		}
		return nil
	}
	return &m.record.Config.Schedule[index-1]
}

func (m *Model) previousLevel() *config.Segment {
	for index := m.record.Checkpoint.SegmentIndex - 2; index >= 0; index-- {
		if m.record.Config.Schedule[index].Kind == config.SegmentLevel {
			return &m.record.Config.Schedule[index]
		}
	}
	return nil
}

func (m *Model) nextBreakLabel() string {
	if m.record.Checkpoint.HoldReason != "" && m.record.Checkpoint.PendingIndex > 0 {
		return "unknown — blocked levels"
	}
	breakNo := 0
	for index := 0; index < m.record.Checkpoint.SegmentIndex && index < len(m.record.Config.Schedule); index++ {
		if m.record.Config.Schedule[index].Kind == config.SegmentBreak {
			breakNo++
		}
	}
	for index := m.record.Checkpoint.SegmentIndex; index < len(m.record.Config.Schedule); index++ {
		segment := m.record.Config.Schedule[index]
		if segment.Kind == config.SegmentBreak {
			breakNo++
			label := segment.Label
			if label == "" {
				label = fmt.Sprintf("Break %d", breakNo)
			}
			return fmt.Sprintf("%s in %s", label, m.untilNextBreak(index))
		}
	}
	return "none configured"
}

func (m *Model) untilNextBreak(targetIndex int) string {
	checkpoint := m.record.Checkpoint
	if checkpoint.SegmentFinished {
		return "unknown"
	}
	remaining := checkpoint.Remaining
	currentIndex := checkpoint.SegmentIndex - 1
	if currentIndex < 0 {
		currentIndex = 0
	}
	for index := currentIndex; index < targetIndex; index++ {
		if index == currentIndex {
			continue
		}
		if m.record.Config.Schedule[index].Kind == config.SegmentLevel || m.record.Config.Schedule[index].Kind == config.SegmentBreak {
			remaining += m.record.Config.Schedule[index].Duration.Value()
		}
	}
	return formatDuration(remaining)
}

func formatDuration(value time.Duration) string {
	seconds := int64(value / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func formatChips(value int64) string {
	if value < 0 {
		return "0"
	}
	return fmt.Sprintf("%d", value)
}

func segmentReminderLines(segment config.Segment) (chipUp, reminder string) {
	reminder = strings.TrimSpace(segment.Reminder)
	if segment.ChipUp > 0 {
		return fmt.Sprintf("Chip up! (%s)", formatDisplayChips(segment.ChipUp)), reminder
	}
	if formatted := formatChipUpReminder(reminder); formatted != reminder {
		return formatted, ""
	}
	return "", reminder
}

func formatChipUpReminder(reminder string) string {
	trimmed := strings.TrimSpace(reminder)
	if !strings.HasPrefix(strings.ToLower(trimmed), "chip up") {
		return trimmed
	}
	runes := []rune(trimmed)
	for i := 0; i < len(runes); i++ {
		if runes[i] < '0' || runes[i] > '9' {
			continue
		}
		start := i
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
		return fmt.Sprintf("Chip up! (%s)", string(runes[start:i]))
	}
	return trimmed
}

func transitionNotice(transitions []domain.Transition, schedule []config.Segment) string {
	var notices []string
	for _, transition := range transitions {
		switch transition.Kind {
		case domain.TransitionLevelStarted:
			notices = append(notices, fmt.Sprintf("Level %d started", countLevel(schedule, transition.ToIndex)))
		case domain.TransitionBreakStarted:
			notices = append(notices, "Break started")
		case domain.TransitionBreakEnded:
			notices = append(notices, "Break ended")
		case domain.TransitionHoldStarted:
			notices = append(notices, "HOLD: "+transition.Reason)
		}
		if transition.ToIndex > 0 && transition.ToIndex <= len(schedule) {
			chipUp, reminder := segmentReminderLines(schedule[transition.ToIndex-1])
			if chipUp != "" && reminder != "" {
				notices = append(notices, chipUp+"\n"+reminder)
			} else if chipUp != "" {
				notices = append(notices, chipUp)
			} else if reminder != "" {
				notices = append(notices, reminder)
			}
		}
	}
	return strings.Join(notices, " · ")
}

func bustNumbersByEventID(events []store.EventRecord) map[int64]int {
	numbers := make(map[int64]int)
	count := 0
	for _, event := range events {
		if event.Kind == "bust_recorded" {
			count++
			numbers[event.ID] = count
		}
	}
	return numbers
}

func countLevel(schedule []config.Segment, target int) int {
	count := 0
	for index := 0; index < target && index < len(schedule); index++ {
		if schedule[index].Kind == config.SegmentLevel {
			count++
		}
	}
	return count
}
