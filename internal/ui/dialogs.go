package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/preferences"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

type dialogKind int

const (
	dialogText dialogKind = iota
	dialogMenu
	dialogConfirm
	dialogMessage
	dialogSelectPlayer
	dialogMultiPlayers
	dialogSelectSegment
	dialogSelectPurchase
	dialogSelectBust
	dialogSelectParticipant
)

type dialogPurpose int

const (
	purposeNone dialogPurpose = iota
	menuTemplate
	menuPlayers
	menuSettings
	menuSoundSettings
	menuBusts
	selectPurchase
	selectInfo
	selectRename
	selectRemove
	selectSitOut
	selectBuster
	selectBustee
	selectVoidPurchase
	selectVoidBust
	selectCorrectBust
	selectReplacement
	selectRemainder
)

type textPurpose int

const (
	textTournamentName textPurpose = iota
	textBrowserFilename
	textAddPlayer
	textRenamePlayer
	textRemoveExplanation
	textIncompleteEnd
	textReopenExplanation
	textRemainingTime
	textVoidPurchaseExplanation
	textVoidBustExplanation
	textCorrectBustExplanation
	textTournamentNameRename
)

type confirmPurpose int

const (
	confirmNone confirmPurpose = iota
	confirmStart
	confirmQuit
	confirmRestorePlayer
	confirmCompletedEnd
	confirmIncompleteEnd
	confirmReopen
	confirmRemovePlayer
	confirmVoidPurchase
	confirmVoidBust
	confirmCorrectBust
	confirmSelectSegment
)

type dialog struct {
	kind        dialogKind
	purpose     dialogPurpose
	textPurpose textPurpose
	confirm     confirmPurpose
	title       string
	message     string
	prompt      string
	input       string
	choices     []string
	cursor      int

	players         []tournament.Player
	selected        map[string]bool
	selectedOrder   []string
	busters         []string
	purchaseKind    tournament.PurchaseKind
	targetPlayer    string
	targetName      string
	targetPurchase  int64
	targetBustEvent int64
	participant     bustParticipantOption
	replacement     *tournament.BustParticipant
	segmentIndex    int
	remaining       *time.Duration
	purchases       []tournament.Purchase
	bustEvents      []int64
	bustLabels      []string
	participants    []bustParticipantOption
	tiedIDs         map[string]bool
	returnTo        *dialog
}

type bustParticipantOption struct {
	BustEventID int64
	BustNumber  int
	PlayerID    string
	Role        string
	Position    int
	Tied        bool
}

func (m *Model) handleDialogKey(key keyEvent) bool {
	d := m.modal
	if d == nil {
		return false
	}
	switch d.kind {
	case dialogText:
		return m.handleTextDialog(key, d)
	case dialogMenu:
		return m.handleMenuDialog(key, d)
	case dialogConfirm:
		return m.handleConfirmDialog(key, d)
	case dialogMessage:
		if key.Type == keyEnter || key.Type == keyEsc {
			m.closeDialog()
		}
	case dialogSelectPlayer:
		return m.handlePlayerDialog(key, d)
	case dialogMultiPlayers:
		return m.handleMultiPlayerDialog(key, d)
	case dialogSelectSegment:
		return m.handleSegmentDialog(key, d)
	case dialogSelectPurchase:
		return m.handlePurchaseDialog(key, d)
	case dialogSelectBust:
		return m.handleBustDialog(key, d)
	case dialogSelectParticipant:
		return m.handleParticipantDialog(key, d)
	}
	return false
}

func (m *Model) handleTextDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyEnter:
		m.submitText(d)
	case keyBackspace:
		runes := []rune(d.input)
		if len(runes) > 0 {
			d.input = string(runes[:len(runes)-1])
		}
	case keySpace:
		d.input += " "
	case keyRunes:
		if !key.Alt {
			d.input += string(key.Runes)
		}
	}
	return false
}

func (m *Model) submitText(d *dialog) {
	value := strings.TrimSpace(d.input)
	switch d.textPurpose {
	case textTournamentName:
		if value == "" {
			m.setStatus("Tournament name is required.", true)
			return
		}
		m.pendingName = value
		m.modal = &dialog{kind: dialogMenu, purpose: menuTemplate, title: "Tournament template", choices: []string{"Use embedded default", "Browse YAML template", "Cancel"}}
	case textBrowserFilename:
		if m.browser == nil {
			m.closeDialog()
			return
		}
		if value == "" {
			value = m.defaultFilename()
		}
		path, err := m.browser.newPath(value)
		if err != nil {
			m.setStatus(err.Error(), true)
			return
		}
		purpose := m.browser.purpose
		m.modal = nil
		m.browser = nil
		m.saveBrowserDestination(path, purpose)
	case textAddPlayer:
		action := m.actionContext()
		player, err := m.service.RegisterPlayer(context.Background(), value, false, action)
		if errors.Is(err, tournament.ErrRestoreConfirmation) {
			m.modal = &dialog{kind: dialogConfirm, title: "Restore removed player?", message: fmt.Sprintf("Restore %q and preserve its existing identity/history?", player.Name), confirm: confirmRestorePlayer, targetPlayer: player.ID, targetName: value}
			return
		}
		if err != nil {
			m.showMessage("Add player failed", err.Error())
			return
		}
		m.modal = nil
		m.refresh()
		m.setStatus("Player registered. Buy-in is a separate action.", false)
	case textRenamePlayer:
		if err := m.service.RenamePlayer(context.Background(), d.targetPlayer, value, m.actionContext()); err != nil {
			m.showMessage("Rename failed", err.Error())
			return
		}
		m.modal = nil
		m.refresh()
		m.setStatus("Player name updated; history preserved.", false)
	case textTournamentNameRename:
		m.renameTournament(value)
	case textRemoveExplanation:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "Remove player?", message: "The player identity will remain in audit history.", confirm: confirmRemovePlayer, targetPlayer: d.targetPlayer, targetName: value}
	case textIncompleteEnd:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "End incomplete tournament?", message: "This will close the tournament without final standings.", confirm: confirmIncompleteEnd, targetName: value}
	case textReopenExplanation:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "Reopen tournament?", message: "It will reopen paused. Settings remain locked; previous results remain history.", confirm: confirmReopen, targetName: value}
	case textRemainingTime:
		remaining, err := time.ParseDuration(value)
		if err != nil || remaining < 0 {
			m.setStatus("Enter a valid remaining duration, e.g. 10m or 90s.", true)
			return
		}
		m.selectSegment(d.segmentIndex, &remaining)
	case textVoidPurchaseExplanation:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "Void purchase?", message: "The original transaction will remain in history.", confirm: confirmVoidPurchase, targetPurchase: d.targetPurchase, targetName: value}
	case textVoidBustExplanation:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "Void bust event?", message: "The original event will remain in history; dependent actions can block this correction.", confirm: confirmVoidBust, targetBustEvent: d.targetBustEvent, targetName: value}
	case textCorrectBustExplanation:
		if value == "" || len([]rune(value)) > 200 {
			m.setStatus("Explanation must contain 1 to 200 characters.", true)
			return
		}
		m.modal = &dialog{kind: dialogConfirm, title: "Correct bust participant?", message: "The original participant will remain in the audit trail.", confirm: confirmCorrectBust, targetName: value, participant: d.participant, replacement: d.replacement}
	}
}

func (m *Model) handleMenuDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.choices) > 0 {
			d.cursor = (d.cursor - 1 + len(d.choices)) % len(d.choices)
		}
	case keyDown:
		if len(d.choices) > 0 {
			d.cursor = (d.cursor + 1) % len(d.choices)
		}
	case keyEnter:
		m.chooseMenu(d)
	}
	return false
}

func (m *Model) chooseMenu(d *dialog) {
	if d.cursor < 0 || d.cursor >= len(d.choices) {
		return
	}
	switch d.purpose {
	case menuTemplate:
		switch d.cursor {
		case 0:
			m.modal = nil
			m.completeNewTemplateChoice()
		case 1:
			m.modal = nil
			m.startBrowser(browseTemplate, screenStartup)
		case 2:
			m.modal = nil
		}
	case menuPlayers:
		if m.record.Status == store.StatusEnded {
			m.openPlayerSelect(selectInfo, m.players)
			return
		}
		switch d.cursor {
		case 0:
			m.modal = &dialog{kind: dialogText, title: "Add player", prompt: "Player name", textPurpose: textAddPlayer}
		case 1:
			m.openPlayerSelect(selectInfo, m.players)
		case 2:
			m.openPlayerSelect(selectRename, nonRemoved(m.players))
		case 3:
			m.openPlayerSelect(selectRemove, nonRemoved(m.players))
		case 4:
			m.openVoidPurchase()
		}
	case menuSettings:
		m.chooseSettings(d.cursor)
	case menuSoundSettings:
		m.chooseSoundSettings(d.cursor)
	case menuBusts:
		m.chooseBusts(d.cursor)
	}
}

func (m *Model) chooseSettings(index int) {
	switch index {
	case 0:
		m.startBrowser(browseSaveTemplate, screenMain)
	case 1:
		m.startBrowser(browseTemplate, screenMain)
	case 2:
		m.modal = &dialog{kind: dialogText, title: "Rename tournament", prompt: "Tournament name", input: m.record.Name, textPurpose: textTournamentNameRename}
	case 3:
		m.modal = &dialog{kind: dialogMenu, purpose: menuSoundSettings, title: "Sound settings", choices: []string{
			"Choose end-of-level sound", "Choose one-minute warning sound",
			"Use default end-of-level sound", "Use default one-minute warning sound",
		}}
	default:
		if index == 4 && m.record.Status == store.StatusEnded {
			m.modal = &dialog{kind: dialogText, title: "Reopen tournament", prompt: "Explanation (required, max 200 chars)", textPurpose: textReopenExplanation}
		}
	}
}

func (m *Model) chooseSoundSettings(index int) {
	switch index {
	case 0:
		m.startBrowser(browseLevelSound, screenMain)
	case 1:
		m.startBrowser(browseMinuteSound, screenMain)
	case 2:
		m.prefs.SoundFile = ""
		if err := preferences.Save(m.prefs); err != nil {
			m.showMessage("Sound preference failed", err.Error())
			return
		}
		m.modal = nil
		m.setStatus("Using default end-of-level sound.", false)
	case 3:
		m.prefs.MinuteSoundFile = ""
		if err := preferences.Save(m.prefs); err != nil {
			m.showMessage("Sound preference failed", err.Error())
			return
		}
		m.modal = nil
		m.setStatus("Using default one-minute warning sound.", false)
	}
}

func (m *Model) chooseBusts(index int) {
	switch index {
	case 0:
		players := activePlayers(m.players)
		if len(players) < 2 {
			m.showMessage("Record bust", "At least two active players are needed.")
			return
		}
		m.modal = &dialog{kind: dialogMultiPlayers, purpose: selectBuster, title: "Select buster(s) · all receive credit", players: players, selected: make(map[string]bool)}
	case 1:
		m.openBustEvents(selectVoidBust)
	case 2:
		m.openBustParticipants()
	}
}

func (m *Model) handleConfirmDialog(key keyEvent, d *dialog) bool {
	if key.Type == keyEsc || key.Type == keyRunes && strings.EqualFold(key.String(), "n") {
		m.closeDialog()
		return false
	}
	if key.Type == keyEnter || key.Type == keyRunes && strings.EqualFold(key.String(), "y") {
		return m.confirmAction(d)
	}
	return false
}

func (m *Model) confirmAction(d *dialog) bool {
	switch d.confirm {
	case confirmStart:
		m.modal = nil
		if err := m.service.StartTournament(context.Background(), time.Now()); err != nil {
			m.showMessage("Start failed", err.Error())
			return false
		}
		m.resetClockAnchor(time.Now())
		m.refresh()
		m.setStatus("Settings locked. Tournament started.", false)
	case confirmQuit:
		return m.finishQuit()
	case confirmRestorePlayer:
		player, err := m.service.RegisterPlayer(context.Background(), d.targetName, true, m.actionContext())
		if err != nil {
			m.showMessage("Restore failed", err.Error())
			return false
		}
		m.modal = nil
		m.refresh()
		m.setStatus(fmt.Sprintf("Restored %s with original identity.", player.Name), false)
	case confirmCompletedEnd:
		m.modal = nil
		m.finalizeTournament("", nil)
	case confirmIncompleteEnd:
		m.modal = nil
		m.finalizeTournament(d.targetName, nil)
	case confirmReopen:
		m.modal = nil
		if err := m.service.ReopenTournament(context.Background(), d.targetName, tournament.ActionContext{}); err != nil {
			m.showMessage("Reopen failed", err.Error())
			return false
		}
		m.refresh()
		m.setStatus("Tournament reopened paused; settings remain locked.", false)
	case confirmRemovePlayer:
		m.modal = nil
		if err := m.service.RemovePlayer(context.Background(), d.targetPlayer, d.targetName, m.actionContext()); err != nil {
			m.showMessage("Remove player failed", err.Error())
			return false
		}
		m.refresh()
		m.setStatus("Player removed; identity retained.", false)
	case confirmVoidPurchase:
		m.modal = nil
		if err := m.service.VoidPurchase(context.Background(), d.targetPurchase, d.targetName, m.actionContext()); err != nil {
			m.showMessage("Void purchase failed", err.Error())
			return false
		}
		m.refresh()
		m.setStatus("Purchase voided; original transaction retained.", false)
	case confirmVoidBust:
		m.modal = nil
		if err := m.service.VoidBust(context.Background(), d.targetBustEvent, d.targetName, m.actionContext()); err != nil {
			m.showMessage("Void bust failed", err.Error())
			return false
		}
		m.refresh()
		m.setStatus("Bust event voided; audit history retained.", false)
	case confirmCorrectBust:
		m.modal = nil
		if err := m.service.CorrectBustParticipant(context.Background(), d.participant.BustEventID, d.participant.PlayerID, d.participant.Role, d.replacement, d.targetName, m.actionContext()); err != nil {
			m.showMessage("Bust correction failed", err.Error())
			return false
		}
		m.refresh()
		m.setStatus("Bust participant corrected; original and correction retained.", false)
	case confirmSelectSegment:
		m.modal = nil
		m.applySegmentSelection(d.segmentIndex, d.remaining)
	}
	if m.suspendedModal != nil && m.modal == nil {
		m.modal, m.suspendedModal = m.suspendedModal, nil
	}
	return false
}

func (m *Model) handlePlayerDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.players) > 0 {
			d.cursor = (d.cursor - 1 + len(d.players)) % len(d.players)
		}
	case keyDown:
		if len(d.players) > 0 {
			d.cursor = (d.cursor + 1) % len(d.players)
		}
	case keyHome:
		if len(d.players) > 0 {
			d.cursor = 0
		}
	case keyEnd:
		if len(d.players) > 0 {
			d.cursor = len(d.players) - 1
		}
	case keyEnter:
		if d.cursor >= 0 && d.cursor < len(d.players) {
			m.choosePlayer(d, d.players[d.cursor])
		}
	}
	return false
}

func (m *Model) choosePlayer(d *dialog, player tournament.Player) {
	switch d.purpose {
	case selectInfo:
		m.showPlayerHistory(player)
		if m.modal != nil && m.modal.kind == dialogMessage {
			m.modal.returnTo = d
		}
	case selectPurchase:
		_, err := m.service.Buy(context.Background(), player.ID, d.purchaseKind, m.actionContext())
		if err != nil {
			m.showMessage("Purchase failed", err.Error())
			return
		}
		m.modal = nil
		m.refresh()
		m.setStatus(fmt.Sprintf("%s recorded for %s.", d.purchaseKind, player.Name), false)
	case selectRename:
		m.modal = &dialog{kind: dialogText, title: "Rename player", prompt: "Player name", input: player.Name, textPurpose: textRenamePlayer, targetPlayer: player.ID}
	case selectRemove:
		m.modal = &dialog{kind: dialogText, title: "Remove player", prompt: "Explanation (required, max 200 chars)", textPurpose: textRemoveExplanation, targetPlayer: player.ID}
	case selectSitOut:
		err := m.service.SetSittingOut(context.Background(), player.ID, !player.SittingOut, m.actionContext())
		if err != nil {
			m.showMessage("Sit-out update failed", err.Error())
			return
		}
		m.modal = nil
		m.refresh()
		if player.SittingOut {
			m.setStatus(player.Name+" returned to the table.", false)
		} else {
			m.setStatus(player.Name+" is sitting out but remains active.", false)
		}
	case selectRemainder:
		m.recordRemainderChoice(player.ID)
	case selectReplacement:
		m.modal = &dialog{kind: dialogText, title: "Correct bust participant", prompt: "Explanation (required, max 200 chars)", textPurpose: textCorrectBustExplanation, participant: d.participant, replacement: &tournament.BustParticipant{PlayerID: player.ID, Role: d.participant.Role, Position: d.participant.Position, Tied: d.participant.Tied}}
	}
}

func (m *Model) handleMultiPlayerDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.players) > 0 {
			d.cursor = (d.cursor - 1 + len(d.players)) % len(d.players)
		}
	case keyDown:
		if len(d.players) > 0 {
			d.cursor = (d.cursor + 1) % len(d.players)
		}
	case keyHome:
		if len(d.players) > 0 {
			d.cursor = 0
		}
	case keyEnd:
		if len(d.players) > 0 {
			d.cursor = len(d.players) - 1
		}
	case keySpace:
		if d.cursor >= 0 && d.cursor < len(d.players) {
			id := d.players[d.cursor].ID
			if d.selected[id] {
				delete(d.selected, id)
				d.selectedOrder = removeString(d.selectedOrder, id)
			} else {
				d.selected[id] = true
				d.selectedOrder = append(d.selectedOrder, id)
			}
		}
	case keyEnter:
		m.advanceBustSelection(d)
	case keyRunes:
		if d.purpose == selectBustee && d.cursor >= 0 && d.cursor < len(d.players) {
			id := d.players[d.cursor].ID
			switch key.String() {
			case "[":
				d.selectedOrder = reorderBustees(d.selectedOrder, d.tiedIDs, id, -1)
			case "]":
				d.selectedOrder = reorderBustees(d.selectedOrder, d.tiedIDs, id, 1)
			default:
				if strings.EqualFold(key.String(), "t") && d.selected[id] {
					if d.tiedIDs == nil {
						d.tiedIDs = make(map[string]bool)
					}
					d.tiedIDs[id] = !d.tiedIDs[id]
				}
			}
		}
	}
	return false
}

func (m *Model) advanceBustSelection(d *dialog) {
	if len(d.selectedOrder) == 0 {
		m.setStatus("Select at least one player.", true)
		return
	}
	if d.purpose == selectBuster {
		selected := make(map[string]bool, len(d.selectedOrder))
		for _, id := range d.selectedOrder {
			selected[id] = true
		}
		players := make([]tournament.Player, 0)
		for _, player := range activePlayers(m.players) {
			if !selected[player.ID] {
				players = append(players, player)
			}
		}
		if len(players) == 0 {
			m.setStatus("At least one buster must not be among the bustees.", true)
			return
		}
		m.modal = &dialog{kind: dialogMultiPlayers, purpose: selectBustee, title: "Bustee order: lowest to highest · [ ] reorder · T ties adjacent", players: players, selected: make(map[string]bool), busters: append([]string(nil), d.selectedOrder...), tiedIDs: make(map[string]bool)}
		return
	}
	busters := append([]string(nil), d.busters...)
	bustees := buildBustees(d.selectedOrder, d.tiedIDs)
	_, err := m.service.RecordBust(context.Background(), tournament.BustRequest{Busters: busters, Bustees: bustees}, m.actionContext())
	if err != nil {
		m.showMessage("Bust record failed", err.Error())
		return
	}
	m.modal = nil
	m.refresh()
	m.setStatus("Bust event recorded.", false)
}

func reorderBustees(order []string, tied map[string]bool, playerID string, direction int) []string {
	index := -1
	for i, id := range order {
		if id == playerID {
			index = i
			break
		}
	}
	if index < 0 || direction == 0 {
		return order
	}
	start, end := busteeTieBounds(order, tied, index)
	if direction < 0 {
		if start == 0 {
			return order
		}
		previousStart, _ := busteeTieBounds(order, tied, start-1)
		result := append([]string(nil), order[:previousStart]...)
		result = append(result, order[start:end]...)
		result = append(result, order[previousStart:start]...)
		return append(result, order[end:]...)
	}
	if end == len(order) {
		return order
	}
	_, nextEnd := busteeTieBounds(order, tied, end)
	result := append([]string(nil), order[:start]...)
	result = append(result, order[end:nextEnd]...)
	result = append(result, order[start:end]...)
	return append(result, order[nextEnd:]...)
}

func busteeTieBounds(order []string, tied map[string]bool, index int) (start, end int) {
	start, end = index, index+1
	for start > 0 && tied[order[start]] && tied[order[start-1]] {
		start--
	}
	for end < len(order) && tied[order[end-1]] && tied[order[end]] {
		end++
	}
	return start, end
}

func busteeGroups(order []string, tied map[string]bool) [][]string {
	var groups [][]string
	for index := 0; index < len(order); {
		end := index + 1
		if tied[order[index]] {
			for end < len(order) && tied[order[end]] {
				end++
			}
		}
		groups = append(groups, order[index:end])
		index = end
	}
	return groups
}

func busteeFinishingLabels(activeBefore int, order []string, tied map[string]bool) map[string]string {
	groups := busteeGroups(order, tied)
	place := activeBefore - len(order) + 1
	if place < 1 {
		place = 1
	}
	labels := make(map[string]string, len(order))
	for index := len(groups) - 1; index >= 0; index-- {
		group := groups[index]
		last := place + len(group) - 1
		label := finishRange(place, last)
		if len(group) > 1 {
			label += " (tie)"
		}
		for _, id := range group {
			labels[id] = label
		}
		place = last + 1
	}
	return labels
}

func buildBustees(order []string, tied map[string]bool) []tournament.Bustee {
	groups := busteeGroups(order, tied)
	result := make([]tournament.Bustee, 0, len(order))
	position := len(groups)
	for _, group := range groups { // Input order is lowest-to-highest; stored positions are best-to-worst.
		for _, id := range group {
			result = append(result, tournament.Bustee{PlayerID: id, Position: position, Tied: len(group) > 1})
		}
		position--
	}
	return result
}

func (m *Model) handleSegmentDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.choices) > 0 {
			d.cursor = (d.cursor - 1 + len(d.choices)) % len(d.choices)
		}
	case keyDown:
		if len(d.choices) > 0 {
			d.cursor = (d.cursor + 1) % len(d.choices)
		}
	case keyHome:
		if len(d.choices) > 0 {
			d.cursor = 0
		}
	case keyEnd:
		if len(d.choices) > 0 {
			d.cursor = len(d.choices) - 1
		}
	case keyEnter:
		m.selectSegment(d.cursor+1, nil)
	case keyRunes:
		if strings.EqualFold(key.String(), "t") && d.cursor >= 0 && d.cursor < len(d.choices) {
			m.modal = &dialog{kind: dialogText, title: "Set remaining time", prompt: "Duration, e.g. 10m or 90s", textPurpose: textRemainingTime, segmentIndex: d.cursor + 1}
		}
	}
	return false
}

func (m *Model) selectSegment(index int, remaining *time.Duration) {
	label := "this level"
	if index > 0 && index <= len(m.record.Config.Schedule) {
		segment := m.record.Config.Schedule[index-1]
		if segment.Kind == config.SegmentLevel {
			level := 0
			for _, candidate := range m.record.Config.Schedule[:index] {
				if candidate.Kind == config.SegmentLevel {
					level++
				}
			}
			label = fmt.Sprintf("Level %d (%d / %d)", level, segment.SmallBlind, segment.BigBlind)
		} else if segment.Label != "" {
			label = segment.Label
		} else {
			label = "this break"
		}
	}
	message := "Jump to " + label + "? This change will be recorded."
	if remaining != nil {
		message = fmt.Sprintf("Jump to %s with %s remaining? This change will be recorded.", label, formatDuration(*remaining))
	}
	m.modal = &dialog{kind: dialogConfirm, title: "Change tournament level?", message: message, confirm: confirmSelectSegment, segmentIndex: index, remaining: remaining}
}

func (m *Model) applySegmentSelection(index int, remaining *time.Duration) {
	transitions, err := m.service.SelectSegment(context.Background(), index, remaining, m.actionContext())
	if err != nil {
		m.showMessage("Level selection failed", err.Error())
		return
	}
	m.modal = nil
	m.refresh()
	if len(transitions) > 0 {
		m.notice = transitionNotice(transitions, m.record.Config.Schedule)
	}
	m.setStatus("Tournament segment changed and audited.", false)
}

func (m *Model) handlePurchaseDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.purchases) > 0 {
			d.cursor = (d.cursor - 1 + len(d.purchases)) % len(d.purchases)
		}
	case keyDown:
		if len(d.purchases) > 0 {
			d.cursor = (d.cursor + 1) % len(d.purchases)
		}
	case keyEnter:
		if d.cursor < 0 || d.cursor >= len(d.purchases) {
			return false
		}
		purchase := d.purchases[d.cursor]
		if purchase.VoidedByEventID != nil {
			m.setStatus("Purchase already voided.", true)
			return false
		}
		m.modal = &dialog{kind: dialogText, title: "Void purchase", prompt: "Explanation (required, max 200 chars)", textPurpose: textVoidPurchaseExplanation, targetPurchase: purchase.ID}
	}
	return false
}

func (m *Model) handleBustDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.bustEvents) > 0 {
			d.cursor = (d.cursor - 1 + len(d.bustEvents)) % len(d.bustEvents)
		}
	case keyDown:
		if len(d.bustEvents) > 0 {
			d.cursor = (d.cursor + 1) % len(d.bustEvents)
		}
	case keyEnter:
		if d.cursor < 0 || d.cursor >= len(d.bustEvents) {
			return false
		}
		id := d.bustEvents[d.cursor]
		if d.purpose == selectVoidBust {
			m.modal = &dialog{kind: dialogText, title: "Void bust event", prompt: "Explanation (required, max 200 chars)", textPurpose: textVoidBustExplanation, targetBustEvent: id}
		} else {
			m.showParticipantPicker(id)
		}
	}
	return false
}

func (m *Model) handleParticipantDialog(key keyEvent, d *dialog) bool {
	switch key.Type {
	case keyEsc:
		m.closeDialog()
	case keyUp:
		if len(d.participants) > 0 {
			d.cursor = (d.cursor - 1 + len(d.participants)) % len(d.participants)
		}
	case keyDown:
		if len(d.participants) > 0 {
			d.cursor = (d.cursor + 1) % len(d.participants)
		}
	case keyEnter:
		if d.cursor < 0 || d.cursor >= len(d.participants) {
			return false
		}
		participant := d.participants[d.cursor]
		players := nonRemoved(m.players)
		m.modal = &dialog{kind: dialogSelectPlayer, purpose: selectReplacement, title: "Replacement participant", players: players, participant: participant}
	case keyRunes:
		if strings.EqualFold(key.String(), "d") && d.cursor >= 0 && d.cursor < len(d.participants) {
			participant := d.participants[d.cursor]
			m.modal = &dialog{kind: dialogText, title: "Remove bust participant", prompt: "Explanation (required, max 200 chars)", textPurpose: textCorrectBustExplanation, participant: participant}
		}
	}
	return false
}

func (m *Model) openBustEvents(purpose dialogPurpose) {
	events, err := m.database.Events(context.Background())
	if err != nil {
		m.showMessage("Bust history failed", err.Error())
		return
	}
	bustNumbers := bustNumbersByEventID(events)
	voided := make(map[int64]bool)
	for _, event := range events {
		if event.Kind == "bust_voided" && event.VoidEventID != nil {
			voided[*event.VoidEventID] = true
		}
	}
	var bustEvents []int64
	var labels []string
	for _, event := range events {
		if event.Kind != "bust_recorded" || voided[event.ID] {
			continue
		}
		participants, err := m.service.EffectiveBustParticipants(context.Background(), event.ID)
		if err != nil {
			m.showMessage("Bust history unavailable", err.Error())
			return
		}
		var busters, bustees []string
		for _, participant := range participants {
			name := participant.PlayerID
			if player, ok := m.playerByID(participant.PlayerID); ok {
				name = player.Name
			}
			if participant.Role == "buster" {
				busters = append(busters, name)
			} else if participant.Role == "bustee" {
				bustees = append(bustees, name)
			}
		}
		bustEvents = append(bustEvents, event.ID)
		labels = append(labels, fmt.Sprintf("Bust #%d · %s · %s · %s", bustNumbers[event.ID], namedParticipants("Buster", busters), namedParticipants("Bustee", bustees), event.WallTime.Format("15:04:05")))
	}
	if len(bustEvents) == 0 {
		m.showMessage("Bust history", "No valid bust events.")
		return
	}
	m.modal = &dialog{kind: dialogSelectBust, purpose: purpose, title: "Select bust event", bustEvents: bustEvents, bustLabels: labels}
}

func namedParticipants(role string, names []string) string {
	if len(names) != 1 {
		role += "s"
	}
	return role + ": " + strings.Join(names, ", ")
}

func (m *Model) showParticipantPicker(bustID int64) {
	events, err := m.database.Events(context.Background())
	if err != nil {
		m.showMessage("Bust participants unavailable", err.Error())
		return
	}
	bustNumber := bustNumbersByEventID(events)[bustID]
	effective, err := m.service.EffectiveBustParticipants(context.Background(), bustID)
	participants := make([]bustParticipantOption, 0, len(effective))
	for _, participant := range effective {
		participants = append(participants, bustParticipantOption{
			BustEventID: bustID, BustNumber: bustNumber, PlayerID: participant.PlayerID, Role: participant.Role,
			Position: participant.Position, Tied: participant.Tied,
		})
	}
	if err != nil {
		m.showMessage("Bust participants unavailable", err.Error())
		return
	}
	if len(participants) == 0 {
		m.showMessage("Bust participants", "No uncorrected participants remain in that bust event.")
		return
	}
	m.modal = &dialog{kind: dialogSelectParticipant, purpose: selectCorrectBust, title: "Select participant to correct", participants: participants}
}

func (m *Model) recordRemainderChoice(playerID string) {
	if m.pendingEndChoices == nil {
		m.pendingEndChoices = make(map[int][]string)
	}
	m.pendingEndChoices[m.pendingRemainderPlace] = append(m.pendingEndChoices[m.pendingRemainderPlace], playerID)
	m.pendingRemainderCount--
	if m.pendingRemainderCount > 0 {
		m.openRemainderPicker()
		return
	}
	m.pendingPrizeGroupIndex++
	m.openRemainderPicker()
}

func (m *Model) openRemainderPicker() {
	for m.pendingPrizeGroupIndex < len(m.pendingPrizeGroups) {
		group := m.pendingPrizeGroups[m.pendingPrizeGroupIndex]
		pending := int(group.PendingRemainderCents / 10)
		if pending == 0 {
			m.pendingPrizeGroupIndex++
			continue
		}
		if m.pendingRemainderCount == 0 {
			m.pendingRemainderPlace = group.FirstPlace
			m.pendingRemainderCount = pending
		}
		chosen := make(map[string]bool)
		for _, id := range m.pendingEndChoices[group.FirstPlace] {
			chosen[id] = true
		}
		var players []tournament.Player
		for _, entry := range group.Players {
			id := entry.PlayerID
			if chosen[id] {
				continue
			}
			if player, ok := m.playerByID(id); ok {
				players = append(players, player)
			}
		}
		m.modal = &dialog{kind: dialogSelectPlayer, purpose: selectRemainder, title: fmt.Sprintf("Choose player receiving extra 0.10 € — place %d", group.FirstPlace), players: players}
		return
	}
	m.finalizeTournament(m.pendingEndExplanation, m.pendingEndChoices)
}

func (m *Model) finalizeTournament(explanation string, choices map[int][]string) {
	result, err := m.service.EndTournament(context.Background(), explanation, choices, m.actionContext())
	if err != nil {
		var pending *tournament.PendingPrizeChoicesError
		if errors.As(err, &pending) {
			m.pendingEndExplanation = explanation
			m.pendingEndChoices = choices
			m.pendingPrizeGroups = pending.Groups
			m.pendingPrizeGroupIndex = 0
			m.pendingRemainderCount = 0
			m.openRemainderPicker()
			return
		}
		m.showMessage("End failed", err.Error())
		return
	}
	m.modal = nil
	m.pendingEndExplanation = ""
	m.pendingEndChoices = nil
	m.pendingPrizeGroups = nil
	m.refresh()
	if result.Completed {
		m.setStatus("Tournament finalized; payouts saved.", false)
	} else {
		m.setStatus("Tournament ended incomplete; no final standings recorded.", false)
	}
}

func (m *Model) openVoidPurchase() {
	purchases, err := m.service.Purchases(context.Background(), "")
	if err != nil {
		m.showMessage("Purchases unavailable", err.Error())
		return
	}
	if len(purchases) == 0 {
		m.showMessage("Purchases", "No purchase transactions exist.")
		return
	}
	m.modal = &dialog{kind: dialogSelectPurchase, purpose: selectVoidPurchase, title: "Select purchase to void", purchases: purchases}
}

func (m *Model) playerByID(id string) (tournament.Player, bool) {
	for _, player := range m.players {
		if player.ID == id {
			return player, true
		}
	}
	return tournament.Player{}, false
}

func (m *Model) closeDialog() {
	if m.modal != nil && m.modal.returnTo != nil {
		m.modal = m.modal.returnTo
		return
	}
	m.modal = nil
	if m.suspendedModal != nil {
		m.modal, m.suspendedModal = m.suspendedModal, nil
	}
}

func (m *Model) openBustParticipants() {
	if m.database == nil {
		return
	}
	events, err := m.database.Events(context.Background())
	if err != nil {
		m.showMessage("Bust participants unavailable", err.Error())
		return
	}
	bustNumbers := bustNumbersByEventID(events)
	voided := make(map[int64]bool)
	for _, event := range events {
		if event.Kind == "bust_voided" && event.VoidEventID != nil {
			voided[*event.VoidEventID] = true
		}
	}
	var participants []bustParticipantOption
	for _, event := range events {
		if event.Kind != "bust_recorded" || voided[event.ID] {
			continue
		}
		effective, err := m.service.EffectiveBustParticipants(context.Background(), event.ID)
		if err != nil {
			m.showMessage("Bust participants unavailable", err.Error())
			return
		}
		for _, participant := range effective {
			participants = append(participants, bustParticipantOption{
				BustEventID: event.ID, BustNumber: bustNumbers[event.ID], PlayerID: participant.PlayerID, Role: participant.Role,
				Position: participant.Position, Tied: participant.Tied,
			})
		}
	}
	if err != nil {
		m.showMessage("Bust participants unavailable", err.Error())
		return
	}
	if len(participants) == 0 {
		m.showMessage("Bust participants", "No uncorrected participants remain.")
		return
	}
	m.modal = &dialog{kind: dialogSelectParticipant, purpose: selectCorrectBust, title: "Select participant to correct", participants: participants}
}

func nonRemoved(players []tournament.Player) []tournament.Player {
	var result []tournament.Player
	for _, player := range players {
		if !player.Removed {
			result = append(result, player)
		}
	}
	return result
}

func removeString(values []string, target string) []string {
	for index, value := range values {
		if value == target {
			return append(values[:index], values[index+1:]...)
		}
	}
	return values
}

func (m *Model) saveTemplateImport(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.showMessage("Template read failed", err.Error())
		return
	}
	template, err := config.ParseTemplate(data)
	if err != nil {
		m.showMessage("Invalid template", err.Error())
		return
	}
	action := m.actionContext()
	if err := m.service.UpdateConfiguration(context.Background(), template, action); err != nil {
		m.showMessage("Settings import failed", err.Error())
		return
	}
	m.modal = nil
	m.refresh()
	m.setStatus("Tournament settings imported and audited.", false)
}

func (m *Model) showFinalResults() {
	result, err := m.service.LatestFinalResult(context.Background())
	if err != nil {
		m.showMessage("Final results unavailable", err.Error())
		return
	}
	lines := []string{"Prize pool · " + config.EuroCents(result.PoolCents).String(), "", "Final standings"}
	place := 1
	for _, group := range result.Standings.Groups {
		label := ordinalPlace(place)
		if len(group.PlayerIDs) > 1 {
			label += "–" + ordinalPlace(place+len(group.PlayerIDs)-1)
		}
		var names []string
		for _, id := range group.PlayerIDs {
			name := id
			if player, ok := m.playerByID(id); ok {
				name = player.Name
			}
			names = append(names, name)
		}
		lines = append(lines, label+" · "+strings.Join(names, ", "))
		place += len(group.PlayerIDs)
	}
	lines = append(lines, "", "Payouts")
	for _, group := range result.Prizes {
		for _, payout := range group.Players {
			name := payout.PlayerID
			if player, ok := m.playerByID(payout.PlayerID); ok {
				name = player.Name
			}
			remainder := ""
			if payout.GetsRemainder {
				remainder = " · tie remainder"
			}
			lines = append(lines, name+" · "+config.EuroCents(payout.AmountCents).String()+remainder)
		}
	}
	m.showMessage("Final results", strings.Join(lines, "\n"))
}

func (m *Model) renameTournament(name string) {
	action := m.actionContext()
	if err := m.database.RenameTournament(context.Background(), name, action.WallTime, action.Checkpoint); err != nil {
		m.showMessage("Rename failed", err.Error())
		return
	}
	m.modal = nil
	m.refresh()
	m.setStatus("Tournament renamed.", false)
}
