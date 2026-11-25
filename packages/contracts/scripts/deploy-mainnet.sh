#!/usr/bin/env bash
# =============================================================================
# Nester Soroban Mainnet Deployment Script
#
# Deploys: VaultToken → Vault (USDC) → Vault (XLM) → Treasury → YieldRegistry
# → AllocationStrategy → Nester contracts, then initializes them all in the
# correct order. Mirrors deploy-testnet.sh, with the testnet-only friendbot
# funding removed and two mainnet-specific safety gates added (see below).
#
# Usage:
#   export DEPLOYER_SECRET=S...your_mainnet_secret_key...
#   export MAINNET_USDC_ISSUER=G...        # verify against Circle's published
#                                           # issuer address yourself — this
#                                           # script does not hardcode it
#   export MAINNET_AUDITED_COMMIT_SHA=<full 40-char commit sha>
#   export MAINNET_DEPLOY_CONFIRM=I_UNDERSTAND
#   bash scripts/deploy-mainnet.sh
#
# Prerequisites:
#   cargo install stellar-cli --features opt
#   rustup show   # must match rust-toolchain.toml — see VERIFY.md
#   make build    (run first to compile WASM with the pinned toolchain)
# =============================================================================
set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────

NETWORK="mainnet"
RPC_URL="https://mainnet.sorobanrpc.com"
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
HORIZON_URL="https://horizon.stellar.org"

WASM_DIR="./target/wasm32-unknown-unknown/release"
OUTPUT_FILE="./scripts/deployed-mainnet.env"

# ── Mainnet safety gate 1: deploying real money requires explicit opt-in ────

if [ "${MAINNET_DEPLOY_CONFIRM:-}" != "I_UNDERSTAND" ]; then
  echo "❌  Refusing to deploy to mainnet."
  echo "   This spends real XLM and deploys contracts that will hold real user"
  echo "   funds. Set MAINNET_DEPLOY_CONFIRM=I_UNDERSTAND to proceed."
  exit 1
fi

# ── Mainnet safety gate 2: refuse to deploy an unaudited commit ─────────────
# issue referenced by the mainnet launch checklist's "Audit Complete" gate:
# the commit that gets deployed must be the exact commit the audit report
# covers, not whatever happens to be checked out.

if [ -z "${MAINNET_AUDITED_COMMIT_SHA:-}" ]; then
  echo "❌  MAINNET_AUDITED_COMMIT_SHA is not set."
  echo "   Set it to the full commit SHA named in the published audit report"
  echo "   (docs/security/) so this script can confirm HEAD matches it."
  exit 1
fi

CURRENT_COMMIT="$(git rev-parse HEAD)"
if [ "$CURRENT_COMMIT" != "$MAINNET_AUDITED_COMMIT_SHA" ]; then
  echo "❌  HEAD does not match the audited commit."
  echo "   HEAD:                      $CURRENT_COMMIT"
  echo "   MAINNET_AUDITED_COMMIT_SHA: $MAINNET_AUDITED_COMMIT_SHA"
  echo "   Check out the audited commit before deploying to mainnet."
  exit 1
fi
echo "✓  HEAD matches the audited commit: $CURRENT_COMMIT"

# ── Resolve mainnet USDC issuer ───────────────────────────────────────────────
# Deliberately not hardcoded (unlike deploy-testnet.sh's USDC_EXPECTED_SAC):
# a wrong mainnet issuer address is a real-funds mistake, not a testnet
# inconvenience. The operator must supply and independently verify it.

if [ -z "${MAINNET_USDC_ISSUER:-}" ]; then
  echo "❌  MAINNET_USDC_ISSUER is not set."
  echo "   Verify the official Circle USDC issuer address on Stellar mainnet"
  echo "   yourself before setting this — do not trust a value from this"
  echo "   script or from chat history."
  exit 1
fi
USDC_ISSUER="$MAINNET_USDC_ISSUER"

# ── Resolve deployer identity ─────────────────────────────────────────────────
# Accepts either a key alias (DEPLOYER_KEY=deployer) or a secret key (DEPLOYER_SECRET=S...)

DEPLOYER_SECRET="${DEPLOYER_SECRET:-${1:-}}"
DEPLOYER_KEY="${DEPLOYER_KEY:-}"

if [ -z "$DEPLOYER_KEY" ] && [ -z "$DEPLOYER_SECRET" ]; then
  echo "❌  Error: no deployer key provided."
  echo "   Option A (key alias): DEPLOYER_KEY=deployer bash scripts/deploy-mainnet.sh"
  echo "   Option B (secret):    DEPLOYER_SECRET=SABCD... bash scripts/deploy-mainnet.sh"
  exit 1
fi

# ── Check stellar CLI ─────────────────────────────────────────────────────────

if ! command -v stellar &>/dev/null; then
  echo "❌  stellar CLI not found."
  exit 1
fi

echo "✓  stellar CLI: $(stellar --version 2>&1 | head -1)"

# ── Verify USDC SAC address derivation ───────────────────────────────────────

echo "⏳  Deriving USDC SAC address from issuer..."
USDC_CONTRACT=$(stellar contract id asset \
  --asset "USDC:${USDC_ISSUER}" \
  --network "$NETWORK" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$NETWORK_PASSPHRASE" \
  2>/dev/null || true)

if [ -z "$USDC_CONTRACT" ]; then
  echo "❌  Could not derive USDC SAC address. Aborting rather than guessing."
  exit 1
fi
echo "✓  USDC SAC derived: $USDC_CONTRACT"

echo "⏳  Deriving native XLM SAC address..."
XLM_CONTRACT=$(stellar contract id asset \
  --asset "native" \
  --network "$NETWORK" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$NETWORK_PASSPHRASE" \
  2>/dev/null || true)

if [ -z "$XLM_CONTRACT" ]; then
  echo "❌  Could not derive the native XLM SAC address. Aborting rather than guessing."
  exit 1
fi
echo "✓  Native XLM SAC derived: $XLM_CONTRACT"

# ── Resolve deployer address ──────────────────────────────────────────────────

if [ -n "$DEPLOYER_KEY" ]; then
  SOURCE_ACCOUNT="$DEPLOYER_KEY"
  DEPLOYER=$(stellar keys address "$DEPLOYER_KEY" 2>/dev/null)
else
  IDENTITY_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/stellar/identity"
  mkdir -p "$IDENTITY_DIR"
  printf 'secret_key = "%s"\n' "$DEPLOYER_SECRET" > "$IDENTITY_DIR/_nester_deploy_mainnet.toml"
  SOURCE_ACCOUNT="_nester_deploy_mainnet"
  DEPLOYER=$(stellar keys address _nester_deploy_mainnet 2>/dev/null)
fi

if [ -z "$DEPLOYER" ]; then
  echo "❌  Could not resolve deployer address."
  exit 1
fi
echo "✓  Deployer: $DEPLOYER"

echo "⏳  Checking mainnet balance..."
BALANCE=$(curl -sf "${HORIZON_URL}/accounts/${DEPLOYER}" | python3 -c "
import sys,json
data=json.load(sys.stdin)
xlm=[b for b in data.get('balances',[]) if b['asset_type']=='native']
print(xlm[0]['balance'] if xlm else '0')
" 2>/dev/null || echo "0")

if python3 -c "import sys; exit(0 if float('${BALANCE}') < 50 else 1)" 2>/dev/null; then
  echo "❌  Deployer balance ($BALANCE XLM) looks too low to cover deploy fees"
  echo "   and base reserves for 7 new contracts. Fund the account and retry."
  exit 1
fi
echo "✓  Deployer balance: $BALANCE XLM"

# ── Build contracts ───────────────────────────────────────────────────────────

echo ""
echo "⏳  Building contracts..."
cd "$(dirname "$0")/.."
cargo build --release --target wasm32-unknown-unknown -p vault-token-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p vault-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p treasury-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p yield-registry-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p allocation-strategy-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p nester-contract 2>&1 | tail -3

echo "⏳  Optimizing WASMs..."
for wasm in vault_token vault_contract treasury_contract yield_registry_contract allocation_strategy_contract nester_contract; do
  stellar contract optimize \
    --wasm "$WASM_DIR/${wasm}.wasm" \
    --wasm-out "$WASM_DIR/${wasm}_opt.wasm" 2>/dev/null
done
echo "✓  Build complete"

# ── Helper: deploy a contract, capturing both its ID and deploy tx hash ─────
# The tx hash is what VERIFY.md's mainnet section checks against Horizon to
# prove the recorded contract ID was actually created by this deploy, not
# just that *a* contract with that WASM hash exists somewhere.

declare -A DEPLOY_TX_HASHES

deploy_contract() {
  local name="$1"
  local wasm="$2"
  echo "" >&2
  echo "⏳  Deploying $name..." >&2
  local raw
  raw=$(stellar contract deploy \
    --wasm "$wasm" \
    --source-account "$SOURCE_ACCOUNT" \
    --network "$NETWORK" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$NETWORK_PASSPHRASE" \
    2>&1)

  local id
  id=$(echo "$raw" | grep -v "^ℹ️\|^🌎\|^✅\|^🔗" | tail -1)
  if [ -z "$id" ]; then
    echo "❌  $name deployment failed" >&2
    echo "$raw" >&2
    exit 1
  fi

  local tx_hash
  tx_hash=$(echo "$raw" | grep -oE 'Signing transaction:\s*[a-f0-9]{64}' | grep -oE '[a-f0-9]{64}' | head -1 || true)
  if [ -z "$tx_hash" ]; then
    echo "⚠️   Could not parse deploy tx hash for $name from stellar-cli output." >&2
    echo "   Look it up manually via the deployer account's Horizon operations" >&2
    echo "   and record it in $OUTPUT_FILE before committing." >&2
    tx_hash="UNKNOWN_LOOK_UP_VIA_HORIZON"
  fi
  DEPLOY_TX_HASHES["$name"]="$tx_hash"

  echo "✓  $name deployed: $id (tx $tx_hash)" >&2
  echo "$id"
}

invoke() {
  local contract_id="$1"
  shift
  stellar contract invoke \
    --id "$contract_id" \
    --source-account "$SOURCE_ACCOUNT" \
    --network "$NETWORK" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$NETWORK_PASSPHRASE" \
    -- "$@"
}

# ── Deploy contracts ──────────────────────────────────────────────────────────

VAULT_TOKEN_USDC_ID=$(deploy_contract "VaultTokenUSDC" "$WASM_DIR/vault_token_opt.wasm")
VAULT_TOKEN_XLM_ID=$(deploy_contract "VaultTokenXLM" "$WASM_DIR/vault_token_opt.wasm")
VAULT_USDC_ID=$(deploy_contract "VaultUSDC" "$WASM_DIR/vault_contract_opt.wasm")
VAULT_XLM_ID=$(deploy_contract "VaultXLM" "$WASM_DIR/vault_contract_opt.wasm")
TREASURY_ID=$(deploy_contract "Treasury" "$WASM_DIR/treasury_contract_opt.wasm")
YIELD_REGISTRY_ID=$(deploy_contract "YieldRegistry" "$WASM_DIR/yield_registry_contract_opt.wasm")
ALLOCATION_STRATEGY_ID=$(deploy_contract "AllocationStrategy" "$WASM_DIR/allocation_strategy_contract_opt.wasm")
NESTER_ID=$(deploy_contract "Nester" "$WASM_DIR/nester_contract_opt.wasm")

# ── Initialize in correct order ───────────────────────────────────────────────

echo ""
echo "⏳  Initializing VaultToken (USDC)..."
invoke "$VAULT_TOKEN_USDC_ID" initialize \
  --admin "$DEPLOYER" \
  --vault "$VAULT_USDC_ID" \
  --name "Nester USDC Vault Token" \
  --symbol "nUSDC" \
  --decimals 7
echo "✓  VaultToken (USDC) initialized"

echo ""
echo "⏳  Initializing VaultToken (XLM)..."
invoke "$VAULT_TOKEN_XLM_ID" initialize \
  --admin "$DEPLOYER" \
  --vault "$VAULT_XLM_ID" \
  --name "Nester XLM Vault Token" \
  --symbol "nXLM" \
  --decimals 7
echo "✓  VaultToken (XLM) initialized"

echo ""
echo "⏳  Initializing Treasury..."
invoke "$TREASURY_ID" initialize \
  --admin "$DEPLOYER" \
  --vault "$VAULT_USDC_ID"
echo "✓  Treasury initialized"

echo ""
echo "⏳  Initializing Vault (USDC)..."
invoke "$VAULT_USDC_ID" initialize \
  --admin "$DEPLOYER" \
  --token_address "$USDC_CONTRACT" \
  --vault_token_address "$VAULT_TOKEN_USDC_ID" \
  --treasury "$TREASURY_ID"
echo "✓  Vault (USDC) initialized"

echo ""
echo "⏳  Initializing Vault (XLM)..."
invoke "$VAULT_XLM_ID" initialize \
  --admin "$DEPLOYER" \
  --token_address "$XLM_CONTRACT" \
  --vault_token_address "$VAULT_TOKEN_XLM_ID" \
  --treasury "$TREASURY_ID"
echo "✓  Vault (XLM) initialized"

echo ""
echo "⏳  Initializing YieldRegistry..."
invoke "$YIELD_REGISTRY_ID" initialize \
  --admin "$DEPLOYER"
echo "✓  YieldRegistry initialized"

echo ""
echo "⏳  Initializing AllocationStrategy..."
invoke "$ALLOCATION_STRATEGY_ID" initialize \
  --admin "$DEPLOYER" \
  --registry_id "$YIELD_REGISTRY_ID"
echo "✓  AllocationStrategy initialized"

echo ""
echo "⏳  Initializing Nester Orchestrator..."
invoke "$NESTER_ID" initialize \
  --admin "$DEPLOYER" \
  --vault_usdc "$VAULT_USDC_ID" \
  --vault_xlm "$VAULT_XLM_ID" \
  --vault_token_usdc "$VAULT_TOKEN_USDC_ID" \
  --vault_token_xlm "$VAULT_TOKEN_XLM_ID" \
  --treasury "$TREASURY_ID" \
  --yield_registry "$YIELD_REGISTRY_ID" \
  --allocation_strategy "$ALLOCATION_STRATEGY_ID"
echo "✓  Nester Orchestrator initialized"

# ── Write output ──────────────────────────────────────────────────────────────

DEPLOY_TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
NESTER_WASM_SHA256="$(sha256sum "$WASM_DIR/nester_contract_opt.wasm" 2>/dev/null | awk '{print $1}')"
VAULT_CONTRACT_WASM_SHA256="$(sha256sum "$WASM_DIR/vault_contract_opt.wasm" 2>/dev/null | awk '{print $1}')"
VAULT_TOKEN_WASM_SHA256="$(sha256sum "$WASM_DIR/vault_token_opt.wasm" 2>/dev/null | awk '{print $1}')"
TREASURY_WASM_SHA256="$(sha256sum "$WASM_DIR/treasury_contract_opt.wasm" 2>/dev/null | awk '{print $1}')"
YIELD_REGISTRY_WASM_SHA256="$(sha256sum "$WASM_DIR/yield_registry_contract_opt.wasm" 2>/dev/null | awk '{print $1}')"
ALLOCATION_STRATEGY_WASM_SHA256="$(sha256sum "$WASM_DIR/allocation_strategy_contract_opt.wasm" 2>/dev/null | awk '{print $1}')"

echo ""
echo "⏳  Writing contract IDs to $OUTPUT_FILE..."
cat > "$OUTPUT_FILE" <<ENV
# Nester Mainnet Contract IDs — generated by deploy-mainnet.sh
# Copy these into apps/dapp/frontend/.env.production
#
# Audited commit (docs/security/ audit report covers this exact SHA):
#   $MAINNET_AUDITED_COMMIT_SHA
# Deployed at (UTC): $DEPLOY_TIMESTAMP
#
# See packages/contracts/scripts/VERIFY.md → "Verifying a deployed mainnet
# contract" for how to reproduce every hash below from source.
#
# wasm sha256 (of the optimized build in $WASM_DIR):
#   nester_contract_opt.wasm:             $NESTER_WASM_SHA256
#   vault_contract_opt.wasm:              $VAULT_CONTRACT_WASM_SHA256
#   vault_token_opt.wasm:                 $VAULT_TOKEN_WASM_SHA256
#   treasury_contract_opt.wasm:           $TREASURY_WASM_SHA256
#   yield_registry_contract_opt.wasm:     $YIELD_REGISTRY_WASM_SHA256
#   allocation_strategy_contract_opt.wasm: $ALLOCATION_STRATEGY_WASM_SHA256
#
# Deploy transaction hashes (verify each on https://stellar.expert or via
# Horizon /transactions/<hash> — confirm it is a create-contract invocation
# whose resulting contract ID matches the corresponding NEXT_PUBLIC_* value):
#   VaultTokenUSDC:      ${DEPLOY_TX_HASHES[VaultTokenUSDC]}
#   VaultTokenXLM:       ${DEPLOY_TX_HASHES[VaultTokenXLM]}
#   VaultUSDC:           ${DEPLOY_TX_HASHES[VaultUSDC]}
#   VaultXLM:            ${DEPLOY_TX_HASHES[VaultXLM]}
#   Treasury:            ${DEPLOY_TX_HASHES[Treasury]}
#   YieldRegistry:       ${DEPLOY_TX_HASHES[YieldRegistry]}
#   AllocationStrategy:  ${DEPLOY_TX_HASHES[AllocationStrategy]}
#   Nester:              ${DEPLOY_TX_HASHES[Nester]}

NEXT_PUBLIC_NESTER_CONTRACT_ID=$NESTER_ID
NEXT_PUBLIC_VAULT_CONTRACT_ID=$VAULT_USDC_ID
NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID=$VAULT_XLM_ID
NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ID=$VAULT_TOKEN_USDC_ID
NEXT_PUBLIC_VAULT_TOKEN_XLM_CONTRACT_ID=$VAULT_TOKEN_XLM_ID
NEXT_PUBLIC_TREASURY_CONTRACT_ID=$TREASURY_ID
NEXT_PUBLIC_YIELD_REGISTRY_CONTRACT_ID=$YIELD_REGISTRY_ID
NEXT_PUBLIC_ALLOCATION_STRATEGY_CONTRACT_ID=$ALLOCATION_STRATEGY_ID
NEXT_PUBLIC_USDC_CONTRACT_ID=$USDC_CONTRACT
NEXT_PUBLIC_XLM_CONTRACT_ID=$XLM_CONTRACT
NEXT_PUBLIC_ADMIN_ADDRESS=$DEPLOYER
ENV

echo ""
echo "=========================================="
echo "✅  MAINNET DEPLOYMENT COMPLETE"
echo "=========================================="
echo ""
echo "Contract IDs saved to: $OUTPUT_FILE"
echo ""
echo "Next steps:"
echo "  1. Commit $OUTPUT_FILE to git — it is the public verification record."
echo "  2. Follow VERIFY.md's mainnet section yourself before announcing the"
echo "     deploy, to confirm what's recorded is actually reproducible."
echo "  3. Copy the contents of $OUTPUT_FILE into:"
echo "     apps/dapp/frontend/.env.production"
