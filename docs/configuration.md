# Tournament YAML configuration

Tournament templates are versioned, validated YAML. The tournament name is entered separately and is intentionally not part of the reusable template. On tournament creation/import, configuration is copied into SQLite; the YAML file is not needed during play.

## Example

```yaml
version: 1
purchases:
  initial_buy_in:
    price: 2.00       # euros; exactly €2.00, stored as integer cents
    chips: 1000
  rebuy:
    price: 2.00
    chips: 1000
  add_on:
    price: 2.00
    chips: 5000
registration:
  rebuys_enabled: true
  registration_deadline:
    at: end_of_break
    target: 1
  add_ons_enabled: true
  add_on_break: 1
chip_values:
  - value: 5
    color: red
  - value: 25
    color: green
  - value: 100
    color: blue
  - value: 500
    color: white
  - value: 1000
    color: pink
schedule:
  - kind: level
    duration: 20m
    small_blind: 5
    big_blind: 10
    ante:
      kind: none
  - kind: break
    label: First break
    duration: 10m
    chipup: 5
  - kind: level
    duration: 20m
    small_blind: 10
    big_blind: 20
    ante:
      kind: per_player
      amount: 1
prize_tiers:
  - min_players: 2
    percentages: [60, 25, 15]
```

The example is intentionally small. A template must contain at least one level, and any configured deadline or add-on window must reference an existing schedule segment.

## Fields and validation

- `version`: currently `1`. Unknown fields and unsupported versions are rejected.
- `purchases`: fixed prices and chip counts for each purchase type. Prices are euro amounts parsed from the original decimal text, never through floating point. Prices must be positive multiples of €0.10; chip counts must be positive integers.
- `registration.rebuys_enabled`: controls rebuy availability. Initial buy-ins still use the registration deadline when rebuys are disabled.
- `registration.registration_deadline`: applies to initial buy-ins and, when enabled, rebuys. `at` is `start_of_level`, `start_of_break`, or `end_of_break`. `target` is the one-based ordinal of that kind of segment, not the absolute schedule row. An end-of-break deadline closes when that break ends even if its following level is blocked.
- `registration.add_ons_enabled` and `add_on_break`: independently enable add-ons and identify the one-based break ordinal. Each active player can buy at most one non-void add-on during that break.
- `chip_values` (optional): denominations and HTML/SVG color names or CSS hex colors displayed in the dashboard’s chip legend. Omit it to hide that legend. Values must be positive and unique.
- `schedule`: ordered `level` and `break` segments. Level durations and break durations use Go duration syntax (`20m`, `10m`, `90s`). A level requires positive small and big blinds, with big blind greater than small blind. A break has no blinds or ante. Breaks follow a level, cannot be consecutive, and cannot end the schedule. Optional break `reminder` text is shown while that break is active and when the transition begins; it does not change chip totals. `chipup` names the chip denomination removed at that break. Once its break starts, that denomination is grayed out and average stack rounds down to the smallest remaining configured denomination.
- `ante.kind`: `none`, `per_player`, or `big_blind`. Per-player ante requirement is multiplied by active players, including sit-outs. Big-blind ante is counted once for blind-level availability. Antes must be positive when enabled.
- `prize_tiers`: minimum entrant count and place percentages. The first threshold must be `2`; thresholds increase strictly. Percentages must be positive, non-increasing by place, and sum exactly to 100% at their configured decimal precision.

Percentages are not silently normalized in configuration. At payout time, unpaid places are omitted and available places are renormalized proportionally. Euro payouts are allocated in integer €0.10 units with deterministic largest-remainder distribution. Tied finishers split the combined prizes for their places; the operator assigns any indivisible €0.10 remainder when finalizing.

## Editing and export

Use **Settings → Import/edit YAML template** before play to validate and apply an edited file. Changes to the price or chip count for a purchase type are blocked until all non-void purchases of that type are voided with explanations. After play starts, configuration remains locked, including after pausing and reopening an ended tournament.

**Settings → Export reusable template** writes the current tournament structure as YAML, without the tournament name. Export refuses to overwrite an existing file.
