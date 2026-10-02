# Database Backup, PITR, and Restore Runbook

## Overview

Nester's PostgreSQL database is the system of record for users, vault records,
transactions, and settlements — money movement. Until now there was no
automated backup strategy and no restore runbook: `docs/DEPLOYMENT.md` §4
already says to "restore from the pre-deployment snapshot" during a rollback,
but nothing actually took that snapshot or documented how to restore one. A
bad migration or data-loss event was unrecoverable in practice (nester#795).

This document covers three things:

1. Taking an automated logical backup (self-hosted path via `pg_dump`, or
   guidance for the managed-DB path).
2. Enabling/verifying continuous WAL archiving for point-in-time recovery
   (PITR) in production.
3. Restoring a backup — to a fresh instance or as a PITR target — verifying
   it, and re-pointing the API at it.

## 1. Automated logical backups

### Self-hosted (docker-compose) path

`scripts/db-backup.sh` runs `pg_dump` in the custom (`-Fc`) format — compressed,
restorable with `pg_restore`, and it supports parallel/selective restore later.
It writes to a temp file and renames on success (so a killed backup never
leaves a truncated file that looks valid), validates the artifact with
`pg_restore --list` before declaring success, and prunes artifacts older than
`BACKUP_RETENTION_DAYS` (default 14).

```bash
make db-backup
# or directly:
DATABASE_DSN="postgres://nester:nester_dev_password@localhost:5432/nester_dev?sslmode=disable" \
  scripts/db-backup.sh
```

Output: `./backups/nester_<UTC timestamp>.dump`.

**Scheduling it.** For a self-hosted deployment, run `scripts/db-backup.sh` on
a schedule (cron, a Kubernetes `CronJob`, or the CI scheduler) against the
production `DATABASE_DSN`, writing `BACKUP_DIR` to durable, off-host storage
(an object-storage-backed mount, not the same disk as the database). This repo
does not prescribe the scheduler — the script is scheduler-agnostic on
purpose, since the deployment target (docker-compose host, Kubernetes,
managed DB) determines the natural place to run it.

**Secrets.** The dump contains only application table contents — hashed
passwords and encrypted secrets are what the schema stores, never plaintext
ones (see `apps/api/internal/domain` for how credentials are stored at rest).
`db-backup.sh` also runs a best-effort tripwire that greps the artifact for
private-key and Sentry-auth-token-shaped strings and fails the backup rather
than let a false positive slip by unnoticed. It is a safety net, not a
guarantee — the actual guarantee is that the schema never stores plaintext
secrets in the first place.

### Managed Postgres (RDS / Cloud SQL / etc.) path

Prefer the provider's native automated-backup + PITR feature over running
`pg_dump` against a managed instance yourself:

- **RDS**: enable automated backups (sets a retention window, 1–35 days) and
  set `backup_retention_period` accordingly. This *is* PITR for RDS — restores
  can target any second within the retention window, not just backup
  boundaries.
- **Cloud SQL**: enable automated backups and turn on point-in-time recovery
  (binary logging for MySQL engines is not applicable here; for Postgres,
  Cloud SQL PITR uses WAL, described in the provider docs).
- Either way, still run `scripts/db-backup.sh` (against a read replica, if one
  exists, to avoid load on the primary) as a second, portable backup path —
  a logical dump restores into any Postgres instance, including a laptop for a
  local restore drill, which a provider-native snapshot generally cannot do
  outside that provider.

## 2. Point-in-time recovery (PITR) via WAL archiving — production

Logical backups (`pg_dump`) only restore to the moment the dump was taken.
PITR — continuous WAL (write-ahead log) archiving — lets a restore target any
point in time, which is what "roll back to 5 minutes before the bad
migration" actually requires.

**Managed Postgres**: enable the provider's PITR feature (see above) —
this is the recommended default for production and requires no
self-hosted WAL plumbing.

**Self-hosted production**: enable WAL archiving in `postgresql.conf`:

```
wal_level = replica
archive_mode = on
archive_command = 'test ! -f /wal-archive/%f && cp %p /wal-archive/%f'
```

Replace the `archive_command` with one that ships WAL segments to durable,
off-host storage (object storage via a tool like `wal-g` or `pgBackRest` is
the common production choice over a bare `cp`, since a `cp` to local disk
provides no protection if the host itself is lost). `wal-g`/`pgBackRest` also
manage base backups + WAL together and are what most self-hosted PITR setups
actually run in production; the `cp` example above is illustrative of the
mechanism, not a production-ready configuration.

**Verifying WAL archiving is actually working** (not just configured):

```sql
SELECT * FROM pg_stat_archiver;
```

`archived_count` should be increasing over time and `last_archived_time`
should be recent. A nonzero `failed_count` growing over time means
`archive_command` is failing — WAL segments are piling up in `pg_wal` and, if
unaddressed, will eventually fill the disk and halt the database. This is the
single most important thing to alert on for a self-hosted PITR setup.

## 3. Restore

### `scripts/db-restore.sh`

```bash
scripts/db-restore.sh <path-to-backup.dump> [target DSN]
# or, against the dev database:
make db-restore FILE=./backups/nester_20260925T120000Z.dump
```

What it does, in order:

1. Validates the archive with `pg_restore --list` before touching the target.
2. Restores with `pg_restore --clean --if-exists --no-owner --no-privileges
   --exit-on-error` — `--clean --if-exists` drops conflicting objects first so
   restoring into a database that already has a (possibly stale) schema
   doesn't fail on "relation already exists"; `--exit-on-error` fails fast on
   a partial restore rather than leaving the target ambiguously half-restored.
3. **Ordered migration-state check.** Queries the restored `schema_migrations`
   table and compares it against every `*.up.sql` file in
   `apps/api/migrations/`. If the restored database is missing a migration
   the current codebase expects, the script fails loudly rather than letting
   the API boot against a schema it doesn't actually match. (If you
   *intentionally* restored an older backup — e.g. PITR to before a bad
   migration — this is expected: run the API's normal migration step
   afterward, `RUN_MIGRATIONS=true` or the API's migrate-up command, to bring
   the schema forward before pointing production traffic at it.)

It deliberately does **not** create or drop the target database for you — a
restore script with a destructive default (e.g. silently dropping and
recreating a database) is exactly the failure mode this runbook exists to
prevent. Point it at a database you've already created (fresh, or a scratch
one — see the restore drill below).

### Restore drill (local, non-destructive)

`make db-restore-drill` takes the most recent backup in `./backups/` and
restores it into a **separate** scratch database (`nester_restore_drill`),
never touching `nester_dev`. Run this periodically — an untested backup is
not a backup, only a hope.

```bash
make db-backup            # produce a fresh artifact
make db-restore-drill     # restore it into a scratch DB and verify
```

### Restoring to a fresh instance (disaster-recovery path)

1. Provision a fresh Postgres instance (or `docker compose up postgres` alone
   against a clean volume).
2. Create the target database: `createdb -U nester nester_dev` (or via the
   managed provider's console/CLI).
3. `scripts/db-restore.sh <dump> <DSN of the fresh instance>`.
4. Confirm the script's own migration check reports success (see above). If
   it doesn't, resolve that before proceeding — do not skip it.
5. Point the API at the restored instance:
   `DATABASE_DSN=<restored DSN>` and start/restart the API. Do **not** set
   `RUN_MIGRATIONS=true` for a full-parity restore — the migrations are
   already applied, and re-running is a no-op at worst but there is no reason
   to invite it during a recovery.
6. Verify the API actually considers the database healthy:

   ```bash
   curl -s $HOST/health/detailed | jq '{status, database}'
   ```

   `database.ok` must be `true` before this instance takes production
   traffic.
7. Spot-check row counts / recent rows on a couple of the money-path tables
   (`transactions`, `settlements`) against what's expected for the backup's
   timestamp, as a sanity check beyond "the schema matches."

### PITR restore (production, provider-specific)

For a managed database, use the provider's point-in-time-restore action
(RDS: `restore-db-instance-to-point-in-time`; Cloud SQL: clone-to-timestamp),
targeting a timestamp just before the incident. This creates a **new**
instance — it does not restore in place — so the cutover is: restore to a new
instance, run the verification steps above against it, then re-point
`DATABASE_DSN` (and DNS/connection string, if applicable) at the new instance.
Never point production at a PITR target you haven't run the verification
steps against.

For self-hosted WAL-archiving PITR, the mechanics (`recovery_target_time` in
`postgresql.conf`/`recovery.signal`, replaying archived WAL up to that point)
are provider/tool-specific (`wal-g`/`pgBackRest` both document this) and
intentionally not duplicated here to avoid drifting out of sync with whichever
tool is actually deployed — follow that tool's restore documentation, then run
the same verification steps (§"Restoring to a fresh instance", steps 4–7)
against the result.

## 4. Retention

- **Logical backups** (`scripts/db-backup.sh`): 14 days by default
  (`BACKUP_RETENTION_DAYS`), pruned automatically after each successful run.
  Tune based on how far back a plausible "restore to before we noticed"
  scenario needs to reach for this deployment.
- **WAL archive / managed PITR window**: production should retain at minimum
  7 days of PITR range — long enough to cover a slow-to-notice data
  corruption bug, which is a more common trigger than a same-day incident.
  RDS/Cloud SQL: set the retention window explicitly; do not rely on the
  provider default, which may be shorter.

## 5. Verification checklist (for a real incident, not just the drill)

- [ ] Restored database's `schema_migrations` matches the migrations the
      currently-deployed API version expects (automated by
      `scripts/db-restore.sh`).
- [ ] `/health/detailed` reports `database.ok: true` against the restored
      instance.
- [ ] Row counts on `transactions` and `settlements` are consistent with the
      backup's timestamp (no unexplained gap or truncation).
- [ ] No plaintext secret was present in the backup artifact (automated by
      `scripts/db-backup.sh`'s tripwire at backup time — re-verify by hand if
      restoring an old, pre-tripwire backup taken before this script existed).
- [ ] A deposit and withdrawal complete end to end against the restored
      instance before it's promoted to serve real traffic — the same bar
      `docs/observability/runbooks/staging-reset-procedure.md` verification
      section uses, because a schema/health check can pass while the money
      path is broken.

## Related

- `docs/DEPLOYMENT.md` §4 (Rollback Procedure) — references this document for
  the database-restore step of a deploy rollback.
- `docs/observability/runbooks/staging-reset-procedure.md` — the analogous
  procedure for resetting staging to empty (not for restoring real data).
- Out of scope here: cloud-provider-specific Infrastructure-as-Code for
  provisioning backups/PITR, and the general deployment runbook (nester#610).
