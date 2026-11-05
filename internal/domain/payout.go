package domain

import (
	"fmt"
	"math/big"
	"sort"

	"git.mpx.sk/elvis/poker_clock/internal/config"
)

// FinishGroup lists players finishing at the same place. Groups are ordered best to worst.
type FinishGroup struct {
	PlayerIDs []string
}

type PlayerPrize struct {
	PlayerID      string
	AmountCents   int64
	GetsRemainder bool
}

type PrizeGroup struct {
	FirstPlace            int
	LastPlace             int
	TotalCents            int64
	Players               []PlayerPrize
	PendingRemainderCents int64
}

type PlacePrize struct {
	Place       int
	AmountCents int64
}

// CalculatePlacePrizes returns the current per-place payout schedule, independent of player standings.
func CalculatePlacePrizes(poolCents int64, entrants int, tiers []config.PrizeTier) ([]PlacePrize, error) {
	if poolCents < 0 || poolCents%10 != 0 {
		return nil, fmt.Errorf("prize pool must be nonnegative and a multiple of €0.10")
	}
	if entrants < 0 {
		return nil, fmt.Errorf("entrant count cannot be negative")
	}
	if entrants == 0 {
		return nil, nil
	}
	if entrants == 1 {
		return []PlacePrize{{Place: 1, AmountCents: poolCents}}, nil
	}
	tier, err := selectPrizeTier(entrants, tiers)
	if err != nil {
		return nil, err
	}
	places := len(tier.Percentages)
	if places > entrants {
		places = entrants
	}
	amounts, err := allocatePlaces(poolCents, tier.Percentages[:places])
	if err != nil {
		return nil, err
	}
	result := make([]PlacePrize, len(amounts))
	for index, amount := range amounts {
		result[index] = PlacePrize{Place: index + 1, AmountCents: amount}
	}
	return result, nil
}

// CalculatePrizes allocates a pool by entrant tier, rounds place prizes to €0.10,
// renormalizes for short fields, and defers tied-split remainders until chosen.
// extraChoices maps each tied group's first place to the player IDs receiving spare €0.10 units.
func CalculatePrizes(poolCents int64, entrants int, tiers []config.PrizeTier, finishes []FinishGroup, extraChoices map[int][]string) ([]PrizeGroup, error) {
	if poolCents < 0 || poolCents%10 != 0 {
		return nil, fmt.Errorf("prize pool must be nonnegative and a multiple of €0.10")
	}
	if entrants < 0 {
		return nil, fmt.Errorf("entrant count cannot be negative")
	}
	if entrants == 0 {
		if len(finishes) != 0 {
			return nil, fmt.Errorf("cannot assign finishing places without entrants")
		}
		if len(extraChoices) != 0 {
			return nil, fmt.Errorf("cannot assign tie remainders without finishing places")
		}
		return nil, nil
	}
	if err := validateFinishes(entrants, finishes); err != nil {
		return nil, err
	}

	placePrizes, err := CalculatePlacePrizes(poolCents, entrants, tiers)
	if err != nil {
		return nil, err
	}
	placeCents := make([]int64, len(placePrizes))
	for index, prize := range placePrizes {
		placeCents[index] = prize.AmountCents
	}

	results := make([]PrizeGroup, 0, len(finishes))
	knownChoiceGroups := make(map[int]struct{})
	place := 1
	for _, finish := range finishes {
		first := place
		last := place + len(finish.PlayerIDs) - 1
		place = last + 1
		groupPool := int64(0)
		for awardedPlace := first; awardedPlace <= last && awardedPlace <= len(placeCents); awardedPlace++ {
			var err error
			groupPool, err = checkedAdd(groupPool, placeCents[awardedPlace-1])
			if err != nil {
				return nil, fmt.Errorf("tied prize pool overflows: %w", err)
			}
		}
		if groupPool == 0 {
			continue
		}
		units := groupPool / 10
		baseUnits := units / int64(len(finish.PlayerIDs))
		spareUnits := units % int64(len(finish.PlayerIDs))
		choices := extraChoices[first]
		if len(choices) > int(spareUnits) {
			return nil, fmt.Errorf("tie at place %d needs at most %d remainder choices", first, spareUnits)
		}
		choiceSet := make(map[string]struct{}, len(choices))
		for _, id := range choices {
			if _, duplicate := choiceSet[id]; duplicate {
				return nil, fmt.Errorf("player %q received more than one tied-prize remainder", id)
			}
			if !contains(finish.PlayerIDs, id) {
				return nil, fmt.Errorf("player %q is not tied at place %d", id, first)
			}
			choiceSet[id] = struct{}{}
		}
		players := make([]PlayerPrize, 0, len(finish.PlayerIDs))
		for _, id := range finish.PlayerIDs {
			amount := baseUnits * 10
			getsRemainder := false
			if _, chosen := choiceSet[id]; chosen {
				amount += 10
				getsRemainder = true
			}
			players = append(players, PlayerPrize{PlayerID: id, AmountCents: amount, GetsRemainder: getsRemainder})
		}
		pending := (spareUnits - int64(len(choices))) * 10
		if spareUnits > 0 {
			knownChoiceGroups[first] = struct{}{}
		}
		results = append(results, PrizeGroup{
			FirstPlace: first, LastPlace: last, TotalCents: groupPool,
			Players: players, PendingRemainderCents: pending,
		})
	}
	for group := range extraChoices {
		if _, ok := knownChoiceGroups[group]; !ok {
			return nil, fmt.Errorf("no tied prize remainder is pending for place %d", group)
		}
	}
	return results, nil
}

func validateFinishes(entrants int, finishes []FinishGroup) error {
	if len(finishes) == 0 {
		return fmt.Errorf("finishing places are required for %d entrants", entrants)
	}
	seen := make(map[string]struct{}, entrants)
	count := 0
	for index, group := range finishes {
		if len(group.PlayerIDs) == 0 {
			return fmt.Errorf("finish group %d cannot be empty", index+1)
		}
		for _, id := range group.PlayerIDs {
			if id == "" {
				return fmt.Errorf("finish group %d contains an empty player ID", index+1)
			}
			if _, exists := seen[id]; exists {
				return fmt.Errorf("player %q appears in more than one finishing place", id)
			}
			seen[id] = struct{}{}
			count++
		}
	}
	if count != entrants {
		return fmt.Errorf("finishing places contain %d players; expected %d entrants", count, entrants)
	}
	return nil
}

func selectPrizeTier(entrants int, tiers []config.PrizeTier) (config.PrizeTier, error) {
	if len(tiers) == 0 || tiers[0].MinPlayers != 2 {
		return config.PrizeTier{}, fmt.Errorf("first prize tier must begin at 2 players")
	}
	selected := -1
	previousThreshold := 1
	for index, tier := range tiers {
		if tier.MinPlayers <= previousThreshold {
			return config.PrizeTier{}, fmt.Errorf("prize tier thresholds must be strictly increasing")
		}
		if err := validatePercentages(tier.Percentages); err != nil {
			return config.PrizeTier{}, fmt.Errorf("prize tier %d: %w", index+1, err)
		}
		if tier.MinPlayers <= entrants {
			selected = index
		}
		previousThreshold = tier.MinPlayers
	}
	if selected < 0 {
		return config.PrizeTier{}, fmt.Errorf("no prize tier configured for %d entrants", entrants)
	}
	return tiers[selected], nil
}

func validatePercentages(percentages []config.Percentage) error {
	if len(percentages) == 0 {
		return fmt.Errorf("at least one payout place is required")
	}
	total := new(big.Rat)
	var previous *big.Rat
	for index, percentage := range percentages {
		value, ok := new(big.Rat).SetString(string(percentage))
		if !ok || value.Sign() <= 0 {
			return fmt.Errorf("payout percentage %d must be positive and decimal", index+1)
		}
		if previous != nil && value.Cmp(previous) > 0 {
			return fmt.Errorf("payout percentages must be non-increasing")
		}
		total.Add(total, value)
		previous = value
	}
	if total.Cmp(big.NewRat(100, 1)) != 0 {
		return fmt.Errorf("payout percentages must sum exactly to 100%%")
	}
	return nil
}

func allocatePlaces(poolCents int64, percentages []config.Percentage) ([]int64, error) {
	if len(percentages) == 0 {
		return nil, fmt.Errorf("no payout places configured")
	}
	poolUnits := big.NewInt(poolCents / 10)
	weights := make([]*big.Rat, len(percentages))
	totalWeight := new(big.Rat)
	for index, percentage := range percentages {
		weight, ok := new(big.Rat).SetString(string(percentage))
		if !ok || weight.Sign() <= 0 {
			return nil, fmt.Errorf("invalid payout percentage %q", percentage)
		}
		weights[index] = weight
		totalWeight.Add(totalWeight, weight)
	}

	amounts := make([]int64, len(percentages))
	remainders := make([]placeRemainder, len(percentages))
	allocated := int64(0)
	for index, weight := range weights {
		share := new(big.Rat).Mul(new(big.Rat).SetInt(poolUnits), weight)
		share.Quo(share, totalWeight)
		whole := new(big.Int).Quo(share.Num(), share.Denom())
		if !whole.IsInt64() {
			return nil, fmt.Errorf("payout allocation is too large")
		}
		units := whole.Int64()
		amounts[index] = units * 10
		allocated += units
		fraction := new(big.Rat).Sub(share, new(big.Rat).SetInt(whole))
		remainders[index] = placeRemainder{place: index, fraction: fraction}
	}
	remaining := poolCents/10 - allocated
	sort.SliceStable(remainders, func(i, j int) bool {
		comparison := remainders[i].fraction.Cmp(remainders[j].fraction)
		if comparison == 0 {
			return remainders[i].place < remainders[j].place
		}
		return comparison > 0
	})
	for index := int64(0); index < remaining; index++ {
		place := remainders[index%int64(len(remainders))].place
		amounts[place] += 10
	}
	return amounts, nil
}

type placeRemainder struct {
	place    int
	fraction *big.Rat
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
