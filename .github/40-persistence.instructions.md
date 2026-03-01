# 40 Persistence Instructions

## Release Phase (Pre-Alpha)

- The project is currently **pre-alpha**.
- During pre-alpha, prefer speed over compatibility for persistence work:
	- breaking schema changes are acceptable,
	- backward-compatible migration paths are optional,
	- destructive reset/recreate flows are acceptable for local/test databases.
- Assume developers may frequently recreate databases while iterating.
- When pre-alpha ends (explicitly stated by the user or removed from instructions), return to conservative migration planning and compatibility expectations.

## Model Conventions

- Use `dal.BaseModel` for ID and timestamps.
- Do not default to `gorm.Model` unless soft-delete is intentionally required.

## Transactionality

- Use explicit transactions for behavior activation/completion, ascension, import/new-game replacement, and market updates.
- Keep run-state resets deterministic and comprehensive.

## Runtime State

- Persist world loop runtime metadata to support shutdown recovery and startup catch-up.
- Persist behavior queue state for observability and replay-safe progression.

## Market and History

- Persist current market prices and historical deltas.
- Market history should remain queryable for ticker-style API responses.
- Backfill/seed history for old saves where required to preserve endpoint usability.
