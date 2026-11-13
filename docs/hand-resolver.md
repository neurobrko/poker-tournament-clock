# Hand Resolver rules

This document defines Texas Hold’em hand comparison used by Hand Resolver. It is an implementation reference, not a tournament ruling or pot calculator.

## Inputs and validation

- Use one standard 52-card deck: ranks `2`–`10`, `J`, `Q`, `K`, `A`; suits clubs, diamonds, hearts, spades.
- Use one community board only (no multiple runouts). Resolve requires exactly five community cards and at least two players with exactly two hole cards each.
- Start with two numbered players; allow adding/removing players down to two. Player names are optional.
- No card may appear more than once across the board and all players’ hole cards.
- With five board cards, deck capacity permits at most 23 players (`5 + 2×23 = 51`).
- Evaluate each player’s best five-card hand from their two hole cards and the five community cards. Using zero, one, or two hole cards is allowed.
- Resolve strength only. Do not infer betting, folded/all-in eligibility, side pots, chip splits, or tournament results.

## Hand categories, strongest first

Compare category first, then compare that category’s tie-break values from left to right. Larger rank values are stronger (`2` = 2 through `A` = 14). Suits never break ties.

| Category | Tie-break values, in order |
| --- | --- |
| Straight flush | Highest card in the straight |
| Four of a kind | Quad rank, then kicker |
| Full house | Three-of-a-kind rank, then pair rank |
| Flush | All five ranks, descending |
| Straight | Highest card in the straight |
| Three of a kind | Trip rank, then both kickers, descending |
| Two pair | Higher pair, lower pair, then kicker |
| One pair | Pair rank, then all three kickers, descending |
| High card | All five ranks, descending |

An ace-high straight flush is commonly called a royal flush; it remains the top straight-flush hand, not a separate category. Ace can be high (`10-J-Q-K-A`) or low (`A-2-3-4-5`). The ace-low straight’s high-card value is 5. Ace cannot wrap (`Q-K-A-2-3` is not a straight).

## Comparing Hold’em hands

For each player, enumerate the 21 five-card subsets of their seven available cards, classify each subset, and retain the strongest. Compare retained hands by category and tie-break values above. Equal category and equal tie-break values mean a tie; do not use suits, unused hole cards, or the order in which cards were selected as further tie-breakers.

For two flushes, compare their highest card; if equal, compare second-highest, then third, fourth, and fifth. Example: `K-J-9-3-2` beats `K-J-7-6-5` because the first two ranks tie and 9 beats 7. If all five ranks tie, the flushes tie regardless of suit.

Apply same lexicographic comparison to each category’s tie-break values. For example, equal pairs compare highest kicker, then next kicker, then last; equal two-pair hands compare the high pair, low pair, then kicker. Only the best five cards count: unused hole cards cannot break ties. A board-only best hand can be shared by every player and therefore produce a tie.

Display players in descending hand-strength order. Players with equal best hands share the same place/rank.

## Resolver interaction

Resolver opens with its single five-card community board ready for selection. From a tournament, it loads active players; otherwise it starts with two numbered players. The complete deck stays visible as a suit-row/rank-column grid, using same card size as board/hole slots. Selected cards become unavailable. Resolved ranking appears in a separate pane to the deck’s right. Clicking a filled slot focuses it; selecting another available card replaces it and returns the old card to the deck. Backspace clears the focused card, or the most recently selected card if focus is empty. `R` clears every card slot. Tab/arrows move through community and player hole-card slots. `+` adds a player; `-` opens a multi-select removal list, while each player row also has a direct mouse remove button. `E` opens the global Edit name picker. Keyboard selection is rank (`J`, `K`, `Q`, `A`, `2`–`9`, `0` or `1` for ten) followed by suit (`H`, `C`, `S`, `D`). The table’s left-side suit legend uses those white letters.

## Sources

- Pagat, [Poker Hand Ranking](https://www.pagat.com/poker/rules/ranking.html), especially its standard-hand section: category order, ace-high/ace-low straights, kickers, flush-by-flush comparison, ties, and the rule that unused cards do not count. Retrieved during development.
- Card Player, [Poker Hands Ranked in Order](https://www.cardplayer.com/rules-of-poker/hand-rankings), for Hold’em hand descriptions and suit-neutral comparisons. Retrieved during development.
- Project’s [Texas Hold’em reference](../internal/ui/rules.md), which documents five-card best-hand selection, ace-high/ace-low straights, suit-neutral comparisons, and split ties.

These pages are development references only. Runtime resolution follows this document.
