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
- Associate player entity with `account_id` (one-to-many to support multiple characters per account).

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
