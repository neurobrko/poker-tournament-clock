package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

type desktopHome struct {
	title             *canvas.Text
	entrants          *widget.Label
	active            *widget.Label
	sittingOut        *widget.Label
	busts             *widget.Label
	buyIns            *widget.Label
	rebuys            *widget.Label
	addOns            *widget.Label
	totalStack        *widget.Label
	averageStack      *widget.Label
	prizePool         *widget.Label
	prizeRows         *fyne.Container
	prizeKey          string
	chipLegendView    *fyne.Container
	chipLegendSegment int
	infoSize          fyne.ThemeSizeName
	leftScroll        *container.Scroll
	blinds            *fitText
	timer             *fitText
	status            *fitText
	chipUpReminder    *canvas.Text
	reminder          *canvas.Text
	message           *widget.Label
	nextLevel         *coloredLine
	nextBreak         *coloredLine
	dualOneLine       bool
	localTime         *widget.Label
	version           *widget.Label
	fullscreen        *widget.Button
	dualVisible       bool
	actionKey         string
	actions           map[rune]*widget.Button
}

func newDesktopHome() *desktopHome {
	return &desktopHome{
		entrants: widget.NewLabel(""), active: widget.NewLabel(""), sittingOut: widget.NewLabel(""), busts: widget.NewLabel(""),
		buyIns: widget.NewLabel(""), rebuys: widget.NewLabel(""), addOns: widget.NewLabel(""),
		totalStack: widget.NewLabel(""), averageStack: widget.NewLabel(""), prizePool: widget.NewLabel(""),
		prizeRows: container.New(layout.NewCustomPaddedVBoxLayout(0)), timer: newFitText("", Spacedust.Foreground, 460, true),
		status:         newFitText("", Spacedust.Green, 64, true),
		chipUpReminder: canvas.NewText("", Spacedust.Red), reminder: canvas.NewText("", Spacedust.Yellow),
		message: widget.NewLabel(""), nextLevel: newColoredLine(2, 21, 16),
		nextBreak: newColoredLine(5, 21, 16),
		localTime: widget.NewLabel(""), version: widget.NewLabel(BuildVersion),
		title: canvas.NewText("", Spacedust.Accent), actions: make(map[rune]*widget.Button),
	}
}

func (d *desktop) mainView() (fyne.CanvasObject, *desktopHome) {
	h := newDesktopHome()
	h.dualVisible = d.dualVisible
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
	h.version.TextStyle = fyne.TextStyle{Monospace: true}
	h.message.Wrapping = fyne.TextWrapOff
	h.message.Truncation = fyne.TextTruncateEllipsis
	footerMessage := container.NewThemeOverride(h.message, newCondensedTheme())
	footerLine := container.New(bottomFooterLayout{}, footerMessage, h.localTime, h.version, h.fullscreen)
	footer := container.NewPadded(footerLine)
	main := container.NewBorder(header, footer, left, right, container.NewPadded(center))
	h.refresh(d.model)
	return main, h
}

func (d *desktop) dualView() (fyne.CanvasObject, *desktopHome) {
	h := newDesktopHome()
	h.infoSize = theme.SizeNameHeadingText
	h.nextBreak.maxTextSize, h.nextBreak.minTextSize = 21, 16
	h.nextLevel.maxTextSize, h.nextLevel.minTextSize = 21, 16
	h.dualOneLine = true
	h.nextBreak.maxParts = 8
	h.title.Alignment = fyne.TextAlignCenter
	h.title.TextSize = 34
	h.title.TextStyle = fyne.TextStyle{Bold: true}
	center := h.clockColumn()
	blindsSize := float32(90)
	h.blinds.maxSize, h.blinds.minSize = blindsSize, 28
	h.blinds.preferred = fyne.NewSize(280, max(float32(120), blindsSize*1.1))
	lineHeight := blindsSize * 1.2
	h.nextBreak.minHeight = lineHeight
	leftScroll := container.NewVScroll(container.New(windowSideInsetLayout{inset: 16}, container.NewVBox(h.tournamentSection(), h.stackPoolSection())))
	rightSections := []fyne.CanvasObject{h.prizeSection()}
	if chips := h.chipSection(d.model.record.Config.ChipValues); chips != nil {
		rightSections = append(rightSections, chips)
	}
	rightScroll := container.NewVScroll(container.New(windowSideInsetLayout{inset: 16}, container.NewVBox(rightSections...)))
	left := container.New(&minimumWidthLayout{width: 280}, leftScroll)
	right := container.New(&minimumWidthLayout{width: 300}, rightScroll)
	header := container.NewPadded(container.NewCenter(h.title))
	h.localTime.Alignment = fyne.TextAlignLeading
	h.localTime.TextStyle = fyne.TextStyle{Monospace: true}
	d.dualFullscreen = d.newButton("⛶  F11", d.toggleFullScreen)
	d.dualFullscreen.Importance = widget.LowImportance
	d.dualClose = d.newButton("Close  ·  C", d.hideDualDisplay)
	d.dualClose.Importance = widget.LowImportance
	footer := container.NewPadded(container.NewHBox(h.localTime, layout.NewSpacer(), d.dualFullscreen, d.dualClose))
	view := container.NewBorder(header, footer, left, right, container.NewPadded(center))
	h.refresh(d.model)
	return view, h
}

func (h *desktopHome) tournamentSection() fyne.CanvasObject {
	stats := container.New(layout.NewCustomPaddedVBoxLayout(0),
		h.statRow("Entrants", h.entrants),
		h.statRow("Active players", h.active),
		h.statRow("Sitting out", h.sittingOut),
		h.statRow("Busts", h.busts),
	)
	return h.section("Tournament", stats)
}

func (h *desktopHome) stackPoolSection() fyne.CanvasObject {
	stackPool := container.New(layout.NewCustomPaddedVBoxLayout(0),
		h.statRow("Buy-ins", h.buyIns),
		h.statRow("Rebuys", h.rebuys),
		h.statRow("Add-ons", h.addOns),
		h.statRow("Prize pool", h.prizePool),
		h.statRow("Total stack", h.totalStack),
		h.statRow("Average stack", h.averageStack),
	)
	return h.section("Stack & pool", stackPool)
}

func (h *desktopHome) prizeSection() fyne.CanvasObject { return h.section("Prizes", h.prizeRows) }

func (h *desktopHome) chipSection(values []config.ChipValue) fyne.CanvasObject {
	if h.chipLegendView == nil && len(values) > 0 {
		h.chipLegendView = chipLegendWithSize(values, nil, h.infoSize != "")
		h.chipLegendSegment = -1
	}
	if h.chipLegendView == nil {
		return nil
	}
	return h.section("Chips", h.chipLegendView)
}

func (h *desktopHome) section(title string, content fyne.CanvasObject) fyne.CanvasObject {
	if h.infoSize != "" && title == "Chips" {
		return compactSectionWithSize(title, content, 7, 26)
	}
	if h.infoSize != "" {
		return compactSectionWithSize(title, content, 7, 26)
	}
	if title == "Chips" {
		return compactSectionWithSpacing(title, content, 8)
	}
	return compactSection(title, content)
}

func (h *desktopHome) statRow(label string, value fyne.CanvasObject) fyne.CanvasObject {
	return statRowSize(label, value, h.infoSize)
}

func (h *desktopHome) leftColumn(chipValues []config.ChipValue) fyne.CanvasObject {
	sections := []fyne.CanvasObject{h.tournamentSection(), h.stackPoolSection(), h.prizeSection()}
	if chip := h.chipSection(chipValues); chip != nil {
		sections = append(sections, chip)
	}
	content := container.New(layout.NewCustomPaddedVBoxLayout(4), sections...)
	h.leftScroll = container.NewScroll(content)
	h.leftScroll.Direction = container.ScrollVerticalOnly
	return h.leftScroll
}

func (h *desktopHome) initializeClockWidgets() *canvas.Text {
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
	h.chipUpReminder.Alignment = fyne.TextAlignCenter
	h.chipUpReminder.TextSize = 32
	h.chipUpReminder.TextStyle = fyne.TextStyle{Bold: true}
	h.reminder.Alignment = fyne.TextAlignCenter
	h.reminder.TextSize = 18
	h.reminder.TextStyle = fyne.TextStyle{Bold: true}
	h.message.Wrapping = fyne.TextWrapWord
	return blindsTitle
}

func (h *desktopHome) clockColumn() fyne.CanvasObject {
	blindsTitle := h.initializeClockWidgets()
	current := container.NewVBox(blindsTitle, h.blinds)
	clock := container.NewMax(h.timer)

	clockRows := []fyne.CanvasObject{container.NewMax(h.status), container.NewMax(h.nextBreak)}
	if !h.dualOneLine {
		clockRows = append(clockRows, container.NewMax(h.nextLevel))
	}
	clockRows = append(clockRows, container.NewCenter(h.chipUpReminder), container.NewCenter(h.reminder))
	clockFooter := container.NewVBox(clockRows...)
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
		{'p', "Players"}, {'o', "Sit out / Return"}, {'l', "Levels"}, {'x', "Bust"}, {'d', "Dual screen"},
		{'s', "Settings"}, {'?', "Rules"}, {'f', "Final results"}, {'n', "Sound"}, {'e', "End tournament"}, {'q', "Exit"},
	}
	buttons := make([]fyne.CanvasObject, 0, len(items)+1)
	buttons = append(buttons, title)
	for _, item := range items {
		key := item.key
		button := d.newButton("", func() {
			if key == ' ' {
				d.dispatchKey(keyEvent{Type: keySpace})
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
		d.toggleFullScreen()
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
	h.prizePool.SetText(m.summary.PrizePoolCents.String())

	completed := m.record.Status == store.StatusEnded && m.record.ResultCompleted != nil && *m.record.ResultCompleted
	prizeKey := fmt.Sprintf("%v/%d/%t", m.placePrizes, m.summary.ActivePlayers, completed)
	if prizeKey != h.prizeKey {
		h.prizeRows.Objects = nil
		for _, prize := range m.placePrizes {
			label := "Winner"
			if prize.Place > 1 {
				label = ordinalPlace(prize.Place)
			}
			amountLabel := widget.NewLabel(config.EuroCents(prize.AmountCents).String())
			row := statRowSize(label, amountLabel, h.infoSize)
			paid := prize.Place > int(m.summary.ActivePlayers) || completed
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
		h.prizeKey = prizeKey
	}

	h.refreshClock(m)
}

func (h *desktopHome) refreshClock(m *Model) {
	remainingChips, removedChips := chipsAfterChipUps(m.record.Config.ChipValues, m.record.Config.Schedule, m.record.Checkpoint.SegmentIndex)
	h.averageStack.SetText(formatDisplayChips(roundedAverageStack(m.summary.AverageStack, remainingChips)))
	if h.chipLegendView != nil && h.chipLegendSegment != m.record.Checkpoint.SegmentIndex {
		updated := chipLegendWithSize(m.record.Config.ChipValues, removedChips, h.infoSize != "")
		h.chipLegendView.Objects = updated.Objects
		h.chipLegendView.Refresh()
		h.chipLegendSegment = m.record.Checkpoint.SegmentIndex
	}
	segment := currentDesktopSegment(m)
	chipUp, reminder := "", ""
	if segment != nil && !m.record.Checkpoint.SegmentFinished {
		chipUp, reminder = segmentReminderLines(*segment)
	}
	h.chipUpReminder.Text = chipUp
	if chipUp == "" {
		h.chipUpReminder.Hide()
	} else {
		h.chipUpReminder.Show()
	}
	h.chipUpReminder.Refresh()
	h.reminder.Text = reminder
	if reminder == "" {
		h.reminder.Hide()
	} else {
		h.reminder.Show()
	}
	h.reminder.Refresh()
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
	if h.dualOneLine {
		h.nextBreak.setParts(dualNextInfoParts(m))
		h.nextBreak.Show()
	} else {
		nextBreak := desktopNextBreakParts(m, m.summary.AverageStack, h.status)
		h.nextBreak.setParts(nextBreak)
		h.nextLevel.setParts(desktopNextBlindsParts(m))
		if len(nextBreak) == 0 {
			h.nextBreak.Hide()
		} else {
			h.nextBreak.Show()
		}
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
	addonOpen := m.record.Config.Registration.AddOnsEnabled && !ended && tournament.AddOnWindowOpen(m.record.Config, m.record.Checkpoint.SegmentIndex, m.record.Checkpoint.SegmentFinished)
	key := fmt.Sprintf("%s/%t/%t/%t/%t/%t/%t/%t/%t", m.record.Status, m.record.RegistrationClosed, m.record.StartedAt != nil, m.record.Config.Registration.RebuysEnabled, m.record.Config.Registration.AddOnsEnabled, addonOpen, m.finalResultAvailable, m.prefs.SoundEnabled, h.dualVisible)
	if key == h.actionKey {
		return
	}
	h.actionKey = key
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
	addonDisabled := !addonOpen
	addonLabel := "A  ·  Add-on"
	if !m.record.Config.Registration.AddOnsEnabled {
		addonLabel += "  ·  OFF"
	} else if ended {
		addonLabel += "  ·  CLOSED"
	} else if addonDisabled {
		addonLabel += "  ·  BREAK ONLY"
	}
	set('a', addonLabel, addonDisabled, false)
	set('p', "P  ·  Players", false, false)
	set('o', "O  ·  Sit out / return", ended, false)
	set('l', "L  ·  Levels", m.record.StartedAt == nil || ended, false)
	set('x', "X  ·  Bust", m.record.StartedAt == nil || ended, false)
	dualLabel := "D  ·  Dual screen"
	if h.dualVisible {
		dualLabel += "  ·  ON"
	}
	set('d', dualLabel, false, h.dualVisible)
	set('s', "S  ·  Settings", false, false)
	set('?', "?  ·  Rules", false, false)
	set('f', "F  ·  Final results", !m.finalResultAvailable, false)
	soundLabel := "N  ·  Sound  ·  OFF"
	if m.prefs.SoundEnabled {
		soundLabel = "N  ·  Sound  ·  ON"
	}
	set('n', soundLabel, false, m.prefs.SoundEnabled)
	set('e', "E  ·  End tournament", m.record.StartedAt == nil || ended, false)
	set('q', "Q  ·  Exit", false, false)
}

func (d *desktop) startupView() fyne.CanvasObject {
	title := canvas.NewText("Poker Tournament Clock", Spacedust.Accent)
	title.TextSize = 40
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter
	subtitle := canvas.NewText("A quieter way to run tournament night", Spacedust.Muted)
	subtitle.TextSize = 20
	subtitle.Alignment = fyne.TextAlignCenter
	newTournament := d.newButton("New tournament (N)", func() { d.activate('n') })
	loadTournament := d.newButton("Load tournament (L)", func() { d.activate('l') })
	editor := d.newButton("Tournament editor (T)", func() { d.activate('t') })
	quit := d.newButton("Quit (Q)", func() { d.activate('q') })
	choices := []*widget.Button{newTournament, loadTournament, editor, quit}
	d.startupButtons = choices
	for index, button := range choices {
		button.Alignment = widget.ButtonAlignCenter
		button.Importance = widget.MediumImportance
		if index == d.model.startupCursor {
			button.Importance = widget.HighImportance
		}
	}
	buttonColumn := container.NewVBox(newTournament, loadTournament, editor, quit)
	buttons := container.NewCenter(container.New(&widthLimitLayout{width: 420}, buttonColumn))
	footer := canvas.NewText("MIT Licence · Marek Paulik · elvis@elvis.sk · github.com/neurobrko/poker-tournament-clock · "+BuildVersion, Spacedust.Muted)
	footer.TextSize = 12
	footer.Alignment = fyne.TextAlignCenter
	body := container.NewVBox(container.NewCenter(title), container.NewCenter(subtitle), layout.NewSpacer(), container.NewPadded(buttons), layout.NewSpacer(), d.statusNotice())
	card := container.NewCenter(container.NewGridWrap(fyne.NewSize(820, 400), softCard("Welcome", body)))
	return container.NewBorder(nil, container.NewPadded(container.NewCenter(footer)), nil, nil, card)
}

func (d *desktop) rulesView() fyne.CanvasObject {
	title := sectionLabel("TEXAS HOLD’EM REFERENCE")
	body := widget.NewRichTextFromMarkdown(d.model.rulesText)
	body.Wrapping = fyne.TextWrapWord
	d.rulesScroll = container.NewVScroll(body)
	maxWidth := fyne.MeasureText(strings.Repeat("0", 120), 16, fyne.TextStyle{}).Width
	content := container.New(&maxWidthLayout{width: maxWidth}, d.rulesScroll)
	close := d.newButton("Back to tournament  ·  Esc", func() { d.dispatchKey(keyEvent{Type: keyEsc}) })
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

func dualNextInfoParts(m *Model) []coloredLinePart {
	countdown := "—"
	if label := m.nextBreakLabel(); !strings.EqualFold(label, "none configured") {
		countdown = "unknown"
		if index := strings.LastIndex(label, " in "); index >= 0 {
			countdown = compactBreakCountdown(strings.TrimSpace(label[index+4:]))
		}
	}
	remaining, _ := chipsAfterChipUps(m.record.Config.ChipValues, m.record.Config.Schedule, m.record.Checkpoint.SegmentIndex)
	average := formatDisplayChips(roundedAverageStack(m.summary.AverageStack, remaining))
	blinds := desktopNextBlinds(m)
	if blinds == "Final level / hold" {
		blinds = "Final/hold"
	} else {
		blinds = strings.ReplaceAll(blinds, " ", "")
	}
	label := func(text string, spaceBefore bool) coloredLinePart {
		return coloredLinePart{text: text, color: Spacedust.Blue, bold: true, fontSize: 21, spaceBefore: spaceBefore, spaceBeforeSize: 64}
	}
	value := func(text string) coloredLinePart {
		return coloredLinePart{text: text, color: Spacedust.Foreground, bold: true, fontSize: 64, spaceBefore: true, spaceBeforeSize: 64}
	}
	separator := coloredLinePart{text: "·", color: Spacedust.Muted, fontSize: 32, spaceBefore: true, spaceBeforeSize: 64}
	return []coloredLinePart{
		label("NEXT BREAK", false), value(countdown), separator,
		label("AVG. STACK", true), value(average), separator,
		label("NEXT BLINDS", true), value(blinds),
	}
}

func compactBreakCountdown(value string) string {
	parsed, err := time.Parse("15:04:05", value)
	if err != nil {
		parsed, err = time.Parse("04:05", value)
	}
	if err != nil {
		return value
	}
	hours, minutes, seconds := parsed.Clock()
	if hours > 0 {
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", seconds)
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
	remainingChips, _ := chipsAfterChipUps(m.record.Config.ChipValues, m.record.Config.Schedule, m.record.Checkpoint.SegmentIndex)
	averageStack = roundedAverageStack(averageStack, remainingChips)
	white := Spacedust.Foreground
	if strings.EqualFold(label, "none configured") {
		return []coloredLinePart{
			{text: "AVG. STACK", color: Spacedust.Blue, bold: true},
			{text: formatDisplayChips(averageStack), color: white, bold: true, fontSource: status, spaceBefore: true},
		}
	}
	if index := strings.LastIndex(label, " in "); index >= 0 {
		label = strings.TrimSpace(label[index+4:])
	}
	return []coloredLinePart{
		{text: "NEXT BREAK", color: Spacedust.Blue, bold: true},
		{text: label, color: white, bold: true, fontSource: status, monospace: true, spaceBefore: true},
		{text: "  ·  ", color: Spacedust.Muted},
		{text: "AVG. STACK", color: Spacedust.Blue, bold: true},
		{text: formatDisplayChips(averageStack), color: white, bold: true, fontSource: status, spaceBefore: true},
	}
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
		label := strings.TrimSpace(segment.Label)
		if label == "" {
			label = fmt.Sprintf("BREAK %d", currentBreakNumber(m))
		} else {
			label = strings.ToUpper(label)
		}
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
	return statRowSize(label, value, "")
}

func statRowSize(label string, value fyne.CanvasObject, size fyne.ThemeSizeName) fyne.CanvasObject {
	key := widget.NewLabel(label)
	key.TextStyle = fyne.TextStyle{Bold: true}
	if size != "" {
		key.SizeName = size
	}
	if valueLabel, ok := value.(*widget.Label); ok {
		valueLabel.Alignment = fyne.TextAlignTrailing
		if size != "" {
			valueLabel.SizeName = size
		}
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
	return compactSectionWithSize(title, content, spacing, 20)
}

func compactSectionWithSize(title string, content fyne.CanvasObject, spacing, textSize float32) fyne.CanvasObject {
	heading := canvas.NewText(title, Spacedust.Blue)
	heading.TextSize = textSize
	heading.TextStyle = fyne.TextStyle{Bold: true}
	return container.New(layout.NewCustomPaddedVBoxLayout(spacing), heading, content)
}

func chipLegend(values []config.ChipValue, removed map[int64]bool) *fyne.Container {
	return chipLegendWithSize(values, removed, false)
}

func chipLegendWithSize(values []config.ChipValue, removed map[int64]bool, large bool) *fyne.Container {
	tokens := chipTokens(values, removed)
	if large {
		for _, token := range tokens {
			token.(*chipToken).large = true
		}
	}
	if len(tokens) == 0 {
		return container.NewVBox(mutedLabel("No chip values"))
	}
	rows := make([]fyne.CanvasObject, 0, (len(tokens)+2)/3)
	for start := 0; start < len(tokens); start += 3 {
		end := min(start+3, len(tokens))
		row := container.NewHBox(tokens[start:end]...)
		rows = append(rows, container.NewCenter(row))
	}
	return container.NewVBox(rows...)
}

func chipTokens(values []config.ChipValue, removed map[int64]bool) []fyne.CanvasObject {
	chips := append([]config.ChipValue(nil), values...)
	sort.Slice(chips, func(i, j int) bool { return chips[i].Value < chips[j].Value })
	tokens := make([]fyne.CanvasObject, 0, len(chips))
	for _, chip := range chips {
		fill, err := config.ParseChipColor(chip.Color)
		if err != nil {
			continue
		}
		if removed[chip.Value] {
			fill = color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xff}
		}
		tokens = append(tokens, newChipToken(formatDisplayChips(chip.Value), fill))
	}
	return tokens
}

func chipsAfterChipUps(chips []config.ChipValue, schedule []config.Segment, throughSegment int) ([]config.ChipValue, map[int64]bool) {
	removed := make(map[int64]bool)
	for index, segment := range schedule {
		if index+1 <= throughSegment && segment.Kind == config.SegmentBreak && segment.ChipUp > 0 {
			removed[segment.ChipUp] = true
		}
	}
	remaining := make([]config.ChipValue, 0, len(chips))
	for _, chip := range chips {
		if !removed[chip.Value] {
			remaining = append(remaining, chip)
		}
	}
	return remaining, removed
}

type chipToken struct {
	widget.BaseWidget
	value string
	fill  color.NRGBA
	large bool
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
	if t.large {
		label.TextSize = 20
	}
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Alignment = fyne.TextAlignCenter
	return &chipTokenRenderer{token: t, circle: circle, label: label, objects: []fyne.CanvasObject{circle, label}}
}

func (t *chipToken) MinSize() fyne.Size {
	if t.large {
		return fyne.NewSize(72, 72)
	}
	return fyne.NewSize(48, 48)
}

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
	text            string
	color           color.NRGBA
	bold            bool
	monospace       bool
	fontSize        float32
	fontSource      *fitText
	spaceBefore     bool
	spaceBeforeSize float32
}

type coloredLine struct {
	widget.BaseWidget
	maxParts    int
	maxTextSize float32
	minTextSize float32
	minHeight   float32
	parts       []coloredLinePart
}

func newColoredLine(maxParts int, maxTextSize, minTextSize float32) *coloredLine {
	line := &coloredLine{maxParts: maxParts, maxTextSize: maxTextSize, minTextSize: minTextSize}
	line.ExtendBaseWidget(line)
	return line
}

func (l *coloredLine) MinSize() fyne.Size {
	if l.minHeight > 0 {
		return fyne.NewSize(220, l.minHeight)
	}
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
				spaceSize := fontSizes[index]
				if part.spaceBeforeSize > 0 {
					spaceSize = part.spaceBeforeSize
				}
				gaps[index] = fyne.MeasureText(" ", spaceSize, fyne.TextStyle{Bold: part.bold}).Width
			}
			total += gaps[index] + measured.Width
		}
		return total
	}
	totalWidth := measure()
	for totalWidth > size.Width {
		shrink := func(valuesOnly bool) bool {
			shrunk := false
			for index, part := range r.line.parts {
				isValue := part.fontSource != nil || part.spaceBefore || part.fontSize > r.line.maxTextSize
				if part.text != "" && index < len(fontSizes) && isValue == valuesOnly && fontSizes[index] > r.line.minTextSize {
					fontSizes[index]--
					shrunk = true
				}
			}
			return shrunk
		}
		if !shrink(true) && !shrink(false) {
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
	if len(objects) < 4 {
		return fyne.NewSize(0, 0)
	}
	messageSize, timeSize, versionSize, buttonSize := objects[0].MinSize(), objects[1].MinSize(), objects[2].MinSize(), objects[3].MinSize()
	height := fyne.Max(messageSize.Height, fyne.Max(timeSize.Height, fyne.Max(versionSize.Height, buttonSize.Height)))
	return fyne.NewSize(160+timeSize.Width+versionSize.Width+buttonSize.Width+24, height)
}
func (bottomFooterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}
	message, localTime, version, fullscreen := objects[0], objects[1], objects[2], objects[3]
	timeSize, versionSize, buttonSize := localTime.MinSize(), version.MinSize(), fullscreen.MinSize()
	messageWidth := size.Width - timeSize.Width - versionSize.Width - buttonSize.Width - 24
	if messageWidth < 0 {
		messageWidth = 0
	}
	message.Move(fyne.NewPos(0, 0))
	message.Resize(fyne.NewSize(messageWidth, size.Height))
	localTime.Move(fyne.NewPos(messageWidth+8, (size.Height-timeSize.Height)/2))
	localTime.Resize(timeSize)
	version.Move(fyne.NewPos(messageWidth+16+timeSize.Width, (size.Height-versionSize.Height)/2))
	version.Resize(versionSize)
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
