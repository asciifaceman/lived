# API Guide

## Base conventions

- Base path: `/v1`
- Standard response envelope:

```json
{
  "status": "success|error",
  "message": "...",
  "requestId": "optional",
  "data": {}
}
```

## Primary groups

- `GET /v1/system/status`: world/runtime snapshot
- `POST /v1/system/behaviors/start`: queue behavior
- `GET /v1/system/behaviors/catalog`: behavior catalog + availability
- `GET /v1/player/status`: player-facing snapshot
- `GET /v1/player/inventory`: inventory
- `GET /v1/player/behaviors`: queue/history

## MMO-specific groups

- `/v1/auth/*`: login/register/refresh/me/logout
- `/v1/onboarding/*`: character onboarding/status
- `/v1/chat/*`: channels/messages/post
- `/v1/feed/*`: public world feed
- `/v1/mmo/*`: realm stats
- `/v1/admin/*`: admin control plane

## Documentation endpoints

- Swagger UI: `/swagger/`
- OpenAPI JSON: `/swagger/openapi.json`

## Auth model (MMO mode)

- Bearer access token for authenticated routes
- Optional `characterId` on many routes to select account character
- Realm is resolved from authenticated character context where required

## Reliability controls

- Optional rate limiting (`LIVED_RATE_LIMIT_*`)
- Optional idempotency replay (`LIVED_IDEMPOTENCY_*`)

## Behavior scheduling modes (v1)

- `POST /v1/system/behaviors/start` supports explicit queue mode contracts:
  - `mode=once` (default): queue a single run
  - `mode=repeat`: continuously re-queue after each completion
  - `mode=repeat-until`: continuously re-queue until `repeatUntil` duration elapses
- `repeatUntil` is only valid when `mode=repeat-until`
- Response data includes resolved mode metadata (`mode`, and for repeat-until: `repeatUntilMinutes`, `repeatUntilTick`)

## Rest recovery behavior (v1)

- Catalog includes a `Rest` player behavior (`player_rest`)
- Rest consumes no stamina and applies accelerated stamina recovery on completion
- Recovery is deterministic and capped by max stamina

## Chat/realm binding (v1)

- Chat reads include binding metadata:
  - `scope`
  - `scopeKey`
- Admin chat channel operations also return the same binding identity fields (`scope`, `scopeKey`, `realmId`)
- Wordlist policy operations return policy binding metadata:
  - `policyScope=global`
  - `policyScopeKey=global`
- Current v1 channel binding is realm-bound instances only (`scope=realm`, `scopeKey=realm:{id}`)
- Current v1 moderation policy binding is global only (applies to all realms/channels)
- Reserved migration shape for future multi-realm channels is an additional scope family with opaque `scopeKey`; clients should treat `scopeKey` as an identity token, not parse-only routing data

For endpoint-level request/response schemas and examples, use Swagger as the source of truth.
