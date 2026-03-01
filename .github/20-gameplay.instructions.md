# 20 Gameplay Instructions

## Core Concepts

- Behavior lifecycle: `queued -> active -> completed|failed`.
- Behavior actor types: `player`, `world`.
- Requirements are evaluated at activation time.
- Costs are applied at activation; outputs are resolved on completion.
- Outputs may include:
  - static output values,
  - optional output expressions (`1-5`, `1+d6`, `3`),
  - optional output chances (`0.0` to `1.0`).

## World Time

- Tick progression is delta-time based and persisted.
- Catch-up on startup is expected.
- Pending behavior summaries are persisted for observability/recovery.

## Market

- Market movement should be behavior-driven by world/AI behaviors.
- Avoid purely random price drift detached from world actions.
- Market has open/closed sessions; certain player behaviors require market open.
- Expose ticker-style market status/history through API.

## Progression Systems

- Inventory is run-state and transactional.
- Unlocks gate behavior accessibility.
- Resource, currency, and special-item requirements should continue to gate behavior readiness.
- Prefer emergent role identity from player actions (trading/social/strength investment) over explicit class selection UX.
- Ascension resets run-state and preserves meta progression.
- Wealth bonus is meta-state and should apply in transparent, explicit ways.
