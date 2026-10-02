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

echo "[*] Step 1: Building contracts for WASM release..."
cd packages/contracts
cargo build --release --target wasm32-unknown-unknown

echo "[*] Step 2: Setting up local test identities & funding deployer..."
# In a true dry-run against local node, we use stellar CLI configured for local network
if ! NETWORK_ADD_OUTPUT=$(stellar config network add --global local \
  --rpc "$RPC_URL" \
  --network-passphrase "$NETWORK_PASSPHRASE" \
  --horizon-url "$HORIZON_URL" 2>&1); then
  if ! echo "$NETWORK_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to configure local network:" >&2
    echo "$NETWORK_ADD_OUTPUT" >&2
    exit 1
  fi
fi

if ! IDENTITY_ADD_OUTPUT=$(stellar config identity add --global deployer --local 2>&1); then
  if ! echo "$IDENTITY_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to add deployer identity:" >&2
    echo "$IDENTITY_ADD_OUTPUT" >&2
    exit 1
  fi
fi

if ! IDENTITY_ADD_OUTPUT=$(stellar config identity add --global multisig-admin --local 2>&1); then
  if ! echo "$IDENTITY_ADD_OUTPUT" | grep -qi "already exists"; then
    echo "Error: failed to add multisig-admin identity:" >&2
    echo "$IDENTITY_ADD_OUTPUT" >&2
    exit 1
  fi
fi

DEPLOYER_PUBKEY=$(stellar config identity address deployer)
MULTISIG_PUBKEY=$(stellar config identity address multisig-admin)

echo "[*] Deployer address: $DEPLOYER_PUBKEY"
echo "[*] Multisig admin address: $MULTISIG_PUBKEY"

echo "[*] Step 3: Deploying Nester core vault contract..."
VAULT_WASM="target/wasm32-unknown-unknown/release/nester_vault.wasm"
if [ ! -f "$VAULT_WASM" ]; then
  echo "Error: Vault WASM not found at $VAULT_WASM"
  exit 1
fi

VAULT_CONTRACT_ID=$(stellar contract deploy \
  --wasm "$VAULT_WASM" \
  --source deployer \
  --network local)

echo "[*] Deployed Nester Vault Contract ID: $VAULT_CONTRACT_ID"

echo "[*] Step 4: Initializing Vault contract parameters..."
stellar contract invoke \
  --id "$VAULT_CONTRACT_ID" \
  --source deployer \
  --network local \
  -- \
  initialize \
  --admin "$DEPLOYER_PUBKEY"

echo "[*] Step 5: Transferring ownership/admin to multisig..."
stellar contract invoke \
  --id "$VAULT_CONTRACT_ID" \
  --source deployer \
  --network local \
  -- \
  transfer_ownership \
  --new_admin "$MULTISIG_PUBKEY"

echo "[*] Step 6: Verifying contract state and ownership..."
ADMIN_RESULT=$(stellar contract invoke \
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
