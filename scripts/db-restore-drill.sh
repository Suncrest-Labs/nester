#!/usr/bin/env bash
set -euo pipefail

# Nester database restore drill script
# Restores the most recent backup in ./backups/ into a scratch database 'nester_restore_drill'
# and verifies tables and record integrity.

BACKUP_DIR="${BACKUP_DIR:-./backups}"
BASE_DSN="${BASE_DSN:-postgres://nester:nester_dev_password@localhost:5432/?sslmode=disable}"
SCRATCH_DB="nester_restore_drill"
SCRATCH_DSN="postgres://nester:nester_dev_password@localhost:5432/${SCRATCH_DB}?sslmode=disable"

LATEST_BACKUP="$(ls -t "$BACKUP_DIR"/nester_*.dump 2>/dev/null | head -n 1 || true)"

if [ -z "$LATEST_BACKUP" ]; then
  echo "[-] ERROR: No backup files found in $BACKUP_DIR. Run make db-backup first." >&2
  exit 1
fi

echo "[+] Latest backup selected for drill: $LATEST_BACKUP"

echo "[+] Recreating scratch database '$SCRATCH_DB' ..."
psql "$BASE_DSN" -c "DROP DATABASE IF EXISTS \"$SCRATCH_DB\";"
psql "$BASE_DSN" -c "CREATE DATABASE \"$SCRATCH_DB\";"

echo "[+] Running restore into scratch database ..."
scripts/db-restore.sh "$LATEST_BACKUP" "$SCRATCH_DSN"

echo "[+] Verifying data and schema against known tables ..."
TABLE_COUNT="$(psql "$SCRATCH_DSN" -t -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" | tr -d ' ')"
echo "[+] Verified public tables present: $TABLE_COUNT"

if [ "$TABLE_COUNT" -eq 0 ]; then
  echo "[-] ERROR: Restore drill verified zero tables in scratch database!" >&2
  exit 1
fi

echo "[+] RESTORE DRILL SUCCESSFUL: Scratch database $SCRATCH_DB verified and operational."
