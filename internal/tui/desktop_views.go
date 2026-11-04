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
	title        *canvas.Text
	entrants     *widget.Label
	active       *widget.Label
	sittingOut   *widget.Label
	busts        *widget.Label
	buyIns       *widget.Label
	rebuys       *widget.Label
	addOns       *widget.Label
	totalStack   *widget.Label
	averageStack *widget.Label
	prizePool    *widget.Label
	prizeRows    *fyne.Container
	leftScroll   *container.Scroll
	blinds       *fitText
	timer        *fitText
	status       *canvas.Text
	message      *widget.Label
	nextLevel    *widget.Label
	nextBreak    *widget.Label
	localTime    *widget.Label
	fullscreen   *widget.Button
	actions      map[rune]*widget.Button
}

func (d *desktop) mainView() (fyne.CanvasObject, *desktopHome) {
	h := &desktopHome{
		entrants: widget.NewLabel(""), active: widget.NewLabel(""), sittingOut: widget.NewLabel(""), busts: widget.NewLabel(""),
		buyIns: widget.NewLabel(""), rebuys: widget.NewLabel(""), addOns: widget.NewLabel(""),
		totalStack: widget.NewLabel(""), averageStack: widget.NewLabel(""), prizePool: widget.NewLabel(""),
		prizeRows: container.New(layout.NewCustomPaddedVBoxLayout(1)), timer: newFitText("", Spacedust.Foreground, 460, true),
		status: canvas.NewText("", Spacedust.Green), message: widget.NewLabel(""), nextLevel: widget.NewLabel(""),
		nextBreak: widget.NewLabel(""), localTime: widget.NewLabel(""),
		actions: make(map[rune]*widget.Button),
	}
	h.title = canvas.NewText("", Spacedust.Accent)
	h.title.Alignment = fyne.TextAlignCenter
	h.title.TextSize = 34
	h.title.TextStyle = fyne.TextStyle{Bold: true}
	h.message.Hide()

	left := container.New(&minimumWidthLayout{width: 242}, h.leftColumn())
	right := container.New(&minimumWidthLayout{width: 220}, d.actionColumn(h))
	center := h.clockColumn()
	header := container.NewPadded(container.NewCenter(h.title))
	h.fullscreen = d.fullscreenButton()
	footerMessage := container.NewThemeOverride(h.message, newCondensedTheme())
	footer := container.NewPadded(container.NewBorder(nil, nil, nil, h.fullscreen, footerMessage))
	main := container.NewBorder(header, footer, left, right, container.NewPadded(center))
	h.refresh(d.model)
	return main, h
}

func (h *desktopHome) leftColumn() fyne.CanvasObject {
	stats := container.New(layout.NewCustomPaddedVBoxLayout(1),
		statRow("Entrants", h.entrants),
		statRow("Active players", h.active),
		statRow("Sitting out", h.sittingOut),
		statRow("Busts", h.busts),
	)
	stackPool := container.New(layout.NewCustomPaddedVBoxLayout(1),
		statRow("Buy-ins", h.buyIns),
		statRow("Rebuys", h.rebuys),
		statRow("Add-ons", h.addOns),
		statRow("Prize pool", h.prizePool),
		statRow("Total stack", h.totalStack),
		statRow("Average stack", h.averageStack),
	)
	sections := container.New(layout.NewCustomPaddedVBoxLayout(2),
		compactSection("Tournament", stats),
		compactSection("Stack & pool", stackPool),
		compactSection("Prizes", h.prizeRows),
	)
	h.leftScroll = container.NewScroll(sections)
	h.leftScroll.Direction = container.ScrollVerticalOnly
	return h.leftScroll
}

func (h *desktopHome) clockColumn() fyne.CanvasObject {
	blindsTitle := canvas.NewText("BLINDS AND ANTE", Spacedust.Muted)
	blindsTitle.TextSize = 15
	blindsTitle.TextStyle = fyne.TextStyle{Bold: true}
	blindsTitle.Alignment = fyne.TextAlignCenter
	h.blinds = newFitText("", Spacedust.Foreground, 64, true)
	h.blinds.minSize = 28
	h.blinds.preferred = fyne.NewSize(220, 72)
	h.status.TextSize = 20
	h.status.TextStyle = fyne.TextStyle{Bold: true}
	h.status.Alignment = fyne.TextAlignCenter
	h.message.Wrapping = fyne.TextWrapWord

	current := container.NewVBox(blindsTitle, h.blinds)
	clock := container.NewMax(h.timer)
	clock = container.NewBorder(nil, container.NewCenter(h.status), nil, nil, clock)
	h.nextLevel.Alignment = fyne.TextAlignCenter
	h.nextLevel.Wrapping = fyne.TextWrapWord
	h.nextBreak.Alignment = fyne.TextAlignCenter
	h.nextBreak.Wrapping = fyne.TextWrapWord
	h.nextBreak.TextStyle = fyne.TextStyle{Monospace: true}
	h.localTime.Alignment = fyne.TextAlignCenter
	h.localTime.TextStyle = fyne.TextStyle{Monospace: true}
	next := container.NewVBox(h.nextLevel, h.nextBreak, h.localTime)
	content := container.NewBorder(current, next, nil, nil, clock)
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
	h.averageStack.SetText(formatDisplayChips(m.summary.AverageStack))
	h.prizePool.SetText(m.summary.PrizePoolCents.String())

	h.prizeRows.Objects = nil
	for _, prize := range m.placePrizes {
		place := prize.Place
		label := "Winner"
		if place > 1 {
			label = ordinalPlace(place)
		}
		line := label + "  ·  " + config.EuroCents(prize.AmountCents).String()
		paid := place > int(m.summary.ActivePlayers)
		if m.record.Status == store.StatusEnded && m.record.ResultCompleted != nil && *m.record.ResultCompleted {
			paid = true
		}
		if paid {
			h.prizeRows.Add(struckThroughLabel(line))
			continue
		}
		row := widget.NewLabel(line)
		row.TextStyle = fyne.TextStyle{Bold: place == 1}
		h.prizeRows.Add(row)
	}
	if len(m.placePrizes) == 0 {
		h.prizeRows.Add(mutedLabel("No valid entrants"))
	}
	h.prizeRows.Refresh()

	segment := currentDesktopSegment(m)
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
	status := desktopStatus(m.record.Status, segment)
	if (m.record.Status == store.StatusRunning || m.record.Status == store.StatusPaused) && segment != nil && segment.Kind == config.SegmentLevel {
		status += fmt.Sprintf("  ·  LEVEL %d", currentLevelNumber(m))
	}
	h.status.Text = status
	switch m.record.Status {
	case store.StatusRunning:
		h.status.Color = Spacedust.Green
	case store.StatusPaused:
		h.status.Color = Spacedust.Yellow
	case store.StatusEnded:
		h.status.Color = Spacedust.Red
	default:
		h.status.Color = Spacedust.Teal
	}
	h.status.Refresh()
	h.nextLevel.SetText("Next blinds  ·  " + desktopNextBlinds(m))
	h.nextBreak.SetText("Next break  ·  " + m.nextBreakLabel())
	h.localTime.SetText("LOCAL TIME   " + time.Now().Format("15:04:05"))
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
	h.message.Show()
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
	close := d.newButton("Back to tournament  ·  Esc", func() { d.dispatchTeaKey(teaKeyEsc()) })
	close.Importance = widget.HighImportance
	return container.NewBorder(container.NewPadded(title), container.NewPadded(close), nil, nil,
		container.NewPadded(container.NewScroll(body)))
}

func currentDesktopSegment(m *Model) *config.Segment {
	if m.service == nil {
		return nil
	}
	return m.currentSegment()
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

func desktopStatus(status store.TournamentStatus, segment *config.Segment) string {
	if segment != nil && segment.Kind == config.SegmentBreak && status != store.StatusEnded {
		if status == store.StatusPaused {
			return "PAUSED  ·  BREAK"
		}
		if status == store.StatusRunning {
			return "BREAK"
		}
	}
	switch status {
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

func struckThroughLabel(value string) fyne.CanvasObject {
	label := canvas.NewText(value, Spacedust.Muted)
	label.TextSize = 16
	line := canvas.NewLine(Spacedust.Muted)
	line.StrokeWidth = 1
	return container.New(strikeThroughLayout{}, label, line)
}

type strikeThroughLayout struct{}

func (strikeThroughLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}
func (strikeThroughLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	label, line := objects[0], objects[1].(*canvas.Line)
	label.Move(fyne.NewPos(0, 0))
	label.Resize(size)
	width := label.MinSize().Width
	if width > size.Width {
		width = size.Width
	}
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
	heading := widget.NewLabel(title)
	heading.TextStyle = fyne.TextStyle{Bold: true}
	return container.New(layout.NewCustomPaddedVBoxLayout(2), heading, content)
}

// fitText scales the clock display to the space offered by its parent while keeping
// the numeral-heavy font legible at both fullscreen and windowed sizes.
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
func (r *fitTextRenderer) MinSize() fyne.Size           { return r.fit.preferred }
func (r *fitTextRenderer) Refresh()                     { canvas.Refresh(r.fit.text) }
func (r *fitTextRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *fitTextRenderer) Destroy()                     {}

func teaKeySpace() tea.Key { return tea.Key{Type: tea.KeySpace} }
func teaKeyEsc() tea.Key   { return tea.Key{Type: tea.KeyEsc} }
