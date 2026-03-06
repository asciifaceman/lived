# Development Workflow

This page covers day-to-day coding workflow for backend + frontend.

## Backend loop

```bash
go run . run
```

Server responsibilities:

- Serve HTTP API (`/v1/*`)
- Run world simulation loop
- Persist runtime and gameplay state

## Frontend loop (optional)

```bash
cd web
npm install
npm run dev
```

To proxy frontend from Go server while preserving `/v1` on Go:

```bash
LIVED_WEB_DEV_PROXY_URL=http://localhost:5173
go run . run
```

## Build frontend for server hosting

```bash
cd web
npm run build
```

## Embedded production frontend build

```bash
cd web
npm run build:embed
cd ..
go build -tags embed_frontend ./...
```

## Useful DB commands

```bash
go run . db setup
go run . db setup --recreate
go run . db migrate
go run . db verify
```

## Testing

```bash
# all tests
go test ./...

# focused server tests
go test ./src/server/system ./src/server/player ./src/server/stream
```

## Feature tracker generation

`docs/feature-tracker.yaml` is canonical. Regenerate markdown view after edits:

```bash
go run ./tools/trackergen -yaml docs/feature-tracker.yaml -md docs/feature-tracker.md
```

One-time import from existing markdown (migration/bootstrap):

```bash
go run ./tools/trackergen -import-md docs/feature-tracker.md -yaml docs/feature-tracker.yaml -md docs/feature-tracker.md
```

## Stream auth transport note

- Preferred MMO stream auth is WebSocket subprotocol bearer (`Sec-WebSocket-Protocol: lived.v1, bearer.<accessToken>`).
- Query-token fallback (`?accessToken=...`) is disabled by default.
- Enable fallback only for compatibility clients:

```bash
LIVED_STREAM_QUERY_ACCESS_TOKEN_ENABLED=true
```

## Docker development stack

Use split compose files:

- app: `devel/app.compose.yaml`
- data: `devel/data.compose.yaml`
- otel: `devel/otel.compose.yaml`

```bash
docker compose -f devel/data.compose.yaml -f devel/otel.compose.yaml -f devel/app.compose.yaml up --build
```

More docker notes: [../devel/README.md](../devel/README.md)
