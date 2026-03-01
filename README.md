# Lived

Lived is a server-authoritative idle/incremental game backend inspired by *Lives Lived*.

This project is entirely vibe coded for fun so it may be absolutely ridiculous. It is not intended to be super serious so give me a break.

## Stack

- Go 1.26
- Echo HTTP API
- GORM + PostgreSQL

## Local Development

## Docker (Player Build)

Build a production image with embedded frontend (single container):

```bash
docker build -t lived:player .
```

Run the container:

```bash
docker run --rm -p 8080:8080 \
	-e LIVED_DATABASE_URL="postgres://postgres:postgres@host.docker.internal:5432/lived?sslmode=disable" \
	lived:player
```

The image serves both API and frontend from the same binary using `embed_frontend`.

Use Docker Compose for a full local player stack (app + Postgres on lived network):

```bash
docker compose up --build
```

Compose details:

- app service: `lived-app`
- database service: `lived-db`
- network: `lived-network`
- postgres volume: `lived-postgres-data`

Stop stack:

```bash
docker compose down
```

Stop and remove DB volume:

```bash
docker compose down -v
```

### 1) Configure environment

Create a local `.env` file from `.env.example` and fill in values for your Postgres server.

Environment variables used by the app:

- `LIVED_HTTP_ADDR` (default `:8080`)
- `LIVED_AUTO_MIGRATE` (default `true`; use `false` or `0` to disable)
- `LIVED_GAME_TICK_INTERVAL` (default `1s`; Go duration, e.g. `500ms`, `2s`)
- `LIVED_GAME_MINUTES_PER_REAL_MINUTE` (default `60`; game-time acceleration)
- `LIVED_POSTGRES_HOST` (default `localhost`)
- `LIVED_POSTGRES_PORT` (default `5432`)
- `LIVED_POSTGRES_USER` (default `postgres`)
- `LIVED_POSTGRES_PASSWORD` (default `postgres`)
- `LIVED_POSTGRES_DBNAME` (default `lived`)
- `LIVED_POSTGRES_ADMIN_DB` (default `postgres`; used by DB setup command)
- `LIVED_POSTGRES_SSLMODE` (default `disable`)
- `LIVED_POSTGRES_TIMEZONE` (default `UTC`)
- `LIVED_DATABASE_URL` (optional DSN override; bypasses split Postgres fields)
- `LIVED_MMO_AUTH_ENABLED` (default `false`; enables `/v1/auth/*` routes)
- `LIVED_MMO_REALM_SCOPING_ENABLED` (default `true`; gates `/v1/mmo/*` stats endpoints)
- `LIVED_MMO_CHAT_ENABLED` (default `true`; gates `/v1/chat/*` and `/v1/feed/*` routes)
- `LIVED_MMO_ADMIN_ENABLED` (default `true`; gates `/v1/admin/*` routes)
- `LIVED_MMO_OTEL_ENABLED` (default `false`; reserved flag for OTel rollout wiring)
- `LIVED_MMO_JWT_ISSUER` (default `lived`)
- `LIVED_MMO_JWT_SECRET` (required in real deployments when MMO auth is enabled)
- `LIVED_MMO_ACCESS_TOKEN_TTL` (default `15m`)
- `LIVED_MMO_REFRESH_TOKEN_TTL` (default `720h`)
- `LIVED_RATE_LIMIT_ENABLED` (default `false`; enables server-side write route throttling)
- `LIVED_RATE_LIMIT_WINDOW` (default `1m`; fixed window duration)
- `LIVED_RATE_LIMIT_AUTH_MAX` (default `20`; max requests per window for `/v1/auth/register|login|refresh`)
- `LIVED_RATE_LIMIT_CHAT_MAX` (default `30`; max requests per window for `POST /v1/chat/messages`)
- `LIVED_RATE_LIMIT_BEHAVIOR_MAX` (default `30`; max requests per window for `POST /v1/system/behaviors/start`)
- `LIVED_RATE_LIMIT_ONBOARD_MAX` (default `10`; max requests per window for `POST /v1/onboarding/start`)
- `LIVED_RATE_LIMIT_IDENTITY` (default `ip`; `ip` uses client IP only, `account_or_ip` uses authenticated account ID with IP fallback)
- `LIVED_IDEMPOTENCY_ENABLED` (default `false`; enables idempotency replay for selected write endpoints)
- `LIVED_IDEMPOTENCY_TTL` (default `10m`; retention window for idempotency records)
- `LIVED_STREAM_MAX_CONNS_PER_ACCOUNT` (default `5`; max concurrent MMO stream sockets per account)
- `LIVED_STREAM_MAX_CONNS_PER_SESSION` (default `2`; max concurrent MMO stream sockets per auth session)

### 2) Create or recreate the development database

Create the configured DB if it does not exist:

```bash
go run . db setup
```

Drop and recreate the configured DB:

```bash
go run . db setup --recreate
```

Run database migrations explicitly:

```bash
go run . db migrate
```

Verify realm-scoped migration/index health (recommended after schema updates on existing databases):

```bash
go run . db verify
```

The command connects to `LIVED_POSTGRES_ADMIN_DB` and creates `LIVED_POSTGRES_DBNAME` with owner `LIVED_POSTGRES_USER`.

### 3) Run the server

```bash
go run . run
```

The root route serves the frontend SPA when `web/dist` exists.
If the frontend is not built yet, `/` returns a guidance response.

### 4) Frontend (React SPA)

Frontend source lives in `web/` (React + TypeScript + Vite + TanStack Query).

UI stream and refresh tuning constants (reconnect/debounce cadence) live in `web/src/uiConfig.ts`.

Current dashboard UX emphasizes viewport efficiency on common desktop resolutions:

- queue/history, inventory+stats, and world feed remain continuously visible;
- secondary views are tabbed (progression tree vs market ticker) to conserve screen space;
- run actions are centralized in a top-right `Actions` dropdown (for example `Start New Game`, `Ascend`) with modal name prompts.

Install dependencies:

```bash
cd web
npm install
```

Run frontend dev server (optional while iterating on UI):

```bash
npm run dev
```

Use Go + Vite HMR on the same origin (`http://localhost:8080`) by setting:

```bash
LIVED_WEB_DEV_PROXY_URL=http://localhost:5173
```

Then run both processes:

```bash
# terminal 1
go run . run

# terminal 2
cd web
npm run dev
```

In this mode, Go continues to serve `/v1`, `/swagger`, and `/health`, while frontend routes are proxied to Vite.

Build frontend for Go-hosted serving at `/`:

```bash
npm run build
```

Production embedded frontend build (bundle SPA into Go binary memory):

```bash
cd web && npm run build:embed
cd ..
go build -tags embed_frontend ./...
```

Notes:

- `build:embed` writes built assets to `src/server/webdist`.
- `-tags embed_frontend` enables embedded in-memory frontend serving.
- If `LIVED_WEB_DEV_PROXY_URL` is set, dev proxy mode still takes precedence.

Then run the Go server from repo root (`go run . run`) and open `http://localhost:8080/`.

Swagger UI is always available while the server is running:

- `http://localhost:8080/swagger/`
- OpenAPI JSON: `http://localhost:8080/swagger/openapi.json`

Server middleware defaults include:

- `RequestID` (request IDs available via `X-Request-ID`)
- panic recovery
- structured request/response logging (method, URI, status, latency, bytes in/out, request ID)

Custom middleware can be injected during app bootstrap via `server.WithMiddleware(...)`.

World time advances continuously in the background while the server process is running.
The loop is delta-time based (elapsed real time per tick), supports restart catch-up, and persists runtime metadata in the `world_runtime_states` table.

## API (v1)

All API endpoints return a standard envelope:

```json
{
  "status": "success|error",
  "message": "narrative or error message",
  "requestId": "optional-request-id",
  "data": {}
}
```

### System

Base path: `/v1/system`

- `GET /export`
	- Exports current save data as a minified base64url payload.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): disabled.
	- Response data: `{ "save": "<base64url>" }`
- `POST /import`
	- Replaces all stored data from an exported payload.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): disabled.
	- Request: `{ "save": "<base64url>" }`
	- Response: standard envelope with narrative success message
- `POST /new`
	- Starts a new game with an initial player name and reset tick.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): disabled; use onboarding endpoints.
	- Request: `{ "name": "PlayerName" }`
	- Response: standard envelope with narrative success message
- `GET /status`
	- Returns world/runtime-oriented status and save payload.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token, resolves account character context (optional `?characterId=<id>` selector), and omits legacy global save export payload.
	- Response data includes version metadata, players, inventory, stats (including stamina-related values), simulation tick, world age (minutes/hours/days), timing config, and persisted pending-behavior metadata.
- `GET /version`
	- Returns version metadata for API/backend/frontend builds.
- `POST /behaviors/start`
	- Queues a player behavior by key.
	- Request: `{ "behaviorKey": "player_scavenge_scrap" }`
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and queues for authenticated account character (optional `?characterId=<id>` selector).
	- When idempotency is enabled, optional `Idempotency-Key` request header prevents duplicate queueing on retries.
	- Idempotent responses include `Idempotency-Status: stored|replayed`.
	- For market-open-required behaviors, optional `marketWait` controls timeout before giving up: `{ "behaviorKey": "player_sell_scrap", "marketWait": "12h" }`
	- Market-open-required behaviors queued overnight now wait for market open instead of failing immediately.
	- Intended starter progression from poverty: `player_scavenge_scrap` then `player_sell_scrap`.
- `GET /behaviors/catalog`
	- Lists player-accessible behavior definitions only (world/AI behaviors are excluded).
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and evaluates availability for the authenticated account character (optional `?characterId=<id>` selector).
	- Includes duration, stamina cost, requirements, costs, outputs, output expressions, output chances, unlock grants, market-open requirements, and availability hints.
	- `queueVisible` indicates the behavior should appear in queue selection (for example path-discovered), while `available` indicates all current requirements are met.
	- This supports natural progression: players can see/discover path options while still being gated by resources/items/currency/special unlocks.
	- Progression identity is emergent from play patterns (behavior choices/stat investment), not an explicit class picker.
- `GET /market/status`
	- Returns ticker-style market snapshot (symbols, prices, deltas, session status, and time to open/close).
	- Optional `?realmId=<id>` selects a specific realm ticker (defaults to `1`).
	- Market closes overnight and reopens based on in-game clock.
	- Endpoint is intentionally public for market-monitor tooling.
- `GET /market/history?symbol=scrap&limit=100&realmId=1`
	- Returns market history entries (tick, price, delta, source, session state) like a stock-market API feed.
	- Optional `realmId` selects a specific realm history stream (defaults to `1`).
	- Endpoint is intentionally public for market-monitor tooling.
- `POST /ascend`
	- Resets run-state and grants permanent meta bonuses.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and applies character-scoped ascension in the selected character realm (optional `?characterId=<id>` selector).
	- Optional request: `{ "name": "NextRunName" }`
	- Meta progression includes ascension count and wealth bonus percentage.
	- Ascension is gated by wealth progression with a scaling requirement per ascension (`250`, then increasing by factor).
	- Each ascension also grants a starting coin stipend on the next run, making early progression faster.

### Auth (MMO Foundation)

Base path: `/v1/auth` (available when `LIVED_MMO_AUTH_ENABLED=true`)

- `POST /register`
	- Creates an account with username/password and returns access+refresh tokens.
	- Request: `{ "username": "player1", "password": "strongpassword" }`
- `POST /login`
	- Authenticates an existing account and returns access+refresh tokens.
	- Request: `{ "username": "player1", "password": "strongpassword" }`
- `POST /refresh`
	- Rotates refresh session and returns a new access+refresh pair.
	- Request: `{ "refreshToken": "<token>" }`
- `POST /logout`
	- Revokes current authenticated session.
	- Requires bearer access token.
- `GET /me`
	- Returns authenticated account identity/roles plus linked characters.
	- Requires bearer access token.

### Onboarding (MMO Foundation)

Base path: `/v1/onboarding` (available when `LIVED_MMO_AUTH_ENABLED=true`)

- `POST /start`
	- Creates first character for the authenticated account in the selected realm.
	- Request: `{ "name": "Aeris", "realmId": 1 }` (`realmId` optional, defaults to `1`).
	- Idempotent per account+realm (returns existing realm character when already onboarded).
	- Requires bearer access token.
- `GET /status`
	- Returns onboarding status and all characters for the authenticated account.
	- Requires bearer access token.

### Player

Base path: `/v1/player`

- `GET /status`
	- Returns player/save-oriented status for client HUDs and save panels.
	- Response data includes version metadata, encoded save blob, primary player presence/name, inventory, stats, player behavior history/queue view, simulation tick/world age, ascension meta progression, and ascension eligibility state (`available`, `requirementCoins`, `currentCoins`, `reason`).
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and resolves status by authenticated account character (optional `?characterId=<id>` selector).
- `GET /inventory`
	- Returns inventory-only player status for lightweight HUD refreshes.
	- Response data includes primary player presence/name, simulation tick, and inventory map.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and resolves inventory by authenticated account character (optional `?characterId=<id>` selector).
- `GET /behaviors`
	- Returns behavior queue/history view for the primary player only.
	- Response data includes primary player presence/name, simulation tick, and filtered player behavior items.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and resolves behaviors by authenticated account character (optional `?characterId=<id>` selector).

### Feed

Base path: `/v1/feed`

- `GET /public?realmId=1&limit=50`
	- Returns realm-scoped public world activity entries (latest first).
	- Includes event tick/day/clock metadata for UI timeline rendering.
	- Intended to be public for spectator dashboards and tooling.

### Chat

Base path: `/v1/chat`

- `GET /channels`
	- Returns available chat channels.
	- Current baseline channel set includes `global`.
- `GET /messages?realmId=1&channel=global&limit=100`
	- Returns realm/channel chat messages with in-world tick/day/clock metadata.
	- Public read endpoint for feed/chat clients.
- `POST /messages`
	- Posts a public chat message (`message`, optional `channel`).
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and posts as the authenticated account character (optional `?characterId=<id>` selector).
	- When idempotency is enabled, optional `Idempotency-Key` request header prevents duplicate posts on retries.
	- Idempotent responses include `Idempotency-Status: stored|replayed`.
	- Message length is capped at 280 characters.

### Admin

Base path: `/v1/admin` (available when `LIVED_MMO_AUTH_ENABLED=true`)

- `GET /realms`
	- Returns discovered realm IDs and active-character counts.
	- Requires bearer access token with `admin` role.
- `GET /stats`
	- Returns operational counters (active accounts/characters/sessions, queued-or-active behaviors, public event count).
	- Includes admin audit aggregates (`total`, `windowTotal`, by-action, by-realm) over a configurable tick window.
	- Optional `windowTicks` query controls aggregate window size (default `1440`, max `43200`).
	- Requires bearer access token with `admin` role.

- `GET /audit?realmId=1&actorAccountId=12&actionKey=account_lock&beforeId=500&limit=100`
	- Returns immutable admin audit entries with optional filters.
	- Supports cursor paging via `beforeId` (fetch entries where `id < beforeId`).
	- Optional `includeRawJson=true` includes decoded `before`/`after` payload fields; default is compact metadata-only rows.
	- Requires bearer access token with `admin` role.
- `GET /audit/:id`
	- Returns a single immutable admin audit entry by id with decoded `before`/`after` payloads.
	- Optional `includeRawJson=true` includes decoded `before`/`after`; default is compact metadata-only response.
	- Requires bearer access token with `admin` role.
- `GET /audit/export?realmId=1&actionKey=account_lock&beforeId=500&limit=100`
	- Exports immutable admin audit entries as CSV.
	- Supports the same optional filters/cursor as `GET /audit`.
	- Requires bearer access token with `admin` role.
- `POST /realms/:id/actions`
	- Applies a realm admin action and records an immutable audit event.
	- Requires bearer access token with `admin` role.
	- Current actions:
		- `market_reset_defaults` (reset key market symbols to default baseline values)
		- `market_set_price` (set a specific symbol price using `itemKey` + positive `price`)
	- Request requires `reasonCode` (optional `note`) for auditability.
- `POST /moderation/accounts/:id/lock`
	- Locks account `:id` and revokes currently active sessions.
	- Requires `reasonCode` (optional `note`) and `admin` role.
- `POST /moderation/accounts/:id/unlock`
	- Unlocks account `:id`.
	- Requires `reasonCode` (optional `note`) and `admin` role.
- `POST /moderation/accounts/:id/roles`
	- Grants/revokes account roles.
	- Request: `{ "roleKey": "moderator", "action": "grant|revoke", "reasonCode": "...", "note": "optional" }`.
	- Requires `admin` role.

### Stream

Base path: `/v1/stream`

- `GET /world` (WebSocket)
	- UI-focused continuous stream for world/player runtime updates.
	- Emits snapshots including tick, day, minuteOfDay, clock, dayPart, market open/closed state, and player summary.
	- MMO mode (`LIVED_MMO_AUTH_ENABLED=true`): requires bearer access token and resolves stream player/realm by authenticated account character (optional `?characterId=<id>` selector).
	- MMO mode enforces configurable concurrent-connection caps per account/session.
	- Intended for responsive UI rendering (no polling delay).
	- Frontend uses this stream when available and falls back to REST polling if disconnected.
	- UI world feed entries include in-world timestamps and can be opened for additional event meaning/context.

API-based play should continue using REST endpoints (`/v1/system/*`, `/v1/player/*`) for deterministic request/response workflows.

Behavior outputs support optional variance expressions:

- static range: `1-5`
- dice range: `1+d6`
- static value: `3`

These expressions are optional and evaluated when a behavior completes.

Behavior outputs also support optional per-item chances (0.0-1.0), so outcomes like coin finds can be probabilistic.

Market movement is AI-driven through world behaviors (`world_market_ai_cycle`, `world_merchant_convoy`) rather than random ticks.

## Design References

- High-level design notes: `docs/game-design.md`
- MMO migration plan: `docs/mmo-migration-plan.md`
- Behavior data index: `docs/game-data/behaviors.yaml`
- Player behavior definitions: `docs/game-data/player-behaviors.yaml`
- World behavior definitions: `docs/game-data/world-behaviors.yaml`
- Item data reference: `docs/game-data/items.yaml`
- Player stats reference: `docs/game-data/player-stats.yaml`
- Ascension reference: `docs/game-data/ascension.yaml`
	- Includes ascension tuning constants (requirement base/growth, wealth bonus, starting coins)

## Copilot Project Memory

- Root guidance: `.github/copilot-instructions.md`
- Domain guidance files (loaded by naming order):
	- `.github/00-codequality.instructions.md`
	- `.github/10-architecture.instructions.md`
	- `.github/20-gameplay.instructions.md`
	- `.github/30-api.instructions.md`
	- `.github/40-persistence.instructions.md`
	- `.github/50-mmo-refactor.instructions.md`

## Docs Freshness Checklist

When changing game features or backend behavior, update documentation in the same PR:

- API routes or payloads changed:
	- `README.md` (API section)
	- OpenAPI/Swagger output served by the app
	- `.github/30-api.instructions.md`
- Gameplay rules, behaviors, unlocks, ascension, or market logic changed:
	- `docs/game-design.md`
	- `docs/game-data/*.yaml` references as needed
	- `.github/20-gameplay.instructions.md`
- Runtime orchestration or package boundaries changed:
	- `README.md` (server/runtime notes)
	- `.github/10-architecture.instructions.md`
- Persistence models, transactions, migrations, or reset semantics changed:
	- `README.md` (DAL/persistence notes)
	- `.github/40-persistence.instructions.md`
- Quality/process expectations changed:
	- `.github/00-codequality.instructions.md`
- Any cross-domain or governance changes:
	- `.github/copilot-instructions.md`

## Server Organization

- Route groups should live in focused server subpackages under `src/server/<group>`.
- Current example: system endpoints are implemented in `src/server/system` and registered from the top-level router.
- Keep root `src/server` package focused on bootstrap concerns (Echo setup, middleware, top-level route wiring, error handling).

## DAL Conventions

- Use `dal.BaseModel` for persistence models (`ID`, `CreatedAt`, `UpdatedAt`).
- Do not embed `gorm.Model` by default.
- Reason: `gorm.Model` adds `DeletedAt` soft-delete behavior, but current save workflows (`/v1/system/import` and `/v1/system/new`) rely on hard delete + recreate semantics.
- Introduce soft delete only when a specific gameplay or audit requirement needs recoverable records, and update handlers/queries explicitly.

## Build

```bash
go build ./...
```

Frontend build:

```bash
cd web && npm run build
```

## Task Runner (Mage + Makefile)

This repo now includes:

- `Magefile.go` as the primary cross-platform task runner (recommended on Windows).
- `Makefile` as a convenience wrapper that forwards to `mage` (convenient on macOS/Linux).

Why both:

- `make` is not built into Windows by default.
- `mage` works consistently on Windows/macOS/Linux and is written in Go.

Install Mage:

```bash
go install github.com/magefile/mage@latest
```

CLI dependency (required for task commands):

```bash
go install github.com/magefile/mage@latest
```

Common tasks:

```bash
mage help
mage build
mage run
mage dev
mage dbSetup
mage dbRecreate
mage dbMigrate
mage dbVerify
mage frontendInstall
mage frontendDev
mage frontendBuild
mage buildEmbed
```

If you have `make` available, these are mirrored as:

```bash
make build
make run
make dev
make db-setup
make db-migrate
make db-verify
make frontend-dev
make build-embed
```

## Command Summary

- `go run . run` — start API server
- `go run . db setup` — create dev database if missing
- `go run . db setup --recreate` — recreate dev database
- `go run . db migrate` — run database migrations
- `go run . db verify` — run realm-scoping migration health checks and print a verification report

## Notes

- `.env` is git-ignored.
- `.env.example` is committed for onboarding.
- Auto-migrations run on startup when `LIVED_AUTO_MIGRATE=true`.
