#!/usr/bin/env bash
# Pre-deploy heuristic smart contract audit via x402 micro-payment API.
#
# Required env:
#   X402_PAYMENT_PROOF  — SOL transaction signature proving 0.005 SOL payment
#                         to AKz1pZ8yxtFQLwTpDKJGZjLeBUX4rnobX7HdMF3uvK6W
#
# Optional env:
#   CONTRACT_AUDIT_ADDRESSES — comma-separated contract addresses to scan
#   CONTRACT_AUDIT_API_URL   — override audit endpoint base URL
#
set -euo pipefail

API_URL="${CONTRACT_AUDIT_API_URL:-https://money-machine-x402-ssyopros.zocomputer.io/api/smart-contract-audit}"
ADDRESSES="${CONTRACT_AUDIT_ADDRESSES:-}"

if [[ -z "${X402_PAYMENT_PROOF:-}" ]]; then
  echo "X402_PAYMENT_PROOF not set — skipping contract audit (configure secret in CI to enable)."
  exit 0
fi

if [[ -z "$ADDRESSES" ]]; then
  echo "CONTRACT_AUDIT_ADDRESSES not set — nothing to audit."
  exit 0
fi

IFS=',' read -ra ADDR_LIST <<< "$ADDRESSES"
failed=0

for addr in "${ADDR_LIST[@]}"; do
  addr="$(echo "$addr" | xargs)"
  [[ -z "$addr" ]] && continue

  echo "Auditing contract: $addr"
  report_file="audit-report-${addr//[^a-zA-Z0-9]/_}.json"

  http_code=$(curl -sS -o "$report_file" -w "%{http_code}" \
    -X GET "${API_URL}?address=${addr}" \
    -H "x-payment-proof: ${X402_PAYMENT_PROOF}")

  if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
    echo "Audit failed for $addr (HTTP $http_code)"
    cat "$report_file" || true
    failed=1
    continue
  fi

  echo "Audit report saved to $report_file"
  if command -v jq >/dev/null 2>&1; then
    critical=$(jq '[.findings[]? | select(.severity == "critical")] | length' "$report_file" 2>/dev/null || echo 0)
    if [[ "$critical" -gt 0 ]]; then
      echo "CRITICAL findings detected for $addr: $critical"
      failed=1
    fi
  fi
done

exit $failed
