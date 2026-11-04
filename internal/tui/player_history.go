package tui

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
	eventByID := make(map[int64]struct {
		kind string
		at   time.Time
	}, len(events))
	voidedBusts := make(map[int64]bool)
	for _, event := range events {
		eventByID[event.ID] = struct {
			kind string
			at   time.Time
		}{kind: event.Kind, at: event.WallTime}
		if event.Kind == "bust_voided" && event.VoidEventID != nil {
			voidedBusts[*event.VoidEventID] = true
		}
	}

	items := make([]playerHistoryItem, 0)
	for _, purchase := range purchases {
		label := purchaseLabel(purchase.Kind)
		item := playerHistoryItem{text: fmt.Sprintf("%s · %s · %s chips", label, purchase.PriceCents, formatDisplayChips(purchase.Chips))}
		if event, ok := eventByID[purchase.EventID]; ok {
			item.at = event.at
		}
		if purchase.VoidedByEventID != nil {
			item.text += " · voided"
		}
		item.text += " · " + item.at.Format("02 Jan 15:04")
		items = append(items, item)
	}

	for _, event := range events {
		if event.Kind != "bust_recorded" {
			continue
		}
		var payload struct {
			Busters []string            `json:"busters"`
			Bustees []tournament.Bustee `json:"bustees"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}
		voided := ""
		if voidedBusts[event.ID] {
			voided = " · voided"
		}
		for _, id := range payload.Busters {
			if id == player.ID {
				items = append(items, playerHistoryItem{
					at: event.WallTime, text: fmt.Sprintf("Buster in bust #%d%s · %s", event.ID, voided, event.WallTime.Format("02 Jan 15:04")),
				})
			}
		}
		for _, bustee := range payload.Bustees {
			if bustee.PlayerID != player.ID {
				continue
			}
			finish := ordinalPlace(bustee.Position)
			if bustee.Tied {
				finish += " (tie)"
			}
			items = append(items, playerHistoryItem{
				at: event.WallTime, text: fmt.Sprintf("Busted · %s in bust #%d%s · %s", finish, event.ID, voided, event.WallTime.Format("02 Jan 15:04")),
			})
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
