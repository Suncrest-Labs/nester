#!/usr/bin/env bash
set -euo pipefail

# Nester automated logical backup script (pg_dump custom format -Fc)
# Stated retention: 14 days default (configurable via BACKUP_RETENTION_DAYS).
# RPO: up to backup frequency (recommended every 1 hour or daily depending on tier).

DATABASE_DSN="${DATABASE_DSN:-postgres://nester:nester_dev_password@localhost:5432/nester_dev?sslmode=disable}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"

mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TEMP_FILE="$BACKUP_DIR/.nester_$TIMESTAMP.dump.tmp"
FINAL_FILE="$BACKUP_DIR/nester_$TIMESTAMP.dump"

echo "[+] Starting database backup to $TEMP_FILE ..."

# Run pg_dump in custom format (-Fc)
pg_dump "$DATABASE_DSN" -Fc --no-owner --no-privileges > "$TEMP_FILE"

# Validate artifact with pg_restore --list
echo "[+] Validating backup artifact with pg_restore --list ..."
pg_restore --list "$TEMP_FILE" > /dev/null

# Tripwire: check for secret patterns
if grep -q -E '(PRIVATE KEY|sentry_auth_token)' "$TEMP_FILE" 2>/dev/null; then
  echo "[-] ERROR: Backup contains forbidden secret-like patterns! Aborting." >&2
  rm -f "$TEMP_FILE"
    exit 1
  fi

# Atomically rename on success
mv "$TEMP_FILE" "$FINAL_FILE"
echo "[+] Backup successfully created and verified: $FINAL_FILE"

# Prune backups older than retention window
echo "[+] Pruning backups older than $BACKUP_RETENTION_DAYS days ..."
find "$BACKUP_DIR" -name "nester_*.dump" -mtime +"$BACKUP_RETENTION_DAYS" -delete
echo "[+] Backup and prune completed successfully."
