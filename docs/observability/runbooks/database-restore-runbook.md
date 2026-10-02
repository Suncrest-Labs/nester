# Production Database Restore Runbook & RTO/RPO Targets

## Objectives & Targets

- **Recovery Point Objective (RPO):** < 24 hours via daily automated encrypted snapshots, and < 5 minutes for point-in-time recovery (PITR) via continuous WAL archiving.
- **Recovery Time Objective (RTO):** < 30 minutes for full logical restoration and verification against the migration chain.

## Automated Backups

Production mainnet database backups run daily via scheduled infrastructure jobs invoking `scripts/db-backup.sh`. Backups are compressed (`-Fc`), validated via `pg_restore --list`, tripwire-checked for sensitive patterns, encrypted with AES-256-CBC, and shipped to off-site object storage (AWS S3 / GCS buckets with object locking).

## Restore Procedure

1. **Provision Target Instance:** Spin up a clean PostgreSQL instance or target database.
2. **Fetch and Decrypt Artifact:** Download the required backup `.dump.enc` from off-site storage.
3. **Execute Restore Script:**
   ```bash
   export BACKUP_ENCRYPTION_KEY="your-secure-key"
   export DATABASE_DSN="postgres://user:pass@host:5432/db?sslmode=disable"
   scripts/db-restore.sh ./backups/nester_20260925T120000Z.dump.enc
   ```
4. **Verify Health & Migrations:** Check API `/health/detailed` and confirm migration consistency.

## Restore Drill Cadence

Restore drills are executed automatically in CI via `make db-restore-drill` to ensure zero drift between disaster recovery documentation and executable reality.
