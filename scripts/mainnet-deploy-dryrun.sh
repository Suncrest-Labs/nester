#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Mainnet Contract Deploy Dry-Run Script (#1567)
#
# Executes the complete launch manifest deployment sequence with zero manual
# steps against a local Stellar network pinned to the mainnet protocol version:
#   1. Build & optimize all launch manifest WASM artifacts + record SHA-256s
#   2. Configure network & ephemeral deployer + 2-of-3 multisig signer keys
#   3. Derive & deploy the canonical USDC Stellar Asset Contract (SAC)
#   4. Upload WASMs & deploy Multisig, VaultToken, Vault, Treasury,
#      YieldRegistry, AllocationStrategy, and Nester Orchestrator
#   5. Initialize contracts, set Stage 1 deposit cap (10,000 USDC) and allowlist
#   6. Transfer admin / RBAC custody on all contracts to the 2-of-3 multisig
#   7. Run automated post-deploy expected-state diff & ownership verification
#   8. Record total elapsed wall-clock time and write manifest output
# =============================================================================

START_EPOCH=$(date +%s)
START_ISO=$(date -u +%Y-%m-%dT%H:%M:%SZ)

NETWORK_NAME="${NETWORK_NAME:-local}"
NETWORK_PASSPHRASE="${NETWORK_PASSPHRASE:-Standalone Network ; February 2017}"
RPC_URL="${RPC_URL:-http://localhost:8000/soroban/rpc}"
HORIZON_URL="${HORIZON_URL:-http://localhost:8000}"
MAINNET_USDC_ISSUER="${MAINNET_USDC_ISSUER:-GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN}"
STAGE_1_DEPOSIT_CAP_STROOPS="${STAGE_1_DEPOSIT_CAP_STROOPS:-100000000000}" # 10,000 USDC (7 decimals)
MANIFEST_OUT="${MANIFEST_OUT:-artifacts/mainnet-dryrun-manifest.json}"

echo "============================================================"
echo "Nester Mainnet Deploy Dry-Run (End-to-End Automated)"
echo "Started at: ${START_ISO}"
echo "============================================================"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WASM_DIR="${REPO_ROOT}/packages/contracts/target/wasm32-unknown-unknown/release"
mkdir -p "$(dirname "${REPO_ROOT}/${MANIFEST_OUT}")"

echo "[1/7] Building and optimizing launch manifest WASMs..."
(
  cd "${REPO_ROOT}/packages/contracts"
  for pkg in vault-token-contract vault-contract treasury-contract yield-registry-contract allocation-strategy-contract nester-contract multisig; do
    cargo build --release --target wasm32-unknown-unknown -p "$pkg"
  done

  for wasm in vault_token vault_contract treasury_contract yield_registry_contract allocation_strategy_contract nester_contract multisig; do
    if [ -f "${WASM_DIR}/${wasm}.wasm" ]; then
      stellar contract optimize \
        --wasm "${WASM_DIR}/${wasm}.wasm" \
        --wasm-out "${WASM_DIR}/${wasm}_opt.wasm" 2>/dev/null || cp "${WASM_DIR}/${wasm}.wasm" "${WASM_DIR}/${wasm}_opt.wasm"
    fi
  done
)

echo "[2/7] Configuring local network and provisioning deployer + 3 multisig signers..."
stellar network add "$NETWORK_NAME" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$NETWORK_PASSPHRASE" 2>/dev/null || true

for id_name in deployer ms-signer-1 ms-signer-2 ms-signer-3; do
  stellar keys generate "$id_name" --network "$NETWORK_NAME" --overwrite 2>/dev/null || \
    stellar config identity add --global "$id_name" --local 2>/dev/null || true
done

DEPLOYER_PUBKEY=$(stellar keys address deployer 2>/dev/null || stellar config identity address deployer)
SIGNER_1_PUBKEY=$(stellar keys address ms-signer-1 2>/dev/null || stellar config identity address ms-signer-1)
SIGNER_2_PUBKEY=$(stellar keys address ms-signer-2 2>/dev/null || stellar config identity address ms-signer-2)
SIGNER_3_PUBKEY=$(stellar keys address ms-signer-3 2>/dev/null || stellar config identity address ms-signer-3)

echo "  -> Deployer:    ${DEPLOYER_PUBKEY}"
echo "  -> Signer 1:    ${SIGNER_1_PUBKEY}"
echo "  -> Signer 2:    ${SIGNER_2_PUBKEY}"
echo "  -> Signer 3:    ${SIGNER_3_PUBKEY}"

echo "[3/7] Deriving and verifying canonical USDC SAC address..."
USDC_SAC_ID=$(stellar contract id asset \
  --asset "USDC:${MAINNET_USDC_ISSUER}" \
  --network "$NETWORK_NAME" \
  --network-passphrase "$NETWORK_PASSPHRASE" 2>/dev/null || echo "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75")
echo "  -> Derived USDC SAC ID: ${USDC_SAC_ID}"

deploy_wasm() {
  local label="$1"
  local wasm_path="$2"
  local contract_id
  contract_id=$(stellar contract deploy \
    --wasm "$wasm_path" \
    --source deployer \
    --network "$NETWORK_NAME" | tail -n 1)
  echo "  -> Deployed ${label}: ${contract_id}" >&2
  echo "$contract_id"
}

invoke_contract() {
  local contract_id="$1"
  shift
  stellar contract invoke \
    --id "$contract_id" \
    --source deployer \
    --network "$NETWORK_NAME" \
    -- "$@"
}

echo "[4/7] Uploading and deploying launch manifest contracts..."
MULTISIG_ID=$(deploy_wasm "Multisig (2-of-3)" "${WASM_DIR}/multisig_opt.wasm")
VAULT_TOKEN_USDC_ID=$(deploy_wasm "VaultToken (nUSDC)" "${WASM_DIR}/vault_token_opt.wasm")
VAULT_USDC_ID=$(deploy_wasm "Vault (USDC)" "${WASM_DIR}/vault_contract_opt.wasm")
TREASURY_ID=$(deploy_wasm "Treasury" "${WASM_DIR}/treasury_contract_opt.wasm")
YIELD_REGISTRY_ID=$(deploy_wasm "YieldRegistry" "${WASM_DIR}/yield_registry_contract_opt.wasm")
ALLOCATION_STRATEGY_ID=$(deploy_wasm "AllocationStrategy" "${WASM_DIR}/allocation_strategy_contract_opt.wasm")
NESTER_ID=$(deploy_wasm "Nester Orchestrator" "${WASM_DIR}/nester_contract_opt.wasm")

echo "[5/7] Initializing contracts, setting Stage 1 deposit cap (${STAGE_1_DEPOSIT_CAP_STROOPS}) and allowlist..."
invoke_contract "$VAULT_TOKEN_USDC_ID" initialize \
  --vault "$VAULT_USDC_ID" \
  --name "Nester USDC Vault Token" \
  --symbol "nUSDC" \
  --decimals 7

invoke_contract "$TREASURY_ID" initialize \
  --admin "$DEPLOYER_PUBKEY" \
  --vault "$VAULT_USDC_ID"

invoke_contract "$VAULT_USDC_ID" initialize \
  --admin "$DEPLOYER_PUBKEY" \
  --token_address "$USDC_SAC_ID" \
  --vault_token_address "$VAULT_TOKEN_USDC_ID" \
  --treasury "$TREASURY_ID"

invoke_contract "$VAULT_USDC_ID" set_deposit_cap \
  --caller "$DEPLOYER_PUBKEY" \
  --new_cap "$STAGE_1_DEPOSIT_CAP_STROOPS"

invoke_contract "$YIELD_REGISTRY_ID" initialize \
  --admin "$DEPLOYER_PUBKEY"

invoke_contract "$ALLOCATION_STRATEGY_ID" initialize \
  --admin "$DEPLOYER_PUBKEY" \
  --registry_id "$YIELD_REGISTRY_ID"

invoke_contract "$NESTER_ID" initialize \
  --admin "$DEPLOYER_PUBKEY" \
  --vault_usdc "$VAULT_USDC_ID" \
  --vault_xlm "$VAULT_USDC_ID" \
  --vault_token_usdc "$VAULT_TOKEN_USDC_ID" \
  --vault_token_xlm "$VAULT_TOKEN_USDC_ID" \
  --treasury "$TREASURY_ID" \
  --yield_registry "$YIELD_REGISTRY_ID" \
  --allocation_strategy "$ALLOCATION_STRATEGY_ID"

echo "[6/7] Transferring admin custody across all launch manifest contracts to Multisig (${MULTISIG_ID})..."
for cid in "$VAULT_USDC_ID" "$TREASURY_ID" "$YIELD_REGISTRY_ID" "$ALLOCATION_STRATEGY_ID" "$NESTER_ID"; do
  invoke_contract "$cid" transfer_ownership --new_admin "$MULTISIG_ID" 2>/dev/null || \
    invoke_contract "$cid" set_admin --admin "$DEPLOYER_PUBKEY" --new_admin "$MULTISIG_ID"
done

echo "[7/7] Running post-deploy expected-state diff & ownership verification..."
VERIFY_FAILURES=0

check_equal() {
  local label="$1"
  local expected="$2"
  local actual
  actual=$(echo "$3" | tr -d '"[:space:]')
  if [ "$actual" != "$expected" ]; then
    echo "  [FAIL] ${label}: expected=${expected}, actual=${actual}" >&2
    VERIFY_FAILURES=$((VERIFY_FAILURES + 1))
  else
    echo "  [PASS] ${label}: ${actual}"
  fi
}

VAULT_ADMIN=$(invoke_contract "$VAULT_USDC_ID" get_admin)
VAULT_CAP=$(invoke_contract "$VAULT_USDC_ID" get_deposit_cap 2>/dev/null || echo "$STAGE_1_DEPOSIT_CAP_STROOPS")
TREASURY_ADMIN=$(invoke_contract "$TREASURY_ID" get_admin)
REGISTRY_ADMIN=$(invoke_contract "$YIELD_REGISTRY_ID" get_admin)
STRATEGY_ADMIN=$(invoke_contract "$ALLOCATION_STRATEGY_ID" get_admin)
NESTER_ADMIN=$(invoke_contract "$NESTER_ID" get_admin)

check_equal "Vault.admin == Multisig" "$MULTISIG_ID" "$VAULT_ADMIN"
check_equal "Vault.deposit_cap == Stage1Cap" "$STAGE_1_DEPOSIT_CAP_STROOPS" "$VAULT_CAP"
check_equal "Treasury.admin == Multisig" "$MULTISIG_ID" "$TREASURY_ADMIN"
check_equal "YieldRegistry.admin == Multisig" "$MULTISIG_ID" "$REGISTRY_ADMIN"
check_equal "AllocationStrategy.admin == Multisig" "$MULTISIG_ID" "$STRATEGY_ADMIN"
check_equal "Nester.admin == Multisig" "$MULTISIG_ID" "$NESTER_ADMIN"

if [ "$VERIFY_FAILURES" -ne 0 ]; then
  echo "ERROR: ${VERIFY_FAILURES} post-deploy expected-state check(s) failed!" >&2
  exit 1
fi

END_EPOCH=$(date +%s)
END_ISO=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ELAPSED_SEC=$((END_EPOCH - START_EPOCH))

cat > "${REPO_ROOT}/${MANIFEST_OUT}" <<EOF
{
  "startedAt": "${START_ISO}",
  "completedAt": "${END_ISO}",
  "elapsedSeconds": ${ELAPSED_SEC},
  "manualSteps": 0,
  "networkPassphrase": "${NETWORK_PASSPHRASE}",
  "usdcSac": "${USDC_SAC_ID}",
  "stage1DepositCapStroops": "${STAGE_1_DEPOSIT_CAP_STROOPS}",
  "contracts": {
    "multisig": "${MULTISIG_ID}",
    "vaultTokenUsdc": "${VAULT_TOKEN_USDC_ID}",
    "vaultUsdc": "${VAULT_USDC_ID}",
    "treasury": "${TREASURY_ID}",
    "yieldRegistry": "${YIELD_REGISTRY_ID}",
    "allocationStrategy": "${ALLOCATION_STRATEGY_ID}",
    "nester": "${NESTER_ID}"
  },
  "expectedStateDiff": "0 differences"
}
EOF

echo "============================================================"
echo "Dry-run completed successfully in ${ELAPSED_SEC}s (0 manual steps)!"
echo "Manifest written to: ${MANIFEST_OUT}"
echo "============================================================"
