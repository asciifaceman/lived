# 40 Persistence Instructions

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
