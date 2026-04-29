#!/usr/bin/env bash
# =============================================================================
# Nester Soroban Testnet Deployment Script
#
# Deploys: VaultToken → Vault (USDC) → Vault (XLM) → Treasury contracts
# then initializes them all in the correct order.
#
# Usage:
#   export DEPLOYER_SECRET=S...your_testnet_secret_key...
#   bash scripts/deploy-testnet.sh
#
# Or pass the secret as an argument:
#   bash scripts/deploy-testnet.sh S...your_testnet_secret_key...
#
# Prerequisites:
#   cargo install stellar-cli --features opt
#   make build   (run first to compile WASM)
# =============================================================================
set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────

NETWORK="testnet"
RPC_URL="https://soroban-testnet.stellar.org"
NETWORK_PASSPHRASE="Test SDF Network ; September 2015"
HORIZON_URL="https://horizon-testnet.stellar.org"

# Testnet token contracts (Stellar Asset Contract wrappers)
USDC_CONTRACT="CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
XLM_CONTRACT="CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"

WASM_DIR="./target/wasm32-unknown-unknown/release"
OUTPUT_FILE="./scripts/deployed-testnet.env"

# ── Resolve deployer identity ─────────────────────────────────────────────────
# Accepts either a key alias (DEPLOYER_KEY=deployer) or a secret key (DEPLOYER_SECRET=S...)

DEPLOYER_SECRET="${DEPLOYER_SECRET:-${1:-}}"
DEPLOYER_KEY="${DEPLOYER_KEY:-}"

if [ -z "$DEPLOYER_KEY" ] && [ -z "$DEPLOYER_SECRET" ]; then
  echo "❌  Error: no deployer key provided."
  echo "   Option A (key alias): DEPLOYER_KEY=deployer bash scripts/deploy-testnet.sh"
  echo "   Option B (secret):    DEPLOYER_SECRET=SABCD... bash scripts/deploy-testnet.sh"
  exit 1
fi

# ── Check stellar CLI ─────────────────────────────────────────────────────────

if ! command -v stellar &>/dev/null; then
  echo "❌  stellar CLI not found."
  exit 1
fi

echo "✓  stellar CLI: $(stellar --version 2>&1 | head -1)"

# ── Resolve deployer address ──────────────────────────────────────────────────

if [ -n "$DEPLOYER_KEY" ]; then
  # Use an existing saved key alias
  SOURCE_ACCOUNT="$DEPLOYER_KEY"
  DEPLOYER=$(stellar keys address "$DEPLOYER_KEY" 2>/dev/null)
else
  # Secret key passed directly — write a temp identity file for stellar-cli v25
  IDENTITY_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/stellar/identity"
  mkdir -p "$IDENTITY_DIR"
  printf 'secret_key = "%s"\n' "$DEPLOYER_SECRET" > "$IDENTITY_DIR/_nester_deploy.toml"
  SOURCE_ACCOUNT="_nester_deploy"
  DEPLOYER=$(stellar keys address _nester_deploy 2>/dev/null)
fi

if [ -z "$DEPLOYER" ]; then
  echo "❌  Could not resolve deployer address."
  exit 1
fi
echo "✓  Deployer: $DEPLOYER"

# Fund from friendbot if balance is low
echo "⏳  Checking testnet balance..."
BALANCE=$(curl -sf "${HORIZON_URL}/accounts/${DEPLOYER}" | python3 -c "
import sys,json
data=json.load(sys.stdin)
xlm=[b for b in data.get('balances',[]) if b['asset_type']=='native']
print(xlm[0]['balance'] if xlm else '0')
" 2>/dev/null || echo "0")

if python3 -c "import sys; exit(0 if float('${BALANCE}') < 10 else 1)" 2>/dev/null; then
  echo "⏳  Low balance ($BALANCE XLM) — funding from friendbot..."
  curl -sf "https://friendbot.stellar.org?addr=${DEPLOYER}" > /dev/null
  echo "✓  Funded from friendbot"
fi

# ── Build contracts ───────────────────────────────────────────────────────────

echo ""
echo "⏳  Building contracts..."
cd "$(dirname "$0")/.."
# Build vault-token first (vault imports its WASM via contractimport!)
cargo build --release --target wasm32-unknown-unknown -p vault-token-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p vault-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p treasury-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p yield-registry-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p allocation-strategy-contract 2>&1 | tail -3
cargo build --release --target wasm32-unknown-unknown -p nester-contract 2>&1 | tail -3

# Optimize WASMs (strips reference-types and other unsupported features)
echo "⏳  Optimizing WASMs..."
for wasm in vault_token vault_contract treasury_contract yield_registry_contract allocation_strategy_contract nester_contract; do
  stellar contract optimize \
    --wasm "$WASM_DIR/${wasm}.wasm" \
    --wasm-out "$WASM_DIR/${wasm}_opt.wasm" 2>/dev/null
done
echo "✓  Build complete"

# ── Helper: deploy a contract ─────────────────────────────────────────────────

deploy_contract() {
  local name="$1"
  local wasm="$2"
  echo "" >&2
  echo "⏳  Deploying $name..." >&2
  local id
  id=$(stellar contract deploy \
    --wasm "$wasm" \
    --source-account "$SOURCE_ACCOUNT" \
    --network "$NETWORK" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$NETWORK_PASSPHRASE" \
    2>&1 | grep -v "^ℹ️\|^🌎\|^✅\|^🔗" | tail -1)
  if [ -z "$id" ]; then
    echo "❌  $name deployment failed" >&2
    exit 1
  fi
  echo "✓  $name deployed: $id" >&2
  echo "$id"
}

# ── Helper: invoke a contract function ───────────────────────────────────────

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

# VaultToken (shared between vaults — each vault gets its own token)
VAULT_TOKEN_USDC_ID=$(deploy_contract "VaultToken (USDC)" "$WASM_DIR/vault_token_opt.wasm")
VAULT_TOKEN_XLM_ID=$(deploy_contract "VaultToken (XLM)" "$WASM_DIR/vault_token_opt.wasm")

# Vaults
VAULT_USDC_ID=$(deploy_contract "Vault (USDC)" "$WASM_DIR/vault_contract_opt.wasm")
VAULT_XLM_ID=$(deploy_contract "Vault (XLM)" "$WASM_DIR/vault_contract_opt.wasm")

# Treasury (one shared treasury)
TREASURY_ID=$(deploy_contract "Treasury" "$WASM_DIR/treasury_contract_opt.wasm")

# Yield Registry
YIELD_REGISTRY_ID=$(deploy_contract "YieldRegistry" "$WASM_DIR/yield_registry_contract_opt.wasm")

# Allocation Strategy
ALLOCATION_STRATEGY_ID=$(deploy_contract "AllocationStrategy" "$WASM_DIR/allocation_strategy_contract_opt.wasm")

# Nester Orchestrator (deployed last — it stores all other contract addresses)
NESTER_ID=$(deploy_contract "Nester" "$WASM_DIR/nester_contract_opt.wasm")

# ── Initialize in correct order ───────────────────────────────────────────────

echo ""
echo "⏳  Initializing VaultToken (USDC)..."
invoke "$VAULT_TOKEN_USDC_ID" initialize \
  --vault "$VAULT_USDC_ID" \
  --name "Nester USDC Vault Token" \
  --symbol "nUSDC" \
  --decimals 7
echo "✓  VaultToken (USDC) initialized"

echo ""
echo "⏳  Initializing VaultToken (XLM)..."
invoke "$VAULT_TOKEN_XLM_ID" initialize \
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

echo ""
echo "⏳  Writing contract IDs to $OUTPUT_FILE..."
cat > "$OUTPUT_FILE" <<ENV
# Nester Testnet Contract IDs — generated by deploy-testnet.sh
# Copy these into apps/dapp/frontend/.env.local

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
echo "✅  DEPLOYMENT COMPLETE"
echo "=========================================="
echo ""
echo "Contract IDs:"
echo "  NESTER:             $NESTER_ID"
echo "  VAULT (USDC):       $VAULT_USDC_ID"
echo "  VAULT (XLM):        $VAULT_XLM_ID"
echo "  VAULT TOKEN (USDC): $VAULT_TOKEN_USDC_ID"
echo "  VAULT TOKEN (XLM):  $VAULT_TOKEN_XLM_ID"
echo "  TREASURY:           $TREASURY_ID"
echo "  YIELD REGISTRY:     $YIELD_REGISTRY_ID"
echo "  ALLOC STRATEGY:     $ALLOCATION_STRATEGY_ID"
echo "  USDC TOKEN:         $USDC_CONTRACT"
echo "  XLM TOKEN:          $XLM_CONTRACT"
echo ""
echo "Contract IDs saved to: $OUTPUT_FILE"
echo ""
echo "Next steps:"
echo "  1. Copy the contents of $OUTPUT_FILE into:"
echo "     apps/dapp/frontend/.env.local"
echo "  2. Restart the dapp dev server"
