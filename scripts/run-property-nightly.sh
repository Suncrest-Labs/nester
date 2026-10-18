#!/usr/bin/env bash
set -euo pipefail

# Nightly property test runner with >=10,000 cases (#1542)
export PROPTEST_CASES="${PROPTEST_CASES:-10000}"

cd "$(dirname "$0")/../packages/contracts"
echo "==> Running property_tests with PROPTEST_CASES=${PROPTEST_CASES}..."
cargo test -p nester-integration-tests property_tests -- --nocapture
