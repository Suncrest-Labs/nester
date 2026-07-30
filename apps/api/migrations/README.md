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

Claim your number when you **open** the PR, not when you start work. Several
PRs developed in parallel each grabbed what was "the next" number at the time,
which is how the collisions at 060/061/069/070/081 happened.

1. Find the next available sequential number:
   ```bash
   ls apps/api/migrations/ | sed 's/_.*//' | sort -n | uniq | tail -1
   ```
2. Check for conflicts (should print nothing):
   ```bash
   ls apps/api/migrations/*.sql \
     | sed -E 's/\.(up|down)\.sql$//' \
     | sort -u \
     | sed -E 's/.*\/([0-9]+)_.*/\1/' \
     | sort | uniq -d
   ```
   This is the exact check CI runs. It strips the `.up`/`.down` suffix before
   deduplicating, so a half-renamed pair is still caught — a naive
   `sed 's/_.*//' | uniq -d` sees `.up` and `.down` as distinct entries and
   reports nothing.
3. Create the pair:
   ```
   NNN_descriptive_name.up.sql   — forward change
   NNN_descriptive_name.down.sql — exact reverse (no-op is acceptable if irreversible)
   ```
4. If your migration depends on an earlier one (e.g. an index on a column another
   migration adds), make sure your number is higher than the one it depends on.
   Renumbering later is not always safe: once a version is recorded in
   `schema_migrations`, renaming the file causes golang-migrate to re-run or skip it.

## ⚠️ Re-auth Required After Migration 009_add_user_roles

After deploying migration `009_add_user_roles`, **all existing admin JWT tokens are stale**. Tokens issued before this migration lack the `Roles` claim and will receive `403 Forbidden` on every role-gated admin endpoint.

**Resolution:** admins must log out and re-authenticate to receive a new token that includes their roles.

## Known Issues

None currently. The prefix collisions previously recorded here (007, 009, 010 —
[#523](https://github.com/Suncrest-Labs/nester/issues/523)) and the later set at
060, 061, 069, 070 and 081 ([#995](https://github.com/Suncrest-Labs/nester/issues/995))
have all been resolved. CI enforces uniqueness on every push.
- There are gaps in the sequence at 004 and 013 — these are expected (migrations were removed) and do not affect operation.
