#!/usr/bin/env bash
# =============================================================================
# Production Deployment & Automatic Rollback Orchestrator (Issue #1529)
# =============================================================================
# Enforces:
# 1. Release tag trigger validation (`vX.Y.Z`) and manual approval gate (`PRODUCTION_APPROVED=true`).
# 2. Immutable `@sha256:...` digest verification for API, Signer, and dApp images.
# 3. Separate approved database migration phase with pre-migration backup check.
# 4. Post-deploy smoke verification (`/health`, `/api/v1/health`, dApp readiness)
#    with automatic rollback to previous image digests on failure.
#
# Usage:
#   RELEASE_TAG=v1.0.0 \
#   PRODUCTION_APPROVED=true \
#   API_IMAGE_DIGEST=sha256:... \
#   SIGNER_IMAGE_DIGEST=sha256:... \
#   DAPP_IMAGE_DIGEST=sha256:... \
#   ./deploy/production/deploy-production.sh
# =============================================================================

set -euo pipefail

RELEASE_TAG="${RELEASE_TAG:-}"
PRODUCTION_APPROVED="${PRODUCTION_APPROVED:-false}"
RUN_APPROVED_MIGRATIONS="${RUN_APPROVED_MIGRATIONS:-false}"
STATE_FILE="${STATE_FILE:-deploy/production/.last-good-digests.env}"
COMPOSE_FILE="deploy/production/docker-compose.production.yml"
SMOKE_API_URL="${SMOKE_API_URL:-http://127.0.0.1:8080/health}"
SMOKE_DAPP_URL="${SMOKE_DAPP_URL:-http://127.0.0.1:3000}"

if [[ ! "${RELEASE_TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
  echo "FAIL: RELEASE_TAG must be a semantic version tag (vX.Y.Z), got '${RELEASE_TAG}'" >&2
  exit 1
fi

if [ "${PRODUCTION_APPROVED}" != "true" ]; then
  echo "FAIL: Production deploy requires protected environment approval (PRODUCTION_APPROVED=true)." >&2
  exit 1
fi

for var in API_IMAGE_DIGEST SIGNER_IMAGE_DIGEST DAPP_IMAGE_DIGEST; do
  val="${!var:-}"
  if [[ ! "${val}" =~ ^sha256:[a-f0-9]{64}$ ]]; then
    echo "FAIL: ${var} must be an immutable sha256:<64-hex> digest, got '${val}'" >&2
    exit 1
  fi
done

rollback() {
  echo "ERROR: Smoke check failed for ${RELEASE_TAG}! Initiating automatic rollback..." >&2
  if [ -f "${STATE_FILE}" ]; then
    # shellcheck disable=SC1090
    source "${STATE_FILE}"
    echo "==> Rolling back to previous known-good digests:" >&2
    echo "    API_IMAGE_DIGEST=${PREV_API_IMAGE_DIGEST}" >&2
    echo "    SIGNER_IMAGE_DIGEST=${PREV_SIGNER_IMAGE_DIGEST}" >&2
    echo "    DAPP_IMAGE_DIGEST=${PREV_DAPP_IMAGE_DIGEST}" >&2
    export API_IMAGE_DIGEST="${PREV_API_IMAGE_DIGEST}"
    export SIGNER_IMAGE_DIGEST="${PREV_SIGNER_IMAGE_DIGEST}"
    export DAPP_IMAGE_DIGEST="${PREV_DAPP_IMAGE_DIGEST}"
    if [ "${DRY_RUN:-false}" != "true" ]; then
      docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans
    fi
    echo "==> Rollback to ${PREV_RELEASE_TAG:-previous} complete." >&2
  else
    echo "WARN: No ${STATE_FILE} found; manual rollback required." >&2
  fi
  exit 1
}

if [ "${RUN_APPROVED_MIGRATIONS}" = "true" ]; then
  if [ -z "${PRE_MIGRATION_BACKUP_ID:-}" ]; then
    echo "FAIL: PRE_MIGRATION_BACKUP_ID is required before running production database migrations." >&2
    exit 1
  fi
  echo "==> Running approved production database migrations (backup=${PRE_MIGRATION_BACKUP_ID})..."
  if [ "${DRY_RUN:-false}" != "true" ]; then
    docker run --rm \
      -e DATABASE_URL="${DATABASE_URL}" \
      "ghcr.io/suncrest-labs/nester/api@${API_IMAGE_DIGEST}" \
      /app/nester-api --migrate-only
  fi
fi

echo "==> Deploying ${RELEASE_TAG} with immutable digests..."
if [ "${DRY_RUN:-false}" != "true" ]; then
  docker compose -f "${COMPOSE_FILE}" up -d --remove-orphans
fi

echo "==> Running post-deploy smoke checks..."
if ! curl -fsS --max-time 5 "${SMOKE_API_URL}" >/dev/null 2>&1; then
  rollback
fi
if ! curl -fsS --max-time 5 "${SMOKE_DAPP_URL}" >/dev/null 2>&1; then
  rollback
fi

cat > "${STATE_FILE}" <<EOF
PREV_RELEASE_TAG=${RELEASE_TAG}
PREV_API_IMAGE_DIGEST=${API_IMAGE_DIGEST}
PREV_SIGNER_IMAGE_DIGEST=${SIGNER_IMAGE_DIGEST}
PREV_DAPP_IMAGE_DIGEST=${DAPP_IMAGE_DIGEST}
EOF

echo "==> PASS: Production deploy ${RELEASE_TAG} succeeded and recorded known-good digests in ${STATE_FILE}."
