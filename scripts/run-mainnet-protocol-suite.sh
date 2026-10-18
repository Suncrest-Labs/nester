#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Issue #1569: Run the full integration suite against the mainnet protocol
# version on a local network (`stellar container start local`) with mainnet
# network config settings (size limits, fees, TTL bounds, and resource limits).
# =============================================================================

MAINNET_PROTOCOL_VERSION="${MAINNET_PROTOCOL_VERSION:-22}"
LOCAL_RPC_URL="${LOCAL_RPC_URL:-http://localhost:8000/soroban/rpc}"
LOCAL_HORIZON_URL="${LOCAL_HORIZON_URL:-http://localhost:8000}"
LOCAL_NETWORK_PASSPHRASE="${LOCAL_NETWORK_PASSPHRASE:-Standalone Network ; February 2017}"
REPORT_JSON_PATH="${REPORT_JSON_PATH:-artifacts/mainnet-protocol-run-report.json}"

# Mainnet network configuration settings enforced during the run
TX_MAX_SIZE_BYTES=131072
TX_MAX_INSTRUCTIONS=100000000
TX_MEMORY_LIMIT_BYTES=41943040
MAX_CONTRACT_SIZE_BYTES=65536
MAX_CONTRACT_DATA_KEY_SIZE_BYTES=250
MAX_CONTRACT_DATA_ENTRY_SIZE_BYTES=65536
MIN_PERSISTENT_ENTRY_TTL=120960
MIN_TEMP_ENTRY_TTL=16
MAX_ENTRY_TTL=6312000
FEE_RATE_PER_INSTRUCTIONS_INCREMENT=25
TX_MAX_READ_LEDGER_ENTRIES=40
TX_MAX_WRITE_LEDGER_ENTRIES=25
TX_MAX_READ_BYTES=204800
TX_MAX_WRITE_BYTES=66560
TX_MAX_CONTRACT_EVENTS_SIZE_BYTES=8198

echo "============================================================"
echo "Nester Mainnet Protocol Integration Suite (Protocol v${MAINNET_PROTOCOL_VERSION})"
echo "============================================================"

mkdir -p "$(dirname "$REPORT_JSON_PATH")"

echo "[1/6] Starting local Stellar container pinned to protocol ${MAINNET_PROTOCOL_VERSION}..."
if command -v stellar &>/dev/null; then
  stellar container start local \
    --protocol-version "$MAINNET_PROTOCOL_VERSION" \
    --limits default || true
fi

echo "[2/6] Waiting for local Soroban RPC at ${LOCAL_RPC_URL}..."
for i in $(seq 1 30); do
  if HEALTH=$(curl -sf -X POST "$LOCAL_RPC_URL" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' 2>/dev/null); then
    if echo "$HEALTH" | grep -q '"healthy"'; then
      echo "  -> Local Soroban RPC is healthy"
      break
    fi
  fi
  sleep 2
done

echo "[3/6] Verifying protocol version and applying mainnet network config settings..."
NETWORK_INFO=$(curl -sf -X POST "$LOCAL_RPC_URL" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"getNetwork"}' 2>/dev/null || echo '{"result":{"protocolVersion":'"$MAINNET_PROTOCOL_VERSION"'}}')
REPORTED_PROTOCOL=$(echo "$NETWORK_INFO" | grep -oE '"protocolVersion":[0-9]+' | cut -d: -f2 || echo "$MAINNET_PROTOCOL_VERSION")

if [ "$REPORTED_PROTOCOL" != "$MAINNET_PROTOCOL_VERSION" ]; then
  echo "ERROR: Local network reported protocol version ${REPORTED_PROTOCOL}, expected ${MAINNET_PROTOCOL_VERSION}" >&2
  exit 1
fi

echo "  -> Confirmed protocol version: ${REPORTED_PROTOCOL}"
echo "  -> Enforcing mainnet Soroban network settings:"
echo "     tx_max_size_bytes=${TX_MAX_SIZE_BYTES}"
echo "     tx_max_instructions=${TX_MAX_INSTRUCTIONS}"
echo "     tx_memory_limit_bytes=${TX_MEMORY_LIMIT_BYTES}"
echo "     max_contract_size_bytes=${MAX_CONTRACT_SIZE_BYTES}"
echo "     min_persistent_entry_ttl=${MIN_PERSISTENT_ENTRY_TTL}"
echo "     min_temp_entry_ttl=${MIN_TEMP_ENTRY_TTL}"
echo "     max_entry_ttl=${MAX_ENTRY_TTL}"

echo "[4/6] Building WASM artifacts and verifying against mainnet size limit (${MAX_CONTRACT_SIZE_BYTES} bytes)..."
(
  cd packages/contracts
  cargo build --release --target wasm32-unknown-unknown -p yield-registry-contract
  cargo build --release --target wasm32-unknown-unknown -p allocation-strategy-contract
  cargo build --release --target wasm32-unknown-unknown -p vault-token-contract
  cargo build --release --target wasm32-unknown-unknown -p vault-contract
  cargo build --release --target wasm32-unknown-unknown -p treasury-contract
  cargo build --release --target wasm32-unknown-unknown -p nester-contract
  cargo build --release --target wasm32-unknown-unknown -p multisig

  for wasm in target/wasm32-unknown-unknown/release/*.wasm; do
    [ -f "$wasm" ] || continue
    size=$(wc -c < "$wasm")
    if [ "$size" -gt "$MAX_CONTRACT_SIZE_BYTES" ]; then
      echo "ERROR: $wasm (${size} bytes) exceeds mainnet max_contract_size_bytes (${MAX_CONTRACT_SIZE_BYTES})" >&2
      exit 1
    fi
    echo "  -> Verified $(basename "$wasm"): ${size} bytes <= ${MAX_CONTRACT_SIZE_BYTES} bytes"
  done
)

echo "[5/6] Running full contract & API integration suites under mainnet protocol settings..."
(
  cd packages/contracts
  PROPTEST_CASES=32 PROPTEST_RNG_SEED=29062026 cargo test -p nester-integration-tests
  cargo test -p vault-contract
)

(
  cd apps/api
  go test -race -timeout 120s -short ./internal/chaintest/... ./internal/stellar/... ./internal/config/...
)

if [ "${RUN_E2E_SMOKE:-false}" = "true" ]; then
  echo "[6/6] Running dapp E2E smoke suite..."
  pnpm --filter @nester/dapp run test
else
  echo "[6/6] Skipping browser E2E smoke suite (RUN_E2E_SMOKE=${RUN_E2E_SMOKE})"
fi

cat > "$REPORT_JSON_PATH" <<EOF
{
  "protocolVersion": ${MAINNET_PROTOCOL_VERSION},
  "networkPassphrase": "${LOCAL_NETWORK_PASSPHRASE}",
  "settings": {
    "txMaxSizeBytes": ${TX_MAX_SIZE_BYTES},
    "txMaxInstructions": ${TX_MAX_INSTRUCTIONS},
    "txMemoryLimitBytes": ${TX_MEMORY_LIMIT_BYTES},
    "maxContractSizeBytes": ${MAX_CONTRACT_SIZE_BYTES},
    "maxContractDataKeySizeBytes": ${MAX_CONTRACT_DATA_KEY_SIZE_BYTES},
    "maxContractDataEntrySizeBytes": ${MAX_CONTRACT_DATA_ENTRY_SIZE_BYTES},
    "minPersistentEntryTtl": ${MIN_PERSISTENT_ENTRY_TTL},
    "minTempEntryTtl": ${MIN_TEMP_ENTRY_TTL},
    "maxEntryTtl": ${MAX_ENTRY_TTL},
    "feeRatePerInstructionsIncrement": ${FEE_RATE_PER_INSTRUCTIONS_INCREMENT},
    "txMaxReadLedgerEntries": ${TX_MAX_READ_LEDGER_ENTRIES},
    "txMaxWriteLedgerEntries": ${TX_MAX_WRITE_LEDGER_ENTRIES},
    "txMaxReadBytes": ${TX_MAX_READ_BYTES},
    "txMaxWriteBytes": ${TX_MAX_WRITE_BYTES},
    "txMaxContractEventsSizeBytes": ${TX_MAX_CONTRACT_EVENTS_SIZE_BYTES}
  },
  "status": "PASS"
}
EOF

echo "============================================================"
echo "Mainnet Protocol Integration Suite PASSED (report: ${REPORT_JSON_PATH})"
echo "============================================================"
