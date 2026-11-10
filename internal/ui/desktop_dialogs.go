package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/store"
)

func (d *desktop) modalOverlay(base fyne.CanvasObject, modal *dialog) fyne.CanvasObject {
	content := container.NewVBox()
	if modal.message != "" {
		message := widget.NewLabel(modal.message)
		message.Wrapping = fyne.TextWrapWord
		if strings.Contains(modal.message, "\n") {
			scroll := container.NewVScroll(message)
			scroll.Direction = container.ScrollVerticalOnly
			content.Add(container.New(&minimumHeightLayout{height: 320}, scroll))
		} else {
			content.Add(message)
		}
	}
	if body := d.modalBody(modal); body != nil {
		content.Add(body)
	}
	if d.model.notice != "" {
		content.Add(mutedLabel(d.model.notice))
	}
	if d.model.statusMessage != "" {
		status := widget.NewLabel(d.model.statusMessage)
		status.Wrapping = fyne.TextWrapWord
		content.Add(status)
	}
	card := container.NewPadded(widget.NewCard(modal.title, "", content))
	overlay := container.NewStack(base, newDesktopScrim(), container.NewCenter(container.New(&widthLimitLayout{width: 720}, card)))
	return overlay
}

func (d *desktop) modalBody(modal *dialog) fyne.CanvasObject {
	switch modal.kind {
	case dialogText:
		return d.textDialog(modal)
	case dialogConfirm:
		return d.confirmDialog()
	case dialogMessage:
		return container.NewHBox(layout.NewSpacer(), d.modalButton("Close", func() { d.closeModal() }))
	case dialogMenu:
		return d.choiceList(modal.choices, modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	case dialogSelectPlayer:
		return d.choiceList(playerDialogLabels(d.model, modal), modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	case dialogMultiPlayers:
		return d.multiPlayerDialog(modal)
	case dialogSelectSegment:
		return d.segmentDialog(modal)
	case dialogSelectPurchase:
		return d.choiceList(purchaseDialogLabels(d.model, modal), modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	case dialogSelectBust:
		return d.choiceList(modal.bustLabels, modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	case dialogSelectParticipant:
		return d.participantDialog(modal)
	default:
		return nil
	}
}

func (d *desktop) textDialog(modal *dialog) fyne.CanvasObject {
	prompt := widget.NewLabel(modal.prompt)
	prompt.Wrapping = fyne.TextWrapWord
	entry := widget.NewEntry()
	entry.SetText(modal.input)
	entry.SetPlaceHolder(modal.prompt)
	entry.OnChanged = func(value string) { modal.input = value }
	maxChars := 200
	if modal.textPurpose == textTournamentName || modal.textPurpose == textTournamentNameRename || modal.textPurpose == textAddPlayer || modal.textPurpose == textRenamePlayer || modal.textPurpose == textPlayerAlias {
		maxChars = store.MaxNameLength
	}
	entry.Validator = func(value string) error {
		if len([]rune(value)) > maxChars {
			return fmt.Errorf("maximum %d characters", maxChars)
		}
		return nil
	}
	d.entry = &desktopEntry{
		Entry:      entry,
		escape:     func() { d.closeModal() },
		fullscreen: func() { d.window.SetFullScreen(!d.window.FullScreen()) },
	}
	submitText := func() {
		modal.input = entry.Text
		if err := entry.Validate(); err != nil {
			d.model.setStatus(err, true)
			d.render(true)
			return
		}
		d.dispatchKey(keyEvent{Type: keyEnter})
	}
	entry.OnSubmitted = func(string) { submitText() }

	submit := d.modalButton("Continue  ·  Enter", submitText)
	submit.Importance = widget.HighImportance
	cancel := d.modalButton("Cancel  ·  Esc", func() { d.closeModal() })
	return container.NewVBox(prompt, d.entry, container.NewHBox(layout.NewSpacer(), cancel, submit))
}

func (d *desktop) confirmDialog() fyne.CanvasObject {
	cancel := d.modalButton("No  ·  Esc", func() { d.closeModal() })
	confirm := d.modalButton("Yes  ·  Enter", func() {
		d.dispatchKey(keyEvent{Type: keyRunes, Runes: []rune{'y'}})
	})
	confirm.Importance = widget.HighImportance
	return container.NewHBox(layout.NewSpacer(), cancel, confirm)
}

func (d *desktop) choiceList(choices []string, cursor int, choose func(int)) fyne.CanvasObject {
	if len(choices) == 0 {
		return widget.NewLabel("Nothing available.")
	}
	rows := make([]fyne.CanvasObject, 0, len(choices))
	for index, choice := range choices {
		index, choice := index, choice
		prefix := ""
		if index == cursor {
			prefix = "›  "
		}
		button := d.newButton(prefix+choice, func() { choose(index) })
		button.Alignment = widget.ButtonAlignLeading
		if index == cursor {
			button.Importance = widget.HighImportance
		}
		rows = append(rows, button)
	}
	return d.listScroll(rows)
}

func (d *desktop) listScroll(rows []fyne.CanvasObject) fyne.CanvasObject {
	content := container.New(layout.NewCustomPaddedVBoxLayout(3), rows...)
	d.modalScroll = container.NewVScroll(content)
	visibleRows := len(rows)
	if visibleRows > 7 {
		visibleRows = 7
	}
	if visibleRows < 1 {
		visibleRows = 1
	}
	return container.New(&minimumHeightLayout{height: float32(visibleRows * 48)}, d.modalScroll)
}

func (d *desktop) segmentDialog(modal *dialog) fyne.CanvasObject {
	list := d.choiceList(modal.choices, modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	setTime := d.modalButton("Set remaining time for selected level  ·  T", func() { d.activate('t') })
	return container.NewVBox(list, container.NewHBox(layout.NewSpacer(), setTime))
}

func (d *desktop) multiPlayerDialog(modal *dialog) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(modal.players))
	finishingLabels := map[string]string(nil)
	if modal.purpose == selectBustee {
		finishingLabels = busteeFinishingLabels(int(d.model.summary.ActivePlayers), modal.selectedOrder, modal.tiedIDs)
	}
	for index, player := range modal.players {
		index, player := index, player
		selected := modal.selected[player.ID]
		cursor := "  "
		if index == modal.cursor {
			cursor = "› "
		}
		label := cursor + "☐  " + player.Name
		if selected {
			label = cursor + "☑  " + player.Name
			if place, ok := finishingLabels[player.ID]; ok {
				label = cursor + "☑  " + place + " · " + player.Name
			}
		}
		button := d.newButton(label, func() {
			d.moveDialogCursor(index, modal.cursor)
			d.dispatchKey(keyEvent{Type: keySpace})
		})
		button.Alignment = widget.ButtonAlignLeading
		if index == modal.cursor {
			button.Importance = widget.HighImportance
		}
		rows = append(rows, button)
	}
	continueButton := d.modalButton("Continue  ·  Enter", func() { d.dispatchKey(keyEvent{Type: keyEnter}) })
	cancelButton := d.modalButton("Cancel  ·  Esc", func() { d.closeModal() })
	if modal.purpose == selectBustee {
		controls := container.NewHBox(
			d.modalButton("Move earlier  ·  [", func() { d.activate('[') }),
			d.modalButton("Move later  ·  ]", func() { d.activate(']') }),
			d.modalButton("Toggle tie  ·  T", func() { d.activate('t') }),
		)
		return container.NewVBox(d.listScroll(rows), controls, container.NewHBox(layout.NewSpacer(), cancelButton, continueButton))
	}
	return container.NewVBox(d.listScroll(rows), container.NewHBox(layout.NewSpacer(), cancelButton, continueButton))
}

func (d *desktop) participantDialog(modal *dialog) fyne.CanvasObject {
	labels := make([]string, 0, len(modal.participants))
	for _, participant := range modal.participants {
		name := participant.PlayerID
		if player, ok := d.model.playerByID(participant.PlayerID); ok {
			name = player.Name
		}
		tie := ""
		if participant.Tied {
			tie = " · tied"
		}
		labels = append(labels, fmt.Sprintf("Bust #%d · %s · %s · order %d%s", participant.BustNumber, name, participant.Role, participant.Position, tie))
	}
	list := d.choiceList(labels, modal.cursor, func(index int) { d.clickChoice(index, modal.cursor) })
	remove := d.modalButton("Remove selected participant  ·  D", func() { d.activate('d') })
	return container.NewVBox(list, container.NewHBox(layout.NewSpacer(), remove))
}

func (d *desktop) moveDialogCursor(target, _ int) {
	if d.model.modal != nil {
		d.model.modal.cursor = target
	}
}

func (d *desktop) modalButton(label string, tapped func()) *widget.Button {
	button := d.newButton(label, tapped)
	button.Importance = widget.MediumImportance
	return button
}

func playerDialogLabels(model *Model, modal *dialog) []string {
	labels := make([]string, 0, len(modal.players))
	for _, player := range modal.players {
		state := ""
		if player.Removed {
			state = " · removed"
		} else if model.record.Status == store.StatusEnded && model.record.ResultCompleted != nil && *model.record.ResultCompleted && player.Active {
			state = " · winner"
		} else if player.SittingOut {
			state = " · sitting out"
		} else if player.Active {
			state = " · active"
		}
		name := player.Name
		if player.Alias != "" && (modal.purpose == selectInfo || modal.purpose == selectRename || modal.purpose == selectRemove || modal.purpose == selectAlias) {
			name += " (" + player.Alias + ")"
		}
		labels = append(labels, name+state)
	}
	return labels
}

func purchaseDialogLabels(model *Model, modal *dialog) []string {
	labels := make([]string, 0, len(modal.purchases))
	for _, purchase := range modal.purchases {
		name := purchase.PlayerID
		if player, ok := model.playerByID(purchase.PlayerID); ok {
			name = player.Name
		}
		state := ""
		if purchase.VoidedByEventID != nil {
			state = " · VOIDED"
		}
		labels = append(labels, fmt.Sprintf("#%d  %s  ·  %s  ·  %d chips  ·  %s%s", purchase.ID, name, purchase.Kind, purchase.Chips, purchase.PriceCents, state))
	}
	return labels
}

func (d *desktop) browserView() fyne.CanvasObject {
	browser := d.model.browser
	title := canvas.NewText(browserTitle(browser.purpose), Spacedust.Accent)
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}
	path := widget.NewLabel(browser.directory)
	path.Wrapping = fyne.TextWrapBreak
	up := d.newButton("Bck · Parent folder", func() { d.dispatchKey(keyEvent{Type: keyBackspace}) })
	entries := make([]fyne.CanvasObject, 0, len(browser.entries))
	for index, entry := range browser.entries {
		index, entry := index, entry
		label := entry.name
		if entry.dir {
			label = "▸  " + label
		}
		button := d.newButton(label, func() {
			d.moveBrowserCursor(index, browser.cursor)
			d.dispatchKey(keyEvent{Type: keyEnter})
		})
		button.Alignment = widget.ButtonAlignLeading
		if index == browser.cursor {
			button.Importance = widget.HighImportance
		}
		if !entry.dir && !entry.selectable {
			button.Disable()
		}
		entries = append(entries, button)
		d.browserButtons = append(d.browserButtons, button)
	}
	list := container.NewVScroll(container.NewVBox(entries...))
	list.Direction = container.ScrollVerticalOnly
	d.browserScroll = list
	hiddenButton := d.newButton(hiddenFilesButtonLabel(browser.showHidden), func() { d.toggleHiddenFiles() })
	unsupportedButton := d.newButton(unsupportedFilesButtonLabel(browser.showUnsupported), func() { d.toggleUnsupportedFiles() })
	d.browserButtons = append(d.browserButtons, hiddenButton, unsupportedButton, up)
	footerButtons := []fyne.CanvasObject{hiddenButton, unsupportedButton}
	if browser.purpose == browseSaveDatabase || browser.purpose == browseSaveTemplate {
		newFile := d.newButton("N · New file", func() { d.activate('n') })
		d.browserButtons = append(d.browserButtons, newFile)
		footerButtons = append(footerButtons, newFile)
	}
	cancel := d.newButton("Esc · Cancel", func() { d.dispatchKey(keyEvent{Type: keyEsc}) })
	d.browserButtons = append(d.browserButtons, cancel)
	footerButtons = append(footerButtons, up, cancel)
	controls := container.NewHScroll(container.NewHBox(footerButtons...))
	body := container.NewBorder(
		container.NewVBox(title, path),
		container.NewVBox(d.statusNotice(), controls),
		nil, nil, list,
	)
	return container.NewPadded(body)
}

func hiddenFilesButtonLabel(showHidden bool) string {
	if showHidden {
		return "H · Hide hidden files"
	}
	return "H · Show hidden files"
}

func unsupportedFilesButtonLabel(showUnsupported bool) string {
	if showUnsupported {
		return "U · Hide unselectable"
	}
	return "U · Show unselectable"
}

func (d *desktop) toggleHiddenFiles() {
	if d.model.browser == nil {
		return
	}
	if err := d.model.browser.toggleHidden(); err != nil {
		d.model.setStatus(err, true)
	}
	d.render(true)
}

func (d *desktop) toggleUnsupportedFiles() {
	if d.model.browser == nil {
		return
	}
	if err := d.model.browser.toggleUnsupported(); err != nil {
		d.model.setStatus(err, true)
	}
	d.render(true)
}

func (d *desktop) moveBrowserCursor(target, _ int) {
	if d.model.browser != nil {
		d.model.browser.cursor = target
	}
}

func (d *desktop) statusNotice() fyne.CanvasObject {
	if d.model.statusMessage == "" && d.model.notice == "" {
		return widget.NewLabel("")
	}
	message := d.model.statusMessage
	if d.model.notice != "" {
		if message != "" {
			message += "  ·  "
		}
		message += d.model.notice
	}
	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord
	return label
}

type desktopEntry struct {
	*widget.Entry
	escape     func()
	fullscreen func()
}

func (e *desktopEntry) AcceptsTab() bool { return true }

func (e *desktopEntry) TypedKey(event *fyne.KeyEvent) {
	if event != nil && event.Name == fyne.KeyTab {
		return
	}
	if event != nil && event.Name == fyne.KeyEscape {
		if e.escape != nil {
			e.escape()
		}
		return
	}
	if event != nil && event.Name == fyne.KeyF11 {
		if e.fullscreen != nil {
			e.fullscreen()
		}
		return
	}
	e.Entry.TypedKey(event)
}

type desktopScrim struct {
	widget.BaseWidget
	rect *canvas.Rectangle
}

func newDesktopScrim() *desktopScrim {
	scrim := &desktopScrim{rect: canvas.NewRectangle(color.NRGBA{R: 0, G: 8, B: 10, A: 220})}
	scrim.ExtendBaseWidget(scrim)
	return scrim
}
func (s *desktopScrim) CreateRenderer() fyne.WidgetRenderer {
	return &desktopScrimRenderer{scrim: s, objects: []fyne.CanvasObject{s.rect}}
}
func (s *desktopScrim) Tapped(*fyne.PointEvent) {}

type desktopScrimRenderer struct {
	scrim   *desktopScrim
	objects []fyne.CanvasObject
}

func (r *desktopScrimRenderer) Layout(size fyne.Size)        { r.scrim.rect.Resize(size) }
func (r *desktopScrimRenderer) MinSize() fyne.Size           { return fyne.NewSize(0, 0) }
func (r *desktopScrimRenderer) Refresh()                     { r.scrim.rect.Refresh() }
func (r *desktopScrimRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *desktopScrimRenderer) Destroy()                     {}

type minimumHeightLayout struct{ height float32 }

func (l *minimumHeightLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(100, l.height)
	}
	size := objects[0].MinSize()
	if size.Height < l.height {
		size.Height = l.height
	}
	return size
}
func (l *minimumHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

type widthLimitLayout struct{ width float32 }

func (l *widthLimitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(l.width, 0)
	}
	return fyne.NewSize(l.width, objects[0].MinSize().Height)
}
func (l *widthLimitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	width := size.Width
	if width > l.width {
		width = l.width
	}
	objects[0].Resize(fyne.NewSize(width, size.Height))
	objects[0].Move(fyne.NewPos((size.Width-width)/2, 0))
}

var _ fyne.Focusable = (*desktopEntry)(nil)
var _ fyne.Tappable = (*desktopScrim)(nil)
