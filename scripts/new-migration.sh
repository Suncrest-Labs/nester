#!/usr/bin/env bash
# new-migration.sh — Generate a new migration file pair with the next
# available sequence number.
#
# Usage:  ./scripts/new-migration.sh <descriptive_name>
# Example: ./scripts/new-migration.sh add_orders_table
set -euo pipefail

MIGRATION_DIR="apps/api/migrations"

if [ -z "${1:-}" ]; then
  echo "Usage: $0 <descriptive_name>"
  echo "Example: $0 add_orders_table"
  exit 1
fi

NAME="$1"

# Determine the next sequence number from existing .up.sql files.
LAST_NUM=$(
  ls "$MIGRATION_DIR"/*.up.sql 2>/dev/null \
    | xargs -I{} basename {} \
    | sed 's/^\([0-9]*\).*/\1/' \
    | sort -n \
    | tail -1
)

NEXT_NUM=$(printf "%03d" $(( 10#${LAST_NUM:-0} + 1 )))

UP_FILE="${MIGRATION_DIR}/${NEXT_NUM}_${NAME}.up.sql"
DOWN_FILE="${MIGRATION_DIR}/${NEXT_NUM}_${NAME}.down.sql"

touch "$UP_FILE" "$DOWN_FILE"

echo "Created migration files:"
echo "  $UP_FILE"
echo "  $DOWN_FILE"
