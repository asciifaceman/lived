# Getting Started

Lived is a server-authoritative idle/incremental game backend with a web client.

## What you get

- Go API server (`Echo`)
- Background world simulation loop
- PostgreSQL persistence (`GORM`)
- Optional embedded frontend build

## Prerequisites

- Go `1.26`
- PostgreSQL `14+` (16 recommended)
- Node.js `20+` (for frontend work)

## 5-minute local boot

1. Configure environment:
   - Copy `.env.example` to `.env`.
   - Set DB values (or set `LIVED_DATABASE_URL`).

2. Create DB:

```bash
go run . db setup
```

3. Run migrations (if needed explicitly):

```bash
go run . db migrate
```

4. Start server:

```bash
go run . run
```

5. Open:
   - App: `http://localhost:8080/`
   - Swagger UI: `http://localhost:8080/swagger/`
   - Health: `http://localhost:8080/health`

## MMO mode quick toggle

Set these in `.env`:

- `LIVED_MMO_AUTH_ENABLED=true`
- `LIVED_MMO_REALM_SCOPING_ENABLED=true`
- `LIVED_MMO_CHAT_ENABLED=true`
- `LIVED_MMO_ADMIN_ENABLED=true`

Then restart server.

## Common first commands

```bash
# recreate database from scratch
go run . db setup --recreate

# verify DB realm/index health
go run . db verify

# grant admin to account
go run . db set-admin --username player1
```

## Next reads

- Development workflow: [development-workflow.md](development-workflow.md)
- API guide: [api-guide.md](api-guide.md)
- Gameplay systems: [gameplay-systems.md](gameplay-systems.md)
