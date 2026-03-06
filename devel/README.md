# Devel Docker Stack

This stack is split into compose files:

- `app.compose.yaml` for `lived-app`,
- `data.compose.yaml` for Postgres,
- `otel.compose.yaml` for Jaeger all-in-one.

## Run

From repo root (full local stack):

```bash
docker compose \
	-f devel/data.compose.yaml \
	-f devel/otel.compose.yaml \
	-f devel/app.compose.yaml \
	up --build
```

From repo root (app + otel, external Postgres):

```bash
docker compose \
	-f devel/otel.compose.yaml \
	-f devel/app.compose.yaml \
	up --build
```

## External Postgres

Set `LIVED_DATABASE_URL` in your shell or `.env` file used by Compose if your Postgres host/service differs from `lived-db`.

Example:

```bash
LIVED_DATABASE_URL=postgres://lived:lived@10.0.0.104:5432/lived?sslmode=disable
```

If you want OTEL export to target a remote endpoint (instead of in-stack `jaeger:4317`), set:

```bash
LIVED_OTEL_ENDPOINT=10.0.0.104:4317
```

## Endpoints

- App API: `http://localhost:8080`
- Prometheus metrics: `http://localhost:8080/metrics`
- Swagger: `http://localhost:8080/swagger/`
- Jaeger UI: `http://localhost:16686`
