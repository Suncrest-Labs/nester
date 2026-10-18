# Production Database Restore Runbook & RTO/RPO Targets

## Objectives & Targets

- **Recovery Point Objective (RPO):** < 24 hours via daily automated encrypted snapshots (measured: `42s` on hourly/on-demand snapshot), and < 5 minutes for point-in-time recovery (PITR) via continuous WAL archiving.
- **Recovery Time Objective (RTO):** < 30 minutes for full logical restoration, migration verification, API readiness, and indexer resumption (measured in #1565 drill: `38s`).

## Automated Backups

Production mainnet database backups run via scheduled infrastructure jobs invoking `scripts/db-backup.sh` with `BACKUP_ENCRYPTION_KEY` set. Backups are compressed (`-Fc`), validated via `pg_restore --list`, tripwire-checked for sensitive patterns, encrypted with `openssl enc -aes-256-cbc -pbkdf2 -salt` (`.dump.enc`), and shipped to off-site object storage (AWS S3 / GCS buckets with object locking).

## Restore Procedure

1. **Provision Target Instance:** Spin up a clean PostgreSQL instance or target scratch database.
2. **Fetch Artifact:** Download the required backup `.dump.enc` from off-site storage.
3. **Execute Restore Script:**
   `scripts/db-restore.sh` automatically detects `.enc` artifacts, decrypts them to a temporary file using `BACKUP_ENCRYPTION_KEY` (`AES-256-CBC` with `PBKDF2`), validates the archive via `pg_restore --list`, restores into `DATABASE_DSN`, verifies all migrations in `apps/api/migrations` are recorded in `schema_migrations`, and shreds the temporary file on exit:
   ```bash
   export BACKUP_ENCRYPTION_KEY="your-secure-key"
   export DATABASE_DSN="postgres://user:pass@host:5432/db?sslmode=disable"
   scripts/db-restore.sh ./backups/nester_20261018T033000Z.dump.enc "$DATABASE_DSN"
   ```
4. **Verify Health, Migrations & Indexer Cursor Resumption:**
   - Start the API against the restored database and check `/readyz` (`HTTP 200`).
   - Confirm the Soroban event indexer resumes from the restored ledger cursor without duplicate postings:
     ```sql
     SELECT max(ledger_sequence) AS latest_ledger, count(*) AS total_events FROM processed_events;
     SELECT count(*) AS duplicates FROM (
       SELECT event_id FROM processed_events GROUP BY event_id HAVING count(*) > 1
     ) d;
     ```

## Restore Drill Cadence

Restore drills are executed via `make db-restore-drill` (`scripts/db-restore-drill.sh`) to ensure zero drift between disaster recovery documentation and executable reality. See `docs/observability/runbooks/database-restore-drill.md` for the latest measured RTO (`38s`) and RPO (`42s`) drill report.
