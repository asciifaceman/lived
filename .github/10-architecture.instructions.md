# 10 Architecture Instructions

## Boundaries

- `pkg/` contains reusable infrastructure and persistence-facing packages.
- `src/app` owns process runtime orchestration (startup, world loop integration).
- `src/gameplay` owns behavior engine/domain runtime logic.
- `src/server` owns HTTP bootstrap concerns (Echo setup, middleware, root route wiring).
- `src/server/<group>` owns route-group handlers (for example `src/server/system`).

## Runtime Shape

- Server and world loop run concurrently.
- World loop is authoritative for time progression and behavior processing.
- API handlers enqueue requests and query state; they do not bypass world progression rules.

## Cross-Cutting Constraints

- Keep transactionality explicit for multi-step updates.
- Keep loop behavior deterministic at lifecycle boundaries.
- Preserve graceful shutdown semantics (finish in-flight tick work and persist runtime state).
