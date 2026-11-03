# Poker clock: stage 2 handoff

## Context

Project: `/data/code/poker_clock`. This is a planning handoff, not an implementation. Read `~/.agents/handoff/261103-1745_poker-clock-stage-1.md` for the application, persistence, TUI, audit, and tournament rules already agreed. Build stage 2 after the stage 1 foundation is ready; do not assume its APIs exist yet.

## Hand resolver `[H]`

- Select participants from currently active registered tournament players only; do not accept arbitrary names or busted players. Resolve Texas Hold'em hands from a **complete five-card community board**. The earlier idea of incomplete-board odds was explicitly withdrawn; no odds or Monte Carlo feature is required.
- Every non-folded player has exactly two known hole cards. A player with no hole cards is folded. Folded players' contributions remain in the pot, but they cannot win. Card entry must be fully keyboard-operable: pick a suit first, then a rank for each card. Support arrow keys and Enter, plus direct suit keys `D`, `C`, `H`, `S` and rank keys `2`–`9`, `T`, `J`, `Q`, `K`, `A`. A direct key selects immediately without Enter. Escape goes back; selected cards can be cleared, and cards already selected must be unavailable. Reject duplicate known cards and invalid card counts. Card-picker input takes precedence over global shortcuts, including `[Q]`, `[H]`, `[T]`, and `[A]`. Require sufficient information to make the result unambiguous.
- Evaluate each eligible player's best five-card hand, winners, hand rank, and ties. Support multiple winners and side pots; a player may be eligible for some pots and not others.
- The operator indicates each player's all-in status and chips committed over the **entire hand**, not just the current betting round. Collect antes separately from matched bets (which include blinds). In particular, a big-blind ante is dead money, not an unmatched bet to return. Derive side pots from contributions, ante rules, and eligibility. Return genuinely uncalled excess rather than treating it as an awarded pot.
- Regression example: three players each commit a matched bet of 100 chips, and the big blind also posts a 100-chip big-blind ante. All 400 chips are in the pot; none of the ante is returned as unmatched.
- The operator enters the smallest chip denomination in play. Split tied pots in whole chips of that denomination; for example, a 25-chip pot split between two players with 5-value chips pays 15 and 10. Let the operator choose which tied winner receives each extra chip. Do not require seat or dealer-button tracking. Verify chip conservation: returned excess plus all awards equals all contributions. Validate impossible or contradictory input and explain errors.
- The resolver is informational and results are transient: do not save resolved hands, automatically change tournament state, or record busts. A winning hand alone does not establish who has chips remaining outside the pot. The operator continues to record busts separately.

## Player history

Stage 1 stores player history but `[P]` remains limited to adding/removing players. In stage 2, `[T]` opens a separate timeline for a selected player. Include removed players, clearly marked as removed, so their audit histories remain accessible. Show relevant existing audit records, including purchases, bust participation as buster or bustee, sit-outs and returns, removal/restoration, voids, all stored timestamps, and required correction explanations. Broader report generation or export is not yet specified.

## Acceptance and design priorities

- Test every hand category, wheel straights, board-playing hands, ties/kickers, duplicate cards, folded contributions, unequal all-ins, separately recorded antes including big-blind ante, multiple side pots, tied winners with odd chips in non-unit denominations, uncalled excess, and conservation of integer chips.
- Keep resolver interactions separate from recorded tournament results. Do not imply that entered hand contributions are known player stacks.
- Clarify with the user any unagreed side-pot validation details, input UX, and additional history-browsing scope before imposing consequential rules. In particular, all-in indicators must be captured, but do not assume every player's entire stack is otherwise tracked.

## Suggested skills

- `grill-me`: resolve remaining product questions. Its installed wrapper refers to a `grilling` skill unavailable in the current harness; interview directly if still unavailable.
- `handoff`: update this handoff as stage 2 decisions change.
- `caveman`: use the user's requested concise conversational style. Keep persisted documents clear and complete.
