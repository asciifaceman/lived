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
