#!/usr/bin/env bash
# =============================================================================
# Weekly Lookalike & Phishing Domain Monitor (Issue #1535)
# =============================================================================
# Scans permutations of nester.finance using `dnstwist`, filters out Nester's
# known owned/defensive domains, and alerts the security team via webhook if any
# newly registered lookalike domain resolves in DNS.
#
# Usage:
#   ./scripts/monitor-phishing-domains.sh
#   PRIMARY_DOMAIN=nester.finance ALERT_WEBHOOK_URL=https://... ./scripts/monitor-phishing-domains.sh
# =============================================================================

set -euo pipefail

PRIMARY_DOMAIN="${PRIMARY_DOMAIN:-nester.finance}"
ALLOWLIST_REGEX='^(nester\.finance|nester\.xyz|nester\.app|nester\.io|nester\.dev|nesterfi\.com|nester-finance\.com)$'
OUTPUT_JSON="${OUTPUT_JSON:-/tmp/nester-dnstwist-registered.json}"

echo "==> Running dnstwist lookalike scan for ${PRIMARY_DOMAIN}..."

if ! command -v dnstwist >/dev/null 2>&1; then
  echo "ERROR: dnstwist is not installed. Install via 'pip install dnstwist[full]' or run via 'docker run elceef/dnstwist'." >&2
  exit 1
fi

dnstwist --registered --format json "${PRIMARY_DOMAIN}" > "${OUTPUT_JSON}"

SUSPICIOUS_DOMAINS="$(jq -r --arg allow "${ALLOWLIST_REGEX}" '
  map(select((.domain | test($allow)) | not))
  | map("\(.domain) (fuzzer=\(.fuzzer), a=\((.dns_a // []) | join(",")), ns=\((.dns_ns // []) | join(",")))")
  | .[]
' "${OUTPUT_JSON}")"

if [ -n "${SUSPICIOUS_DOMAINS}" ]; then
  echo "ALERT: Newly resolving lookalike domain(s) detected for ${PRIMARY_DOMAIN}:" >&2
  echo "${SUSPICIOUS_DOMAINS}" >&2

  if [ -n "${ALERT_WEBHOOK_URL:-}" ]; then
    jq -n \
      --arg domain "${PRIMARY_DOMAIN}" \
      --arg findings "${SUSPICIOUS_DOMAINS}" \
      '{
        severity: "critical",
        event: "phishing_lookalike_domain_detected",
        primary_domain: $domain,
        suspicious_domains: ($findings | split("\n")),
        runbook: "docs/security/phishing-takedown-runbook.md"
      }' | curl -fsSL -X POST -H "Content-Type: application/json" -d @- "${ALERT_WEBHOOK_URL}" >/dev/null
  fi

  exit 2
fi

echo "==> PASS: No unauthorized resolving lookalike domains detected for ${PRIMARY_DOMAIN}."
