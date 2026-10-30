#!/usr/bin/env bash
# =============================================================================
# Nester Savings Testnet Deployment Script
#
# Deploys savings_core, savings_reserve, adapter_blend and adapter_defindex,
# initializes them in dependency order, then proposes the two AddAdapter
# changes. Proposals sit behind the core's timelock; this script prints their
# ids and the execute commands but does not wait.
#
# Order (the core checks reserve.core() == core when it initializes):
#   deploy core -> deploy reserve -> reserve.initialize(core)
#   -> core.initialize(reserve) -> adapters -> propose AddAdapter x2
#
# Usage:
#   export DEPLOYER_KEY=deployer            # or DEPLOYER_SECRET=S...
#   export BLEND_POOL=C...                  # Blend pool holding $ASSET
#   export BLEND_RESERVE_INDEX=0            # index of $ASSET in that pool
#   bash scripts/deploy-savings-testnet.sh [--testnet-timelock SECONDS]
#
# Optional environment:
#   ASSET           savings asset (default: Blend's testnet USDC, the asset the
#                   Blend testnet pools and the DeFindex testnet vault hold)
#   DEFINDEX_VAULT  DeFindex vault (default: the testnet USDC vault)
#
# Prerequisites: stellar CLI, binaryen or the stellar CLI for wasm-opt,
# rust target wasm32-unknown-unknown.
# =============================================================================
set -euo pipefail

NETWORK="testnet"
RPC_URL="https://soroban-testnet.stellar.org"
NETWORK_PASSPHRASE="Test SDF Network ; September 2015"
HORIZON_URL="https://horizon-testnet.stellar.org"

# Blend's testnet USDC (USDC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56),
# not Circle's testnet USDC.
DEFAULT_ASSET="CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU"
DEFAULT_DEFINDEX_VAULT="CBMVK2JK6NTOT2O4HNQAIQFJY232BHKGLIMXDVQVHIIZKDACXDFZDWHN"

ASSET="${ASSET:-$DEFAULT_ASSET}"
DEFINDEX_VAULT="${DEFINDEX_VAULT:-$DEFAULT_DEFINDEX_VAULT}"
BLEND_POOL="${BLEND_POOL:-}"
BLEND_RESERVE_INDEX="${BLEND_RESERVE_INDEX:-}"

# The core refuses timelocks below this when proposing; keep the same floor.
MIN_TIMELOCK=3600
TESTNET_TIMELOCK=3600

# 1,000 USDC absolute cap (7 decimals), 50% relative cap.
ABS_CAP="10000000000"
REL_CAP_BPS=5000
WASM_MAX_BYTES=102400

WASM_DIR="./target/wasm32-unknown-unknown/release"
OUTPUT_FILE="./scripts/deployed-testnet.env"

# -- Arguments ----------------------------------------------------------------

DEPLOYER_SECRET="${DEPLOYER_SECRET:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --testnet-timelock)
      [ "$#" -ge 2 ] || { echo "error: --testnet-timelock needs a value" >&2; exit 1; }
      TESTNET_TIMELOCK="$2"
      shift 2
      ;;
    --testnet-timelock=*)
      TESTNET_TIMELOCK="${1#*=}"
      shift
      ;;
    S*)
      DEPLOYER_SECRET="$1"
      shift
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

case "$TESTNET_TIMELOCK" in
  ''|*[!0-9]*)
    echo "error: --testnet-timelock must be a whole number of seconds" >&2
    exit 1
    ;;
esac
if [ "$TESTNET_TIMELOCK" -lt "$MIN_TIMELOCK" ]; then
  echo "error: --testnet-timelock must be at least $MIN_TIMELOCK seconds (got $TESTNET_TIMELOCK)" >&2
  exit 1
fi

if [ -z "$BLEND_POOL" ] || [ -z "$BLEND_RESERVE_INDEX" ]; then
  echo "error: set BLEND_POOL and BLEND_RESERVE_INDEX (the position of $ASSET in the pool's reserve list)" >&2
  exit 1
fi
case "$BLEND_RESERVE_INDEX" in
  ''|*[!0-9]*)
    echo "error: BLEND_RESERVE_INDEX must be a non-negative integer" >&2
    exit 1
    ;;
esac

DEPLOYER_KEY="${DEPLOYER_KEY:-}"
if [ -z "$DEPLOYER_KEY" ] && [ -z "$DEPLOYER_SECRET" ]; then
  echo "error: no deployer key provided (set DEPLOYER_KEY=<alias> or DEPLOYER_SECRET=S...)" >&2
  exit 1
fi

if ! command -v stellar &>/dev/null; then
  echo "error: stellar CLI not found" >&2
  exit 1
fi
echo "stellar CLI: $(stellar --version 2>&1 | head -1)"

# -- Deployer identity --------------------------------------------------------

if [ -n "$DEPLOYER_KEY" ]; then
  SOURCE_ACCOUNT="$DEPLOYER_KEY"
else
  IDENTITY_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/stellar/identity"
  mkdir -p "$IDENTITY_DIR"
  printf 'secret_key = "%s"\n' "$DEPLOYER_SECRET" > "$IDENTITY_DIR/_nester_deploy.toml"
  SOURCE_ACCOUNT="_nester_deploy"
fi
DEPLOYER="$(stellar keys address "$SOURCE_ACCOUNT" 2>/dev/null || true)"
if [ -z "$DEPLOYER" ]; then
  echo "error: could not resolve deployer address" >&2
  exit 1
fi
echo "Deployer: $DEPLOYER"

BALANCE="$(curl -sf "${HORIZON_URL}/accounts/${DEPLOYER}" | python3 -c "
import sys,json
data=json.load(sys.stdin)
xlm=[b for b in data.get('balances',[]) if b['asset_type']=='native']
print(xlm[0]['balance'] if xlm else '0')
" 2>/dev/null || echo "0")"
if python3 -c "import sys; exit(0 if float('${BALANCE}') < 10 else 1)" 2>/dev/null; then
  echo "Low balance ($BALANCE XLM): funding from friendbot..."
  curl -sf "https://friendbot.stellar.org?addr=${DEPLOYER}" > /dev/null
fi

# -- Build, size-check and optimize -------------------------------------------

cd "$(dirname "$0")/.."
REPO_ROOT="$(cd ../.. && pwd)"

echo ""
echo "Building contracts..."
cargo build --release --target wasm32-unknown-unknown \
  -p savings-core -p savings-reserve -p adapter-blend-contract -p adapter-defindex-contract

echo "Checking WASM size (limit $WASM_MAX_BYTES bytes)..."
"$REPO_ROOT/scripts/check-wasm-size.sh" "$WASM_MAX_BYTES" \
  "$WASM_DIR/savings_core.wasm" \
  "$WASM_DIR/savings_reserve.wasm" \
  "$WASM_DIR/adapter_blend.wasm" \
  "$WASM_DIR/adapter_defindex.wasm"

echo "Optimizing WASMs..."
for wasm in savings_core savings_reserve adapter_blend adapter_defindex; do
  stellar contract optimize \
    --wasm "$WASM_DIR/${wasm}.wasm" \
    --wasm-out "$WASM_DIR/${wasm}_opt.wasm" >/dev/null
done

# -- Helpers ------------------------------------------------------------------

deploy_contract() {
  local name="$1" wasm="$2" id
  echo "" >&2
  echo "Deploying $name..." >&2
  id="$(stellar contract deploy \
    --wasm "$wasm" \
    --source-account "$SOURCE_ACCOUNT" \
    --network "$NETWORK" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$NETWORK_PASSPHRASE" \
    2>&1 | grep -E '^C[A-Z2-7]{55}$' | tail -1)"
  if [ -z "$id" ]; then
    echo "error: $name deployment failed" >&2
    exit 1
  fi
  echo "$name deployed: $id" >&2
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

# Set KEY=VALUE in the env file, replacing an existing line and keeping every
# other line as is.
set_env() {
  local key="$1" value="$2" tmp
  mkdir -p "$(dirname "$OUTPUT_FILE")"
  touch "$OUTPUT_FILE"
  if grep -q "^${key}=" "$OUTPUT_FILE"; then
    tmp="$(mktemp)"
    awk -v k="$key" -v v="$value" 'BEGIN{p=k"="} index($0,p)==1{print p v; next} {print}' "$OUTPUT_FILE" > "$tmp"
    cat "$tmp" > "$OUTPUT_FILE"
    rm -f "$tmp"
  else
    printf '%s=%s\n' "$key" "$value" >> "$OUTPUT_FILE"
  fi
}

# -- Deploy -------------------------------------------------------------------

CORE_ID="$(deploy_contract "SavingsCore" "$WASM_DIR/savings_core_opt.wasm")"
RESERVE_ID="$(deploy_contract "SavingsReserve" "$WASM_DIR/savings_reserve_opt.wasm")"
BLEND_ADAPTER_ID="$(deploy_contract "AdapterBlend" "$WASM_DIR/adapter_blend_opt.wasm")"
DEFINDEX_ADAPTER_ID="$(deploy_contract "AdapterDefindex" "$WASM_DIR/adapter_defindex_opt.wasm")"

# -- Initialize ---------------------------------------------------------------

echo ""
echo "Initializing SavingsReserve (core = $CORE_ID)..."
invoke "$RESERVE_ID" initialize --core "$CORE_ID"

echo ""
echo "Initializing SavingsCore (timelock ${TESTNET_TIMELOCK}s)..."
invoke "$CORE_ID" initialize \
  --owner "$DEPLOYER" \
  --asset "$ASSET" \
  --reserve "$RESERVE_ID" \
  --name "Nester Savings USDC" \
  --symbol "nsUSDC" \
  --timelock "$TESTNET_TIMELOCK"

echo ""
echo "Initializing AdapterBlend..."
invoke "$BLEND_ADAPTER_ID" initialize \
  --vault "$CORE_ID" \
  --pool "$BLEND_POOL" \
  --underlying "$ASSET" \
  --reserve_index "$BLEND_RESERVE_INDEX"

echo ""
echo "Initializing AdapterDefindex..."
invoke "$DEFINDEX_ADAPTER_ID" initialize \
  --core "$CORE_ID" \
  --defindex_vault "$DEFINDEX_VAULT" \
  --underlying "$ASSET"

# -- Propose adapters ---------------------------------------------------------

propose_add_adapter() {
  local adapter="$1" id
  id="$(invoke "$CORE_ID" propose \
    --caller "$DEPLOYER" \
    --change "{\"AddAdapter\":[\"${adapter}\",\"${ABS_CAP}\",${REL_CAP_BPS}]}" \
    | tr -d '"[:space:]')"
  case "$id" in
    ''|*[!0-9]*)
      echo "error: could not read the pending change id for $adapter (got: $id)" >&2
      exit 1
      ;;
  esac
  echo "$id"
}

echo ""
echo "Proposing AddAdapter for both adapters (abs cap 1,000 USDC, rel cap ${REL_CAP_BPS} bps)..."
BLEND_CHANGE_ID="$(propose_add_adapter "$BLEND_ADAPTER_ID")"
DEFINDEX_CHANGE_ID="$(propose_add_adapter "$DEFINDEX_ADAPTER_ID")"
PROPOSED_AT="$(date +%s)"
EARLIEST_EXECUTE="$(date -u -d "@$((PROPOSED_AT + TESTNET_TIMELOCK))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
  || date -u -r "$((PROPOSED_AT + TESTNET_TIMELOCK))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
  || echo "proposal time + ${TESTNET_TIMELOCK}s")"

# -- Write output -------------------------------------------------------------

echo ""
echo "Writing contract IDs to $OUTPUT_FILE..."
set_env NEXT_PUBLIC_SAVINGS_CORE_ID "$CORE_ID"
set_env NEXT_PUBLIC_SAVINGS_RESERVE_ID "$RESERVE_ID"
set_env NEXT_PUBLIC_ADAPTER_BLEND_ID "$BLEND_ADAPTER_ID"
set_env NEXT_PUBLIC_ADAPTER_DEFINDEX_ID "$DEFINDEX_ADAPTER_ID"

echo ""
echo "=========================================="
echo "SAVINGS DEPLOYMENT COMPLETE"
echo "=========================================="
echo "  Core:             $CORE_ID"
echo "  Reserve:          $RESERVE_ID"
echo "  Adapter (Blend):  $BLEND_ADAPTER_ID"
echo "  Adapter (DeFindex): $DEFINDEX_ADAPTER_ID"
echo "  Asset:            $ASSET"
echo "  Timelock:         ${TESTNET_TIMELOCK}s"
echo ""
echo "Pending changes: Blend=$BLEND_CHANGE_ID, DeFindex=$DEFINDEX_CHANGE_ID"
echo "Earliest execute time (UTC): $EARLIEST_EXECUTE"
echo "Anyone may execute after that time:"
echo "  stellar contract invoke --id $CORE_ID --source-account $SOURCE_ACCOUNT --network $NETWORK -- execute --id $BLEND_CHANGE_ID"
echo "  stellar contract invoke --id $CORE_ID --source-account $SOURCE_ACCOUNT --network $NETWORK -- execute --id $DEFINDEX_CHANGE_ID"
echo ""
echo "Contract IDs saved to: $OUTPUT_FILE"
