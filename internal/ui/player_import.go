package ui

import (
	"context"
	"fmt"
	"os"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

type playerImportAction int

const (
	playerImportAdd playerImportAction = iota
	playerImportNeedsChoice
	playerImportKeep
	playerImportUpdateAlias
	playerImportRestore
	playerImportSkip
)

type pendingPlayerImport struct {
	player  config.Player
	action  playerImportAction
	id      string
	removed bool
}

func (m *Model) importPlayersFrom(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.showMessage("Player import failed", err.Error())
		return
	}
	players, err := config.ParsePlayersYAML(data)
	if err != nil {
		m.showMessage("Invalid player list", err.Error())
		return
	}
	m.beginPlayerImport(players, nil)
}

func (m *Model) beginPlayerImport(players []config.Player, template *config.Template) {
	m.refresh()
	if m.record.StartedAt != nil {
		m.pendingTemplateImport = nil
		m.showMessage("Import unavailable", "Import players before starting the tournament.")
		return
	}
	m.pendingTemplateImport = template
	m.pendingPlayerImports = make([]pendingPlayerImport, 0, len(players))
	for _, player := range players {
		operation := pendingPlayerImport{player: player, action: playerImportAdd}
		if existing, ok := m.findPlayerByName(player.Name); ok {
			operation.action, operation.id, operation.removed = playerImportNeedsChoice, existing.ID, existing.Removed
		}
		m.pendingPlayerImports = append(m.pendingPlayerImports, operation)
	}
	m.promptPlayerImportConflict()
}

func (m *Model) findPlayerByName(name string) (tournament.Player, bool) {
	_, key, err := tournament.NormalizePlayerName(name)
	if err != nil {
		return tournament.Player{}, false
	}
	for _, player := range m.players {
		if _, existingKey, err := tournament.NormalizePlayerName(player.Name); err == nil && key == existingKey {
			return player, true
		}
	}
	return tournament.Player{}, false
}

func (m *Model) promptPlayerImportConflict() {
	for index, operation := range m.pendingPlayerImports {
		if operation.action != playerImportNeedsChoice {
			continue
		}
		choices := []string{"Keep existing", "Update alias", "Cancel import"}
		message := "A player with this name already exists."
		if operation.removed {
			choices = []string{"Restore and update alias", "Skip removed player", "Cancel import"}
			message = "A removed player with this name exists."
		}
		m.modal = &dialog{kind: dialogMenu, purpose: menuPlayerImportConflict, title: "Player name conflict", message: fmt.Sprintf("%s\n%s", message, operation.player.Name), choices: choices}
		m.pendingPlayerImports[index].action = playerImportNeedsChoice
		return
	}
	m.modal = nil
	m.applyPlayerImport()
}

func (m *Model) choosePlayerImportConflict(choice int) {
	for index, operation := range m.pendingPlayerImports {
		if operation.action != playerImportNeedsChoice {
			continue
		}
		if choice == 2 {
			m.cancelPlayerImport()
			return
		}
		if operation.removed {
			if choice == 0 {
				m.pendingPlayerImports[index].action = playerImportRestore
			} else {
				m.pendingPlayerImports[index].action = playerImportSkip
			}
		} else if choice == 0 {
			m.pendingPlayerImports[index].action = playerImportKeep
		} else {
			m.pendingPlayerImports[index].action = playerImportUpdateAlias
		}
		m.promptPlayerImportConflict()
		return
	}
	m.cancelPlayerImport()
}

func (m *Model) cancelPlayerImport() {
	m.pendingPlayerImports = nil
	m.pendingTemplateImport = nil
	m.modal = nil
	m.setStatus("Player import cancelled; roster unchanged.", false)
}

func (m *Model) applyPlayerImport() {
	changes := make([]tournament.PlayerImportChange, 0, len(m.pendingPlayerImports))
	added, restored, updated, skipped := 0, 0, 0, 0
	for _, operation := range m.pendingPlayerImports {
		switch operation.action {
		case playerImportAdd:
			changes = append(changes, tournament.PlayerImportChange{Action: tournament.PlayerImportAdd, Name: operation.player.Name, Alias: operation.player.Alias})
			added++
		case playerImportKeep, playerImportSkip:
			skipped++
		case playerImportUpdateAlias:
			changes = append(changes, tournament.PlayerImportChange{Action: tournament.PlayerImportUpdateAlias, PlayerID: operation.id, Name: operation.player.Name, Alias: operation.player.Alias})
			updated++
		case playerImportRestore:
			changes = append(changes, tournament.PlayerImportChange{Action: tournament.PlayerImportRestore, PlayerID: operation.id, Name: operation.player.Name, Alias: operation.player.Alias})
			restored++
		}
	}

	template := m.pendingTemplateImport
	if err := m.service.ApplyPlayerImport(context.Background(), template, changes, m.actionContext()); err != nil {
		m.pendingPlayerImports = nil
		m.pendingTemplateImport = nil
		m.refresh()
		m.showMessage("Player import failed", fmt.Sprintf("No changes were applied: %v", err))
		return
	}
	m.pendingPlayerImports = nil
	m.pendingTemplateImport = nil
	m.refresh()
	if template != nil {
		m.setStatus(fmt.Sprintf("Tournament settings imported; roster merged: %d added, %d aliases updated, %d restored, %d kept/skipped.", added, updated, restored, skipped), false)
	} else {
		m.setStatus(fmt.Sprintf("Player import complete: %d added, %d aliases updated, %d restored, %d kept/skipped.", added, updated, restored, skipped), false)
	}
}

func (m *Model) currentRosterPlayers() []config.Player {
	players := make([]config.Player, 0, len(m.players))
	for _, player := range m.players {
		if !player.Removed {
			players = append(players, config.Player{Name: player.Name, Alias: player.Alias})
		}
	}
	return players
}

func (m *Model) exportPlayers(path string) {
	m.refresh()
	data, err := config.MarshalPlayersYAML(m.currentRosterPlayers())
	if err != nil {
		m.showMessage("Player export failed", err.Error())
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		m.showMessage("Player export failed", err.Error())
		return
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		m.showMessage("Player export failed", err.Error())
		return
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		m.showMessage("Player export failed", err.Error())
		return
	}
	if err := file.Close(); err != nil {
		m.showMessage("Player export failed", err.Error())
		return
	}
	m.setStatus("Current non-removed players exported.", false)
}
