# Lived

Lived is a server-authoritative idle/incremental game backend inspired by *Lives Lived*.

This project is entirely vibe coded for fun so it may be absolutely ridiculous. It is not intended to be super serious so give me a break.

## Stack

- Go 1.26
- Echo HTTP API
- GORM + PostgreSQL

## Local Development

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

### 2) Create or recreate the development database

Create the configured DB if it does not exist:

```bash
go run . db setup
```

Drop and recreate the configured DB:

```bash
go run . db setup --recreate
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
	- Response data: `{ "save": "<base64url>" }`
- `POST /import`
	- Replaces all stored data from an exported payload.
	- Request: `{ "save": "<base64url>" }`
	- Response: standard envelope with narrative success message
- `POST /new`
	- Starts a new game with an initial player name and reset tick.
	- Request: `{ "name": "PlayerName" }`
	- Response: standard envelope with narrative success message
- `GET /status`
	- Returns world/runtime-oriented status and save payload.
	- Response data includes version metadata, players, inventory, stats (including stamina-related values), simulation tick, world age (minutes/hours/days), timing config, and persisted pending-behavior metadata.
- `GET /version`
	- Returns version metadata for API/backend/frontend builds.
- `POST /behaviors/start`
	- Queues a player behavior by key.
	- Request: `{ "behaviorKey": "player_scavenge_scrap" }`
	- For market-open-required behaviors, optional `marketWait` controls timeout before giving up: `{ "behaviorKey": "player_sell_scrap", "marketWait": "12h" }`
	- Market-open-required behaviors queued overnight now wait for market open instead of failing immediately.
	- Intended starter progression from poverty: `player_scavenge_scrap` then `player_sell_scrap`.
- `GET /behaviors/catalog`
	- Lists player-accessible behavior definitions only (world/AI behaviors are excluded).
	- Includes duration, stamina cost, requirements, costs, outputs, output expressions, output chances, unlock grants, market-open requirements, and availability hints.
	- `queueVisible` indicates the behavior should appear in queue selection (for example path-discovered), while `available` indicates all current requirements are met.
	- This supports natural progression: players can see/discover path options while still being gated by resources/items/currency/special unlocks.
	- Progression identity is emergent from play patterns (behavior choices/stat investment), not an explicit class picker.
- `GET /market/status`
	- Returns ticker-style market snapshot (symbols, prices, deltas, session status, and time to open/close).
	- Market closes overnight and reopens based on in-game clock.
- `GET /market/history?symbol=scrap&limit=100`
	- Returns market history entries (tick, price, delta, source, session state) like a stock-market API feed.
- `POST /ascend`
	- Resets run-state and grants permanent meta bonuses.
	- Optional request: `{ "name": "NextRunName" }`
	- Meta progression includes ascension count and wealth bonus percentage.
	- Ascension is gated by wealth progression with a scaling requirement per ascension (`250`, then increasing by factor).
	- Each ascension also grants a starting coin stipend on the next run, making early progression faster.

### Player

Base path: `/v1/player`

- `GET /status`
	- Returns player/save-oriented status for client HUDs and save panels.
	- Response data includes version metadata, encoded save blob, primary player presence/name, inventory, stats, player behavior history/queue view, simulation tick/world age, ascension meta progression, and ascension eligibility state (`available`, `requirementCoins`, `currentCoins`, `reason`).
- `GET /inventory`
	- Returns inventory-only player status for lightweight HUD refreshes.
	- Response data includes primary player presence/name, simulation tick, and inventory map.
- `GET /behaviors`
	- Returns behavior queue/history view for the primary player only.
	- Response data includes primary player presence/name, simulation tick, and filtered player behavior items.

### Stream

Base path: `/v1/stream`

- `GET /world` (WebSocket)
	- UI-focused continuous stream for world/player runtime updates.
	- Emits snapshots including tick, day, minuteOfDay, clock, dayPart, market open/closed state, and primary-player summary.
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
make frontend-dev
make build-embed
```

## Command Summary

- `go run . run` — start API server
- `go run . db setup` — create dev database if missing
- `go run . db setup --recreate` — recreate dev database

## Notes

- `.env` is git-ignored.
- `.env.example` is committed for onboarding.
- Auto-migrations run on startup when `LIVED_AUTO_MIGRATE=true`.
