#!/usr/bin/env bash
set -euo pipefail

# Nightly runner for vault-contract cargo-fuzz targets (#1544)
# Runs conversion_math, fee_tiers, lock_settlement, and queue_processing for
# FUZZ_SECONDS_PER_TARGET seconds each and copies any crash artifacts into
# FUZZ_ARTIFACTS_DIR for CI artifact upload.

FUZZ_SECONDS_PER_TARGET="${FUZZ_SECONDS_PER_TARGET:-180}"
FUZZ_ARTIFACTS_DIR="${FUZZ_ARTIFACTS_DIR:-$(pwd)/fuzz-artifacts}"
TARGETS=(
  "conversion_math"
  "fee_tiers"
  "lock_settlement"
  "queue_processing"
)

mkdir -p "$FUZZ_ARTIFACTS_DIR"
cd "$(dirname "$0")/../packages/contracts/contracts/vault"

for target in "${TARGETS[@]}"; do
  echo "==> Running cargo fuzz target: $target (${FUZZ_SECONDS_PER_TARGET}s)"
  if ! cargo +nightly fuzz run "$target" -- -max_total_time="$FUZZ_SECONDS_PER_TARGET"; then
    echo "ERROR: Fuzz target $target crashed! Copying artifacts to $FUZZ_ARTIFACTS_DIR"
    if [ -d "fuzz/artifacts/$target" ]; then
      cp -r "fuzz/artifacts/$target" "$FUZZ_ARTIFACTS_DIR/"
    fi
    exit 1
  fi
done

echo "==> All ${#TARGETS[@]} fuzz targets completed without crashes."
