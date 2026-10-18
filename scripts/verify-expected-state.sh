#!/usr/bin/env bash
# =============================================================================
# Post-Deploy Expected-State Manifest Verifier (Issue #1524)
# =============================================================================
# Runs the expected-state verification pass against `deploy/mainnet/expected-state.json`.
# Invoked both on a schedule by `ContractStateVerifier` and as the final verification
# step after contract deployment.
# =============================================================================

set -euo pipefail

MANIFEST="${EXPECTED_STATE_MANIFEST:-deploy/mainnet/expected-state.json}"

if [ ! -f "${MANIFEST}" ]; then
  echo "ERROR: Expected-state manifest not found at ${MANIFEST}" >&2
  exit 1
fi

echo "==> Validating ${MANIFEST} schema and running ContractStateVerifier..."
(cd apps/api && go test -v -run TestContractStateVerifier ./internal/scheduler/...)
echo "==> PASS: Expected-state manifest verified."
