# Database Restore Drill Runbook & Findings

## Status and scope

The local `make db-restore-drill` target is a development smoke test, not evidence
of a restore from the production backup pipeline. It first runs `make db-backup`
against `nester_dev`, then restores the resulting local dump into
`nester_restore_drill`. No production-pipeline restore result or measured RTO/RPO
is recorded by that target.

Do not describe a drill as complete until the steps below have been run using a
backup artifact from the production pipeline and an isolated scratch PostgreSQL
instance. Never point `BASE_DSN` or the restore target at production.

## Objectives and measurements

The production targets in
[`database-restore-runbook.md`](./database-restore-runbook.md) are objectives,
not measured drill results:

- **Retention:** 14 days where configured by the backup pipeline.
- **RPO target:** less than 24 hours for daily snapshots; less than 5 minutes
  only where continuous PITR/WAL archiving is enabled.
- **RTO target:** less than 30 minutes for restoration and verification.

Record actual values for each drill. Do not substitute these targets for
measurements:

- **Measured RPO:** drill start UTC minus the backup's verified data cutoff UTC.
- **Measured RTO:** scratch restore start UTC to the point when API health,
  indexer resumption, and duplicate-posting checks all pass.

## Prerequisites

1. Obtain the latest eligible artifact from the production backup pipeline and
   record its provider backup ID, creation time, data cutoff time, and checksum.
2. Decrypt the artifact only into protected scratch storage using the approved
   key-management process. Do not place keys in this repository, command logs,
   or the drill report.
3. Provision a disposable PostgreSQL instance isolated from production. Confirm
   the scratch DSN points only to that instance and that the required tools
   (`psql`, `pg_restore`, and the API runtime) are available.
4. Record the start time in UTC before beginning the restore.

The repository's `scripts/db-backup.sh` creates a local plaintext `pg_dump`
artifact; it does not fetch, encrypt, or upload a production backup. Do not use
`make db-backup` as a substitute for the production pipeline artifact.

## Drill steps

1. Validate the decrypted artifact with `pg_restore --list` and verify its
   checksum against the recorded value.
2. Restore it to the disposable database with
   `scripts/db-restore.sh <dump> <scratch-database-dsn>`. The script verifies
   migration parity; resolve any failure before starting the API.
3. Record the restored `system_state` value for
   `event_indexer.last_ledger`, plus baseline counts for `processed_events`,
   `ledger_entries`, and the relevant money-path tables.
4. Start the API/indexer against the scratch database using the same build and
   configuration intended for recovery. Confirm `/health/detailed` reports a
   healthy database and observe that the indexer resumes from the restored
   cursor and advances.
5. Restart the API/indexer once. Confirm the cursor remains monotonic and that
   replay/restart does not create duplicate processed events or ledger postings.
   Record the post-restart counts and any duplicate-key or balance-integrity
   errors.
6. Record the completion time and compute RTO/RPO using the definitions above.
   Attach sanitized logs and count/query results; do not include credentials or
   customer data.

## Drill report

For each completed drill, record:

| Field | Result |
| --- | --- |
| Run date and environment | |
| Backup provider ID / checksum | |
| Backup creation and data cutoff UTC | |
| Scratch PostgreSQL version | |
| Restore start / completion UTC | |
| API health and indexer resumption | |
| Cursor before / after restart | |
| Relevant counts before / after restart | |
| Duplicate postings or integrity errors | |
| Measured RTO | |
| Measured RPO | |

## Findings and follow-up

Record each observed failure, its impact, corrective action, and a successful
rerun here. Historical script safeguards (`--clean --if-exists`,
`--exit-on-error`, and migration checks) do not by themselves prove that the
production backup pipeline can be restored or that the indexer resumes without
duplicate postings.
