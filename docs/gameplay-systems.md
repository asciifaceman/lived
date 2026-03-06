# Gameplay Systems

## World simulation

- Server owns time and simulation progression.
- World loop processes queued/active/completed/failed behaviors.
- Runtime state is persisted for restart continuity.

## Behavior model

Behavior lifecycle:

- `queued`
- `active`
- `completed`
- `failed`

Behavior catalog rows can include:

- display metadata (`name`, `label`, `summary`)
- requirements/costs/unlocks
- queue visibility and availability hints

Rest behavior:

- `player_rest` is a first-class player behavior for stamina recovery
- Passive tick recovery always applies; rest adds an accelerated completion-time recovery burst
- Rest recovery is deterministic and capped at max stamina

## Player state model

Player snapshots expose:

- inventory/currency
- `coreStats` (trainable attributes)
- `derivedStats` (computed/runtime values)
- compatibility `stats` map

## Market system

- Realm-scoped ticker and history
- Session open/close windows
- Market-dependent behaviors can wait for open session via `marketWait`

## Ascension

- Run-state reset with persistent meta progression effects
- MMO mode supports character/realm scoped ascension flows

## Realm model

- Realm IDs partition runtime and gameplay state
- API handlers resolve realm context from authenticated character in MMO mode
