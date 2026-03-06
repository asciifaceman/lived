# Admin and Operations

## Admin API scope

Admin routes are under `/v1/admin/*` and require admin role.

Typical domains:

- realm/system actions
- account moderation and role changes
- character moderation
- chat channel/policy controls
- audit queries and exports

## Audit model

Admin actions are recorded as immutable audit events with:

- actor identity
- reason code + optional note
- before/after payload snapshots
- occurred tick and realm context

## Realm operations

Current capabilities include market and maintenance controls, with additional realm lifecycle work tracked in the feature tracker.

## Account/session controls

- lock/unlock accounts
- role grant/revoke
- optional session revocation on status updates

## Chat moderation controls

- channel lifecycle actions
- channel moderation actions (ban/unban/kick)
- system message publication
- global wordlist policy controls

## Operational references

- Feature tracker: [feature-tracker.md](feature-tracker.md)
- MMO migration plan: [mmo-migration-plan.md](mmo-migration-plan.md)

## Execution Tracking Note

- This document is operational architecture/reference context.
- Live execution state and prioritization belong in [feature-tracker.md](feature-tracker.md).

## Process-oriented findings (2026-03-01 reference snapshot)

The current admin surface is functional, but still mixes intent styles (state-upsert vs explicit process steps). These findings are historical context; active follow-up should be tracked in [feature-tracker.md](feature-tracker.md):

1. **Mixed command semantics across domains**
	- Some actions are explicit process commands (`grant`, `revoke`, `pause`, `resume`, `remove`), while others remain upsert-like (`chat channel create` also edits/reactivates).
	- Follow-up: split create/edit/reactivate into distinct command contracts where operator intent matters.

2. **Lifecycle transitions are not first-class**
	- Channel workflows now improved in UI, but backend still models lifecycle through create+remove/upsert toggles instead of explicit transition verbs.
	- Follow-up: define domain transitions (`create`, `edit`, `activate`, `deactivate`, `attach-to-realm`, `detach-from-realm`) and audit against transition names.

3. **Operator context can be inferred instead of declared**
	- Several handlers infer defaults (`realmId=1`, default reasons) that are useful for resiliency but reduce explicit operation intent.
	- Follow-up: require explicit operator context for high-impact actions while keeping safe defaults only for low-risk reads.

4. **No preflight for destructive operations**
	- Destructive actions execute directly and rely on UI confirmations.
	- Follow-up: add optional preflight/preview contracts for destructive operations and structured operator confirmation metadata in audit payloads.

5. **Tab-level admin health visibility is improving but incomplete**
	- Admin currently loads all domains in one pass; failures are partially surfaced.
	- Follow-up: continue per-tab diagnostics/retries so operators can recover specific panes without full refresh.

6. **Channel domain model is still partially binding-centric (high priority)**
	- Intended model: `Channel -> Realm bindings`, where channel metadata (name/subject/description) is global and bindings only control realm visibility/membership.
	- Current behavior still exposes binding-centric edit/flush context in places, which is confusing and can imply `channel:realm` is the domain entity.
	- Follow-up: implement BUG-023 by separating global channel configuration operations from realm membership operations in both API semantics and Admin UX.
