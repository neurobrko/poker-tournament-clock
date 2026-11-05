package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) dialogView(d *dialog) string {
	var lines []string
	lines = append(lines, titleStyle.Render(d.title), "")
	if d.message != "" {
		lines = append(lines, d.message, "")
	}
	switch d.kind {
	case dialogText:
		if d.prompt != "" {
			lines = append(lines, d.prompt)
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(d.input+"▏"))
		lines = append(lines, helpStyle.Render("Enter submit · Esc cancel · Ctrl+U clear"))
	case dialogMenu:
		lines = append(lines, renderChoices(d.choices, d.cursor)...)
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter choose · Esc cancel"))
	case dialogConfirm:
		lines = append(lines, helpStyle.Render("Y / Enter confirm · N / Esc cancel"))
	case dialogMessage:
		lines = append(lines, helpStyle.Render("Enter or Esc close"))
	case dialogSelectPlayer:
		for index, player := range d.players {
			mark := "  "
			if index == d.cursor {
				mark = "> "
			}
			state := ""
			if player.Removed {
				state = " [removed]"
			} else if player.SittingOut {
				state = " [sitting out]"
			} else if player.Active {
				state = " [active]"
			}
			lines = append(lines, mark+player.Name+state)
		}
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter choose · Esc cancel"))
	case dialogMultiPlayers:
		if d.purpose == selectBustee && len(d.busters) > 0 {
			lines = append(lines, "Buster(s): "+playerNames(m, d.busters), "")
		}
		for index, player := range d.players {
			mark := "[ ]"
			if d.selected[player.ID] {
				mark = "[x]"
			}
			cursor := " "
			if index == d.cursor {
				cursor = ">"
			}
			tie := ""
			if d.tiedIDs[player.ID] {
				tie = " (tie)"
			}
			lines = append(lines, fmt.Sprintf("%s %s %s%s", cursor, mark, player.Name, tie))
		}
		if d.purpose == selectBustee {
			lines = append(lines, "", helpStyle.Render("Space select in finish order · T mark tie group · Enter record · Esc cancel"))
		} else {
			lines = append(lines, helpStyle.Render("Space toggle · Enter continue · Esc cancel"))
		}
	case dialogSelectSegment:
		for index, choice := range d.choices {
			mark := "  "
			if index == d.cursor {
				mark = "> "
			}
			lines = append(lines, mark+choice)
		}
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter jump · T set remaining time · Esc cancel"))
	case dialogSelectPurchase:
		for index, purchase := range d.purchases {
			mark := "  "
			if index == d.cursor {
				mark = "> "
			}
			name := purchase.PlayerID
			if player, ok := m.playerByID(purchase.PlayerID); ok {
				name = player.Name
			}
			voided := ""
			if purchase.VoidedByEventID != nil {
				voided = " [voided]"
			}
			lines = append(lines, fmt.Sprintf("%s#%d %s %s %s %d chips%s", mark, purchase.ID, name, purchase.Kind, purchase.PriceCents, purchase.Chips, voided))
		}
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter void · Esc cancel"))
	case dialogSelectBust:
		for index, eventID := range d.bustEvents {
			mark := "  "
			if index == d.cursor {
				mark = "> "
			}
			label := fmt.Sprintf("Bust event #%d", eventID)
			if index < len(d.bustLabels) {
				label = d.bustLabels[index]
			}
			lines = append(lines, mark+label)
		}
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter choose · Esc cancel"))
	case dialogSelectParticipant:
		for index, participant := range d.participants {
			mark := "  "
			if index == d.cursor {
				mark = "> "
			}
			name := participant.PlayerID
			if player, ok := m.playerByID(participant.PlayerID); ok {
				name = player.Name
			}
			tie := ""
			if participant.Tied {
				tie = " tied"
			}
			lines = append(lines, fmt.Sprintf("%sBust #%d · %s · %s · order %d%s", mark, participant.BustEventID, name, participant.Role, participant.Position, tie))
		}
		lines = append(lines, helpStyle.Render("↑/↓ select · Enter replace · D remove · Esc cancel"))
	}
	if m.notice != "" {
		lines = append(lines, "", warnStyle.Render(m.notice))
	}
	if m.statusMessage != "" {
		lines = append(lines, "", renderStatus(m.statusMessage, m.statusIsError))
	}
	content := strings.Join(lines, "\n")
	width := m.width - 8
	if width > 92 {
		width = 92
	}
	if width < 40 {
		width = 40
	}
	box := panelStyle.Width(width).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func playerNames(model *Model, ids []string) string {
	var names []string
	for _, id := range ids {
		if player, ok := model.playerByID(id); ok {
			names = append(names, player.Name)
		} else {
			names = append(names, id)
		}
	}
	return strings.Join(names, ", ")
}

func renderChoices(choices []string, cursor int) []string {
	lines := make([]string, 0, len(choices))
	for index, choice := range choices {
		mark := "  "
		if index == cursor {
			mark = "> "
		}
		lines = append(lines, mark+choice)
	}
	return lines
}

func (m *Model) rulesView() string {
	lines := strings.Split(m.rulesText, "\n")
	maxOffset := len(lines) - (m.height - 5)
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.rulesOffset > maxOffset {
		m.rulesOffset = maxOffset
	}
	end := m.rulesOffset + m.height - 4
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[m.rulesOffset:end], "\n")
	footer := helpStyle.Render("Arrows scroll · PgUp/PgDn page · Home/End jump · ? or Esc close · Space pause/resume")
	return titleStyle.Render("TEXAS HOLD’EM RULES") + "\n\n" + body + "\n\n" + footer
}
