# Copilot Instructions

## Mission and Product Context

- This is **Lived**, an idle/incremental game inspired by "Lives Lived".
- The server owns game time and runs the continuous simulation/game loop.
- Players interact through an **Echo-based HTTP API** and may build their own clients.
- API usability and learnability are first-class goals.

## Core Technology Stack

- Language target: **Go 1.26** (align new code and recommendations with Go 1.26 semantics).
- HTTP framework: **Echo**.
- Persistence: **GORM + PostgreSQL**.
- Deployment target: likely **Docker container**.
- Authentication model is currently undecided (single-tenant local vs multiplayer).

## Project Layout and Boundaries

- Place reusable, discrete libraries in: `pkg/PACKAGE_NAME`.
- Place data access / DAL and database-facing reusable packages in `pkg/` (for example `pkg/dal`, `pkg/db`, `pkg/migrations`).
- Place core game orchestration/domain/runtime code in: `src/PACKAGE_NAME`.
- Keep package APIs cohesive and small; avoid cross-package coupling unless required.
- Respect Go conventions for naming, package structure, and exported identifiers.
- Organize HTTP API groups into focused subpackages under `src/server/<group>` (for example `src/server/system`) while keeping `src/server` for server bootstrap/wiring.

## Instruction Governance

- Treat this instruction set as a living document; update it over time based on what is built, learned, and changed in the project.
- It is acceptable to split guidance into multiple domain-specific instruction files over time (for example API, persistence, simulation), as long as references are maintained clearly.
- Keep split instruction files consistent with this root guidance and avoid conflicting directives.
- Domain-specific instruction files are located under `.github/` and loaded by naming order:
	- `00-codequality.instructions.md`
	- `10-architecture.instructions.md`
	- `20-gameplay.instructions.md`
	- `30-api.instructions.md`
	- `40-persistence.instructions.md`
- New sessions should treat this root file + ordered domain files as the baseline project memory.

## Architecture Principles

- Prefer coherent architecture over quick hacks; changes should integrate with existing code paths.
- Do not introduce patterns that diverge from established project conventions without explicit reason.
- Favor explicit boundaries between:
	- game domain logic,
	- simulation loop/runtime orchestration,
	- transport layer (Echo handlers),
	- persistence layer (GORM repositories/models).
- Keep logic deterministic and testable where practical.

## Gameplay Runtime Conventions

- Model time-based gameplay as scheduled behaviors/actions processed by the world loop.
- Keep behavior lifecycle explicit (`queued`, `active`, `completed`, `failed`) and persisted.
- Evaluate behavior requirements (unlocks/resources) at activation time and apply costs/outputs transactionally.
- Allow optional output variance expressions for behavior results (for example static ranges and dice-like notation) while keeping deterministic contracts around when rolls are resolved (on completion).
- Keep world-driven behaviors (AI/system) observable via world events/status.
- Prefer market movement driven by explicit world/AI behaviors over arbitrary random price mutation.
- Maintain inventory and ascension as first-class gameplay systems:
	- inventory holds run-state resources and currency,
	- ascension persists meta bonuses while resetting run-state.

## Concurrency and Reliability Expectations

- Treat the game loop and API as concurrent actors; design for race-free behavior.
- Guard shared mutable state explicitly (channels, mutexes, worker ownership, or transaction boundaries).
- Avoid data races, unsafe global mutation, and hidden side effects.
- Propagate context (`context.Context`) through DB and request operations.
- Handle graceful shutdown and ensure loop/persistence consistency during stop/restart.
- World loop shutdown should finish in-flight tick work, then flush loop runtime state so scheduled behavior queues can resume on next startup.
- Tick simulation should be delta-time based (elapsed real time) to support catch-up and future time-scaling behavior.

## API Design and Swagger Requirements

- The entire API should be described comprehensively in Swagger/OpenAPI.
- Documentation should teach players how to use the API and understand gameplay interactions.
- For each endpoint, include clear:
	- purpose and gameplay meaning,
	- request/response schemas,
	- validation rules,
	- error cases and status codes,
	- examples.
- Keep API naming and resource models intuitive and easy to understand.

## Frontend UX Guidance

- Prefer viewport-efficient dashboard layouts that minimize page-level scrolling on common desktop resolutions.
- Keep core operational panels (queue/history, player state, world feed) continuously visible when practical.
- Use tabs/popovers/modals for secondary views and infrequent run-control actions to preserve space.
- Favor emergent progression identity from player actions over explicit class-pick UX unless product requirements change.

## Database and Persistence Guidance

- Use GORM and Postgres features in maintainable, explicit ways.
- Support GORM-driven migrations for database provisioning and lifecycle management.
- Prefer explicit transactions for multi-step state updates that must be atomic.
- Design schemas/migrations around gameplay invariants and consistency.
- Avoid leaky persistence details in API/domain layers.
- Be mindful of long-running loop writes and contention with request-driven updates.

## DAL Model Conventions

- Default to embedding `dal.BaseModel` (`ID`, `CreatedAt`, `UpdatedAt`) for GORM entities.
- Do not default to `gorm.Model` because `DeletedAt` introduces soft-delete semantics.
- Current save lifecycle endpoints (import/new game) assume deterministic hard delete + recreate behavior.
- Only introduce soft-delete (`DeletedAt`) when explicitly required by product behavior (for example recovery/audit flows), and update queries/handlers to use it intentionally.

## Dependency and Library Policy

- Prefer actively maintained, widely used libraries.
- Avoid abandoned/archived dependencies.
- Verify a library actually exists and is viable before suggesting or implementing it.
- Verify suggested snippets against official docs/current APIs before applying.
- When uncertain between alternatives, present tradeoffs and ask before locking in architecture.

## Decision-Making Behavior for Copilot

- Ask clarifying questions when requirements are ambiguous or architecture-impacting.
- Do not silently make high-impact product decisions (e.g., auth strategy, tenancy model, multiplayer model).
- Keep changes scoped, coherent, and integrated with the current codebase.
- Prefer incremental, reviewable changes over sweeping rewrites.

## Testing Expectations

- Write actionable unit tests that validate project behavior and domain rules.
- Test game logic, API behavior, and persistence interactions where relevant.
- Do not write tests that primarily validate Go, OS behavior, or third-party library internals.
- Favor deterministic tests (controllable time/state) for idle simulation logic.
- Add regression tests when fixing bugs.

## Versioning Discipline

- When shipping behavior-visible or API-visible changes, bump exposed version metadata in `pkg/version/version.go`.
- When UI behavior or UX changes are shipped, bump `web/package.json` version and keep it aligned with `pkg/version/version.go` frontend version.
- Prefer small incremental version bumps for each meaningful feature/fix pass so API/UI clients can reason about change sets.

## Quality Bar

- Prioritize working, stable, thread-safe code.
- Keep error handling explicit and meaningful.
- Ensure new code composes cleanly with existing packages and conventions.
- Optimize for maintainability and clarity for future contributors.

