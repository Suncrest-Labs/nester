#!/usr/bin/env bash
# =============================================================================
# Test suite for Mainnet USDC SAC derivation and assertion in deploy flow (#1494)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_SCRIPT="$SCRIPT_DIR/mainnet-deploy-dryrun.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

EXPECTED_ISSUER="GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
EXPECTED_SAC_ID="CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75"
WRONG_SAC_ID="CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"

MOCK_STELLAR="$TMP_DIR/mock-stellar.sh"
cat > "$MOCK_STELLAR" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [ "\${1:-}" = "contract" ] && [ "\${2:-}" = "id" ] && [ "\${3:-}" = "asset" ]; then
  if [[ "\$*" == *"--asset USDC:${EXPECTED_ISSUER}"* ]] && [[ "\$*" == *"--network mainnet"* ]]; then
    echo "${EXPECTED_SAC_ID}"
    exit 0
  fi
  echo "unexpected stellar contract id asset args: \$*" >&2
  exit 1
fi

if [ "\${1:-}" = "contract" ] && [ "\${2:-}" = "invoke" ]; then
  if [[ "\$*" == *"get_token"* ]]; then
    echo "\"\${MOCK_VAULT_TOKEN_RETURN:-${EXPECTED_SAC_ID}}\""
    exit 0
  fi
fi

echo "unexpected mock stellar invocation: \$*" >&2
exit 1
EOF
chmod +x "$MOCK_STELLAR"

echo "[test 1] Unset USDC_CONTRACT derives and accepts Circle Mainnet USDC SAC..."
STELLAR_BIN="$MOCK_STELLAR" bash "$DEPLOY_SCRIPT" --verify-usdc-sac >/dev/null

echo "[test 2] Matching USDC_CONTRACT succeeds..."
STELLAR_BIN="$MOCK_STELLAR" USDC_CONTRACT="$EXPECTED_SAC_ID" bash "$DEPLOY_SCRIPT" --verify-usdc-sac >/dev/null

echo "[test 3] Mismatched USDC_CONTRACT fails before deployment..."
if OUTPUT=$(STELLAR_BIN="$MOCK_STELLAR" USDC_CONTRACT="$WRONG_SAC_ID" bash "$DEPLOY_SCRIPT" --verify-usdc-sac 2>&1); then
  echo "FAIL: expected deploy script to fail on mismatched USDC_CONTRACT" >&2
  exit 1
fi
if ! echo "$OUTPUT" | grep -q "does not match derived Mainnet USDC SAC ID"; then
  echo "FAIL: unexpected error message on mismatch: $OUTPUT" >&2
  exit 1
fi

echo "[test 4] Post-deploy vault get_token matching derived SAC succeeds..."
STELLAR_BIN="$MOCK_STELLAR" VERIFY_VAULT_ID="CVAULT123" MOCK_VAULT_TOKEN_RETURN="$EXPECTED_SAC_ID" \
  bash "$DEPLOY_SCRIPT" --verify-usdc-sac >/dev/null

echo "[test 5] Post-deploy vault get_token mismatch fails..."
if OUTPUT=$(STELLAR_BIN="$MOCK_STELLAR" VERIFY_VAULT_ID="CVAULT123" MOCK_VAULT_TOKEN_RETURN="$WRONG_SAC_ID" \
  bash "$DEPLOY_SCRIPT" --verify-usdc-sac 2>&1); then
  echo "FAIL: expected deploy script to fail when vault get_token differs from derived SAC" >&2
  exit 1
fi
if ! echo "$OUTPUT" | grep -q "deployed vault token address does not match derived Mainnet USDC SAC ID"; then
  echo "FAIL: unexpected error message on post-deploy token mismatch: $OUTPUT" >&2
  exit 1
fi

echo "ALL USDC SAC DEPLOY TESTS PASSED"
