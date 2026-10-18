#!/usr/bin/env bash
# ==============================================================================
# check-contract-freeze.sh (#1554)
#
# Enforces the Stage 1 launch scope freeze and change-control policy defined in
# docs/LAUNCH_GO_NO_GO.md.
#
# Once the tag `audit-candidate-1` exists (or when FREEZE_BASE_REF is provided),
# any change under `packages/contracts/` must carry an explicit change-control
# marker (`[audit-fix]`, `AUDIT-###`, `[p0-fix]`, `[p1-fix]`, or `INC-###`) in
# the commit log or PR body (`PR_BODY` / `CHANGE_CONTROL_OVERRIDE`).
# ==============================================================================

set -euo pipefail

FREEZE_TAG="${FREEZE_BASE_REF:-audit-candidate-1}"
HEAD_REF="${FREEZE_HEAD_REF:-HEAD}"

if ! git rev-parse --verify --quiet "${FREEZE_TAG}^{commit}" >/dev/null; then
  echo "[contract-freeze] Tag '${FREEZE_TAG}' is not yet cut. Scope freeze check is in pre-tag standby mode (PASS)."
  exit 0
fi

CHANGED_CONTRACT_FILES="$(git diff --name-only "${FREEZE_TAG}...${HEAD_REF}" -- packages/contracts/ || true)"

if [ -z "${CHANGED_CONTRACT_FILES}" ]; then
  echo "[contract-freeze] No files under packages/contracts/ modified since '${FREEZE_TAG}' (PASS)."
  exit 0
fi

echo "[contract-freeze] Detected modifications under packages/contracts/ after '${FREEZE_TAG}':"
echo "${CHANGED_CONTRACT_FILES}" | sed 's/^/  - /'

COMMIT_MESSAGES="$(git log --format=%B "${FREEZE_TAG}..${HEAD_REF}" -- packages/contracts/ || true)"
COMBINED_TEXT="${COMMIT_MESSAGES} ${PR_BODY:-} ${CHANGE_CONTROL_OVERRIDE:-}"

if echo "${COMBINED_TEXT}" | grep -Eqi '(\[audit-fix\]|AUDIT-[0-9]+|\[p0-fix\]|\[p1-fix\]|INC-[0-9]+)'; then
  echo "[contract-freeze] Valid change-control marker found for post-${FREEZE_TAG} contract change (PASS)."
  exit 0
fi

echo "[contract-freeze] ERROR: Files under packages/contracts/ were modified after '${FREEZE_TAG}' without a required change-control marker ([audit-fix], AUDIT-###, [p0-fix], [p1-fix], or INC-###)." >&2
echo "[contract-freeze] See docs/LAUNCH_GO_NO_GO.md ('Stage 1 Launch Scope Freeze & Change-Control Rule')." >&2
exit 1
