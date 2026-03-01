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

## Docker development stack

Use split compose files:

- app: `devel/app.compose.yaml`
- data: `devel/data.compose.yaml`
- otel: `devel/otel.compose.yaml`

```bash
docker compose -f devel/data.compose.yaml -f devel/otel.compose.yaml -f devel/app.compose.yaml up --build
```

More docker notes: [../devel/README.md](../devel/README.md)
