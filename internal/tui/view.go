package tui

import (
	"fmt"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	goodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	badStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
)

func (m *Model) View() string {
	if m.tooSmall {
		return m.smallTerminalView()
	}
	if m.browser != nil {
		return m.browserView()
	}
	if m.modal != nil {
		return m.dialogView(m.modal)
	}
	if m.screen == screenRules {
		return m.rulesView()
	}
	if m.screen == screenStartup || m.database == nil {
		return m.startupView()
	}
	return m.mainView()
}

func (m *Model) startupView() string {
	choices := []string{"New tournament", "Load tournament", "Quit"}
	var lines []string
	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("POKER CLOCK"))
	lines = append(lines, "Self-hosted tournament clock")
	lines = append(lines, "")
	for index, choice := range choices {
		mark := "  "
		if index == m.startupCursor {
			mark = "> "
		}
		lines = append(lines, mark+choice)
	}
	lines = append(lines, "", helpStyle.Render("↑/↓ select · Enter open · Q exit"))
	if m.statusMessage != "" {
		lines = append(lines, "", renderStatus(m.statusMessage, m.statusIsError))
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

func (m *Model) smallTerminalView() string {
	message := fmt.Sprintf("Terminal too small: %d × %d. Resize to at least 120 × 36.\n\n", m.width, m.height)
	if m.modal != nil && m.modal.kind == dialogConfirm {
		message += "\n\n" + m.modal.title + "\n" + m.modal.message + "\nY/Enter confirm · N/Esc cancel"
	} else if m.service == nil {
		message += "Resize to continue startup, or press Q to quit (confirmation required)."
	} else if m.record.Status == store.StatusRunning {
		message += "Clock continues. Space pauses; Q exits (confirmation required)."
	} else {
		message += "Space starts/resumes; Q exits (confirmation required)."
	}
	if m.statusMessage != "" {
		message += "\n\n" + m.statusMessage
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, warnStyle.Render(message))
}

func (m *Model) browserView() string {
	var builder strings.Builder
	builder.WriteString(titleStyle.Render("FILE BROWSER"))
	builder.WriteString("\n")
	builder.WriteString(m.browser.directory)
	builder.WriteString("\n\n")
	start := 0
	if m.browser.cursor >= m.height-8 {
		start = m.browser.cursor - (m.height - 9)
	}
	end := start + m.height - 9
	if end > len(m.browser.entries) {
		end = len(m.browser.entries)
	}
	for index := start; index < end; index++ {
		entry := m.browser.entries[index]
		prefix := "  "
		if index == m.browser.cursor {
			prefix = "> "
		}
		name := entry.name
		if entry.dir {
			name = goodStyle.Render(name)
		}
		builder.WriteString(prefix + name + "\n")
	}
	builder.WriteString("\n")
	builder.WriteString(helpStyle.Render("↑/↓ move · Enter open/select · Backspace parent · N new file · Esc cancel"))
	if m.statusMessage != "" {
		builder.WriteString("\n\n")
		builder.WriteString(renderStatus(m.statusMessage, m.statusIsError))
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, builder.String(), lipgloss.WithWhitespaceChars(" "))
}

func (m *Model) mainView() string {
	rightWidth := m.width / 7
	if rightWidth < 28 {
		rightWidth = 28
	}
	remainingWidth := m.width - rightWidth
	leftWidth := remainingWidth / 3
	centerWidth := remainingWidth - leftWidth
	left := panel("TOURNAMENT", leftWidth, m.leftPanel())
	center := panel("CLOCK", centerWidth, m.centerPanel())
	right := panel("ACTIONS", rightWidth, m.rightPanel())
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	footer := ""
	if m.notice != "" {
		footer += warnStyle.Render(m.notice) + "  "
	}
	if m.statusMessage != "" {
		footer += renderStatus(m.statusMessage, m.statusIsError)
	}
	return content + "\n" + lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(footer)
}

func panel(title string, totalWidth int, content string) string {
	contentWidth := totalWidth - 4
	if contentWidth < 10 {
		contentWidth = 10
	}
	body := titleStyle.Render(title) + "\n" + content
	return panelStyle.Width(contentWidth).Render(body)
}

func (m *Model) leftPanel() string {
	var lines []string
	lines = append(lines,
		fmt.Sprintf("Entrants       %d", m.summary.Entrants),
		fmt.Sprintf("Active players %d", m.summary.ActivePlayers),
		"",
		fmt.Sprintf("Buy-ins  %d · %s", m.summary.InitialBuyIns, m.summary.InitialBuyInCents),
		fmt.Sprintf("Rebuys   %d · %s", m.summary.Rebuys, m.summary.RebuyCents),
		fmt.Sprintf("Add-ons  %d · %s", m.summary.AddOns, m.summary.AddOnCents),
		"",
		fmt.Sprintf("Total stack   %s", formatChips(m.summary.TotalStack)),
		fmt.Sprintf("Average stack %s", formatChips(m.summary.AverageStack)),
		fmt.Sprintf("Prize pool    %s", m.summary.PrizePoolCents),
		"",
		m.prizeHeading(),
	)
	if len(m.prizes) == 0 {
		lines = append(lines, helpStyle.Render("No standings yet"))
	} else {
		for _, group := range m.prizes {
			for _, player := range group.Players {
				name := player.PlayerID
				if p, ok := m.playerByID(player.PlayerID); ok {
					name = p.Name
				}
				prize := config.EuroCents(player.AmountCents).String()
				if player.GetsRemainder {
					prize += " +€0.10"
				}
				lines = append(lines, fmt.Sprintf("%d. %-12s %s", group.FirstPlace, truncate(name, 12), prize))
			}
			if group.PendingRemainderCents > 0 {
				lines = append(lines, warnStyle.Render(fmt.Sprintf("Pending tie remainder: %s", config.EuroCents(group.PendingRemainderCents))))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) prizeHeading() string {
	if m.record.Status == store.StatusEnded {
		if m.record.ResultCompleted != nil && *m.record.ResultCompleted {
			return "Final prizes"
		}
		return "No final standings"
	}
	return "Provisional prizes"
}

func (m *Model) centerPanel() string {
	var lines []string
	current := m.currentSegment()
	if current == nil {
		lines = append(lines, "Current blinds: —")
	} else if current.Kind == config.SegmentBreak && !m.record.Checkpoint.SegmentFinished {
		lines = append(lines, warnStyle.Render("BREAK"))
	} else if current.Kind == config.SegmentLevel {
		lines = append(lines, fmt.Sprintf("Blinds  %s / %s", formatChips(current.SmallBlind), formatChips(current.BigBlind)))
		lines = append(lines, formatAnte(current.Ante))
	} else if current.Kind == config.SegmentBreak && m.record.Checkpoint.SegmentFinished {
		if level := m.previousLevel(); level != nil {
			lines = append(lines, fmt.Sprintf("Blinds  %s / %s", formatChips(level.SmallBlind), formatChips(level.BigBlind)))
		}
	}
	lines = append(lines, "")
	timer := formatDuration(m.record.Checkpoint.Remaining)
	if m.record.Checkpoint.HoldReason != "" {
		timer = "HOLD"
	}
	if m.record.Status == store.StatusNotStarted {
		timer = formatDuration(m.record.Checkpoint.Remaining)
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220")).Render("       "+timer))
	lines = append(lines, statusLabel(m.record.Status), "")
	lines = append(lines, fmt.Sprintf("Local time  %s", time.Now().Format("15:04:05")))
	lines = append(lines, "Next level  "+m.nextLevelLabel())
	lines = append(lines, "Next break  "+m.nextBreakLabel())
	if m.record.Checkpoint.HoldReason != "" {
		lines = append(lines, warnStyle.Render("Hold: "+m.record.Checkpoint.HoldReason))
	}
	if current != nil && current.Reminder != "" {
		lines = append(lines, "", warnStyle.Render(current.Reminder))
	}
	if m.record.Status == store.StatusEnded {
		lines = append(lines, "", badStyle.Render("TOURNAMENT ENDED"))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) rightPanel() string {
	ended := m.record.Status == store.StatusEnded
	buyIn := "[B] Buy-in"
	if m.record.RegistrationClosed || ended {
		buyIn = "[B] Buy-in [closed]"
	}
	addon := "[A] Add-on"
	addonDisabled := ended || !m.record.Config.Registration.AddOnsEnabled || !m.addOnWindowOpen()
	if !m.record.Config.Registration.AddOnsEnabled {
		addon = "[A] Add-on [off]"
	} else if ended {
		addon = "[A] Add-on [closed]"
	} else if addonDisabled {
		addon = "[A] Add-on [break only]"
	}
	rebuy := "[R] Rebuy"
	if !m.record.Config.Registration.RebuysEnabled {
		rebuy = "[R] Rebuy [off]"
	} else if m.record.RegistrationClosed || ended {
		rebuy = "[R] Rebuy [closed]"
	}
	return strings.Join([]string{
		actionButton(buyIn, m.record.RegistrationClosed),
		actionButton(addon, addonDisabled),
		actionButton(rebuy, !m.record.Config.Registration.RebuysEnabled || m.record.RegistrationClosed || ended),
		actionButton("[P] Players", ended),
		actionButton("[L] Level", m.record.StartedAt == nil || m.record.Status == store.StatusEnded),
		actionButton("[X] Bust", m.record.StartedAt == nil || m.record.Status == store.StatusEnded),
		actionButton("[S] Settings", false),
		actionButton("[H] Resolver (Stage 2)", true),
		actionButton("[?] Rules", false),
		actionButton("[O] Sit out", m.record.Status == store.StatusEnded),
		actionButton("[Space] Start/pause", m.record.Status == store.StatusEnded),
		actionButton("[E] End", m.record.StartedAt == nil || m.record.Status == store.StatusEnded),
		actionButton("[Q] Exit", false),
		actionButton("[N] Sound on/off", false),
	}, "\n")
}

func (m *Model) addOnWindowOpen() bool {
	if m.record.StartedAt == nil || m.record.Checkpoint.SegmentFinished || m.record.Checkpoint.SegmentIndex < 1 {
		return false
	}
	ordinal := 0
	for index, segment := range m.record.Config.Schedule {
		if segment.Kind != config.SegmentBreak {
			continue
		}
		ordinal++
		if ordinal == m.record.Config.Registration.AddOnBreak {
			return index+1 == m.record.Checkpoint.SegmentIndex
		}
	}
	return false
}

func actionButton(label string, disabled bool) string {
	style := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	if disabled {
		style = style.Foreground(lipgloss.Color("241")).Background(lipgloss.Color("235"))
	} else {
		style = style.Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24"))
	}
	return style.Render(label)
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

func (m *Model) nextLevelLabel() string {
	if m.record.Checkpoint.PendingIndex > 0 && m.record.Checkpoint.PendingIndex <= len(m.record.Config.Schedule) {
		segment := m.record.Config.Schedule[m.record.Checkpoint.PendingIndex-1]
		return fmt.Sprintf("%d/%d%s", segment.SmallBlind, segment.BigBlind, holdSuffix(m.record.Checkpoint.HoldReason))
	}
	for index := m.record.Checkpoint.SegmentIndex; index < len(m.record.Config.Schedule); index++ {
		segment := m.record.Config.Schedule[index]
		if segment.Kind == config.SegmentLevel {
			return fmt.Sprintf("%d/%d", segment.SmallBlind, segment.BigBlind)
		}
	}
	return "Final level / hold"
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
	var target *config.Segment
	for index := m.record.Checkpoint.SegmentIndex; index < len(m.record.Config.Schedule); index++ {
		segment := m.record.Config.Schedule[index]
		if segment.Kind == config.SegmentBreak {
			breakNo++
			label := segment.Label
			if label == "" {
				label = fmt.Sprintf("Break %d", breakNo)
			}
			target = &segment
			return fmt.Sprintf("%s in %s (clock time)", label, m.untilNextBreak(index))
		}
	}
	if target == nil {
		return "none configured"
	}
	return "unknown"
}

func (m *Model) untilNextBreak(targetIndex int) string {
	if m.record.Checkpoint.SegmentFinished {
		return "unknown"
	}
	remaining := m.record.Checkpoint.Remaining
	for index := m.record.Checkpoint.SegmentIndex; index < targetIndex; index++ {
		if index == m.record.Checkpoint.SegmentIndex-1 {
			continue
		}
		if m.record.Config.Schedule[index].Kind == config.SegmentLevel || m.record.Config.Schedule[index].Kind == config.SegmentBreak {
			remaining += m.record.Config.Schedule[index].Duration.Value()
		}
	}
	return formatDuration(remaining)
}

func formatAnte(ante config.AnteConfig) string {
	switch ante.Kind {
	case config.AnteBigBlind:
		return fmt.Sprintf("Big-blind ante  %s", formatChips(ante.Amount))
	case config.AntePerPlayer:
		return fmt.Sprintf("Per-player ante %s", formatChips(ante.Amount))
	default:
		return "Ante  none"
	}
}

func statusLabel(status store.TournamentStatus) string {
	switch status {
	case store.StatusRunning:
		return goodStyle.Render("RUNNING")
	case store.StatusPaused:
		return warnStyle.Render("PAUSED")
	case store.StatusEnded:
		return badStyle.Render("ENDED")
	default:
		return "READY — settings unlocked"
	}
}

func renderStatus(message string, isError bool) string {
	if isError {
		return badStyle.Render("Error: " + message)
	}
	return goodStyle.Render(message)
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

func holdSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (held)"
}

func truncate(value string, length int) string {
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	if length <= 1 {
		return string(runes[:length])
	}
	return string(runes[:length-1]) + "…"
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
	}
	return strings.Join(notices, " · ")
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
