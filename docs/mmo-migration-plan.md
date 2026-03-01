# MMO Migration Reference

This document is now a stable architecture/decision reference.

## Source of Truth for Execution

- Active work tracking (bugs/features/priorities/action-state): `docs/feature-tracker.md`
- MMO implementation guidance for Copilot sessions:
  - `.github/50-mmo-refactor.instructions.md`
  - `.github/60-mmo-delivery.instructions.md`

## MMO Direction (Stable)

Lived is migrating toward an authenticated, realm-scoped multiplayer architecture with:

- account ownership boundaries,
- shared realm simulation/economy,
- public feed/chat with moderation,
- admin-authorized operations,
- observability-first runtime operations.

## Settled Constraints

- Keep world simulation deterministic and transactionally safe.
- Keep API/DB changes reflected in Swagger and docs.
- Keep MMO rollout gated and reversible where practical.
- Keep player-facing import/export/new-game disabled in MMO mode.

## Notes

- Historical phase/checklist details previously maintained in this file are now captured as actionable tracker items in `docs/feature-tracker.md`.
- Guidance text that should steer code generation lives in `.github` instruction files, not in tracker docs.
