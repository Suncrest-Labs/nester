#!/usr/bin/env bash
set -euo pipefail

# Nester database restore script
# Usage: scripts/db-restore.sh <path-to-backup.dump> [target DSN]

BACKUP_FILE="${1:-}"
TARGET_DSN="${2:-${DATABASE_DSN:-postgres://nester:nester_dev_password@localhost:5432/nester_dev?sslmode=disable}}"

if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
  echo "[-] ERROR: Please specify a valid backup file path." >&2
  echo "Usage: $0 <path-to-backup.dump> [target DSN]" >&2
  exit 1
fi

echo "[+] Validating backup file: $BACKUP_FILE ..."
pg_restore --list "$BACKUP_FILE" > /dev/null

echo "[+] Restoring database from $BACKUP_FILE into target ..."
pg_restore --dbname="$TARGET_DSN" --clean --if-exists --no-owner --no-privileges --exit-on-error "$BACKUP_FILE"

echo "[+] Database restore completed successfully."
echo "[+] Verifying migration state ..."

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATIONS_DIR="${SCRIPT_DIR}/../apps/api/migrations"

if ! command -v psql >/dev/null 2>&1; then
  echo "[-] WARNING: psql not found on PATH — skipping automated migration-version check." >&2
else
  restored_versions="$(psql "$TARGET_DSN" -Atc "SELECT version FROM schema_migrations ORDER BY version;" 2>/dev/null || true)"

  if [ -z "$restored_versions" ]; then
    echo "[-] ERROR: schema_migrations is empty or missing after restore. Do not point the API at this database until this is resolved." >&2
    exit 1
  fi

  missing=0
  for up_file in "$MIGRATIONS_DIR"/*.up.sql; do
    [ -e "$up_file" ] || continue
    version="$(basename "$up_file" .up.sql)"
    if ! grep -qxF "$version" <<<"$restored_versions"; then
      echo "[-] MISSING migration in restored database: ${version}" >&2
      missing=1
    fi
  done

  if [ "$missing" -eq 1 ]; then
    echo "[-] ERROR: Restored database is missing migrations this codebase expects. Do not point the API at this database until this is resolved." >&2
    exit 1
  fi

  echo "[+] Migration state OK: restored database has all expected migrations applied."
fi
