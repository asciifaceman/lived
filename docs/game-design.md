# Lived Game Design (v0)

## Pillars

1. Start from poverty and work upward through repeatable, time-based actions.
2. The world advances continuously while the server is running.
3. Behaviors are composable building blocks for both player and world simulation.
4. Ascension resets run-state but grants permanent meta progression.

## Core Loop

1. Queue a player behavior.
2. Behavior consumes time in the world loop and resolves with outputs.
3. Outputs feed inventory, unlocks, and higher-value behaviors.
4. Repeat to increase wealth and unlock progression branches.
5. Ascend to restart with permanent bonuses.

## Client Transport Model

- REST endpoints remain canonical for API-driven play and scripted clients.
- WebSocket stream endpoints provide UI-oriented live updates (tick/time-of-day/state snapshots).
- UI clients should use the stream for display freshness and REST for authoritative actions.
- UI layout should preserve always-visible operational context (queue, player state, world feed) while placing larger secondary panels behind tabs to maximize viewport usage.

## Behavior Model

- A behavior is a scheduled unit of work with:
  - `key`
  - `actorType` (`player` or `world`)
  - `durationMinutes`
  - `requirements` (unlocks/resources)
  - `costs`
  - `outputs`
  - optional `outputExpressions` for variance (for example `1-5`, `1+d6`)
  - optional per-output `chance` values (`0.0`-`1.0`)
  - optional market-open requirements for market-facing actions
  - optional repeat interval for world automation
- Lifecycle: `queued -> active -> completed|failed`
- Costs and requirements are evaluated at activation.
- Outputs resolve on completion.

## Inventory and Wealth

- Inventory is item-based and ledger-like.
- Wealth is represented by `coins`.
- Starter progression (abject poverty):
  - `player_scavenge_scrap` yields low-value resources and tiny coin gains.
  - `player_sell_scrap` converts gathered material into meaningful coins.

## Player Stats

- Player stats are persistent run-state progression values that can be improved through behaviors.
- Initial stat set:
  - `strength`: improves chopping speed and wood output.
  - `social`: improves sale outcomes (for example scrap sale price bonus).
  - `stamina`: current energy pool consumed by selected player behaviors.
  - `max_stamina`: stamina capacity ceiling.
  - `stamina_recovery_rate`: passive stamina recharge speed over time.
- Starter stat-improvement behaviors include:
  - `player_pushups` (minor strength growth)
  - `player_run_training` (max stamina growth)
  - `player_socialize_market` (social growth)
  - `player_weight_training` (moderate strength growth after equipment unlock)
- Recovery-rate progression is tied to sustained stamina use (not rapid idle toggling), so endurance improves through continued exertion.
- Stat references and tuning notes are tracked in `docs/game-data/player-stats.yaml`.
- UI may expose path influence telemetry derived from completed behaviors + stats so role identity is visible without a hard class lock-in.

## World Behaviors

- `world_market_ai_cycle` runs on a repeating schedule and applies AI-driven market decisions.
- `world_merchant_convoy` periodically applies explicit market demand pressure.
- These emit world events visible in status and prove time/world activity independent of player requests.
- Market behavior is session-aware: the market closes overnight and reopens based on in-game time.
- Market-open-required player behaviors should queue through overnight closures and activate/complete once market reopens, unless their configured timeout window is exceeded.

## Gating

- Resource gates: minimum inventory requirements.
- Unlock gates: specific unlock keys required by behavior definitions.
- Gates are evaluated at behavior activation time.
- Path/class identity should be emergent from repeated behavior choices (for example trading-focused vs strength-focused), not chosen via a dedicated class selection step.
- Behavior discovery/visibility may broaden based on unlock progression while still respecting resource/item/currency/special-item requirements for actual execution.
- Player behavior catalog should only expose player-accessible behavior definitions (world/AI behaviors stay internal).

## Ascension

- Ascension resets run-state (world tick, inventory, queued behaviors, world events, unlocks).
- Meta-state persists and grows:
  - ascension count
  - permanent wealth bonus (%)
- Wealth bonus applies to coin outputs from behaviors.
- Ascension availability is coin-gated and scales by ascension count.
- Each ascension grants starting coins for the next run to accelerate early progression.
- Tunable constants are tracked in `docs/game-data/ascension.yaml`.

## Design Follow-Ups (Reference)

Live execution state for these items belongs in [feature-tracker.md](feature-tracker.md).

1. Add behavior priority and cancellation semantics.
2. Move behavior definitions from static map to data-driven loading.
3. Expand market model with richer symbol set and liquidity constraints.
4. Add balancing pass for durations/yields and ascension pacing.
5. Add deterministic test harnesses for behavior progression and market session boundaries.
