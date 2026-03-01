# 00 Code Quality Instructions

## Objective

Ensure changes are stable, deterministic, and easy to reason about in a server-authoritative simulation codebase.

## Rules

- Prefer small, composable functions over large monoliths.
- Keep side effects explicit and close to transaction boundaries.
- Use clear, descriptive names for gameplay concepts (behavior, unlock, inventory, market, ascension).
- Keep business rules out of transport handlers where possible.
- Avoid hidden global state.
- Keep error messages actionable for API clients and logs.
- Preserve backwards compatibility for persisted state where practical.

## Verification

- Run `gofmt` on changed Go files.
- Run `go build ./...` after non-trivial changes.
- Prefer targeted runtime/API smoke checks when changing loop or behavior resolution logic.

## Documentation

- Update `README.md` when behavior, endpoint, or configuration semantics change.
- Keep design references in `docs/` aligned with implemented behavior.
