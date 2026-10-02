# Database Restore Drill Runbook & Findings

## Overview
This runbook details our automated and scheduled database restore drills, satisfying launch requirements for proven backup integrity and disaster recovery preparedness (nester#795).

## Stated SLOs, RPO, and Retention
- **Retention:** Automated backups are retained for 14 days (`BACKUP_RETENTION_DAYS=14`). Older artifacts are automatically pruned by `scripts/db-backup.sh`.
- **RPO (Recovery Point Objective):** $\le 1$ hour (based on hourly cron scheduling of automated `pg_dump` plus continuous WAL archiving in production).
- **RTO (Recovery Time Objective):** $\le 15$ minutes for logical full restore plus migration validation.

## Restore Drill Schedule & Automation
Restore drills are scheduled weekly via CI job or cron runner executing `make db-restore-drill` against an isolated scratch database (`nester_restore_drill`), preventing any production impact.

## Drill Runbook Steps
1. Produce or fetch the latest backup artifact: `make db-backup`
2. Execute the automated scratch restore drill: `make db-restore-drill`
3. Verify table counts, foreign key constraints, and migration table parity.

## Drill Findings & Fold-Back
- **Finding 1:** Initial restores failed when tables already existed. Resolved by adding `--clean --if-exists` flags to `pg_restore` in `scripts/db-restore.sh`.
- **Finding 2:** Silent partial failures risked corrupted states. Resolved by adding `--exit-on-error` to `pg_restore` and table count verification assertions in `db-restore-drill.sh`.
