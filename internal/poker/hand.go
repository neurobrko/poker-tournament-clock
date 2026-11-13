package poker

import (
	"errors"
	"fmt"
	"sort"
)

type Suit uint8

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
)

type Card struct {
	Rank uint8
	Suit Suit
}

type Category uint8

const (
	HighCard Category = iota
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

type HandValue struct {
	Category Category
	Tie      [5]uint8
	Cards    [5]Card
}

func RankName(rank uint8) string {
	switch rank {
	case 14:
		return "A"
	case 13:
		return "K"
	case 12:
		return "Q"
	case 11:
		return "J"
	case 10:
		return "10"
	default:
		return fmt.Sprint(rank)
	}
}

func (c Category) String() string {
	return [...]string{"High card", "One pair", "Two pair", "Three of a kind", "Straight", "Flush", "Full house", "Four of a kind", "Straight flush"}[c]
}

// EvaluateBest returns the best five-card poker value from five, six, or seven unique cards.
func EvaluateBest(cards []Card) (HandValue, error) {
	if len(cards) < 5 || len(cards) > 7 {
		return HandValue{}, errors.New("hand requires five to seven cards")
	}
	seen := make(map[Card]bool, len(cards))
	for _, card := range cards {
		if card.Rank < 2 || card.Rank > 14 || card.Suit > Spades {
			return HandValue{}, fmt.Errorf("invalid card: %+v", card)
		}
		if seen[card] {
			return HandValue{}, fmt.Errorf("duplicate card: %+v", card)
		}
		seen[card] = true
	}

	var best HandValue
	first := true
	for a := 0; a < len(cards)-4; a++ {
		for b := a + 1; b < len(cards)-3; b++ {
			for c := b + 1; c < len(cards)-2; c++ {
				for d := c + 1; d < len(cards)-1; d++ {
					for e := d + 1; e < len(cards); e++ {
						value := evaluateFive([5]Card{cards[a], cards[b], cards[c], cards[d], cards[e]})
						if first || Compare(value, best) > 0 {
							best, first = value, false
						}
					}
				}
			}
		}
	}
	return best, nil
}

// Compare returns 1 when a wins, -1 when b wins, and 0 for an exact tie.
func Compare(a, b HandValue) int {
	if a.Category != b.Category {
		if a.Category > b.Category {
			return 1
		}
		return -1
	}
	for i := range a.Tie {
		if a.Tie[i] != b.Tie[i] {
			if a.Tie[i] > b.Tie[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func evaluateFive(cards [5]Card) HandValue {
	counts := [15]uint8{}
	ranks := [5]uint8{}
	flush := true
	for i, card := range cards {
		counts[card.Rank]++
		ranks[i] = card.Rank
		if i > 0 && card.Suit != cards[0].Suit {
			flush = false
		}
	}
	sort.Slice(ranks[:], func(i, j int) bool { return ranks[i] > ranks[j] })
	straightHigh := straight(ranks)

	groups := make([]rankGroup, 0, 5)
	for rank := uint8(14); rank >= 2; rank-- {
		if counts[rank] > 0 {
			groups = append(groups, rankGroup{rank: rank, count: counts[rank]})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].rank > groups[j].rank
	})

	var value HandValue
	switch {
	case flush && straightHigh > 0:
		value = HandValue{Category: StraightFlush}
		value.Tie[0] = straightHigh
	case groups[0].count == 4:
		value = HandValue{Category: FourOfAKind}
		value.Tie[0], value.Tie[1] = groups[0].rank, groups[1].rank
	case groups[0].count == 3 && groups[1].count == 2:
		value = HandValue{Category: FullHouse}
		value.Tie[0], value.Tie[1] = groups[0].rank, groups[1].rank
	case flush:
		value = HandValue{Category: Flush, Tie: ranks}
	case straightHigh > 0:
		value = HandValue{Category: Straight}
		value.Tie[0] = straightHigh
	case groups[0].count == 3:
		value = HandValue{Category: ThreeOfAKind}
		value.Tie[0] = groups[0].rank
		copy(value.Tie[1:], ranksWithoutCounts(counts))
	case groups[0].count == 2 && groups[1].count == 2:
		value = HandValue{Category: TwoPair}
		value.Tie[0], value.Tie[1] = groups[0].rank, groups[1].rank
		value.Tie[2] = groups[2].rank
	case groups[0].count == 2:
		value = HandValue{Category: OnePair}
		value.Tie[0] = groups[0].rank
		copy(value.Tie[1:], ranksWithoutCounts(counts))
	default:
		value = HandValue{Category: HighCard, Tie: ranks}
	}
	value.Cards = orderedCards(cards, value.Category, straightHigh == 5)
	return value
}

func orderedCards(cards [5]Card, category Category, wheel bool) [5]Card {
	counts := [15]uint8{}
	for _, card := range cards {
		counts[card.Rank]++
	}
	grouped := category == FourOfAKind || category == FullHouse || category == ThreeOfAKind || category == TwoPair || category == OnePair
	sort.Slice(cards[:], func(i, j int) bool {
		a, b := cards[i], cards[j]
		if grouped && counts[a.Rank] != counts[b.Rank] {
			return counts[a.Rank] > counts[b.Rank]
		}
		aRank, bRank := a.Rank, b.Rank
		if wheel && aRank == 14 {
			aRank = 1
		}
		if wheel && bRank == 14 {
			bRank = 1
		}
		if aRank != bRank {
			return aRank > bRank
		}
		return a.Suit < b.Suit
	})
	return cards
}

type rankGroup struct {
	rank  uint8
	count uint8
}

func straight(ranks [5]uint8) uint8 {
	if ranks[0]-ranks[4] == 4 {
		for i := 1; i < len(ranks); i++ {
			if ranks[i-1]-ranks[i] != 1 {
				return 0
			}
		}
		return ranks[0]
	}
	if ranks == [5]uint8{14, 5, 4, 3, 2} {
		return 5
	}
	return 0
}

func ranksWithoutCounts(counts [15]uint8) []uint8 {
	out := make([]uint8, 0, 3)
	for rank := uint8(14); rank >= 2; rank-- {
		if counts[rank] == 1 {
			out = append(out, rank)
		}
	}
	return out
}
