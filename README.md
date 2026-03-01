# Lived

<img src="docs/static/text_logo.png" alt="Lived" width="280" />

Lived is a server-authoritative idle/incremental game backend.
It is 100% unapologetically vibe coded for fun to explore the technology and make something ridiculous I'd never execute on otherwise.

Beware, skeletons lay beyond here. This repo is comprised of a series of mistakes, inconsistencies, and ignored festering problems. While I am almost obsessively in control of the direction, and steering, I ain't reviewing all of that.

This repo includes:

- Go API server and simulation runtime
- PostgreSQL persistence layer
- Web frontend (React + TypeScript)
- MMO-auth/chat/admin mode toggles

## Table of Contents

- [Who this is for](#who-this-is-for)
- [Start here](#start-here)
- [Project map](#project-map)
- [Quick commands](#quick-commands)
- [Documentation by domain](#documentation-by-domain)
- [API docs](#api-docs)
- [Development principles](#development-principles)

## Who this is for

- New contributors who want to run the game server quickly
- API users building their own clients
- Operators running MMO/auth/chat/admin configurations

## Start here

If you only read one page first, read:

- [docs/getting-started.md](docs/getting-started.md)

Then pick your path:

- Build or change code daily: [docs/development-workflow.md](docs/development-workflow.md)
- Understand routes/contracts: [docs/api-guide.md](docs/api-guide.md)
- Understand progression/runtime systems: [docs/gameplay-systems.md](docs/gameplay-systems.md)
- Work on admin or moderation flows: [docs/admin-and-operations.md](docs/admin-and-operations.md)
- Work on metrics/tracing/ops telemetry: [docs/observability-and-telemetry.md](docs/observability-and-telemetry.md)

## Project map

Top-level layout:

- `cmd/` CLI command wiring
- `pkg/` reusable infrastructure/data packages
- `src/` game domain, runtime, and server handlers
- `web/` frontend application
- `docs/` product and engineering documentation
- `devel/` docker + local stack assets

## Quick commands

```bash
# run server
go run . run

# create db
go run . db setup

# recreate db
go run . db setup --recreate

# run migrations
go run . db migrate

# run tests
go test ./...
```

Frontend:

```bash
cd web
npm install
npm run dev
```

Embedded frontend production build:

```bash
cd web
npm run build:embed
cd ..
go build -tags embed_frontend ./...
```

## Documentation by domain

Core docs:

- Getting started: [docs/getting-started.md](docs/getting-started.md)
- Development workflow: [docs/development-workflow.md](docs/development-workflow.md)
- API guide: [docs/api-guide.md](docs/api-guide.md)
- Gameplay systems: [docs/gameplay-systems.md](docs/gameplay-systems.md)
- Admin and operations: [docs/admin-and-operations.md](docs/admin-and-operations.md)
- Observability and telemetry: [docs/observability-and-telemetry.md](docs/observability-and-telemetry.md)

Planning/tracking docs:

- Canonical tracker: [docs/feature-tracker.md](docs/feature-tracker.md)
- MMO migration plan: [docs/mmo-migration-plan.md](docs/mmo-migration-plan.md)
- Game design notes: [docs/game-design.md](docs/game-design.md)

## API docs

When server is running:

- Swagger UI: `http://localhost:8080/swagger/`
- OpenAPI JSON: `http://localhost:8080/swagger/openapi.json`
- Health: `http://localhost:8080/health`
- Metrics: `http://localhost:8080/metrics`

Current notable contracts:

- Onboarding status returns realm metadata (`name`, `whitelistOnly`, `canCreateCharacter`, `decommissioned`) for realm selector UX.
- Onboarding start enforces whitelist-only realm access (non-admin accounts require an active admin grant).
- Admin realm endpoints support realm metadata updates and realm access grant/revoke workflows.

## Development principles

- Server is authoritative for simulation time and state
- Realm scoping is explicit in MMO mode
- API usability and learnability matter as much as raw functionality
- Tracker state is canonical in [docs/feature-tracker.md](docs/feature-tracker.md)
- Pre-alpha policy allows breaking changes when they simplify architecture
- Documentation is updated in the same change pass as behavior/API updates (at minimum `README.md` + Swagger/OpenAPI contract updates)
