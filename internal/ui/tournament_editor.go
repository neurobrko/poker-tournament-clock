package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"gopkg.in/yaml.v3"
)

type editorFieldKind int

const (
	editorTextField editorFieldKind = iota
	editorCheckField
	editorSelectField
)

type editorField struct {
	key     string
	label   string
	kind    editorFieldKind
	options []string
}

func (m *Model) openEditorSources() {
	m.modal = &dialog{kind: dialogMenu, purpose: menuEditorSource, title: "Tournament editor", choices: []string{
		"Use embedded default", "Load existing YAML template", "Start from scratch", "Cancel",
	}}
}

func (m *Model) chooseEditorSource(index int) {
	switch index {
	case 0:
		template, err := config.DefaultTemplate()
		if err != nil {
			m.showMessage("Editor unavailable", err.Error())
			return
		}
		m.startEditor(template)
	case 1:
		m.modal = nil
		m.startBrowser(browseEditorTemplate, screenStartup)
	case 2:
		template, err := config.DefaultTemplate()
		if err != nil {
			m.showMessage("Editor unavailable", err.Error())
			return
		}
		template.Schedule = nil
		template.ChipValues = nil
		template.Players = nil
		template.Registration = config.RegistrationConfig{
			RebuysEnabled:        true,
			RegistrationDeadline: config.Deadline{At: config.DeadlineAtLevelStart, Target: 1},
		}
		template.PrizeTiers = []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"100"}}}
		m.startEditor(template)
	case 3:
		m.modal = nil
	}
}

func (m *Model) startEditor(template config.Template) {
	m.editorTemplate = template
	m.editorName = ""
	m.editorAfterSave = false
	m.editorDirty = false
	m.modal = nil
	m.screen = screenEditor
}

func (m *Model) handleEditorKey(key keyEvent) bool {
	if key.Type == keyEsc {
		m.leaveEditor()
	} else if key.Type == keyRunes && strings.EqualFold(key.String(), "q") {
		m.askQuit()
	}
	return false
}

func (m *Model) leaveEditor() {
	if m.editorDirty {
		m.modal = &dialog{kind: dialogConfirm, title: "Discard tournament draft?", message: "Unsaved editor changes will be lost.", confirm: confirmDiscardEditor}
		return
	}
	m.screen = screenStartup
	m.setStatus(nil, false)
}

func (m *Model) moveEditorSchedule(index, delta int) {
	target := index + delta
	if index < 0 || index >= len(m.editorTemplate.Schedule) || target < 0 || target >= len(m.editorTemplate.Schedule) {
		return
	}
	m.editorTemplate.Schedule[index], m.editorTemplate.Schedule[target] = m.editorTemplate.Schedule[target], m.editorTemplate.Schedule[index]
	m.editorDirty = true
}

func (m *Model) saveEditorTemplate() {
	if err := m.editorTemplate.Validate(); err != nil {
		m.setStatus(err, true)
		return
	}
	if m.editorAfterSave {
		if err := validateEditorTournamentName(m.editorName); err != nil {
			m.setStatus(err, true)
			return
		}
	}
	m.startBrowser(browseEditorSaveTemplate, screenEditor)
}

func (m *Model) createEditorTournament() {
	if err := validateEditorTournamentName(m.editorName); err != nil {
		m.setStatus(err, true)
		return
	}
	if err := m.editorTemplate.Validate(); err != nil {
		m.setStatus(err, true)
		return
	}
	m.pendingName = strings.TrimSpace(m.editorName)
	m.pendingTemplate = m.editorTemplate
	m.startBrowser(browseSaveDatabase, screenEditor)
}

func validateEditorTournamentName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tournament name is required")
	}
	if len([]rune(name)) > store.MaxNameLength {
		return fmt.Errorf("tournament name cannot exceed %d characters", store.MaxNameLength)
	}
	return nil
}

func (m *Model) saveEditorTemplateFile(path string) {
	data, err := config.MarshalTemplate(m.editorTemplate)
	if err != nil {
		m.showMessage("Template save failed", err.Error())
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		m.showMessage("Template save failed", err.Error())
		return
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		m.showMessage("Template save failed", err.Error())
		return
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		m.showMessage("Template save failed", err.Error())
		return
	}
	if err := file.Close(); err != nil {
		m.showMessage("Template save failed", err.Error())
		return
	}
	m.editorDirty = false
	if m.editorAfterSave {
		m.pendingName = strings.TrimSpace(m.editorName)
		m.pendingTemplate = m.editorTemplate
		m.startBrowser(browseSaveDatabase, screenEditor)
		return
	}
	m.setStatus("Reusable template saved without a tournament name.", false)
}

func (m *Model) openEditorForm(purpose dialogPurpose, index int) {
	values := make(map[string]string)
	title := ""
	switch purpose {
	case editorFormPurchases:
		title = "Purchases and registration"
		values = map[string]string{
			"initial_price":   editorPrice(m.editorTemplate.Purchases.InitialBuyIn.Price),
			"initial_chips":   strconv.FormatInt(m.editorTemplate.Purchases.InitialBuyIn.Chips, 10),
			"rebuy_price":     editorPrice(m.editorTemplate.Purchases.Rebuy.Price),
			"rebuy_chips":     strconv.FormatInt(m.editorTemplate.Purchases.Rebuy.Chips, 10),
			"addon_price":     editorPrice(m.editorTemplate.Purchases.AddOn.Price),
			"addon_chips":     strconv.FormatInt(m.editorTemplate.Purchases.AddOn.Chips, 10),
			"rebuys_enabled":  strconv.FormatBool(m.editorTemplate.Registration.RebuysEnabled),
			"deadline_at":     string(m.editorTemplate.Registration.RegistrationDeadline.At),
			"deadline_target": strconv.Itoa(m.editorTemplate.Registration.RegistrationDeadline.Target),
			"addons_enabled":  strconv.FormatBool(m.editorTemplate.Registration.AddOnsEnabled),
			"addon_break":     strconv.Itoa(m.editorTemplate.Registration.AddOnBreak),
		}
	case editorFormLevel:
		segment := config.Segment{Kind: config.SegmentLevel, Duration: config.Duration(20 * time.Minute), SmallBlind: 5, BigBlind: 10, Ante: config.AnteConfig{Kind: config.AnteNone}}
		if index >= 0 {
			segment = m.editorTemplate.Schedule[index]
			title = "Edit blind level"
		} else {
			title = "Add blind level"
		}
		values = map[string]string{
			"duration": time.Duration(segment.Duration).String(), "small_blind": strconv.FormatInt(segment.SmallBlind, 10),
			"big_blind": strconv.FormatInt(segment.BigBlind, 10), "ante_kind": string(segment.Ante.Kind),
			"ante_amount": strconv.FormatInt(segment.Ante.Amount, 10), "reminder": segment.Reminder,
		}
	case editorFormBreak:
		breakCount := 1
		for _, segment := range m.editorTemplate.Schedule {
			if segment.Kind == config.SegmentBreak {
				breakCount++
			}
		}
		segment := config.Segment{Kind: config.SegmentBreak, Label: fmt.Sprintf("Break %d", breakCount), Duration: config.Duration(10 * time.Minute)}
		if index >= 0 {
			segment = m.editorTemplate.Schedule[index]
			title = "Edit break"
		} else {
			title = "Add break"
		}
		values = map[string]string{
			"label": segment.Label, "duration": time.Duration(segment.Duration).String(),
			"chipup": strconv.FormatInt(segment.ChipUp, 10), "reminder": segment.Reminder,
		}
	case editorFormChip:
		title = "Add chip value"
		chip := config.ChipValue{Value: 5, Color: "white"}
		if index >= 0 {
			title = "Edit chip value"
			chip = m.editorTemplate.ChipValues[index]
		}
		values = map[string]string{"value": strconv.FormatInt(chip.Value, 10), "color": chip.Color}
	case editorFormPlayer:
		title = "Add player"
		player := config.Player{}
		if index >= 0 {
			title = "Edit player"
			player = m.editorTemplate.Players[index]
		}
		values = map[string]string{"name": player.Name, "alias": player.Alias}
	case editorFormPrize:
		title = "Add prize tier"
		tier := config.PrizeTier{MinPlayers: 2, Percentages: []config.Percentage{"100"}}
		if len(m.editorTemplate.PrizeTiers) > 0 {
			tier.MinPlayers = m.editorTemplate.PrizeTiers[len(m.editorTemplate.PrizeTiers)-1].MinPlayers + 1
		}
		if index >= 0 {
			title = "Edit prize tier"
			tier = m.editorTemplate.PrizeTiers[index]
		}
		percentages := make([]string, len(tier.Percentages))
		for i, percentage := range tier.Percentages {
			percentages[i] = string(percentage)
		}
		values = map[string]string{"min_players": strconv.Itoa(tier.MinPlayers), "percentages": strings.Join(percentages, ", ")}
	}
	m.modal = &dialog{kind: dialogEditorForm, purpose: purpose, title: title, formValues: values, editorIndex: index}
}

func (m *Model) handleEditorFormKey(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyEnter:
		m.saveEditorForm(d)
	}
	return false
}

func (m *Model) saveEditorForm(d *dialog) {
	template := m.editorTemplate
	values := d.formValues
	var err error
	switch d.purpose {
	case editorFormPurchases:
		var initial, rebuy, addon config.PurchaseType
		if initial, err = editorPurchase(values, "initial"); err == nil {
			rebuy, err = editorPurchase(values, "rebuy")
		}
		if err == nil {
			addon, err = editorPurchase(values, "addon")
		}
		if err == nil {
			template.Purchases = config.PurchaseConfig{InitialBuyIn: initial, Rebuy: rebuy, AddOn: addon}
			template.Registration = config.RegistrationConfig{
				RebuysEnabled:        parseEditorBool(values["rebuys_enabled"]),
				RegistrationDeadline: config.Deadline{At: config.DeadlineAt(values["deadline_at"])},
				AddOnsEnabled:        parseEditorBool(values["addons_enabled"]),
			}
			template.Registration.RegistrationDeadline.Target, err = parseEditorInt(values, "deadline_target")
			if err == nil {
				template.Registration.AddOnBreak, err = parseEditorInt(values, "addon_break")
			}
		}
	case editorFormLevel:
		segment := config.Segment{Kind: config.SegmentLevel}
		segment.Duration, err = parseEditorDuration(values, "duration")
		if err == nil {
			segment.SmallBlind, err = parseEditorInt64(values, "small_blind")
		}
		if err == nil {
			segment.BigBlind, err = parseEditorInt64(values, "big_blind")
		}
		segment.Ante.Kind = config.AnteKind(values["ante_kind"])
		if err == nil && segment.Ante.Kind != config.AnteNone {
			segment.Ante.Amount, err = parseEditorInt64(values, "ante_amount")
		}
		segment.Reminder = strings.TrimSpace(values["reminder"])
		if err == nil {
			template.Schedule, err = putEditorSchedule(template.Schedule, d.editorIndex, segment)
		}
	case editorFormBreak:
		segment := config.Segment{Kind: config.SegmentBreak, Label: strings.TrimSpace(values["label"])}
		segment.Duration, err = parseEditorDuration(values, "duration")
		if err == nil {
			segment.ChipUp, err = parseEditorInt64(values, "chipup")
		}
		segment.Reminder = strings.TrimSpace(values["reminder"])
		if err == nil {
			template.Schedule, err = putEditorSchedule(template.Schedule, d.editorIndex, segment)
		}
	case editorFormChip:
		var chip config.ChipValue
		chip.Value, err = parseEditorInt64(values, "value")
		chip.Color = strings.TrimSpace(values["color"])
		if err == nil {
			template.ChipValues, err = putEditorChip(template.ChipValues, d.editorIndex, chip)
		}
	case editorFormPlayer:
		player := config.Player{Name: strings.TrimSpace(values["name"]), Alias: strings.TrimSpace(values["alias"])}
		if player.Name == "" {
			err = fmt.Errorf("player name is required")
		} else {
			template.Players, err = putEditorPlayer(template.Players, d.editorIndex, player)
		}
	case editorFormPrize:
		var tier config.PrizeTier
		tier.MinPlayers, err = parseEditorInt(values, "min_players")
		if err == nil {
			tier.Percentages, err = parseEditorPercentages(values["percentages"])
		}
		if err == nil {
			template.PrizeTiers, err = putEditorPrize(template.PrizeTiers, d.editorIndex, tier)
		}
	}
	if err != nil {
		m.setStatus(err, true)
		return
	}
	m.editorTemplate = template
	m.editorDirty = true
	m.modal = nil
	m.setStatus("Editor draft updated.", false)
}

func editorPurchase(values map[string]string, prefix string) (config.PurchaseType, error) {
	var purchase config.PurchaseType
	err := yaml.Unmarshal([]byte(values[prefix+"_price"]), &purchase.Price)
	if err != nil {
		return purchase, fmt.Errorf("%s price: %w", prefix, err)
	}
	purchase.Chips, err = parseEditorInt64(values, prefix+"_chips")
	return purchase, err
}

func parseEditorDuration(values map[string]string, key string) (config.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(values[key]))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 20m or 10m", key)
	}
	return config.Duration(value), nil
}

func parseEditorInt64(values map[string]string, key string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(values[key]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}

func parseEditorInt(values map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(values[key]))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}

func parseEditorBool(value string) bool { return value == "true" }

func parseEditorPercentages(value string) ([]config.Percentage, error) {
	parts := strings.Split(value, ",")
	percentages := make([]config.Percentage, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("enter comma-separated percentages totaling 100")
		}
		percentages = append(percentages, config.Percentage(part))
	}
	return percentages, nil
}

func putEditorSchedule(values []config.Segment, index int, segment config.Segment) ([]config.Segment, error) {
	if index < 0 {
		return append(values, segment), nil
	}
	if index >= len(values) {
		return nil, fmt.Errorf("schedule item no longer exists")
	}
	values[index] = segment
	return values, nil
}

func putEditorChip(values []config.ChipValue, index int, chip config.ChipValue) ([]config.ChipValue, error) {
	if index < 0 {
		return append(values, chip), nil
	}
	if index >= len(values) {
		return nil, fmt.Errorf("chip value no longer exists")
	}
	values[index] = chip
	return values, nil
}

func putEditorPlayer(values []config.Player, index int, player config.Player) ([]config.Player, error) {
	if index < 0 {
		return append(values, player), nil
	}
	if index >= len(values) {
		return nil, fmt.Errorf("player no longer exists")
	}
	values[index] = player
	return values, nil
}

func putEditorPrize(values []config.PrizeTier, index int, prize config.PrizeTier) ([]config.PrizeTier, error) {
	if index < 0 {
		return append(values, prize), nil
	}
	if index >= len(values) {
		return nil, fmt.Errorf("prize tier no longer exists")
	}
	values[index] = prize
	return values, nil
}

func editorPrice(price config.EuroCents) string {
	return fmt.Sprintf("%d.%02d", int64(price)/100, int64(price)%100)
}

func (d *desktop) editorView() fyne.CanvasObject {
	title := sectionLabel("TOURNAMENT EDITOR")
	name := widget.NewEntry()
	name.SetPlaceHolder("Tournament name (required to create)")
	name.SetText(d.model.editorName)
	name.OnChanged = func(value string) { d.model.editorName, d.model.editorDirty = value, true }
	d.entry = &desktopEntry{Entry: name, escape: func() { d.dispatchKey(keyEvent{Type: keyEsc}) }, fullscreen: d.toggleFullScreen}

	purchaseSummary := fmt.Sprintf("Buy-in %s / %d chips · Rebuy %s / %d · Add-on %s / %d", editorPrice(d.model.editorTemplate.Purchases.InitialBuyIn.Price), d.model.editorTemplate.Purchases.InitialBuyIn.Chips, editorPrice(d.model.editorTemplate.Purchases.Rebuy.Price), d.model.editorTemplate.Purchases.Rebuy.Chips, editorPrice(d.model.editorTemplate.Purchases.AddOn.Price), d.model.editorTemplate.Purchases.AddOn.Chips)
	registrationSummary := fmt.Sprintf("Rebuys %t · deadline %s #%d · add-ons %t at break #%d", d.model.editorTemplate.Registration.RebuysEnabled, d.model.editorTemplate.Registration.RegistrationDeadline.At, d.model.editorTemplate.Registration.RegistrationDeadline.Target, d.model.editorTemplate.Registration.AddOnsEnabled, d.model.editorTemplate.Registration.AddOnBreak)
	purchaseEdit := d.newButton("Edit purchases and registration", func() { d.model.openEditorForm(editorFormPurchases, -1); d.render(true) })

	rows := make([]fyne.CanvasObject, 0, len(d.model.editorTemplate.Schedule)+2)
	levelNo, breakNo := 0, 0
	for index, segment := range d.model.editorTemplate.Schedule {
		index, segment := index, segment
		label := ""
		if segment.Kind == config.SegmentLevel {
			levelNo++
			label = fmt.Sprintf("Level %d · %s · %d/%d · %s", levelNo, time.Duration(segment.Duration), segment.SmallBlind, segment.BigBlind, segment.Ante.Kind)
		} else {
			breakNo++
			label = fmt.Sprintf("Break %d · %s · %s · chip up %d", breakNo, segment.Label, time.Duration(segment.Duration), segment.ChipUp)
		}
		purpose := editorFormLevel
		if segment.Kind == config.SegmentBreak {
			purpose = editorFormBreak
		}
		up := d.newButton("↑", func() { d.model.moveEditorSchedule(index, -1); d.render(true) })
		down := d.newButton("↓", func() { d.model.moveEditorSchedule(index, 1); d.render(true) })
		if index == 0 {
			up.Disable()
		}
		if index == len(d.model.editorTemplate.Schedule)-1 {
			down.Disable()
		}
		rows = append(rows, editorRow(label, up, down, d.newButton("Edit", func() { d.model.openEditorForm(purpose, index); d.render(true) }), d.newButton("Remove", func() {
			d.model.editorTemplate.Schedule = append(d.model.editorTemplate.Schedule[:index], d.model.editorTemplate.Schedule[index+1:]...)
			d.model.editorDirty = true
			d.render(true)
		})))
	}
	if len(rows) == 0 {
		rows = append(rows, mutedLabel("No levels or breaks yet."))
	}
	addBreak := d.newButton("Add break", func() { d.model.openEditorForm(editorFormBreak, -1); d.render(true) })
	if len(d.model.editorTemplate.Schedule) == 0 || d.model.editorTemplate.Schedule[len(d.model.editorTemplate.Schedule)-1].Kind != config.SegmentLevel {
		addBreak.Disable()
	}
	rows = append(rows, container.NewHBox(d.newButton("Add level", func() { d.model.openEditorForm(editorFormLevel, -1); d.render(true) }), addBreak))

	chipRows := make([]fyne.CanvasObject, 0, len(d.model.editorTemplate.ChipValues)+1)
	for index, chip := range d.model.editorTemplate.ChipValues {
		index, chip := index, chip
		chipRows = append(chipRows, editorRow(fmt.Sprintf("%d · %s", chip.Value, chip.Color), d.newButton("Edit", func() { d.model.openEditorForm(editorFormChip, index); d.render(true) }), d.newButton("Remove", func() {
			d.model.editorTemplate.ChipValues = append(d.model.editorTemplate.ChipValues[:index], d.model.editorTemplate.ChipValues[index+1:]...)
			d.model.editorDirty = true
			d.render(true)
		})))
	}
	chipRows = append(chipRows, d.newButton("Add chip value", func() { d.model.openEditorForm(editorFormChip, -1); d.render(true) }))

	playerRows := make([]fyne.CanvasObject, 0, len(d.model.editorTemplate.Players)+1)
	for index, player := range d.model.editorTemplate.Players {
		index, player := index, player
		label := player.Name
		if player.Alias != "" {
			label += " (" + player.Alias + ")"
		}
		playerRows = append(playerRows, editorRow(label, d.newButton("Edit", func() { d.model.openEditorForm(editorFormPlayer, index); d.render(true) }), d.newButton("Remove", func() {
			d.model.editorTemplate.Players = append(d.model.editorTemplate.Players[:index], d.model.editorTemplate.Players[index+1:]...)
			d.model.editorDirty = true
			d.render(true)
		})))
	}
	playerRows = append(playerRows, d.newButton("Add player", func() { d.model.openEditorForm(editorFormPlayer, -1); d.render(true) }))

	prizeRows := make([]fyne.CanvasObject, 0, len(d.model.editorTemplate.PrizeTiers)+1)
	for index, prize := range d.model.editorTemplate.PrizeTiers {
		index, prize := index, prize
		percentages := make([]string, len(prize.Percentages))
		for i, percentage := range prize.Percentages {
			percentages[i] = string(percentage)
		}
		prizeRows = append(prizeRows, editorRow(fmt.Sprintf("%d+ players · %s%%", prize.MinPlayers, strings.Join(percentages, "/")), d.newButton("Edit", func() { d.model.openEditorForm(editorFormPrize, index); d.render(true) }), d.newButton("Remove", func() {
			d.model.editorTemplate.PrizeTiers = append(d.model.editorTemplate.PrizeTiers[:index], d.model.editorTemplate.PrizeTiers[index+1:]...)
			d.model.editorDirty = true
			d.render(true)
		})))
	}
	prizeRows = append(prizeRows, d.newButton("Add prize tier", func() { d.model.openEditorForm(editorFormPrize, -1); d.render(true) }))

	createAfter := widget.NewCheck("Create tournament after saving template", func(value bool) { d.model.editorAfterSave = value })
	createAfter.SetChecked(d.model.editorAfterSave)
	content := container.NewVBox(
		d.statusNotice(),
		compactSection("Tournament name", name),
		compactSection("Purchases and registration", container.NewVBox(widget.NewLabel(purchaseSummary), widget.NewLabel(registrationSummary), purchaseEdit)),
		compactSection("Blind schedule", container.NewVBox(rows...)),
		compactSection("Chip values", container.NewVBox(chipRows...)),
		compactSection("Players", container.NewVBox(playerRows...)),
		compactSection("Prize tiers", container.NewVBox(prizeRows...)),
		mutedLabel("Templates remain nameless. Add levels and breaks in order; settings are validated when saved or created."),
	)
	scroll := container.NewVScroll(content)
	scroll.Direction = container.ScrollVerticalOnly
	back := d.newButton("Back", func() { d.model.leaveEditor(); d.render(true) })
	save := d.newButton("Save template", func() { d.model.saveEditorTemplate(); d.render(true) })
	create := d.newButton("Create tournament", func() { d.model.createEditorTournament(); d.render(true) })
	create.Importance = widget.HighImportance
	footer := container.NewPadded(container.NewHBox(back, createAfter, layout.NewSpacer(), save, create))
	return container.NewBorder(container.NewPadded(title), footer, nil, nil, container.New(windowSideInsetLayout{inset: 16}, scroll))
}

func editorRow(label string, buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, nil, container.NewHBox(buttons...), widget.NewLabel(label))
}

func editorFields(purpose dialogPurpose) []editorField {
	switch purpose {
	case editorFormPurchases:
		return []editorField{
			{key: "initial_price", label: "Initial buy-in price (€)"}, {key: "initial_chips", label: "Initial buy-in chips"},
			{key: "rebuy_price", label: "Rebuy price (€)"}, {key: "rebuy_chips", label: "Rebuy chips"},
			{key: "addon_price", label: "Add-on price (€)"}, {key: "addon_chips", label: "Add-on chips"},
			{key: "rebuys_enabled", label: "Rebuys enabled", kind: editorCheckField},
			{key: "deadline_at", label: "Registration closes", kind: editorSelectField, options: []string{string(config.DeadlineAtLevelStart), string(config.DeadlineAtBreakStart), string(config.DeadlineAtBreakEnd)}},
			{key: "deadline_target", label: "Deadline segment number"},
			{key: "addons_enabled", label: "Add-ons enabled", kind: editorCheckField}, {key: "addon_break", label: "Add-on break number"},
		}
	case editorFormLevel:
		return []editorField{
			{key: "duration", label: "Duration (e.g. 20m)"}, {key: "small_blind", label: "Small blind"}, {key: "big_blind", label: "Big blind"},
			{key: "ante_kind", label: "Ante type", kind: editorSelectField, options: []string{string(config.AnteNone), string(config.AntePerPlayer), string(config.AnteBigBlind)}},
			{key: "ante_amount", label: "Ante amount"}, {key: "reminder", label: "Reminder"},
		}
	case editorFormBreak:
		return []editorField{{key: "label", label: "Break name"}, {key: "duration", label: "Duration (e.g. 10m)"}, {key: "chipup", label: "Chip denomination to remove (0 for none)"}, {key: "reminder", label: "Reminder"}}
	case editorFormChip:
		return []editorField{{key: "value", label: "Chip denomination"}, {key: "color", label: "Color name or hex"}}
	case editorFormPlayer:
		return []editorField{{key: "name", label: "Player name"}, {key: "alias", label: "Alias (optional)"}}
	case editorFormPrize:
		return []editorField{{key: "min_players", label: "Minimum players"}, {key: "percentages", label: "Place percentages (comma-separated, total 100)"}}
	default:
		return nil
	}
}

func (d *desktop) editorFormDialog(modal *dialog) fyne.CanvasObject {
	fields := editorFields(modal.purpose)
	rows := make([]fyne.CanvasObject, 0, len(fields)+1)
	firstEntry := true
	for _, field := range fields {
		field := field
		switch field.kind {
		case editorCheckField:
			check := widget.NewCheck(field.label, func(value bool) { modal.formValues[field.key] = strconv.FormatBool(value) })
			check.SetChecked(parseEditorBool(modal.formValues[field.key]))
			rows = append(rows, check)
		case editorSelectField:
			selectField := widget.NewSelect(field.options, func(value string) { modal.formValues[field.key] = value })
			selectField.SetSelected(modal.formValues[field.key])
			rows = append(rows, container.NewBorder(nil, nil, widget.NewLabel(field.label), nil, selectField))
		default:
			entry := widget.NewEntry()
			entry.SetPlaceHolder(field.label)
			entry.SetText(modal.formValues[field.key])
			entry.OnChanged = func(value string) { modal.formValues[field.key] = value }
			entry.OnSubmitted = func(string) { d.dispatchKey(keyEvent{Type: keyEnter}) }
			wrapped := &desktopEntry{Entry: entry, escape: func() { d.closeModal() }, fullscreen: d.toggleFullScreen}
			if firstEntry {
				d.entry = wrapped
				firstEntry = false
			}
			rows = append(rows, container.NewBorder(nil, nil, widget.NewLabel(field.label), nil, wrapped))
		}
	}
	save := d.modalButton("Save", func() { d.dispatchKey(keyEvent{Type: keyEnter}) })
	save.Importance = widget.HighImportance
	cancel := d.modalButton("Cancel", func() { d.closeModal() })
	rows = append(rows, container.NewHBox(layout.NewSpacer(), cancel, save))
	scroll := container.NewVScroll(container.NewVBox(rows...))
	scroll.Direction = container.ScrollVerticalOnly
	return scroll
}
