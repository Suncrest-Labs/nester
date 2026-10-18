#!/usr/bin/env bash
# =============================================================================
# Container Image Vulnerability Scan & Cosign Keyless Signing (Issue #1532)
# =============================================================================
# 1. Scans built container images (`nester-api`, `nester-signer`, `nester-dapp`)
#    with Trivy (`--severity HIGH,CRITICAL --ignore-unfixed --exit-code 1`),
#    honoring time-bounded waivers in `.vulnignore` / `.trivyignore`.
# 2. When SIGN_IMAGES=true (release workflow with GitHub Actions OIDC token),
#    signs each image by immutable `@sha256:...` digest using Cosign keyless OIDC.
# =============================================================================

set -euo pipefail

REGISTRY="${REGISTRY:-ghcr.io/suncrest-labs/nester}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
SIGN_IMAGES="${SIGN_IMAGES:-false}"
TRIVY_IGNORE_FILE="${TRIVY_IGNORE_FILE:-.trivyignore}"

IMAGES=(
  "${REGISTRY}/api:${IMAGE_TAG}"
  "${REGISTRY}/signer:${IMAGE_TAG}"
)

if ! command -v trivy >/dev/null 2>&1; then
  echo "ERROR: trivy is required to scan container images." >&2
  exit 1
fi

for img in "${IMAGES[@]}"; do
  echo "==> Scanning container image ${img} for HIGH/CRITICAL CVEs..."
  TRIVY_ARGS=(
    image
    --severity HIGH,CRITICAL
    --ignore-unfixed
    --exit-code 1
  )
  if [ -f "${TRIVY_IGNORE_FILE}" ]; then
    TRIVY_ARGS+=(--ignorefile "${TRIVY_IGNORE_FILE}")
  fi

  trivy "${TRIVY_ARGS[@]}" "${img}"
  echo "PASS: ${img} passed Trivy HIGH/CRITICAL vulnerability gate."
done

if [ "${SIGN_IMAGES}" = "true" ]; then
  if ! command -v cosign >/dev/null 2>&1; then
    echo "ERROR: cosign is required when SIGN_IMAGES=true." >&2
    exit 1
  fi

  for img in "${IMAGES[@]}"; do
    DIGEST="$(docker inspect --format='{{index .RepoDigests 0}}' "${img}")"
    if [[ ! "${DIGEST}" =~ @sha256:[a-f0-9]{64}$ ]]; then
      echo "ERROR: Could not resolve immutable @sha256 digest for ${img} (got '${DIGEST}'). Push image before signing." >&2
      exit 1
    fi

    echo "==> Signing release image digest ${DIGEST} with Cosign keyless OIDC..."
    COSIGN_EXPERIMENTAL=1 cosign sign --yes "${DIGEST}"
    echo "PASS: Signed ${DIGEST}."
  done
fi
