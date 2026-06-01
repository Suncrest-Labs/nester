# Migration Runbook

This directory contains [golang-migrate](https://github.com/golang-migrate/migrate) SQL migration files for the Nester API database.

## Running Migrations

### Via Docker Compose (recommended for local dev)

Migrations run automatically on `make dev` when `RUN_MIGRATIONS=true` is set in `.env`.

### Manually with golang-migrate

```bash
# Apply all pending migrations
migrate -path apps/api/migrations -database "$DATABASE_DSN" up

# Apply migrations up to a specific version
migrate -path apps/api/migrations -database "$DATABASE_DSN" goto 12
```

### Check current version

```bash
migrate -path apps/api/migrations -database "$DATABASE_DSN" version
```

## Rolling Back

```bash
# Roll back one migration
migrate -path apps/api/migrations -database "$DATABASE_DSN" down 1

# Roll back N migrations
migrate -path apps/api/migrations -database "$DATABASE_DSN" down N
```

> **Warning:** Rolling back in production should be rare and done with a DB backup in hand. Always test the down migration locally first.

## Adding a New Migration

1. Find the next available sequential number:
   ```bash
   ls apps/api/migrations/ | sed 's/_.*//' | sort -n | uniq | tail -1
   ```
2. Check for conflicts (should print nothing):
   ```bash
   ls apps/api/migrations/ | sed 's/_.*//' | sort | uniq -d
   ```
3. Create the pair:
   ```
   NNN_descriptive_name.up.sql   — forward change
   NNN_descriptive_name.down.sql — exact reverse (no-op is acceptable if irreversible)
   ```

## ⚠️ Re-auth Required After Migration 009_add_user_roles

After deploying migration `009_add_user_roles`, **all existing admin JWT tokens are stale**. Tokens issued before this migration lack the `Roles` claim and will receive `403 Forbidden` on every role-gated admin endpoint.

**Resolution:** admins must log out and re-authenticate to receive a new token that includes their roles.

## Known Issues

- Numbering collisions exist at prefixes 007, 009, and 010. See [#523](https://github.com/Suncrest-Labs/nester/issues/523) for the fix tracking these conflicts.
- There are gaps in the sequence at 004 and 013 — these are expected (migrations were removed) and do not affect operation.
