#!/usr/bin/env bash
set -euo pipefail

# Mainnet Contract Deploy Dry-Run Script
# Practices the exact deploy sequence: contract build/deploy, initialization,
# ownership transfer to multisig, and verification against a local Stellar network
# mirroring the mainnet ledger version and protocol settings.

echo "============================================================"
echo "Nester Mainnet Contract Deploy Dry-Run"
echo "============================================================"

NETWORK_PASSPHRASE="Test SDF Network ; September 2015"
RPC_URL="http://localhost:8000/soroban/rpc"
HORIZON_URL="http://localhost:8000"
STELLAR_BIN="${STELLAR_BIN:-stellar}"

# Circle's published Stellar Mainnet USDC issuer account (Issue #1494).
# Source URL: https://developers.circle.com/stablecoins/usdc-contract-addresses
# Additional reference: https://www.circle.com/multi-chain-usdc/stellar
CIRCLE_MAINNET_USDC_ISSUER="GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
CIRCLE_MAINNET_USDC_ASSET="USDC:${CIRCLE_MAINNET_USDC_ISSUER}"

derive_and_assert_mainnet_usdc_sac() {
  echo "[*] Deriving Mainnet USDC SAC ID for ${CIRCLE_MAINNET_USDC_ASSET}..."
  DERIVED_USDC_SAC_ID=$(
    "$STELLAR_BIN" contract id asset \
      --asset "$CIRCLE_MAINNET_USDC_ASSET" \
      --network mainnet 2>/dev/null | tr -d '[:space:]'
  )

  if [[ -z "$DERIVED_USDC_SAC_ID" || ! "$DERIVED_USDC_SAC_ID" =~ ^C[A-Z0-9]{55}$ ]]; then
    echo "Error: failed to derive a valid Mainnet USDC SAC contract ID (got '${DERIVED_USDC_SAC_ID}')." >&2
    exit 1
  fi

  CONFIGURED_USDC_CONTRACT="${USDC_CONTRACT:-$DERIVED_USDC_SAC_ID}"
  if [ "$CONFIGURED_USDC_CONTRACT" != "$DERIVED_USDC_SAC_ID" ]; then
    echo "Error: configured USDC_CONTRACT does not match derived Mainnet USDC SAC ID!" >&2
    echo "  asset:               ${CIRCLE_MAINNET_USDC_ASSET}" >&2
    echo "  derived SAC ID:      ${DERIVED_USDC_SAC_ID}" >&2
    echo "  configured contract: ${CONFIGURED_USDC_CONTRACT}" >&2
    exit 1
  fi

  USDC_CONTRACT="$DERIVED_USDC_SAC_ID"
  echo "[*] Verified Mainnet USDC SAC ID: $USDC_CONTRACT"
}

assert_vault_token_matches_derived_sac() {
  local vault_id="$1"
  local network_name="${2:-local}"
  local onchain_token
  onchain_token=$(
    "$STELLAR_BIN" contract invoke \
      --id "$vault_id" \
      --source deployer \
      --network "$network_name" \
      -- \
      get_token | tr -d '"[:space:]'
  )

  echo "[*] Vault get_token returned: $onchain_token"
  if [ "$onchain_token" != "$DERIVED_USDC_SAC_ID" ]; then
    echo "Error: deployed vault token address does not match derived Mainnet USDC SAC ID!" >&2
    echo "  expected (derived SAC): $DERIVED_USDC_SAC_ID" >&2
    echo "  actual (get_token):     $onchain_token" >&2
    exit 1
  fi
  echo "[*] Verified deployed vault underlying token matches Mainnet USDC SAC: $onchain_token"
}

if [ "${1:-}" = "--verify-usdc-sac" ]; then
  derive_and_assert_mainnet_usdc_sac
  if [ -n "${VERIFY_VAULT_ID:-}" ]; then
    assert_vault_token_matches_derived_sac "$VERIFY_VAULT_ID" "${VERIFY_NETWORK:-local}"
  fi
  exit 0
fi

derive_and_assert_mainnet_usdc_sac

echo "[*] Step 1: Building contracts for WASM release..."
cd packages/contracts
cargo build --release --target wasm32-unknown-unknown

echo "[*] Step 2: Setting up local test identities & funding deployer..."
# In a true dry-run against local node, we use stellar CLI configured for local network
if ! NETWORK_ADD_OUTPUT=$("$STELLAR_BIN" config network add --global local \
  --rpc "$RPC_URL" \
  --network-passphrase "$NETWORK_PASSPHRASE" \
  --horizon-url "$HORIZON_URL" 2>&1); then
  if ! echo "$NETWORK_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to configure local network:" >&2
    echo "$NETWORK_ADD_OUTPUT" >&2
    exit 1
  fi
fi

if ! IDENTITY_ADD_OUTPUT=$("$STELLAR_BIN" config identity add --global deployer --local 2>&1); then
  if ! echo "$IDENTITY_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to add deployer identity:" >&2
    echo "$IDENTITY_ADD_OUTPUT" >&2
    exit 1
  fi
fi

if ! IDENTITY_ADD_OUTPUT=$("$STELLAR_BIN" config identity add --global multisig-admin --local 2>&1); then
  if ! echo "$IDENTITY_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to add multisig-admin identity:" >&2
    echo "$IDENTITY_ADD_OUTPUT" >&2
    exit 1
  fi
fi

DEPLOYER_PUBKEY=$("$STELLAR_BIN" config identity address deployer)
MULTISIG_PUBKEY=$("$STELLAR_BIN" config identity address multisig-admin)

echo "[*] Deployer address: $DEPLOYER_PUBKEY"
echo "[*] Multisig admin address: $MULTISIG_PUBKEY"

echo "[*] Step 3: Deploying Nester core vault contract..."
VAULT_WASM="target/wasm32-unknown-unknown/release/nester_vault.wasm"
if [ ! -f "$VAULT_WASM" ]; then
  echo "Error: Vault WASM not found at $VAULT_WASM"
  exit 1
fi

VAULT_CONTRACT_ID=$("$STELLAR_BIN" contract deploy \
  --wasm "$VAULT_WASM" \
  --source deployer \
  --network local)

echo "[*] Deployed Nester Vault Contract ID: $VAULT_CONTRACT_ID"

echo "[*] Step 4: Initializing Vault contract parameters..."
"$STELLAR_BIN" contract invoke \
  --id "$VAULT_CONTRACT_ID" \
  --source deployer \
  --network local \
  -- \
  initialize \
  --admin "$DEPLOYER_PUBKEY" \
  --token_address "$DERIVED_USDC_SAC_ID"

echo "[*] Step 5: Transferring ownership/admin to multisig..."
"$STELLAR_BIN" contract invoke \
  --id "$VAULT_CONTRACT_ID" \
  --source deployer \
  --network local \
  -- \
  transfer_ownership \
  --new_admin "$MULTISIG_PUBKEY"

echo "[*] Step 6: Verifying contract state, underlying USDC SAC, and ownership..."
assert_vault_token_matches_derived_sac "$VAULT_CONTRACT_ID" "local"

ADMIN_RESULT=$("$STELLAR_BIN" contract invoke \
  --id "$VAULT_CONTRACT_ID" \
  --source deployer \
  --network local \
  -- \
  get_admin)

echo "[*] get_admin returned: $ADMIN_RESULT"

if [ "$ADMIN_RESULT" != "$MULTISIG_PUBKEY" ]; then
  echo "Error: ownership transfer verification failed." >&2
  echo "  expected admin (multisig): $MULTISIG_PUBKEY" >&2
  echo "  actual admin returned:     $ADMIN_RESULT" >&2
  exit 1
fi

echo "[*] Verified contract admin matches multisig: $ADMIN_RESULT"
echo "============================================================"
echo "Dry-run completed successfully! All steps verified end-to-end."
echo "============================================================"
