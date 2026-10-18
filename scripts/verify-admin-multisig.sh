#!/usr/bin/env bash
set -euo pipefail

# Automated pre-deploy / expected-state check verifying the on-chain multisig
# thresholds and signer weights of the mainnet admin account against
# docs/security/multisig-signers.md (#1538).

HORIZON_URL="${STELLAR_HORIZON_URL:-https://horizon.stellar.org}"
ADMIN_ACCOUNT="${MAINNET_ADMIN_ADDRESS:-GADMIN7NESTERMAINNETMULTISIG3OF5ACCOUNTADDRESS7777777777}"
EXPECTED_M="${EXPECTED_MULTISIG_M:-3}"
EXPECTED_N="${EXPECTED_MULTISIG_N:-5}"

echo "==> Querying Horizon /accounts/${ADMIN_ACCOUNT}..."
ACCOUNT_JSON=$(curl -sf "${HORIZON_URL}/accounts/${ADMIN_ACCOUNT}")

MASTER_WEIGHT=$(echo "$ACCOUNT_JSON" | jq -r --arg admin "$ADMIN_ACCOUNT" '.signers[] | select(.key == $admin) | .weight')
LOW_T=$(echo "$ACCOUNT_JSON" | jq -r '.thresholds.low_threshold')
MED_T=$(echo "$ACCOUNT_JSON" | jq -r '.thresholds.med_threshold')
HIGH_T=$(echo "$ACCOUNT_JSON" | jq -r '.thresholds.high_threshold')
ACTIVE_SIGNERS=$(echo "$ACCOUNT_JSON" | jq -r --arg admin "$ADMIN_ACCOUNT" '[.signers[] | select(.key != $admin and .weight > 0)] | length')
MAX_SIGNER_WEIGHT=$(echo "$ACCOUNT_JSON" | jq -r --arg admin "$ADMIN_ACCOUNT" '[.signers[] | select(.key != $admin and .weight > 0) | .weight] | max // 0')

echo "   master_weight=${MASTER_WEIGHT} low=${LOW_T} med=${MED_T} high=${HIGH_T} active_signers=${ACTIVE_SIGNERS} max_signer_weight=${MAX_SIGNER_WEIGHT}"

if [ "$MASTER_WEIGHT" != "0" ]; then
  echo "FAIL: master key weight is ${MASTER_WEIGHT} (expected 0)"
  exit 1
fi

if [ "$LOW_T" != "$EXPECTED_M" ] || [ "$MED_T" != "$EXPECTED_M" ] || [ "$HIGH_T" != "$EXPECTED_M" ]; then
  echo "FAIL: thresholds (${LOW_T}/${MED_T}/${HIGH_T}) do not match expected ${EXPECTED_M}/${EXPECTED_M}/${EXPECTED_M}"
  exit 1
fi

if [ "$ACTIVE_SIGNERS" != "$EXPECTED_N" ]; then
  echo "FAIL: active signer count is ${ACTIVE_SIGNERS} (expected ${EXPECTED_N})"
  exit 1
fi

if [ "$MAX_SIGNER_WEIGHT" != "1" ]; then
  echo "FAIL: max non-master signer weight is ${MAX_SIGNER_WEIGHT} (expected 1)"
  exit 1
fi

echo "PASS: Mainnet admin multisig matches ${EXPECTED_M}-of-${EXPECTED_N} policy with master_weight=0."
