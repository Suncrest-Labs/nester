#!/usr/bin/env bash
# check_migration_numbers.sh — Detect duplicate migration sequence‐number
# prefixes in apps/api/migrations.  Intended to be called from CI.
set -euo pipefail

MIGRATION_DIR="${1:-apps/api/migrations}"

if [ ! -d "$MIGRATION_DIR" ]; then
  echo "ERROR: Migration directory '$MIGRATION_DIR' not found."
  exit 1
fi

# Extract the numeric prefix from each *.up.sql file and look for duplicates.
duplicates=$(
  ls "$MIGRATION_DIR"/*.up.sql 2>/dev/null \
    | xargs -I{} basename {} \
    | sed 's/^\([0-9]*\).*/\1/' \
    | sort \
    | uniq -d
)

if [ -n "$duplicates" ]; then
  echo "ERROR: Duplicate migration sequence numbers found: $duplicates"
  echo ""
  echo "Conflicting files:"
  for num in $duplicates; do
    ls "$MIGRATION_DIR/${num}_"* 2>/dev/null | sed 's/^/  /'
  done
  exit 1
fi

echo "Migration sequence numbers are unique ✓"
