#!/usr/bin/env bash
# Wait for all Nester services to become healthy before running E2E tests.
# Used in CI when starting services via Docker Compose.
#
# Usage: ./scripts/wait-for-services.sh [--timeout 120]

set -euo pipefail

TIMEOUT=120
INTERVAL=3

while [[ $# -gt 0 ]]; do
  case $1 in
    --timeout) TIMEOUT="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

wait_for_url() {
  local name="$1"
  local url="$2"
  local elapsed=0

  echo "Waiting for $name at $url ..."
  until curl -sf "$url" > /dev/null 2>&1; do
    if [[ $elapsed -ge $TIMEOUT ]]; then
      echo "ERROR: $name did not become ready within ${TIMEOUT}s"
      exit 1
    fi
    sleep "$INTERVAL"
    elapsed=$((elapsed + INTERVAL))
  done
  echo "  $name is ready (${elapsed}s)"
}

# Dapp frontend (Next.js)
FRONTEND_URL="${E2E_BASE_URL:-http://localhost:3001}"
wait_for_url "Dapp Frontend" "$FRONTEND_URL"

# Dapp backend (Express)
BACKEND_URL="${DAPP_BACKEND_URL:-http://localhost:8080}/health-check"
wait_for_url "Dapp Backend" "$BACKEND_URL"

echo ""
echo "All services are ready. Proceeding with E2E tests."
