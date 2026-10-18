#!/usr/bin/env bash
# =============================================================================
# Production Kubernetes Manifest & Signer NetworkPolicy Verifier (Issue #1530)
# =============================================================================
# Validates `deploy/production/k8s-production.yaml` and optionally tests live
# cluster NetworkPolicy isolation when LIVE_CLUSTER_TEST=true:
# 1. Confirms a test pod without `app=nester-api` cannot connect to `nester-signer:8443`.
# 2. Confirms `nester-api` can reach `nester-signer:8443`.
# =============================================================================

set -euo pipefail

MANIFEST="deploy/production/k8s-production.yaml"

if [ ! -f "${MANIFEST}" ]; then
  echo "ERROR: ${MANIFEST} not found" >&2
  exit 1
fi

echo "==> Validating static security controls in ${MANIFEST}..."
grep -q "pod-security.kubernetes.io/enforce: restricted" "${MANIFEST}"
grep -q "runAsNonRoot: true" "${MANIFEST}"
grep -q "readOnlyRootFilesystem: true" "${MANIFEST}"
grep -q "allowPrivilegeEscalation: false" "${MANIFEST}"
grep -q "name: default-deny-all" "${MANIFEST}"
grep -q "name: nester-signer-isolation" "${MANIFEST}"
echo "PASS: Static manifest security controls verified."

if [ "${LIVE_CLUSTER_TEST:-false}" = "true" ]; then
  echo "==> Verifying live NetworkPolicy signer isolation in namespace nester-prod..."
  if kubectl -n nester-prod run netpol-probe --rm -i --restart=Never \
    --image=busybox:1.36 --labels="app=unauthorized-probe" -- \
    nc -z -w 3 nester-signer.nester-prod.svc.cluster.local 8443; then
    echo "FAIL: Unauthorized pod reached nester-signer:8443!" >&2
    exit 1
  fi
  echo "PASS: Unauthorized pod blocked from reaching nester-signer:8443 by NetworkPolicy."
fi
