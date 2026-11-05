package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

type playerHistoryItem struct {
	at   time.Time
	text string
}

func (m *Model) showPlayerHistory(player tournament.Player) {
	purchases, err := m.service.Purchases(context.Background(), player.ID)
	if err != nil {
		m.showMessage("Player history unavailable", err.Error())
		return
	}
	events, err := m.database.Events(context.Background())
	if err != nil {
		m.showMessage("Player history unavailable", err.Error())
		return
	}
	eventByID := make(map[int64]time.Time, len(events))
	voidedBusts := make(map[int64]bool)
	for _, event := range events {
		eventByID[event.ID] = event.WallTime
		if event.Kind == "bust_voided" && event.VoidEventID != nil {
			voidedBusts[*event.VoidEventID] = true
		}
	}

	items := make([]playerHistoryItem, 0)
	for _, purchase := range purchases {
		label := purchaseLabel(purchase.Kind)
		item := playerHistoryItem{text: fmt.Sprintf("%s · %s · %s chips", label, purchase.PriceCents, formatDisplayChips(purchase.Chips))}
		if at, ok := eventByID[purchase.EventID]; ok {
			item.at = at
		}
		if purchase.VoidedByEventID != nil {
			item.text += " · voided"
		}
		item.text += " · " + item.at.Format("02 Jan 15:04")
		items = append(items, item)
	}

	for _, event := range events {
		switch event.Kind {
		case "bust_recorded":
			voided := ""
			if voidedBusts[event.ID] {
				voided = " · voided"
				var original struct {
					Busters []string            `json:"busters"`
					Bustees []tournament.Bustee `json:"bustees"`
				}
				if err := json.Unmarshal(event.Payload, &original); err != nil {
					continue
				}
				for _, id := range original.Busters {
					if id == player.ID {
						items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Buster in bust #%d%s · %s", event.ID, voided, event.WallTime.Format("02 Jan 15:04"))})
					}
				}
				for _, bustee := range original.Bustees {
					if bustee.PlayerID == player.ID {
						finish := ordinalPlace(bustee.Position)
						if bustee.Tied {
							finish += " (tie)"
						}
						items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Busted · %s in bust #%d%s · %s", finish, event.ID, voided, event.WallTime.Format("02 Jan 15:04"))})
					}
				}
				continue
			}
			participants, err := m.service.EffectiveBustParticipants(context.Background(), event.ID)
			if err != nil {
				m.showMessage("Player history unavailable", err.Error())
				return
			}
			for _, participant := range participants {
				if participant.PlayerID != player.ID {
					continue
				}
				if participant.Role == "buster" {
					items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Buster in bust #%d%s · %s", event.ID, voided, event.WallTime.Format("02 Jan 15:04"))})
					continue
				}
				finish := ordinalPlace(participant.Position)
				if participant.Tied {
					finish += " (tie)"
				}
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Busted · %s in bust #%d%s · %s", finish, event.ID, voided, event.WallTime.Format("02 Jan 15:04"))})
			}
		case "bust_participant_corrected":
			var payload struct {
				BustEventID int64                       `json:"bust_event_id"`
				Supersedes  tournament.BustParticipant  `json:"supersedes"`
				Replacement *tournament.BustParticipant `json:"replacement"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				continue
			}
			if payload.Supersedes.PlayerID == player.ID {
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Corrected out of bust #%d · %s", payload.BustEventID, event.WallTime.Format("02 Jan 15:04"))})
			}
			if payload.Replacement != nil && payload.Replacement.PlayerID == player.ID {
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Added to bust #%d by correction · %s", payload.BustEventID, event.WallTime.Format("02 Jan 15:04"))})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at.After(items[j].at) })
	lines := make([]string, 0, len(items)+1)
	if len(items) == 0 {
		lines = append(lines, "No purchases or busts recorded yet.")
	} else {
		lines = append(lines, "Purchases and busts · newest first")
		for _, item := range items {
			lines = append(lines, item.text)
		}
	}
	m.showMessage("Player history · "+player.Name, strings.Join(lines, "\n"))
}

func purchaseLabel(kind tournament.PurchaseKind) string {
	switch kind {
	case tournament.PurchaseInitialBuyIn:
		return "Buy-in"
	case tournament.PurchaseRebuy:
		return "Rebuy"
	case tournament.PurchaseAddOn:
		return "Add-on"
	default:
		return string(kind)
	}
}
