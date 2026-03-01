# 30 API Instructions

## Response Contract

- Use the standard response envelope for API responses:
  - `status`
  - `message`
  - `requestId`
  - `data`

## Grouping

- Keep endpoints grouped by domain under `/v1/<group>`.
- System domain currently includes save lifecycle, behavior queueing/catalog, market status/history, and ascension.

## Documentation Expectations

- Keep OpenAPI in sync with implemented endpoints and payload semantics.
- Document player-facing restrictions clearly (for example world behaviors excluded from player catalog).
- Document market session behavior (open/closed windows) and query options for history endpoints.

## Error Handling

- Return meaningful HTTP status codes.
- Avoid leaking internals in client-facing messages.
- Keep logs rich enough to diagnose failures (request ID, route, status, error).
