package domain

import (
	"reflect"
	"testing"

	"git.mpx.sk/elvis/poker_clock/internal/config"
)

func TestRequiredChipsForAnteKinds(t *testing.T) {
	tests := []struct {
		name    string
		segment config.Segment
		players int64
		want    int64
	}{
		{
			name: "no ante", segment: config.Segment{Kind: config.SegmentLevel, SmallBlind: 5000, BigBlind: 10000, Ante: config.AnteConfig{Kind: config.AnteNone}}, want: 15000,
		},
		{
			name: "big blind ante counted once", segment: config.Segment{Kind: config.SegmentLevel, SmallBlind: 5000, BigBlind: 10000, Ante: config.AnteConfig{Kind: config.AnteBigBlind, Amount: 10000}}, players: 8, want: 25000,
		},
		{
			name: "per-player ante includes sit-outs", segment: config.Segment{Kind: config.SegmentLevel, SmallBlind: 5, BigBlind: 10, Ante: config.AnteConfig{Kind: config.AntePerPlayer, Amount: 2}}, players: 3, want: 21,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RequiredChips(tt.segment, tt.players)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("RequiredChips = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLevelAvailabilityUsesInclusiveChipThreshold(t *testing.T) {
	level := config.Segment{Kind: config.SegmentLevel, SmallBlind: 5000, BigBlind: 10000, Ante: config.AnteConfig{Kind: config.AnteBigBlind, Amount: 10000}}
	available, required, reason, err := LevelAvailability(level, 25000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !available || required != 25000 || reason != "" {
		t.Fatalf("at-threshold availability = %v, %d, %q", available, required, reason)
	}
	available, required, reason, err = LevelAvailability(level, 24999, 2)
	if err != nil {
		t.Fatal(err)
	}
	if available || required != 25000 || reason == "" {
		t.Fatalf("below-threshold availability = %v, %d, %q", available, required, reason)
	}
}

func TestRequiredChipsRejectsOverflowAndInvalidSegment(t *testing.T) {
	level := config.Segment{Kind: config.SegmentLevel, SmallBlind: 1, BigBlind: 1, Ante: config.AnteConfig{Kind: config.AntePerPlayer, Amount: 1 << 62}}
	if _, err := RequiredChips(level, 4); err == nil {
		t.Fatal("RequiredChips accepted overflowing ante total")
	}
	if _, err := RequiredChips(config.Segment{Kind: config.SegmentBreak}, 1); err == nil {
		t.Fatal("RequiredChips accepted a break")
	}
}

func TestCalculatePlacePrizesSelectsInclusiveTiersAndUsesCurrentPool(t *testing.T) {
	tiers := []config.PrizeTier{
		{MinPlayers: 2, Percentages: []config.Percentage{"70", "30"}},
		{MinPlayers: 5, Percentages: []config.Percentage{"60", "25", "15"}},
		{MinPlayers: 10, Percentages: []config.Percentage{"40", "30", "20", "10"}},
	}
	cases := []struct {
		entrants int
		want     []int64
	}{
		{2, []int64{7000, 3000}},
		{4, []int64{7000, 3000}},
		{5, []int64{6000, 2500, 1500}},
		{10, []int64{4000, 3000, 2000, 1000}},
	}
	for _, test := range cases {
		got, err := CalculatePlacePrizes(10000, test.entrants, tiers)
		if err != nil {
			t.Fatalf("entrants %d: %v", test.entrants, err)
		}
		if len(got) != len(test.want) {
			t.Fatalf("entrants %d: prizes = %+v", test.entrants, got)
		}
		for index, amount := range test.want {
			if got[index].Place != index+1 || got[index].AmountCents != amount {
				t.Fatalf("entrants %d place %d = %+v, want %d cents", test.entrants, index+1, got[index], amount)
			}
		}
	}
	one, err := CalculatePlacePrizes(1230, 1, nil)
	if err != nil || len(one) != 1 || one[0].AmountCents != 1230 {
		t.Fatalf("one-entrant schedule = %+v, %v", one, err)
	}
	zero, err := CalculatePlacePrizes(0, 0, nil)
	if err != nil || len(zero) != 0 {
		t.Fatalf("zero-entrant schedule = %+v, %v", zero, err)
	}
}

func TestCalculatePrizesUsesExactMoneyAndLargestRemainder(t *testing.T) {
	groups := []FinishGroup{{PlayerIDs: []string{"first"}}, {PlayerIDs: []string{"second"}}, {PlayerIDs: []string{"third"}}}
	got, err := CalculatePrizes(1000, 3, []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"60", "25", "15"}}}, groups, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []PrizeGroup{
		{FirstPlace: 1, LastPlace: 1, TotalCents: 600, Players: []PlayerPrize{{PlayerID: "first", AmountCents: 600}}},
		{FirstPlace: 2, LastPlace: 2, TotalCents: 250, Players: []PlayerPrize{{PlayerID: "second", AmountCents: 250}}},
		{FirstPlace: 3, LastPlace: 3, TotalCents: 150, Players: []PlayerPrize{{PlayerID: "third", AmountCents: 150}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prizes = %+v, want %+v", got, want)
	}

	// 60/25/15 of €1 leaves two equal €0.05 remainders; lower place wins tie-break.
	got, err = CalculatePrizes(100, 3, []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"60", "25", "15"}}}, groups, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TotalCents != 60 || got[1].TotalCents != 30 || got[2].TotalCents != 10 {
		t.Fatalf("deterministic remainder distribution = %+v", got)
	}
	var total int64
	for _, prize := range got {
		total += prize.TotalCents
	}
	if total != 100 {
		t.Fatalf("allocated %d cents, want 100", total)
	}
}

func TestCalculatePrizesRenormalizesForShortFields(t *testing.T) {
	tiers := []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"60", "30", "10"}}}
	got, err := CalculatePrizes(1000, 2, tiers, []FinishGroup{{PlayerIDs: []string{"a"}}, {PlayerIDs: []string{"b"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TotalCents != 670 || got[1].TotalCents != 330 {
		t.Fatalf("two-player renormalized prizes = %+v, want 670/330 cents", got)
	}
}

func TestTiedPrizeRemainderIsPendingUntilChosen(t *testing.T) {
	tiers := []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"100"}}}
	finishes := []FinishGroup{{PlayerIDs: []string{"a", "b"}}}
	got, err := CalculatePrizes(90, 2, tiers, finishes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PendingRemainderCents != 10 || got[0].Players[0].AmountCents+got[0].Players[1].AmountCents != 80 {
		t.Fatalf("unresolved tied split = %+v", got[0])
	}

	got, err = CalculatePrizes(90, 2, tiers, finishes, map[int][]string{1: {"b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PendingRemainderCents != 0 || got[0].Players[0].AmountCents != 40 || got[0].Players[1].AmountCents != 50 || !got[0].Players[1].GetsRemainder {
		t.Fatalf("resolved tied split = %+v", got[0])
	}
	if _, err := CalculatePrizes(90, 2, tiers, finishes, map[int][]string{1: {"not-tied"}}); err == nil {
		t.Fatal("accepted a remainder choice for a player outside the tie")
	}
}

func TestCalculatePrizesHandlesSingleAndZeroEntrants(t *testing.T) {
	groups := []FinishGroup{{PlayerIDs: []string{"only"}}}
	got, err := CalculatePrizes(1230, 1, nil, groups, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TotalCents != 1230 || got[0].Players[0].AmountCents != 1230 {
		t.Fatalf("single entrant payout = %+v", got)
	}
	got, err = CalculatePrizes(0, 0, nil, nil, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("zero entrant result = %+v, %v", got, err)
	}
}

func TestCalculatePrizesRejectsInvalidFinishData(t *testing.T) {
	tiers := []config.PrizeTier{{MinPlayers: 2, Percentages: []config.Percentage{"100"}}}
	if _, err := CalculatePrizes(100, 2, tiers, []FinishGroup{{PlayerIDs: []string{"same"}}, {PlayerIDs: []string{"same"}}}, nil); err == nil {
		t.Fatal("accepted duplicate player finishing place")
	}
	if _, err := CalculatePrizes(101, 2, tiers, []FinishGroup{{PlayerIDs: []string{"a"}}, {PlayerIDs: []string{"b"}}}, nil); err == nil {
		t.Fatal("accepted pool not divisible into €0.10 units")
	}
}
