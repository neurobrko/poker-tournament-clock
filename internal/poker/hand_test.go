package poker

import "testing"

func TestFlushTieBreaksRanksNotSuits(t *testing.T) {
	kingHigh, err := EvaluateBest(cards(Hearts, 13, 11, 9, 3, 2))
	if err != nil {
		t.Fatal(err)
	}
	other, err := EvaluateBest(cards(Clubs, 13, 11, 7, 6, 5))
	if err != nil {
		t.Fatal(err)
	}
	if Compare(kingHigh, other) <= 0 {
		t.Fatalf("K-J-9-3-2 flush should beat K-J-7-6-5: %+v vs %+v", kingHigh, other)
	}
	sameRanks, err := EvaluateBest(cards(Spades, 13, 11, 9, 3, 2))
	if err != nil {
		t.Fatal(err)
	}
	if Compare(kingHigh, sameRanks) != 0 {
		t.Fatalf("suits changed equal flushes: %+v vs %+v", kingHigh, sameRanks)
	}
}

func TestBestHandCategoriesAndKickers(t *testing.T) {
	tests := []struct {
		name  string
		cards []Card
		want  HandValue
	}{
		{"wheel", []Card{{14, Hearts}, {2, Clubs}, {3, Spades}, {4, Hearts}, {5, Diamonds}}, HandValue{Category: Straight, Tie: [5]uint8{5}}},
		{"duplicate rank is not a straight", []Card{{9, Hearts}, {9, Clubs}, {8, Spades}, {7, Hearts}, {5, Diamonds}}, HandValue{Category: OnePair, Tie: [5]uint8{9, 8, 7, 5}}},
		{"straight flush", cards(Spades, 10, 11, 12, 13, 14), HandValue{Category: StraightFlush, Tie: [5]uint8{14}}},
		{"four of a kind", []Card{{9, Hearts}, {9, Clubs}, {9, Spades}, {9, Diamonds}, {14, Hearts}}, HandValue{Category: FourOfAKind, Tie: [5]uint8{9, 14}}},
		{"full house", []Card{{14, Hearts}, {14, Clubs}, {14, Spades}, {13, Hearts}, {13, Diamonds}}, HandValue{Category: FullHouse, Tie: [5]uint8{14, 13}}},
		{"three of a kind kickers", []Card{{12, Hearts}, {12, Clubs}, {12, Spades}, {14, Diamonds}, {9, Hearts}}, HandValue{Category: ThreeOfAKind, Tie: [5]uint8{12, 14, 9}}},
		{"two pair kicker", []Card{{14, Hearts}, {14, Clubs}, {13, Spades}, {13, Hearts}, {12, Diamonds}}, HandValue{Category: TwoPair, Tie: [5]uint8{14, 13, 12}}},
		{"pair kickers", []Card{{11, Hearts}, {11, Clubs}, {14, Spades}, {9, Hearts}, {3, Diamonds}}, HandValue{Category: OnePair, Tie: [5]uint8{11, 14, 9, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateBest(tt.cards)
			if err != nil {
				t.Fatal(err)
			}
			if got.Category != tt.want.Category || got.Tie != tt.want.Tie {
				t.Fatalf("hand value = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBestOfSevenCanUseBoardAndHoleCards(t *testing.T) {
	cards := []Card{
		{14, Spades}, {13, Spades}, // hole
		{12, Spades}, {11, Spades}, {10, Spades}, {2, Hearts}, {2, Clubs}, // board
	}
	got, err := EvaluateBest(cards)
	if err != nil {
		t.Fatal(err)
	}
	if got.Category != StraightFlush || got.Tie != [5]uint8{14} {
		t.Fatalf("best seven-card hand = %+v", got)
	}
}

func TestBestHandCardsAreAllFiveInRankingOrder(t *testing.T) {
	trips, err := EvaluateBest([]Card{{4, Clubs}, {4, Diamonds}, {4, Hearts}, {14, Spades}, {12, Clubs}})
	if err != nil {
		t.Fatal(err)
	}
	if trips.Cards != ([5]Card{{4, Clubs}, {4, Diamonds}, {4, Hearts}, {14, Spades}, {12, Clubs}}) {
		t.Fatalf("three-of-a-kind display cards = %+v", trips.Cards)
	}
	pairs, err := EvaluateBest([]Card{{12, Clubs}, {12, Hearts}, {4, Clubs}, {4, Diamonds}, {14, Spades}})
	if err != nil {
		t.Fatal(err)
	}
	if pairs.Cards != ([5]Card{{12, Clubs}, {12, Hearts}, {4, Clubs}, {4, Diamonds}, {14, Spades}}) {
		t.Fatalf("two-pair display cards = %+v", pairs.Cards)
	}
}

func TestEqualBestHandsTie(t *testing.T) {
	board := []Card{{14, Hearts}, {14, Clubs}, {13, Spades}, {13, Hearts}, {12, Diamonds}}
	first, err := EvaluateBest(append(append([]Card(nil), board...), Card{2, Hearts}, Card{3, Clubs}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateBest(append(append([]Card(nil), board...), Card{4, Hearts}, Card{5, Clubs}))
	if err != nil {
		t.Fatal(err)
	}
	if Compare(first, second) != 0 {
		t.Fatalf("board-only best hand should tie: %+v vs %+v", first, second)
	}
}

func TestEvaluateBestRejectsInvalidInputs(t *testing.T) {
	for _, cards := range [][]Card{
		{{2, Clubs}, {3, Clubs}, {4, Clubs}, {5, Clubs}},
		{{2, Clubs}, {3, Clubs}, {4, Clubs}, {5, Clubs}, {2, Clubs}},
		{{1, Clubs}, {3, Clubs}, {4, Clubs}, {5, Clubs}, {6, Clubs}},
	} {
		if _, err := EvaluateBest(cards); err == nil {
			t.Errorf("EvaluateBest(%v) accepted invalid cards", cards)
		}
	}
}

func cards(suit Suit, ranks ...uint8) []Card {
	out := make([]Card, len(ranks))
	for i, rank := range ranks {
		out[i] = Card{Rank: rank, Suit: suit}
	}
	return out
}
