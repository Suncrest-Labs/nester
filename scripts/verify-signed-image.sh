#!/usr/bin/env bash
# =============================================================================
# Deploy-Time Container Image Signature & Digest Verification (Issue #1532)
# =============================================================================
# Verifies that:
# 1. Every production image reference is pinned by immutable `@sha256:...` digest
#    (rejects mutable `:latest` or branch tags).
# 2. The image digest carries a valid Cosign keyless signature in Rekor issued by
#    GitHub Actions OIDC (`https://token.actions.githubusercontent.com`) for the
#    `Suncrest-Labs/nester` repository.
# =============================================================================

set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "Usage: $0 <image@sha256:digest> [additional-images...]" >&2
  exit 1
fi

EXPECTED_ISSUER="${COSIGN_OIDC_ISSUER:-https://token.actions.githubusercontent.com}"
EXPECTED_IDENTITY_REGEXP="${COSIGN_IDENTITY_REGEXP:-^https://github.com/Suncrest-Labs/nester/\.github/workflows/.+@refs/(heads/(main|dev)|tags/v.+)$}"

if ! command -v cosign >/dev/null 2>&1; then
  echo "ERROR: cosign is required to verify signed release images before deployment." >&2
  exit 1
fi

for img_ref in "$@"; do
  if [[ ! "${img_ref}" =~ @sha256:[a-f0-9]{64}$ ]]; then
    echo "FAIL: Refusing to deploy '${img_ref}' — production deployments must pull by immutable @sha256 digest, never a mutable tag." >&2
    exit 1
  fi

  echo "==> Verifying Cosign keyless OIDC signature for ${img_ref}..."
  cosign verify \
    --certificate-oidc-issuer "${EXPECTED_ISSUER}" \
    --certificate-identity-regexp "${EXPECTED_IDENTITY_REGEXP}" \
    "${img_ref}" >/dev/null

  echo "PASS: Verified Cosign signature and Rekor transparency log entry for ${img_ref}."
done
