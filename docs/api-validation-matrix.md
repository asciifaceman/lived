# API Validation Matrix

This matrix defines server-side validation and error semantics for mutating API endpoints.

## Validation Contract

- JSON payload parsing for mutating endpoints is strict via `src/server/requestbind.JSON`:
  - rejects malformed JSON,
  - rejects unknown fields,
  - rejects trailing payload content.
- `400 Bad Request` is for malformed payloads and field/value validation failures.
- `409 Conflict` is for valid requests blocked by current world/account/realm state.

## Mutating Endpoint Matrix

| Endpoint | Payload Validation | Domain Validation | `400` examples | `409` examples |
| --- | --- | --- | --- | --- |
| `POST /v1/auth/register` | strict JSON + required username/password bounds | unique username | invalid payload, short username/password | username already taken |
| `POST /v1/auth/login` | strict JSON + required credentials | account/session state gates | invalid payload, missing username/password | n/a |
| `POST /v1/auth/refresh` | strict JSON + required token | session/account active checks | invalid payload, missing refresh token | n/a |
| `POST /v1/onboarding/start` | strict JSON + name bounds | realm access + create-only uniqueness semantics | invalid payload, invalid name | character name already taken, character already exists in realm (use switch) |
| `POST /v1/onboarding/switch` | strict JSON + required `characterId` | account ownership + active-character selection invariants | invalid payload, missing/invalid `characterId` | character is not active and cannot be selected |
| `POST /v1/chat/messages` | strict JSON + message/channel constraints | channel moderation/binding rules | invalid payload, invalid message/channel | n/a |
| `POST /v1/system/import` | strict JSON + save payload checks | mode/state gates | invalid payload, invalid save data | disabled in MMO mode |
| `POST /v1/system/new` | strict JSON + required name | mode/state gates | invalid payload, missing name | disabled in MMO mode |
| `POST /v1/system/behaviors/start` | strict JSON + behavior/mode fields | queue/exclusive state rules | invalid payload, invalid mode/character | behavior exclusive-group conflict |
| `POST /v1/system/ascend` | strict JSON + optional name | ascension eligibility state | invalid payload | ascension not currently eligible |
| `POST /v1/admin/realms` | strict JSON + command fields | realm create invariants | invalid payload, invalid name/reason | n/a |
| `POST /v1/admin/realms/:id/actions` | strict JSON + action schema | lifecycle invariants | invalid payload/action | realm delete/decommission conflicts |
| `POST /v1/admin/realms/:id/config` | strict JSON + command/schema | realm config constraints | invalid payload/command/name | n/a |
| `POST /v1/admin/realms/:id/access/grant` | strict JSON + account/reason fields | access/presence constraints | invalid payload/accountId | n/a |
| `POST /v1/admin/realms/:id/access/revoke` | strict JSON + account/reason fields | access/presence constraints | invalid payload/accountId | n/a |
| `POST /v1/admin/moderation/accounts/:id/lock` | strict JSON + reason fields | self/admin safety checks | invalid payload/reason | cannot lock own account / last admin guard |
| `POST /v1/admin/moderation/accounts/:id/unlock` | strict JSON + reason fields | account existence/state | invalid payload/reason | n/a |
| `POST /v1/admin/moderation/accounts/:id/status` | strict JSON + status schema | self/admin safety checks | invalid payload/status | self-lock/last-admin guard |
| `POST /v1/admin/moderation/accounts/:id/roles` | strict JSON + role/action schema | self/admin safety checks | invalid payload/role/action | self-admin revoke / last-admin guard |
| `POST /v1/admin/moderation/accounts/bulk` | strict JSON + command schema | bulk realm/account constraints | invalid payload/command/limit | per-account conflict reasons in result rows |
| `POST /v1/admin/moderation/characters/:id` | strict JSON + command/schema | character constraints | invalid payload/fields | n/a |
| `POST /v1/admin/chat/channels` | strict JSON + channel schema | channel lifecycle constraints | invalid payload/channel metadata | channel key already exists |
| `DELETE /v1/admin/chat/channels/:key` | path+query validation | channel/binding existence | invalid key | n/a |
| `POST /v1/admin/chat/channels/:key/flush` | strict JSON + command/schema | binding existence | invalid payload/scope/reason | n/a |
| `POST /v1/admin/chat/channels/:key/moderation` | strict JSON + moderation schema | binding/account/action constraints | invalid payload/action/accountId | n/a |
| `POST /v1/admin/chat/channels/:key/system-message` | strict JSON + message schema | binding constraints | invalid payload/message | n/a |
| `POST /v1/admin/chat/wordlist` | strict JSON + rule schema | uniqueness/scope constraints | invalid payload/rule | n/a |
| `DELETE /v1/admin/chat/wordlist/:ruleId` | path validation | rule existence | invalid rule id | n/a |

## Notes

- Non-mutating GET/stream endpoints are excluded from this matrix.
- `423 Locked` and `403 Forbidden` remain intentionally distinct from `409` where policy/auth state is the primary reason for rejection.
