#!/usr/bin/env bash
# =============================================================================
# DNS & TLS Hardening Verification Script (Issue #1534)
# =============================================================================
# Verifies DNSSEC (`ad` flag), CAA records, and HSTS preload header across
# Nester production domains (`nester.finance`, `app.nester.finance`, `api.nester.finance`).
# =============================================================================

set -euo pipefail

DOMAIN="${DOMAIN:-nester.finance}"
RESOLVER="${RESOLVER:-1.1.1.1}"
SUBDOMAINS=("${DOMAIN}" "app.${DOMAIN}" "api.${DOMAIN}")

echo "==> 1. Verifying DNSSEC Authenticated Data (ad) flag for ${DOMAIN}..."
DNSSEC_OUT="$(dig @"${RESOLVER}" +dnssec "${DOMAIN}" A)"
echo "${DNSSEC_OUT}"

if ! echo "${DNSSEC_OUT}" | grep -E "flags:.* ad[ ;]" >/dev/null; then
  echo "FAIL: DNSSEC 'ad' flag not present for ${DOMAIN}" >&2
  exit 1
fi
echo "PASS: DNSSEC 'ad' flag verified."

echo "==> 2. Verifying CAA records for ${DOMAIN}..."
CAA_OUT="$(dig @"${RESOLVER}" +short "${DOMAIN}" CAA)"
echo "${CAA_OUT}"

if [ -z "${CAA_OUT}" ] || ! echo "${CAA_OUT}" | grep -E 'issue(wild)? "' >/dev/null; then
  echo "FAIL: Missing CAA issue restriction records on ${DOMAIN}" >&2
  exit 1
fi
echo "PASS: CAA records verified."

echo "==> 3. Verifying HSTS preload header on production subdomains..."
EXPECTED_HSTS="max-age=63072000; includeSubDomains; preload"
for host in "${SUBDOMAINS[@]}"; do
  HSTS_HEADER="$(curl -fsSI "https://${host}" | tr -d '\r' | grep -i '^strict-transport-security:' || true)"
  echo "${host} -> ${HSTS_HEADER}"
  if ! echo "${HSTS_HEADER}" | grep -qi "max-age=63072000" || \
     ! echo "${HSTS_HEADER}" | grep -qi "includesubdomains" || \
     ! echo "${HSTS_HEADER}" | grep -qi "preload"; then
    echo "FAIL: ${host} missing required HSTS header (${EXPECTED_HSTS})" >&2
    exit 1
  fi
done

echo "==> ALL DNS & TLS HARDENING CHECKS PASSED FOR ${DOMAIN}."
