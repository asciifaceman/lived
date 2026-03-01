# 50 MMO Refactor Instructions

## Scope and Intent

- This file defines long-horizon guidance for converting Lived from single-save mode to MMO-style multiplayer.
- Treat this as persistent implementation memory across context windows/agents.
- Keep changes incremental, migration-safe, and feature-flagged where practical.

## MMO Product Goals

- Shared economy market where many authenticated players act concurrently.
- Public world feed showing visible player actions (privacy-safe and moderation-aware).
- Player onboarding flow that creates account identity and initial playable character/profile.
- Support multiple player characters/profiles per account.
- Admin-authorized controls and operational views (not public player powers).
- MMO telemetry endpoints (active players, online counts, economy stats, throughput).

## Non-Goals (for initial MMO migration)

- Do not attempt full distributed microservices architecture in first pass.
- Do not design irreversible one-shot migrations without rollback paths.
- Do not keep player-side save import/export in competitive multiplayer mode.

## Hard Constraints

- Preserve deterministic world-tick behavior and transactional gameplay updates.
- API and DB changes must remain documented in Swagger and docs.
- Avoid introducing global mutable state outside DB/explicit synchronization.
- Backward compatibility should be explicit: support legacy single-player mode only behind clear flags if retained.

## Key Architectural Shifts Required

1. Identity and tenancy:
   - Add account identity (`account_id`) and authenticated player ownership boundaries.
   - Introduce explicit world/realm scope (`world_id` or `realm_id`) on shared entities.
2. Runtime ownership:
   - Replace single global world assumptions with realm-scoped processing.
   - Ensure world loop can process one or many realms deterministically.
3. Data model normalization:
   - Remove assumptions that “first player” is the active player.
   - Replace global hard resets/import behavior with admin-only world lifecycle ops.
4. Transport and visibility:
   - Authenticated REST + stream channels with per-user context.
   - Public action log and chat with moderation and rate limits.

## Observed Project Risks/Concerns (from current codebase)

- Handlers currently rely on `loadPrimaryPlayer` (first row semantics), which is incompatible with MMO identity.
- `replaceGameState` does global hard deletes/recreates; this is unsafe for multiplayer and must be removed/restricted.
- World state/runtime state use single global keys (e.g., `world`), implying one simulation context.
- Market prices are currently global by `item_key` with no realm partitioning.
- Player name uniqueness currently global and not account-scoped.
- Stream payload currently summarizes one primary player, not authenticated actor context.
- Auth is undecided and absent; all gameplay endpoints are effectively anonymous single-tenant.
- Export/import/new-game flows assume local single-player reset semantics and conflict with competitive MMO fairness.

## Mandatory MMO Implementation Order

1. **Auth Foundation**
   - JWT auth (short-lived access token + refresh token) and secure password hashing.
   - Account model and ownership checks on all player operations.
2. **Realm Partitioning**
   - Introduce realm/world identifiers and scope all relevant tables/queries.
   - Ensure market and world events are realm-scoped.
3. **Player Context APIs**
   - Replace primary-player loading with authenticated player lookup.
   - Add onboarding endpoints for first character/profile creation.
4. **Public Systems**
   - Public action feed and player chat (rate limits + moderation controls).
5. **Admin Plane**
   - Admin-authorized endpoints/UI for realm controls, player moderation, and diagnostics.
6. **Legacy Flow Decommissioning**
   - Disable player-facing import/export/new-game in MMO mode.
   - If needed, provide admin-only realm snapshot tooling.

## Endpoint Families to Add

- `/v1/auth/*`: register/login/refresh/logout/me
- `/v1/onboarding/*`: create profile/character, tutorial bootstrap
- `/v1/chat/*`: world chat channels, send/read with pagination
- `/v1/mmo/stats/*`: active players, concurrent online, events per minute, market health
- `/v1/admin/*`: realm management, moderation, economy controls, audits

## Data Migration Principles

- Add columns/tables with nullable/backfill first, then tighten constraints.
- Use dual-read/write transition windows where needed.
- Prefer idempotent migration scripts and explicit backfill jobs.
- No destructive data deletes in migration phase without backup + restore test.

## Security and Abuse Controls

- Password hashing with strong adaptive algorithm.
- JWT key rotation strategy and token revocation handling.
- Request rate limiting on auth/chat/start-behavior endpoints.
- Anti-abuse guardrails for chat spam and behavior queue flooding.
- Authorization checks in every handler (account/player/realm ownership).

## Authentication Baseline (Required)

- Access token: JWT, short-lived (15 minutes default).
- Refresh token: opaque random token, stored hashed server-side, rotating on refresh.
- Refresh lifetime: 30 days default; revoked on logout/password change/admin lock.
- Password hashing: Argon2id or bcrypt with current-recommendation cost params.
- Email dependency is intentionally excluded for now (no email verification/reset requirement in initial MMO rollout).
- Account recovery without email should be treated as an explicit future system (for example recovery codes or admin-assisted recovery policy).
- JWT signing keys must support active+next key rotation (`kid` support).

## Authorization Matrix (Minimum)

- `player` role:
   - can access own player/profile/inventory/behaviors,
   - can post chat in allowed channels,
   - cannot access admin endpoints.
- `moderator` role:
   - includes player permissions,
   - can hide/delete chat messages and feed events,
   - can mute/suspend chat accounts.
- `admin` role:
   - full control over realms/economy toggles/moderation/operational actions.
- All handlers must enforce: authenticated account, role checks, ownership checks, realm checks.

## WebSocket/Session Rules

- Stream connections must require auth (Bearer token or signed session token on connect).
- Presence should be tracked by realm and account/session id with heartbeat timeout.
- Reconnect semantics:
   - client resumes from last seen event id or receives fresh snapshot + deltas.
   - server enforces max concurrent connections per account/session.
- Fanout constraints:
   - bounded buffered channels,
   - drop/backpressure strategy,
   - clear disconnect behavior on slow consumers.

## Competitive Integrity and Anti-Abuse

- Idempotency keys for write-heavy actions (queue behavior, chat post, purchases/sales).
- Per-route and per-account rate limits with burst + sustained windows.
- Suspicious activity scoring for:
   - behavior queue spam,
   - anomalous economy gains,
   - chat spam/repetition.
- Admin tooling must include temporary lock, mute, and audit trace.

## SRE and Operational Requirements

- Define SLOs before launch:
   - API availability,
   - tick lag/processing latency,
   - stream delivery latency.
- Required observability:
   - structured logs with request/account/realm correlation ids,
   - metrics (auth success/failure, online users, queue depth, chat throughput),
   - distributed tracing for critical flows where feasible.
- Backup/recovery:
   - daily backups + tested restore runbook,
   - migration rollback plans for every destructive schema change.

## OpenTelemetry Baseline (Settled)

- Backend target (initial): OTel Collector + Jaeger + Prometheus/Grafana.
- Required signals in Phase 1 MMO rollout:
   - traces,
   - metrics,
   - logs correlation (trace_id/span_id in structured logs),
   - profiles.
- Sampling policy:
   - development: always-on tracing,
   - production: parent-based with 10% root sampling.
- Privacy policy:
   - moderate redaction by default,
   - never emit credentials/tokens/secrets,
   - chat content is excluded from telemetry payload attributes by default,
   - usernames may be emitted when needed for operations.

## OTel Instrumentation Minimum Coverage

- HTTP server middleware (Echo) with route-level spans.
- GORM/Postgres query spans and latency/error metrics.
- World tick pipeline spans (tick start/end, activation/completion phases, queue depth gauge).
- Stream/chat publish path spans and throughput/error metrics.
- Auth pipeline spans (login/refresh/logout) with redacted attributes only.

## Migration Runbook Discipline

- Every schema migration must include:
   - forward SQL/GORM migration,
   - backfill step,
   - validation query set,
   - rollback strategy.
- Use expand-migrate-contract strategy:
   - expand schema,
   - dual-write/read,
   - verify parity,
   - contract old fields/paths.
- Never flip all endpoints at once when ownership/realm semantics are changing.

## Privacy, Retention, and Compliance Baseline

- Public feed must avoid exposing private inventory/account-sensitive details.
- Retention policies must be explicit for:
   - chat messages,
   - moderation actions,
   - audit events,
   - telemetry aggregates.
- Support account lifecycle controls:
   - account deactivation,
   - session revocation,
   - personal data export/delete workflows as product policy requires.

## Chat and Public Feed Rules

- Public feed should include only explicitly visible actions.
- Keep private/inventory-sensitive actions excluded or summarized.
- Add retention policy and pagination to avoid unbounded payload growth.
- Include moderation states for chat (`active`, `hidden`, `deleted`, `muted-source`).

## Admin and Operations Requirements

- Admin role model (`account_roles`) and secure checks.
- Realm health/status endpoints (tick lag, queue depth, errors).
- Audit logging for admin actions.
- Feature flags to gate MMO rollout phases.
- Admin tooling must support controlled market intervention for breakage recovery:
   - targeted symbol price correction/reset,
   - bounded market deltas,
   - optional realm-wide market normalization action.
- Every market intervention must require reason code + operator note and emit immutable audit events.

## Testing Gate for Each MMO Phase

- Unit tests for auth claims, permission checks, and gameplay ownership rules.
- Integration tests for realm-scoped market behavior.
- Regression tests for queue activation/completion under concurrent players.
- Load tests for chat/feed endpoints and stream fan-out behavior.

## Documentation Requirements

- Update Swagger for every new endpoint family and auth requirement.
- Keep `docs/game-design.md` aligned with multiplayer rules.
- Keep MMO migration plan doc current with completed phases and decisions.

## Decision Log Discipline

- Every architecture-impacting MMO decision must be recorded in the migration plan:
  - decision,
  - alternatives considered,
  - rationale,
  - rollback/mitigation plan.

## Settled Product Decisions (Current)

- No email dependency in initial MMO auth stack.
- Multiple characters per account are supported.
