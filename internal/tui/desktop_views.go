package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type desktopHome struct {
	title         *canvas.Text
	entrants      *widget.Label
	active        *widget.Label
	sittingOut    *widget.Label
	busts         *widget.Label
	buyIns        *widget.Label
	rebuys        *widget.Label
	addOns        *widget.Label
	totalStack    *widget.Label
	averageStack  *widget.Label
	prizePool     *widget.Label
	prizeRows     *fyne.Container
	leftScroll    *container.Scroll
	blinds        *fitText
	timer         *fitText
	status        *fitText
	breakReminder *canvas.Text
	message       *widget.Label
	nextLevel     *coloredLine
	nextBreak     *coloredLine
	localTime     *widget.Label
	fullscreen    *widget.Button
	actions       map[rune]*widget.Button
}

func (d *desktop) mainView() (fyne.CanvasObject, *desktopHome) {
	h := &desktopHome{
		entrants: widget.NewLabel(""), active: widget.NewLabel(""), sittingOut: widget.NewLabel(""), busts: widget.NewLabel(""),
		buyIns: widget.NewLabel(""), rebuys: widget.NewLabel(""), addOns: widget.NewLabel(""),
		totalStack: widget.NewLabel(""), averageStack: widget.NewLabel(""), prizePool: widget.NewLabel(""),
		prizeRows: container.New(layout.NewCustomPaddedVBoxLayout(0)), timer: newFitText("", Spacedust.Foreground, 460, true),
		status: newFitText("", Spacedust.Green, 64, true), breakReminder: canvas.NewText("", Spacedust.Yellow),
		message: widget.NewLabel(""), nextLevel: newColoredLine(2, 21, 16),
		nextBreak: newColoredLine(5, 21, 16),
		localTime: widget.NewLabel(""),
		actions:   make(map[rune]*widget.Button),
	}
	h.title = canvas.NewText("", Spacedust.Accent)
	h.title.Alignment = fyne.TextAlignCenter
	h.title.TextSize = 34
	h.title.TextStyle = fyne.TextStyle{Bold: true}
	h.message.Hide()

	left := container.New(&minimumWidthLayout{width: 242}, h.leftColumn(d.model.record.Config.ChipValues))
	right := container.New(&minimumWidthLayout{width: 220}, d.actionColumn(h))
	center := h.clockColumn()
	header := container.NewPadded(container.NewCenter(h.title))
	h.fullscreen = d.fullscreenButton()
	h.localTime.Alignment = fyne.TextAlignTrailing
	h.localTime.TextStyle = fyne.TextStyle{Monospace: true}
	h.message.Wrapping = fyne.TextWrapOff
	h.message.Truncation = fyne.TextTruncateEllipsis
	footerMessage := container.NewThemeOverride(h.message, newCondensedTheme())
	footerLine := container.New(bottomFooterLayout{}, footerMessage, h.localTime, h.fullscreen)
	footer := container.NewPadded(footerLine)
	main := container.NewBorder(header, footer, left, right, container.NewPadded(center))
	h.refresh(d.model)
	return main, h
}

func (h *desktopHome) leftColumn(chipValues []config.ChipValue) fyne.CanvasObject {
	stats := container.New(layout.NewCustomPaddedVBoxLayout(0),
		statRow("Entrants", h.entrants),
		statRow("Active players", h.active),
		statRow("Sitting out", h.sittingOut),
		statRow("Busts", h.busts),
	)
	stackPool := container.New(layout.NewCustomPaddedVBoxLayout(0),
		statRow("Buy-ins", h.buyIns),
		statRow("Rebuys", h.rebuys),
		statRow("Add-ons", h.addOns),
		statRow("Prize pool", h.prizePool),
		statRow("Total stack", h.totalStack),
		statRow("Average stack", h.averageStack),
	)
	sectionItems := []fyne.CanvasObject{
		compactSection("Tournament", stats),
		compactSection("Stack & pool", stackPool),
		compactSection("Prizes", h.prizeRows),
	}
	if len(chipValues) > 0 {
		sectionItems = append(sectionItems, compactSectionWithSpacing("Chips", chipLegend(chipValues), 8))
	}
	sections := container.New(layout.NewCustomPaddedVBoxLayout(4), sectionItems...)
	h.leftScroll = container.NewScroll(sections)
	h.leftScroll.Direction = container.ScrollVerticalOnly
	return h.leftScroll
}

func (h *desktopHome) clockColumn() fyne.CanvasObject {
	blindsTitle := canvas.NewText("BLINDS AND ANTE", Spacedust.Blue)
	blindsTitle.TextSize = 21
	blindsTitle.TextStyle = fyne.TextStyle{Bold: true}
	blindsTitle.Alignment = fyne.TextAlignCenter
	h.blinds = newFitText("", Spacedust.Foreground, 84, true)
	h.blinds.minSize = 28
	h.blinds.preferred = fyne.NewSize(220, 96)
	h.blinds.text.TextStyle = fyne.TextStyle{Bold: true}
	h.status.minSize = 24
	h.status.preferred = fyne.NewSize(220, 64)
	h.status.text.TextStyle = fyne.TextStyle{Bold: true}
	h.breakReminder.Alignment = fyne.TextAlignCenter
	h.breakReminder.TextSize = 18
	h.breakReminder.TextStyle = fyne.TextStyle{Bold: true}
	h.message.Wrapping = fyne.TextWrapWord

	current := container.NewVBox(blindsTitle, h.blinds)
	clock := container.NewMax(h.timer)

	clockFooter := container.NewVBox(
		container.NewMax(h.status),
		container.NewMax(h.nextBreak),
		container.NewMax(h.nextLevel),
		container.NewCenter(h.breakReminder),
	)
	clock = container.NewBorder(nil, clockFooter, nil, nil, clock)
	content := container.NewBorder(current, nil, nil, nil, clock)
	return container.NewPadded(content)
}

func (d *desktop) actionColumn(h *desktopHome) fyne.CanvasObject {
	title := canvas.NewText("ACTIONS", Spacedust.Muted)
	title.TextSize = 14
	title.TextStyle = fyne.TextStyle{Bold: true}
	items := []struct {
		key   rune
		label string
	}{
		{' ', "Start / Pause"}, {'b', "Buy-in"}, {'r', "Rebuy"}, {'a', "Add-on"},
		{'p', "Players"}, {'o', "Sit out / Return"}, {'l', "Levels"}, {'x', "Bust"},
		{'s', "Settings"}, {'?', "Rules"}, {'h', "Hand resolver · later"},
		{'t', "Timeline · later"}, {'n', "Sound"}, {'e', "End tournament"}, {'q', "Exit"},
	}
	buttons := make([]fyne.CanvasObject, 0, len(items)+1)
	buttons = append(buttons, title)
	for _, item := range items {
		key := item.key
		button := d.newButton("", func() {
			if key == ' ' {
				d.dispatchTeaKey(teaKeySpace())
			} else {
				d.activate(key)
			}
		})
		button.Alignment = widget.ButtonAlignLeading
		button.Importance = widget.MediumImportance
		h.actions[key] = button
		buttons = append(buttons, button)
	}
	scroll := container.NewScroll(container.NewVBox(buttons...))
	scroll.Direction = container.ScrollVerticalOnly
	return container.NewBorder(nil, nil, nil, nil, scroll)
}

func (d *desktop) fullscreenButton() *widget.Button {
	button := d.newButton("⛶  F11", func() {
		d.window.SetFullScreen(!d.window.FullScreen())
		d.window.Canvas().Focus(d.keys)
	})
	button.Importance = widget.LowImportance
	return button
}

func (h *desktopHome) refresh(m *Model) {
	name := strings.TrimSpace(m.record.Name)
	if name == "" {
		name = "Poker Clock"
	}
	h.title.Text = name
	h.title.Refresh()
	h.entrants.SetText(fmt.Sprintf("%d", m.summary.Entrants))
	h.active.SetText(fmt.Sprintf("%d", m.summary.ActivePlayers))
	sittingOut := 0
	for _, player := range m.players {
		if player.Active && !player.Removed && player.SittingOut {
			sittingOut++
		}
	}
	h.sittingOut.SetText(fmt.Sprintf("%d", sittingOut))
	h.busts.SetText(fmt.Sprintf("%d / %d", m.summary.CurrentBusts, m.summary.TotalBusts))
	h.buyIns.SetText(formatPurchaseCount(m.summary.InitialBuyIns, m.summary.InitialBuyInCents, m.record.Config.Purchases.InitialBuyIn.Price))
	h.rebuys.SetText(formatPurchaseCount(m.summary.Rebuys, m.summary.RebuyCents, m.record.Config.Purchases.Rebuy.Price))
	h.addOns.SetText(formatPurchaseCount(m.summary.AddOns, m.summary.AddOnCents, m.record.Config.Purchases.AddOn.Price))
	h.totalStack.SetText(formatDisplayChips(m.summary.TotalStack))
	h.averageStack.SetText(formatDisplayChips(roundedAverageStack(m.summary.AverageStack, m.record.Config.ChipValues)))
	h.prizePool.SetText(m.summary.PrizePoolCents.String())

	h.prizeRows.Objects = nil
	for _, prize := range m.placePrizes {
		place := prize.Place
		label := "Winner"
		if place > 1 {
			label = ordinalPlace(place)
		}
		amountLabel := widget.NewLabel(config.EuroCents(prize.AmountCents).String())
		row := statRow(label, amountLabel)
		paid := place > int(m.summary.ActivePlayers)
		if m.record.Status == store.StatusEnded && m.record.ResultCompleted != nil && *m.record.ResultCompleted {
			paid = true
		}
		if paid {
			h.prizeRows.Add(struckThroughObject(row))
			continue
		}
		h.prizeRows.Add(row)
	}
	if len(m.placePrizes) == 0 {
		h.prizeRows.Add(mutedLabel("No valid entrants"))
	}
	h.prizeRows.Refresh()

	segment := currentDesktopSegment(m)
	reminder := ""
	if segment != nil && segment.Kind == config.SegmentBreak && !m.record.Checkpoint.SegmentFinished {
		reminder = formatChipUpReminder(segment.Reminder)
	}
	h.breakReminder.Text = reminder
	if reminder == "" {
		h.breakReminder.Hide()
	} else {
		h.breakReminder.Show()
	}
	h.breakReminder.Refresh()
	if segment == nil {
		h.blinds.setText("— / — / —")
	} else if segment.Kind == config.SegmentBreak && !m.record.Checkpoint.SegmentFinished {
		h.blinds.setText("BREAK")
	} else {
		if segment.Kind == config.SegmentBreak {
			if previous := m.previousLevel(); previous != nil {
				segment = previous
			}
		}
		ante := int64(0)
		if segment.Ante.Kind != config.AnteNone {
			ante = segment.Ante.Amount
		}
		h.blinds.setText(formatDisplayChips(segment.SmallBlind) + "  /  " + formatDisplayChips(segment.BigBlind) + "  /  " + formatDisplayChips(ante))
	}

	timer := "READY"
	if m.record.Checkpoint.HoldReason != "" {
		timer = "HOLD"
	} else if m.record.Status != store.StatusNotStarted {
		timer = formatDuration(m.record.Checkpoint.Remaining)
	}
	h.timer.setText(timer)
	status := desktopStatus(m, segment)
	h.status.text.Color = Spacedust.Teal
	switch m.record.Status {
	case store.StatusRunning:
		h.status.text.Color = Spacedust.Green
	case store.StatusPaused:
		h.status.text.Color = Spacedust.Yellow
	case store.StatusEnded:
		h.status.text.Color = Spacedust.Red
	}
	h.status.setText(status)
	h.status.Refresh()
	h.nextLevel.setParts(desktopNextBlindsParts(m))
	nextBreak := desktopNextBreakParts(m, m.summary.AverageStack, h.status)
	h.nextBreak.setParts(nextBreak)
	if len(nextBreak) == 0 {
		h.nextBreak.Hide()
	} else {
		h.nextBreak.Show()
	}
	h.localTime.SetText(time.Now().Format("15:04:05"))
	var messages []string
	if notice := strings.TrimSpace(m.notice); notice != "" {
		messages = append(messages, notice)
	}
	if status := strings.TrimSpace(m.statusMessage); status != "" {
		messages = append(messages, status)
	}
	message := strings.Join(messages, "  ·  ")
	h.message.SetText(message)
	if m.statusIsError {
		h.message.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		h.message.TextStyle = fyne.TextStyle{}
	}
	if message == "" {
		h.message.Hide()
	} else {
		h.message.Show()
	}
	h.updateActions(m)
}

func (h *desktopHome) updateActions(m *Model) {
	ended := m.record.Status == store.StatusEnded
	closed := m.record.RegistrationClosed || ended
	active := func(key rune) *widget.Button { return h.actions[key] }
	set := func(key rune, label string, disabled bool, important bool) {
		button := active(key)
		if button == nil {
			return
		}
		button.SetText(label)
		button.Importance = widget.MediumImportance
		if important {
			button.Importance = widget.HighImportance
		}
		if disabled {
			button.Disable()
		} else {
			button.Enable()
		}
		button.Refresh()
	}

	clockLabel := "Start tournament"
	clockEnabled := !ended
	clockOn := false
	switch m.record.Status {
	case store.StatusRunning:
		clockLabel, clockOn = "Pause clock", true
	case store.StatusPaused:
		clockLabel = "Resume clock"
	case store.StatusEnded:
		clockLabel = "Tournament ended"
	}
	set(' ', "SPACE  ·  "+clockLabel, !clockEnabled, clockOn)
	buyInLabel := "B  ·  Buy-in"
	if closed {
		buyInLabel += "  ·  CLOSED"
	}
	set('b', buyInLabel, closed, false)
	rebuyDisabled := !m.record.Config.Registration.RebuysEnabled || closed
	rebuyLabel := "R  ·  Rebuy"
	if !m.record.Config.Registration.RebuysEnabled {
		rebuyLabel += "  ·  OFF"
	} else if closed {
		rebuyLabel += "  ·  CLOSED"
	}
	set('r', rebuyLabel, rebuyDisabled, false)
	addonDisabled := ended || !m.record.Config.Registration.AddOnsEnabled || !m.addOnWindowOpen()
	addonLabel := "A  ·  Add-on"
	if !m.record.Config.Registration.AddOnsEnabled {
		addonLabel += "  ·  OFF"
	} else if ended || m.record.RegistrationClosed {
		addonLabel += "  ·  CLOSED"
	} else if addonDisabled {
		addonLabel += "  ·  BREAK ONLY"
	}
	set('a', addonLabel, addonDisabled, false)
	set('p', "P  ·  Players", ended, false)
	set('o', "O  ·  Sit out / return", ended, false)
	set('l', "L  ·  Levels", m.record.StartedAt == nil || ended, false)
	set('x', "X  ·  Bust", m.record.StartedAt == nil || ended, false)
	set('s', "S  ·  Settings", false, false)
	set('?', "?  ·  Rules", false, false)
	set('h', "H  ·  Hand resolver  ·  later", true, false)
	set('t', "T  ·  Timeline  ·  later", true, false)
	soundLabel := "N  ·  Sound  ·  OFF"
	if m.prefs.SoundEnabled {
		soundLabel = "N  ·  Sound  ·  ON"
	}
	set('n', soundLabel, false, m.prefs.SoundEnabled)
	set('e', "E  ·  End tournament", m.record.StartedAt == nil || ended, false)
	set('q', "Q  ·  Exit", false, false)
}

func (d *desktop) startupView() fyne.CanvasObject {
	title := canvas.NewText("POKER CLOCK", Spacedust.Accent)
	title.TextSize = 48
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter
	subtitle := canvas.NewText("A quieter way to run tournament night", Spacedust.Muted)
	subtitle.TextSize = 20
	subtitle.Alignment = fyne.TextAlignCenter
	newTournament := d.newButton("New tournament (N)", func() { d.activate('n') })
	loadTournament := d.newButton("Load tournament (L)", func() { d.activate('l') })
	quit := d.newButton("Quit (Q)", func() { d.activate('q') })
	choices := []*widget.Button{newTournament, loadTournament, quit}
	d.startupButtons = choices
	for index, button := range choices {
		button.Alignment = widget.ButtonAlignCenter
		button.Importance = widget.MediumImportance
		if index == d.model.startupCursor {
			button.Importance = widget.HighImportance
		}
	}
	buttons := container.NewGridWithColumns(3, newTournament, loadTournament, quit)
	body := container.NewVBox(container.NewCenter(title), container.NewCenter(subtitle), layout.NewSpacer(), buttons, layout.NewSpacer(), d.statusNotice())
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(820, 400), softCard("Welcome", body)))
}

func (d *desktop) rulesView() fyne.CanvasObject {
	title := sectionLabel("TEXAS HOLD’EM REFERENCE")
	body := widget.NewRichTextFromMarkdown(d.model.rulesText)
	body.Wrapping = fyne.TextWrapWord
	d.rulesScroll = container.NewVScroll(body)
	maxWidth := fyne.MeasureText(strings.Repeat("0", 120), 16, fyne.TextStyle{}).Width
	content := container.New(&maxWidthLayout{width: maxWidth}, d.rulesScroll)
	close := d.newButton("Back to tournament  ·  Esc", func() { d.dispatchTeaKey(teaKeyEsc()) })
	close.Importance = widget.HighImportance
	return container.NewBorder(container.NewPadded(title), container.NewPadded(close), nil, nil, content)
}

func currentDesktopSegment(m *Model) *config.Segment {
	if m.service == nil {
		return nil
	}
	return m.currentSegment()
}

func currentBreakNumber(m *Model) int {
	count := 0
	limit := m.record.Checkpoint.SegmentIndex
	if limit > len(m.record.Config.Schedule) {
		limit = len(m.record.Config.Schedule)
	}
	for i := 0; i < limit; i++ {
		if m.record.Config.Schedule[i].Kind == config.SegmentBreak {
			count++
		}
	}
	return count
}

func currentLevelNumber(m *Model) int {
	count := 0
	limit := m.record.Checkpoint.SegmentIndex
	if limit > len(m.record.Config.Schedule) {
		limit = len(m.record.Config.Schedule)
	}
	for i := 0; i < limit; i++ {
		if m.record.Config.Schedule[i].Kind == config.SegmentLevel {
			count++
		}
	}
	return count
}

func desktopNextBlindsParts(m *Model) []coloredLinePart {
	white := Spacedust.Foreground
	return []coloredLinePart{
		{text: "NEXT BLINDS", color: Spacedust.Blue, bold: true},
		{text: " " + desktopNextBlinds(m), color: white, bold: true},
	}
}

func roundedAverageStack(average int64, chips []config.ChipValue) int64 {
	minimum := int64(0)
	for _, chip := range chips {
		if chip.Value > 0 && (minimum == 0 || chip.Value < minimum) {
			minimum = chip.Value
		}
	}
	if minimum == 0 {
		return average
	}
	return average / minimum * minimum
}

func desktopNextBreakParts(m *Model, averageStack int64, status *fitText) []coloredLinePart {
	label := m.nextBreakLabel()
	if strings.EqualFold(label, "none configured") {
		return nil
	}
	if index := strings.LastIndex(label, " in "); index >= 0 {
		label = strings.TrimSpace(label[index+4:])
	}
	white := Spacedust.Foreground
	averageStack = roundedAverageStack(averageStack, m.record.Config.ChipValues)
	return []coloredLinePart{
		{text: "NEXT BREAK", color: Spacedust.Blue, bold: true},
		{text: label, color: white, bold: true, fontSource: status, monospace: true, spaceBefore: true},
		{text: "  ·  ", color: Spacedust.Muted},
		{text: "AVG. STACK", color: Spacedust.Blue, bold: true},
		{text: formatDisplayChips(averageStack), color: white, bold: true, fontSource: status, spaceBefore: true},
	}
}

func desktopNextBreak(m *Model, averageStack int64) string {
	var value strings.Builder
	for _, part := range desktopNextBreakParts(m, averageStack, nil) {
		if part.spaceBefore {
			value.WriteByte(' ')
		}
		value.WriteString(part.text)
	}
	return value.String()
}

func desktopNextBlinds(m *Model) string {
	var segment *config.Segment
	if index := m.record.Checkpoint.PendingIndex; index > 0 && index <= len(m.record.Config.Schedule) {
		segment = &m.record.Config.Schedule[index-1]
	} else {
		start := m.record.Checkpoint.SegmentIndex
		if start < 0 {
			start = 0
		}
		for i := start; i < len(m.record.Config.Schedule); i++ {
			if m.record.Config.Schedule[i].Kind == config.SegmentLevel {
				segment = &m.record.Config.Schedule[i]
				break
			}
		}
	}
	if segment == nil {
		return "Final level / hold"
	}
	ante := int64(0)
	if segment.Ante.Kind != config.AnteNone {
		ante = segment.Ante.Amount
	}
	label := fmt.Sprintf("%s / %s / %s", formatDisplayChips(segment.SmallBlind), formatDisplayChips(segment.BigBlind), formatDisplayChips(ante))
	if m.record.Checkpoint.HoldReason != "" {
		label += " · held"
	}
	return label
}

func desktopStatus(m *Model, segment *config.Segment) string {
	if segment != nil && segment.Kind == config.SegmentBreak && !m.record.Checkpoint.SegmentFinished && m.record.Status != store.StatusEnded {
		label := fmt.Sprintf("BREAK %d", currentBreakNumber(m))
		if m.record.Status == store.StatusPaused {
			return label + " PAUSED"
		}
		return label
	}
	if segment != nil && segment.Kind == config.SegmentLevel && (m.record.Status == store.StatusRunning || m.record.Status == store.StatusPaused) {
		state := "RUNNING"
		if m.record.Status == store.StatusPaused {
			state = "PAUSED"
		}
		return fmt.Sprintf("LEVEL %d %s", currentLevelNumber(m), state)
	}
	switch m.record.Status {
	case store.StatusRunning:
		return "RUNNING"
	case store.StatusPaused:
		return "PAUSED"
	case store.StatusEnded:
		return "TOURNAMENT ENDED"
	default:
		return "READY"
	}
}

func formatDisplayChips(value int64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	}
	if value >= 1_000 && value%1_000 == 0 {
		return fmt.Sprintf("%dk", value/1_000)
	}
	if value >= 10_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return formatChips(value)
}

func ordinalPlace(place int) string {
	suffix := "th"
	if place%100 < 11 || place%100 > 13 {
		switch place % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s place", place, suffix)
}

func statRow(label string, value fyne.CanvasObject) fyne.CanvasObject {
	key := widget.NewLabel(label)
	key.TextStyle = fyne.TextStyle{Bold: true}
	if valueLabel, ok := value.(*widget.Label); ok {
		valueLabel.Alignment = fyne.TextAlignTrailing
	}
	return container.NewBorder(nil, nil, key, nil, value)
}

func formatPurchaseCount(count int64, total, configured config.EuroCents) string {
	unit := configured
	if count > 0 {
		unit = total / config.EuroCents(count)
	}
	return fmt.Sprintf("%d @ %s", count, unit)
}

func struckThroughObject(content fyne.CanvasObject) fyne.CanvasObject {
	line := canvas.NewLine(Spacedust.Muted)
	line.StrokeWidth = 1
	return container.New(strikeThroughRowLayout{}, content, line)
}

type strikeThroughRowLayout struct{}

func (strikeThroughRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}
func (strikeThroughRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	content, line := objects[0], objects[1].(*canvas.Line)
	content.Move(fyne.NewPos(0, 0))
	content.Resize(size)
	width := size.Width
	y := size.Height / 2
	line.Position1 = fyne.NewPos(0, y)
	line.Position2 = fyne.NewPos(width, y)
	line.StrokeWidth = 1
	line.Refresh()
}

func sectionLabel(value string) fyne.CanvasObject {
	label := canvas.NewText(value, Spacedust.Teal)
	label.TextSize = 13
	label.TextStyle = fyne.TextStyle{Bold: true}
	return label
}

func mutedLabel(value string) fyne.CanvasObject {
	label := widget.NewLabel(value)
	label.TextStyle = fyne.TextStyle{Italic: true}
	return label
}

func softCard(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return widget.NewCard(title, "", content)
}

func compactSection(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return compactSectionWithSpacing(title, content, 2)
}

func compactSectionWithSpacing(title string, content fyne.CanvasObject, spacing float32) fyne.CanvasObject {
	heading := canvas.NewText(title, Spacedust.Blue)
	heading.TextSize = 20
	heading.TextStyle = fyne.TextStyle{Bold: true}
	return container.New(layout.NewCustomPaddedVBoxLayout(spacing), heading, content)
}

func chipLegend(values []config.ChipValue) fyne.CanvasObject {
	tokens := make([]fyne.CanvasObject, 0, len(values))
	for _, chip := range values {
		fill, err := config.ParseChipColor(chip.Color)
		if err != nil {
			continue
		}
		tokens = append(tokens, newChipToken(formatDisplayChips(chip.Value), fill))
	}
	if len(tokens) == 0 {
		return mutedLabel("No chip values")
	}
	rows := make([]fyne.CanvasObject, 0, (len(tokens)+2)/3)
	for start := 0; start < len(tokens); start += 3 {
		end := min(start+3, len(tokens))
		row := container.NewHBox(tokens[start:end]...)
		rows = append(rows, container.NewCenter(row))
	}
	return container.NewVBox(rows...)
}

type chipToken struct {
	widget.BaseWidget
	value string
	fill  color.NRGBA
}

func newChipToken(value string, fill color.NRGBA) *chipToken {
	token := &chipToken{value: value, fill: fill}
	token.ExtendBaseWidget(token)
	return token
}

func (t *chipToken) CreateRenderer() fyne.WidgetRenderer {
	foreground := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if float64(t.fill.R)*0.299+float64(t.fill.G)*0.587+float64(t.fill.B)*0.114 >= 155 {
		foreground = color.NRGBA{A: 0xff}
	}
	circle := canvas.NewCircle(t.fill)
	circle.StrokeColor = color.NRGBA{R: 0x20, G: 0x2a, B: 0x2a, A: 0xff}
	circle.StrokeWidth = 1
	label := canvas.NewText(t.value, foreground)
	label.TextSize = 15
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Alignment = fyne.TextAlignCenter
	return &chipTokenRenderer{token: t, circle: circle, label: label, objects: []fyne.CanvasObject{circle, label}}
}

func (t *chipToken) MinSize() fyne.Size { return fyne.NewSize(48, 48) }

type chipTokenRenderer struct {
	token   *chipToken
	circle  *canvas.Circle
	label   *canvas.Text
	objects []fyne.CanvasObject
}

func (r *chipTokenRenderer) Layout(size fyne.Size) {
	diameter := fyne.Min(size.Width, size.Height)
	position := fyne.NewPos((size.Width-diameter)/2, (size.Height-diameter)/2)
	r.circle.Move(position)
	r.circle.Resize(fyne.NewSize(diameter, diameter))
	textSize := fyne.MeasureText(r.label.Text, r.label.TextSize, r.label.TextStyle)
	r.label.Move(fyne.NewPos(position.X+(diameter-textSize.Width)/2, position.Y+(diameter-textSize.Height)/2))
	r.label.Resize(textSize)
}

func (r *chipTokenRenderer) MinSize() fyne.Size { return r.token.MinSize() }
func (r *chipTokenRenderer) Refresh() {
	r.circle.FillColor = r.token.fill
	canvas.Refresh(r.circle)
	canvas.Refresh(r.label)
}
func (r *chipTokenRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *chipTokenRenderer) Destroy()                     {}

type coloredLinePart struct {
	text        string
	color       color.NRGBA
	bold        bool
	monospace   bool
	fontSize    float32
	fontSource  *fitText
	spaceBefore bool
}

type coloredLine struct {
	widget.BaseWidget
	maxParts    int
	maxTextSize float32
	minTextSize float32
	parts       []coloredLinePart
}

func newColoredLine(maxParts int, maxTextSize, minTextSize float32) *coloredLine {
	line := &coloredLine{maxParts: maxParts, maxTextSize: maxTextSize, minTextSize: minTextSize}
	line.ExtendBaseWidget(line)
	return line
}

func (l *coloredLine) MinSize() fyne.Size {
	fontSize := l.maxTextSize
	for _, part := range l.parts {
		if part.fontSize > fontSize {
			fontSize = part.fontSize
		}
		if part.fontSource != nil && part.fontSource.text.TextSize > fontSize {
			fontSize = part.fontSource.text.TextSize
		}
	}
	return fyne.NewSize(220, fontSize*1.4)
}

func (l *coloredLine) setParts(parts []coloredLinePart) {
	l.parts = parts
	l.Refresh()
}

func (l *coloredLine) CreateRenderer() fyne.WidgetRenderer {
	texts := make([]*canvas.Text, l.maxParts)
	objects := make([]fyne.CanvasObject, l.maxParts)
	for index := range texts {
		texts[index] = canvas.NewText("", Spacedust.Foreground)
		objects[index] = texts[index]
	}
	return &coloredLineRenderer{line: l, texts: texts, objects: objects}
}

type coloredLineRenderer struct {
	line    *coloredLine
	texts   []*canvas.Text
	objects []fyne.CanvasObject
}

func (r *coloredLineRenderer) Layout(size fyne.Size) {
	widths := make([]float32, len(r.texts))
	heights := make([]float32, len(r.texts))
	baselines := make([]float32, len(r.texts))
	gaps := make([]float32, len(r.texts))
	fontSizes := make([]float32, len(r.texts))
	styles := make([]fyne.TextStyle, len(r.texts))
	measure := func() float32 {
		total := float32(0)
		for index, text := range r.texts {
			if index >= len(r.line.parts) || r.line.parts[index].text == "" {
				widths[index], heights[index] = 0, 0
				continue
			}
			part := r.line.parts[index]
			gaps[index] = 0
			fontSize := part.fontSize
			if fontSize == 0 {
				fontSize = r.line.maxTextSize
			}
			if part.fontSource != nil && part.fontSource.text.TextSize > 0 {
				fontSize = part.fontSource.text.TextSize
			}
			if fontSizes[index] == 0 {
				fontSizes[index] = fontSize
			}
			styles[index] = fyne.TextStyle{Bold: part.bold, Monospace: part.monospace}
			text.TextSize = fontSizes[index]
			text.TextStyle = styles[index]
			measured, baseline := fyne.CurrentApp().Driver().RenderedTextSize(part.text, fontSizes[index], styles[index], nil)
			widths[index], heights[index], baselines[index] = measured.Width, measured.Height, baseline
			if part.spaceBefore {
				gaps[index] = fyne.MeasureText(" ", fontSizes[index], fyne.TextStyle{Bold: part.bold}).Width
			}
			total += gaps[index] + measured.Width
		}
		return total
	}
	totalWidth := measure()
	for totalWidth > size.Width {
		shrunk := false
		for index, part := range r.line.parts {
			if part.text != "" && index < len(fontSizes) && fontSizes[index] > r.line.minTextSize {
				fontSizes[index]--
				shrunk = true
			}
		}
		if !shrunk {
			break
		}
		totalWidth = measure()
	}
	x := (size.Width - totalWidth) / 2
	maxHeight, anchor := float32(0), 0
	for index, height := range heights {
		if height > maxHeight {
			maxHeight, anchor = height, index
		}
	}
	lineTop := (size.Height - maxHeight) / 2
	targetBaseline := lineTop + baselines[anchor]
	for index, text := range r.texts {
		if index >= len(r.line.parts) || r.line.parts[index].text == "" {
			text.Hide()
			continue
		}
		text.Show()
		text.Text = r.line.parts[index].text
		text.Color = r.line.parts[index].color
		text.TextSize = fontSizes[index]
		text.TextStyle = styles[index]
		x += gaps[index]
		text.Move(fyne.NewPos(x, targetBaseline-baselines[index]))
		text.Resize(fyne.NewSize(widths[index], heights[index]))
		canvas.Refresh(text)
		x += widths[index]
	}
}

func (r *coloredLineRenderer) MinSize() fyne.Size { return r.line.MinSize() }
func (r *coloredLineRenderer) Refresh() {
	r.Layout(r.line.Size())
}
func (r *coloredLineRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *coloredLineRenderer) Destroy()                     {}

// fitText scales the clock display to the space offered by its parent while keeping
// the numeral-heavy font legible at both fullscreen and windowed sizes.
type bottomFooterLayout struct{}

func (bottomFooterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}
	messageSize, timeSize, buttonSize := objects[0].MinSize(), objects[1].MinSize(), objects[2].MinSize()
	height := fyne.Max(messageSize.Height, fyne.Max(timeSize.Height, buttonSize.Height))
	return fyne.NewSize(160+timeSize.Width+buttonSize.Width+16, height)
}
func (bottomFooterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	message, localTime, fullscreen := objects[0], objects[1], objects[2]
	timeSize, buttonSize := localTime.MinSize(), fullscreen.MinSize()
	messageWidth := size.Width - timeSize.Width - buttonSize.Width - 16
	if messageWidth < 0 {
		messageWidth = 0
	}
	message.Move(fyne.NewPos(0, 0))
	message.Resize(fyne.NewSize(messageWidth, size.Height))
	localTime.Move(fyne.NewPos(messageWidth+8, (size.Height-timeSize.Height)/2))
	localTime.Resize(timeSize)
	fullscreen.Move(fyne.NewPos(size.Width-buttonSize.Width, (size.Height-buttonSize.Height)/2))
	fullscreen.Resize(buttonSize)
}

type maxWidthLayout struct{ width float32 }

func (l *maxWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

func (l *maxWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	width := fyne.Min(size.Width, l.width)
	objects[0].Move(fyne.NewPos((size.Width-width)/2, 0))
	objects[0].Resize(fyne.NewSize(width, size.Height))
}

type minimumWidthLayout struct{ width float32 }

func (l *minimumWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	height := float32(0)
	if len(objects) > 0 {
		height = objects[0].MinSize().Height
	}
	return fyne.NewSize(l.width, height)
}
func (l *minimumWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

type fitText struct {
	widget.BaseWidget
	text      *canvas.Text
	maxSize   float32
	minSize   float32
	preferred fyne.Size
}

func newFitText(value string, foreground color.Color, max float32, bold bool) *fitText {
	text := canvas.NewText(value, foreground)
	text.Alignment = fyne.TextAlignCenter
	text.TextStyle = fyne.TextStyle{Bold: bold, Monospace: true}
	f := &fitText{text: text, maxSize: max, minSize: 56, preferred: fyne.NewSize(220, 120)}
	f.ExtendBaseWidget(f)
	return f
}

func (f *fitText) setText(value string) {
	if f.text.Text == value {
		return
	}
	f.text.Text = value
	f.Refresh()
}

func (f *fitText) CreateRenderer() fyne.WidgetRenderer {
	return &fitTextRenderer{fit: f, objects: []fyne.CanvasObject{f.text}}
}

type fitTextRenderer struct {
	fit     *fitText
	objects []fyne.CanvasObject
}

func (r *fitTextRenderer) Layout(size fyne.Size) {
	text := r.fit.text
	fontSize := r.fit.maxSize
	available := fyne.NewSize(size.Width*0.9, size.Height*0.9)
	for fontSize > r.fit.minSize {
		measure := fyne.MeasureText(text.Text, fontSize, text.TextStyle)
		if measure.Width <= available.Width && measure.Height <= available.Height {
			break
		}
		fontSize -= 4
	}
	text.TextSize = fontSize
	text.Resize(size)
	text.Move(fyne.NewPos(0, 0))
}
func (r *fitTextRenderer) MinSize() fyne.Size { return r.fit.preferred }
func (r *fitTextRenderer) Refresh() {
	r.Layout(r.fit.Size())
	canvas.Refresh(r.fit.text)
}
func (r *fitTextRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *fitTextRenderer) Destroy()                     {}

func teaKeySpace() tea.Key { return tea.Key{Type: tea.KeySpace} }
func teaKeyEsc() tea.Key   { return tea.Key{Type: tea.KeyEsc} }
