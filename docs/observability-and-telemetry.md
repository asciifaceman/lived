# Observability and Telemetry

## Health and metrics

- Health: `/health`
- Prometheus metrics: `/metrics`

Current metric families include:

- world tick runs/errors/duration
- world tick advance minutes
- stream active connections
- stream connection rejections by reason

## Tracing

OpenTelemetry support is controlled by env flags:

- `LIVED_MMO_OTEL_ENABLED`
- `LIVED_OTEL_ENDPOINT`
- `LIVED_OTEL_SERVICE_NAME`
- `LIVED_OTEL_INSECURE`
- `LIVED_OTEL_SAMPLE_RATIO`

## Structured logging

HTTP logs include request metadata and, when tracing is active, correlation fields:

- `trace_id`
- `span_id`
- `trace_sampled`

## Local stack with Jaeger

Use compose split files under `devel/`:

```bash
docker compose -f devel/otel.compose.yaml -f devel/app.compose.yaml up --build
```

Jaeger UI default: `http://localhost:16686`

## Dashboard artifact

Reference Grafana dashboard JSON:

- `devel/grafana-dashboard.json`
