# 60 MMO Delivery Instructions

## Purpose

- This file translates remaining MMO migration work into execution guidance for coding sessions.
- Do not use this file as a checklist tracker; tracker state belongs in `docs/feature-tracker.md`.

## Source of Priorities

- Use `docs/feature-tracker.md` as the canonical task state.
- Treat `P0` and `P1` MMO-tagged items as default next work unless user reprioritizes.
- Session workflow expectation:
  - mark selected item `in-progress` when implementation begins,
  - update `Next Action` if scope/approach changes,
  - mark item `done` (or `blocked` with reason) before ending the session.

## MMO Remaining Work Focus

1. Realm scoping completion
- Remove remaining unscoped query/read paths.
- Keep default realm behavior explicit and test-covered.
- Never introduce new global-singleton assumptions in MMO mode.

2. Chat/admin control plane completion
- Add/finish channel lifecycle controls and participant moderation controls.
- Keep reason-code + note + immutable audit trail on admin actions.
- Keep moderation endpoints bounded (limits/pagination/windowed actions).

3. Realm lifecycle operations
- Define and implement safe realm archive/delete semantics.
- Add maintenance broadcast/drain hooks with clear operator API contracts.

4. Observability completion
- Extend OTel beyond baseline traces/metrics to profile signal integration.
- Ensure non-HTTP runtime logs include trace correlation when spans are active.

5. Realtime resilience
- Add authenticated stream resume protocol using cursor or snapshot fallback.
- Enforce bounded buffering/backpressure policies consistently.

6. Security operations hardening
- Implement JWT signing key rotation with `kid` support.
- Keep token/session revocation semantics explicit and regression-tested.

## Implementation Rules

- Preserve deterministic tick semantics and transaction boundaries.
- Prefer incremental, reviewable slices over broad rewrites.
- Keep MMO mode behavior explicit in route gating and handler logic.
- For behavior-visible/API-visible MMO changes, update:
  - `src/server/swagger.go`
  - `README.md`
  - `docs/feature-tracker.md` action state

## Testing and Validation

- For each MMO change slice, run targeted tests first, then `go test ./...`.
- Run frontend build when API payloads/UI contracts change.
- Add regression tests for authorization, realm scoping, and moderation guardrails.

## Anti-Drift Guardrails

- Do not maintain duplicate task state in strategy docs.
- Keep strategy/reference docs descriptive, not status-driven.
- Keep status in one place: `docs/feature-tracker.md`.
