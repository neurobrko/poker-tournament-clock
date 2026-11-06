package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

type playerHistoryItem struct {
	at      time.Time
	text    string
	section string
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
	record, err := m.database.Tournament(context.Background())
	if err != nil {
		m.showMessage("Player history unavailable", err.Error())
		return
	}
	ranking, err := playerHistoryRanking(context.Background(), m.service, record, player)
	if err != nil {
		m.showMessage("Player history unavailable", err.Error())
		return
	}
	eventByID := make(map[int64]time.Time, len(events))
	bustNumber := bustNumbersByEventID(events)
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
		item := playerHistoryItem{section: "PURCHASES", text: fmt.Sprintf("%s · %s · %s chips", label, purchase.PriceCents, formatDisplayChips(purchase.Chips))}
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
						items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Buster in bust #%d%s · %s", bustNumber[event.ID], voided, event.WallTime.Format("02 Jan 15:04"))})
					}
				}
				for _, bustee := range original.Bustees {
					if bustee.PlayerID == player.ID {
						items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Busted in bust #%d%s · %s", bustNumber[event.ID], voided, event.WallTime.Format("02 Jan 15:04"))})
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
					items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Buster in bust #%d%s · %s", bustNumber[event.ID], voided, event.WallTime.Format("02 Jan 15:04"))})
					continue
				}
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Busted in bust #%d%s · %s", bustNumber[event.ID], voided, event.WallTime.Format("02 Jan 15:04"))})
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
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Corrected out of bust #%d · %s", bustNumber[payload.BustEventID], event.WallTime.Format("02 Jan 15:04"))})
			}
			if payload.Replacement != nil && payload.Replacement.PlayerID == player.ID {
				items = append(items, playerHistoryItem{at: event.WallTime, text: fmt.Sprintf("Added to bust #%d by correction · %s", bustNumber[payload.BustEventID], event.WallTime.Format("02 Jan 15:04"))})
			}
		}
	}
	for index := range items {
		if items[index].section == "" {
			items[index].section = "BUSTS"
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at.After(items[j].at) })
	lines := make([]string, 0, len(items)+7)
	if ranking != "" {
		lines = append(lines, "RANKING", "  • "+ranking, "")
	}
	if len(items) == 0 {
		lines = append(lines, "No purchases or busts recorded yet.")
	} else {
		for _, section := range []string{"PURCHASES", "BUSTS"} {
			lines = append(lines, section+" · newest first")
			found := false
			for _, item := range items {
				if item.section != section {
					continue
				}
				lines = append(lines, "  • "+item.text)
				found = true
			}
			if !found {
				lines = append(lines, "  —")
			}
			lines = append(lines, "")
		}
		lines = lines[:len(lines)-1]
	}
	m.showMessage("Player history · "+player.Name, strings.Join(lines, "\n"))
}

func playerHistoryRanking(ctx context.Context, service *tournament.Service, record store.TournamentRecord, player tournament.Player) (string, error) {
	if record.Status == store.StatusEnded && record.ResultCompleted != nil && *record.ResultCompleted {
		final, err := service.LatestFinalResult(ctx)
		if err == nil {
			if first, last, ok := playerFinish(final.Standings.Groups, player.ID); ok {
				label := "Final result · " + finishRange(first, last)
				for _, group := range final.Prizes {
					for _, prize := range group.Players {
						if prize.PlayerID == player.ID && prize.AmountCents > 0 {
							label += " · prize " + config.EuroCents(prize.AmountCents).String()
						}
					}
				}
				return label, nil
			}
		} else if !errors.Is(err, tournament.ErrNoFinalResult) {
			return "", err
		}
	}
	if !rankingReady(record) || !player.HasInitialBuyIn || player.Active || player.Removed {
		return "", nil
	}
	standings, err := service.CurrentStandings(ctx)
	if err != nil {
		return "", err
	}
	if first, last, ok := playerFinish(standings.Groups, player.ID); ok {
		return finishRange(first, last), nil
	}
	return "", nil
}

func rankingReady(record store.TournamentRecord) bool {
	if !record.RegistrationClosed {
		return false
	}
	deadline := record.Config.Registration.RegistrationDeadline
	kind, target := config.SegmentLevel, deadline.Target
	if deadline.At == config.DeadlineAtBreakStart || deadline.At == config.DeadlineAtBreakEnd {
		kind = config.SegmentBreak
	}
	index, ordinal := 0, 0
	for i, segment := range record.Config.Schedule {
		if segment.Kind == kind {
			ordinal++
			if ordinal == target {
				index = i + 1
				break
			}
		}
	}
	if kind == config.SegmentBreak {
		index++
	}
	return index > 0 && record.Checkpoint.SegmentIndex >= index
}

func playerFinish(groups []domain.FinishGroup, playerID string) (first, last int, found bool) {
	first = 1
	for _, group := range groups {
		last = first + len(group.PlayerIDs) - 1
		for _, id := range group.PlayerIDs {
			if id == playerID {
				return first, last, true
			}
		}
		first = last + 1
	}
	return 0, 0, false
}

func finishRange(first, last int) string {
	if first == last {
		return ordinalPlace(first)
	}
	return ordinalPlace(first) + "–" + ordinalPlace(last)
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
