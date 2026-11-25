# Production Database Restore Runbook & RTO/RPO Targets

## Objectives & Targets

- **Recovery Point Objective (RPO):** < 24 hours via daily automated encrypted snapshots, and < 5 minutes for point-in-time recovery (PITR) via continuous WAL archiving.
- **Recovery Time Objective (RTO):** < 30 minutes for full logical restoration and verification against the migration chain.

## Automated Backups

The repository's `scripts/db-backup.sh` creates and validates a local custom-format
`pg_dump` artifact. It does not encrypt or upload backups. Production encryption,
retention, off-site replication, and PITR are responsibilities of the configured
infrastructure backup pipeline; verify those controls and the source artifact
with the provider before beginning a restore drill. Do not treat the local
development backup target as a production backup.

## Restore Procedure

1. **Provision Target Instance:** Spin up a clean PostgreSQL instance or target database.
2. **Fetch and Decrypt Artifact:** Retrieve the selected production artifact
   through the approved provider workflow. Decrypt it only into protected
   scratch storage using the provider's key-management process. The output
   passed to `scripts/db-restore.sh` must be a PostgreSQL custom-format dump
   that `pg_restore --list` can read; the script does not decrypt `.dump.enc`
   files.
3. **Execute Restore Script:**
   ```bash
   scripts/db-restore.sh ./scratch/nester_20260925T120000Z.dump "$SCRATCH_DATABASE_DSN"
   ```
   `SCRATCH_DATABASE_DSN` must point to a disposable, isolated database, never
   production. Do not place passwords or decryption keys in the runbook,
   repository, or shell history.
4. **Verify Health & Migrations:** Check API `/health/detailed` and confirm migration consistency.

## Restore Drill Cadence

The local `make db-restore-drill` target only backs up `nester_dev` and restores
that local artifact to `nester_restore_drill`; it does not exercise the
production backup pipeline, launch the API/indexer, or measure RTO/RPO. Use the
production-artifact procedure in
[`database-restore-drill.md`](./database-restore-drill.md) for launch evidence.
Record and review the actual measurements before claiming a production restore
drill has passed.
