# MMO Migration Plan (Draft v0)

## Objective

Convert Lived from single-save/single-primary-player architecture into a multiplayer incremental game with:

- authenticated players,
- shared realm economy/market,
- public world activity log,
- chat,
- admin-authorized operations,
- MMO telemetry endpoints.

## Current-State Summary (Baseline)

- Single-player assumptions exist in API handlers and runtime helper flows.
- Lifecycle endpoints (`new/import/export`) currently reset global state.
- No account authentication or authorization boundary.
- Stream and status surfaces assume one primary player context.
- Market and world runtime are effectively global singleton state.

## Target-State Summary

- Account-authenticated, realm-scoped multiplayer architecture.
- Shared market per realm with concurrent player interactions.
- Public visible-action feed and world chat.
- Admin control plane for moderation and realm operations.
- Player-facing save import/export disabled in MMO mode.

## Delivery Mode

- Execute via feature flags with phased rollout:
	- `mmo_auth_enabled`
	- `mmo_realm_scoping_enabled`
	- `mmo_chat_enabled`
	- `mmo_admin_enabled`
	- `mmo_otel_enabled`
- Prefer dark-launch + internal accounts before public rollout.

## Current Progress Snapshot (2026-02-28)

### Completed (implemented)

- MMO auth foundation behind `LIVED_MMO_AUTH_ENABLED` with register/login/refresh/logout/me.
- Account/session/role models and dedicated `characters` ownership table are in place.
- Onboarding endpoints are implemented (`/v1/onboarding/start`, `/v1/onboarding/status`).
- Player endpoints are migrated to authenticated account-character resolution:
	- `/v1/player/status`,
	- `/v1/player/inventory`,
	- `/v1/player/behaviors`.
- System write/read surfaces migrated or gated for MMO safety:
	- `/v1/system/behaviors/start` migrated to authenticated character queueing,
	- `/v1/system/behaviors/catalog` migrated to authenticated character availability,
	- `/v1/system/status` migrated to authenticated character context (legacy save payload suppressed in MMO),
	- `/v1/system/export`, `/v1/system/import`, `/v1/system/new` disabled in MMO mode,
	- `/v1/system/ascend` supports authenticated character-scoped ascension in MMO mode (optional `characterId` selector).

### In Progress / Not Yet Implemented

- Realm/data partitioning is in progress: `realm_id` schema scaffolding has been added to core runtime/economy/player-state entities, with realm-aware write/read paths now implemented for behavior queueing, market/economy event pipelines, snapshot reads, and core player-state helpers (inventory/stats/unlocks) in tick processing. Remaining runtime and API queries still need full default realm scoping/dual-read-write completion.
- MMO stats endpoints are implemented (`/v1/mmo/stats/system`, `/v1/mmo/stats/players`, `/v1/mmo/stats/economy`) with realm-scoped aggregation.
- Feed/chat baseline endpoints are implemented (`/v1/feed/public`, `/v1/chat/channels`, `/v1/chat/messages`) with realm-scoped reads and MMO-authenticated posting.
- Granular MMO rollout flags are now wired in config/route gating (`LIVED_MMO_REALM_SCOPING_ENABLED`, `LIVED_MMO_CHAT_ENABLED`, `LIVED_MMO_ADMIN_ENABLED`, `LIVED_MMO_OTEL_ENABLED`) in addition to `LIVED_MMO_AUTH_ENABLED`.
- Stream world endpoint now requires bearer auth in MMO mode and resolves player/realm from authenticated account character context (optional `characterId` selector), replacing MMO-mode primary-player assumptions.
- Stream world endpoint now enforces configurable concurrent connection caps per account/session in MMO mode.
- Configurable fixed-window rate limiting is implemented for high-risk write endpoints (`/v1/auth/register|login|refresh`, `POST /v1/chat/messages`, `POST /v1/system/behaviors/start`, `POST /v1/onboarding/start`) via environment settings, including optional account-aware identity keys (`account_or_ip`) for authenticated routes.
- Configurable idempotency-key replay support is implemented for `POST /v1/chat/messages` and `POST /v1/system/behaviors/start` (optional `Idempotency-Key` header, payload mismatch conflict, TTL-bound record retention).
- Admin baseline endpoints are implemented (`/v1/admin/realms`, `/v1/admin/stats`, `POST /v1/admin/realms/:id/actions`) with MMO bearer auth + admin-role enforcement.
- Admin realm actions currently support market reset/price-set operations with required reason codes and immutable audit rows (`admin_audit_events`).
- Admin moderation endpoints are implemented for account lock/unlock and role grant/revoke (`POST /v1/admin/moderation/accounts/:id/lock`, `/unlock`, `/roles`) with required reason codes and audit logging.
- Admin audit read endpoint is implemented (`GET /v1/admin/audit`) with filtering by realm/account/action, bounded limits, and `beforeId` cursor paging.
- Admin audit detail endpoint is implemented (`GET /v1/admin/audit/:id`) for single-entry drill-down with decoded before/after payloads.
- Admin audit list/detail endpoints now support `includeRawJson` query control for compact responses by default and optional decoded payload inclusion.
- Admin stats endpoint now includes recent admin-audit aggregates by action and realm, with configurable `windowTicks` query window.
- Admin audit CSV export endpoint is implemented (`GET /v1/admin/audit/export`) for operator incident-review workflows.
- OTel runtime integration (collector export, trace/metric/log correlation wiring) is not implemented yet.

### Documentation Status

- `README.md` and this migration plan reflect current implemented MMO endpoint behavior.
- `src/server/swagger.go` has been synchronized with current MMO auth/onboarding and endpoint gating semantics.
- Keep OpenAPI synced as new realm-scoped/chat/admin/stats endpoints are added.

## OpenTelemetry Decisions (Settled)

- Backend: OTel Collector + Jaeger + Prometheus/Grafana.
- Signals required in MMO rollout:
	- traces,
	- metrics,
	- logs correlation,
	- profiles.
- Sampling:
	- dev: always-on,
	- prod: parent-based 10%.
- Privacy policy:
	- moderate redaction,
	- no secrets/tokens in telemetry,
	- chat message content excluded from telemetry attributes by default.

## Authentication and Session Specification (Baseline)

- Access JWT TTL: 15 minutes.
- Refresh token TTL: 30 days.
- Refresh rotation: required on every refresh.
- Refresh storage: server-side hashed tokens with device/session metadata.
- Revocation triggers:
	- logout,
	- password change,
	- admin account lock,
	- suspicious session risk rule.
- Email dependency is intentionally out of scope for initial MMO rollout.
- Password reset/recovery must use a non-email strategy (deferred design; recovery codes/admin recovery policy).

## Authorization Matrix (Initial)

| Capability | Player | Moderator | Admin |
|---|---:|---:|---:|
| Read/write own player state | ✅ | ✅ | ✅ |
| Post chat | ✅ | ✅ | ✅ |
| Moderate chat/feed | ❌ | ✅ | ✅ |
| Realm controls/economy admin | ❌ | ❌ | ✅ |
| Access admin telemetry endpoints | ❌ | ⚠️ (read-only optional) | ✅ |

Ownership and realm checks are mandatory on all non-public endpoints.

---

## Phase 0 — MMO Foundation Decisions (1-2 weeks)

### Deliverables

- Finalize tenancy model (single global realm vs multiple realms/shards).
- Finalize auth method (local credentials JWT now, external IdP optional later).
- Finalize visibility policy for public actions and private actions.
- Define moderation policy baseline for chat/feed.

### Acceptance Criteria

- Written decisions captured in this document with rationale and rollback notes.
- API naming conventions for MMO endpoint families approved.

---

## Phase 1 — Identity and Auth (2-4 weeks)

### Schema/Additions

- `accounts` (username, password hash, status).
- `account_sessions` or refresh token table.
- `account_roles` (admin/moderator/player).
- `characters` ownership table (`account_id`, `player_id`, `realm_id`, metadata) to support multiple characters per account without coupling identity to runtime player rows.

### API

- `POST /v1/auth/register`
- `POST /v1/auth/login`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`
- `GET /v1/auth/me`

### Runtime/API Refactor

- Add auth middleware and claim propagation.
- Replace `loadPrimaryPlayer` usage with authenticated player resolution.

### Acceptance Criteria

- All player endpoints require valid auth and ownership checks.
- Token refresh flow and logout invalidation covered by tests.
- Multi-character account ownership constraints are enforced in player endpoints.

### Implementation Checklist

- Add auth middleware and request context actor extraction.
- Add login throttling and account lock policy.
- Add auth audit events (`login_success`, `login_failure`, `token_refresh`, `logout`).
- Add Swagger security scheme docs for Bearer auth.

---

## Phase 2 — Realm Scoping and Data Partitioning (3-6 weeks)

### Schema Evolution

Add `realm_id` (or `world_id`) to realm-sensitive entities:

- world state/runtime state,
- market prices/history,
- behavior instances,
- world events,
- inventory entries,
- unlocks/stats,
- chat/public feed entries.

### Migration Strategy

- Introduce nullable columns and dual-write where needed.
- Backfill existing rows to default realm.
- Tighten constraints only after backfill validation.

### Acceptance Criteria

- Queries are realm-scoped by default.
- No cross-realm data leakage in API/stream responses.

### Data Migration Runbook (Per table group)

1. Add nullable `realm_id`.
2. Backfill to default realm.
3. Add dual-write path.
4. Validate parity + row counts.
5. Enforce `NOT NULL` + indexes/constraints.
6. Remove legacy unscoped query paths.

---

## Phase 3 — Onboarding and Player Lifecycle (2-3 weeks)

### API

- `POST /v1/onboarding/start` (create initial player profile/character)
- `GET /v1/onboarding/status`

### Product Changes

- Remove/disable player-facing `new/import/export` in MMO mode.
- Keep optional admin-only snapshot import/export for realm maintenance.

### Acceptance Criteria

- New account can onboard without manual DB intervention.
- No player endpoint depends on singleton save reset behavior.

### Additional Rules

- Onboarding must be idempotent per account+realm.
- Character/profile naming policy must be enforced consistently.
- Legacy new/import/export endpoints return clear MMO-mode rejection messages.

---

## Phase 4 — Shared Economy Hardening (2-5 weeks)

### Work

- Audit market mutations for high-concurrency correctness.
- Add DB constraints and transaction patterns for race-free updates.
- Add economy anomaly metrics/alerts.

### Acceptance Criteria

- Concurrent player actions do not corrupt prices/inventory.
- Economy behavior deterministic at tick boundaries.

### Additional Safeguards

- Idempotency keys required for write actions touching economy state.
- Detect and alert on economy anomalies (sudden inflation/deflation, outlier gains).

---

## Phase 5 — Public Feed and Chat (3-5 weeks)

### API

- `GET /v1/feed/public`
- `GET /v1/chat/channels`
- `GET /v1/chat/messages`
- `POST /v1/chat/messages`

### Rules

- Feed includes only visibility-approved actions.
- Chat rate limits + moderation states.
- Pagination/retention policy for both feed and chat.

### WebSocket/Presence Requirements

- Authenticated connect with token verification.
- Presence heartbeat and timeout-based disconnect cleanup.
- Resume model: last event id cursor or full snapshot fallback.
- Slow-consumer policy with bounded buffers and disconnect thresholds.

### Acceptance Criteria

- Public activity and chat are stable under load.
- Moderation controls can hide/remove abusive content.

---

## Phase 6 — Admin Plane (2-4 weeks)

### API

- `GET /v1/admin/realms`
- `POST /v1/admin/realms/:id/actions`
- `GET /v1/admin/stats`
- `POST /v1/admin/moderation/*`

### UI/Operational

- Admin-only panel for realm state, moderation, and system controls.
- Audit logging for admin actions.

### Acceptance Criteria

- Non-admin accounts cannot access admin capabilities.
- Admin actions are auditable and reversible where applicable.

### Minimum Admin Functions

- Account lock/unlock, mute/unmute, role assignment.
- Realm pause/resume and maintenance mode.
- Economy control toggles (safe-guarded with audit logs).
- Market intervention controls for breakage recovery:
	- symbol price correction/reset,
	- bounded manual delta application,
	- realm market normalization/reset workflow.

### Safeguards for Market Intervention

- Mandatory reason code and free-text operator note.
- Immutable audit event containing actor, realm, before/after value, and timestamp.
- Optional dual-control mode (second admin approval) for high-impact actions.

---

## Phase 7 — MMO Stats and Observability (1-3 weeks)

### API

- `GET /v1/mmo/stats/players`
- `GET /v1/mmo/stats/economy`
- `GET /v1/mmo/stats/system`

### Metrics

- Active player count, concurrent sessions.
- Event throughput, queue depth, tick lag.
- Chat throughput and moderation queue metrics.

### OTel Implementation Tasks

- Add OTel SDK/bootstrap package and environment-driven configuration.
- Wire Echo HTTP middleware for trace context propagation.
- Instrument DB calls and world tick pipeline spans.
- Emit MMO-specific metrics (tick lag, queue depth, online users, chat throughput, auth failures).
- Add log correlation fields (`trace_id`, `span_id`) to structured logs.
- Add profiling export path and sampling controls per environment.

### Acceptance Criteria

- MMO health visible without DB spelunking.
- Dashboards/alerts for key SLO regressions.
- Jaeger traces and Prometheus metrics are usable in local docker-based stack.
- Redaction policy is validated in tests/log inspection (no secrets/tokens leaked).

### Required Launch SLOs

- API availability target (define numeric objective).
- Tick processing lag budget.
- Stream event delivery latency budget.

---

## Phase 8 — Cutover and Legacy Cleanup (1-2 weeks)

### Work

- Remove dead single-save code paths.
- Finalize feature flags and defaults for MMO mode.
- Document operational runbooks.

### Acceptance Criteria

- End-to-end MMO flow works from register → onboarding → play → social → admin oversight.
- Legacy player-facing import/export/new game no longer available in MMO mode.

### Cutover Checklist

- Final data backfill validation complete.
- Security review complete (authz, rate limiting, token handling).
- Load test baseline passed for chat/feed/world updates.
- Rollback runbook tested in staging.

---

## Cross-Cutting Technical Concerns

1. **Security:** JWT expiry/rotation, refresh revocation, abuse throttling.
2. **Consistency:** transaction boundaries for shared economy actions.
3. **Performance:** chat/feed pagination and stream fanout strategy.
4. **Moderation:** chat/reporting tooling to avoid community toxicity.
5. **Data migration safety:** staged constraints and rollback plans.

## Test Debt Checklist (Deferred Until MMO Core Stabilizes)

- Integration: ascension isolation for two characters in the same realm (A ascends, B unchanged).
- Integration: ascension isolation across realms (realm 1 ascension does not mutate realm 2 state).
- Integration: market mutation isolation across realms during concurrent behavior completions.
- Integration: world-loop multi-realm tick progression parity under restart/catch-up.

### Unit Tests to Keep Now (No DB)

- Ascension requirement scaling and eligibility messaging/value boundaries.
- Duration parsing behavior for market wait input units (`m`, `h`, `d`) and invalid values.
- Realm helper normalization defaults (`realm_id` fallback behavior where applicable).

## Compliance and Retention Baseline

- Define retention windows for:
	- chat content,
	- public feed records,
	- moderation actions,
	- auth/session audit logs.
- Define account lifecycle handling:
	- deactivation,
	- deletion policies,
	- data export support per policy.

## Suggested First Sprint Backlog (Implementation Start)

1. Add `accounts` + password hash + JWT middleware skeleton.
2. Add authenticated `GET /v1/auth/me`.
3. Introduce request-scoped actor context in handlers.
4. Refactor one endpoint (`/v1/player/status`) to authenticated-player semantics.
5. Add test coverage for auth-required player status access.
6. Add initial `accounts`, `account_sessions`, and `account_roles` migrations.
7. Introduce realm id scaffold on world/market tables (nullable + default realm backfill).
8. Add OTel bootstrap + Echo middleware + baseline traces/metrics behind `mmo_otel_enabled`.

## Open Decisions (Need Product Call)

1. Single shared realm at launch, or multiple realms/shards from day one?
2. Username policy: unique globally vs unique per realm?
3. Chat scope: global realm chat only, or channels (trade/help/local) at launch?
4. Moderation staffing/automation level expected for launch?
5. Should admin snapshot import/export exist in launch build or post-launch tooling?
6. Access/refresh token TTL defaults and device/session policy?
7. Moderation model at launch: manual only vs auto-flag + manual review?
8. Presence scope: per-realm online only or global online counts as well?
9. Non-email account recovery policy: recovery codes vs admin-assisted flow?

## Decision Log

> Add entries here as decisions are made.

- 2026-02-28: Initial MMO auth excludes email dependency (no email verification/reset requirement at launch).
- 2026-02-28: Accounts may own multiple player characters.
- 2026-02-28: Character ownership uses a dedicated `characters` table linked to runtime `players` rows.
- 2026-02-28: Added authenticated `/v1/onboarding/start` and `/v1/onboarding/status` baseline endpoints with transactional character creation.
- 2026-02-28: Migrated `/v1/player/status` to authenticated account-character resolution in MMO mode (supports optional `characterId` selector).
- 2026-02-28: Migrated `/v1/player/inventory` and `/v1/player/behaviors` to authenticated account-character resolution in MMO mode (supports optional `characterId` selector).
- 2026-02-28: Migrated `POST /v1/system/behaviors/start` to authenticated account-character queueing in MMO mode (supports optional `characterId` selector).
- 2026-02-28: Migrated `POST /v1/system/ascend` routing to MMO auth-aware flow and temporarily disabled MMO execution to prevent global-reset corruption until realm-scoped ascension is implemented.
- 2026-02-28: Disabled legacy `/v1/system/export`, `/v1/system/import`, and `/v1/system/new` in MMO mode to prevent global save lifecycle operations.
- 2026-02-28: Migrated `/v1/system/behaviors/catalog` to authenticated account-character availability evaluation in MMO mode (supports optional `characterId` selector).
- 2026-02-28: Migrated `/v1/system/status` to authenticated account-character context in MMO mode (supports optional `characterId` selector) and removed MMO exposure of legacy global save payload.
- 2026-02-28: Product policy affirmed that market monitor endpoints (ticker/history) may remain public for third-party monitoring unless they begin exposing account-private state.
- 2026-02-28: Updated OpenAPI/Swagger spec to include MMO auth/onboarding endpoints and current auth/gating semantics, while keeping market ticker/history endpoints publicly accessible.
- 2026-02-28: Started Phase 2 schema scaffolding by adding defaulted `realm_id` columns to core realm-sensitive DAL entities; runtime queries are still in single-realm compatibility mode pending dual-read/write transition.
- 2026-02-28: Added first realm-aware dual-write hook by propagating selected character `realm_id` into queued player behavior writes.
- 2026-02-28: Added realm-aware market and world-event pipeline updates: market price/history reads+writes now scope by `realm_id`, AI market deltas use realm-scoped aggregates, and public market status/history endpoints support optional `realmId` query selection.
- 2026-02-28: Scoped MMO-resolved gameplay snapshot reads by `realm_id` for player/system responses (market tickers, behavior history, and world events) to prevent cross-realm leakage in authenticated character views.
- 2026-02-28: Updated `market_prices` model index strategy to realm+symbol uniqueness to support independent per-realm ticker rows without symbol collisions.
- 2026-02-28: Made world-loop persistence realm-aware for default realm operation (`world_states` and `world_runtime_states` updates/loads now scope by `realm_id`) and routed tick processing through realm-specific gameplay entrypoints.
- 2026-02-28: Scoped behavior tick helper operations by `realm_id` (requirements, inventory deltas, stat reads/writes, unlock grants, and stamina recovery), reducing cross-realm state interaction risk during runtime processing.
- 2026-02-28: Updated system status runtime metadata loading to resolve `world_runtime_states` by selected realm context in MMO character views, so pending behavior summaries align with realm-scoped tick processing.
- 2026-02-28: Hardened world state/runtime schema constraints for realm partitioning by enforcing one `world_states` row per realm and realm+key uniqueness for `world_runtime_states`.
- 2026-02-28: World loop now processes all known realms each tick (discovered from character/world/runtime tables) and runs realm-specific tick advancement/runtime persistence per realm.
- 2026-02-28: Scoped ascension persistence/eligibility to realm context (`ascension_states` now realm+key addressed), and moved realm tick bonus reads to realm-specific ascension state.
- 2026-02-28: Added migration hardening steps around AutoMigrate: realm backfill (`NULL/0 -> 1`), per-realm duplicate normalization for world/runtime/market/ascension tables, legacy single-column unique index discovery+drop, and explicit creation of required realm-scoped composite unique indexes.
- 2026-02-28: Added operator preflight command `go run . db verify` to emit realm-scoping migration health reports (missing indexes, duplicate-key groups, invalid realm ids) and fail non-healthy checks.
- 2026-02-28: Added explicit `go run . db migrate` command and CI workflow gate (`.github/workflows/db-verify.yml`) executing setup -> migrate -> verify on Postgres for pre-merge migration health validation.
- 2026-02-28: Enabled MMO ascension endpoint with authenticated character+realm scoping and player-local run-state reset semantics; removed temporary MMO ascend disablement.
