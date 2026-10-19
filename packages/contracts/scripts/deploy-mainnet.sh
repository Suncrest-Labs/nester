#!/usr/bin/env bash
# =============================================================================
# Nester Soroban Mainnet Deployment Script
#
# Deploys and initializes the savings-layer contracts on Stellar mainnet, then
# hardens them for a guarded launch:
#   - per-vault deposit cap (set_max_deposit)
#   - deposit allowlist gate enabled (set_deposit_allowlist_enabled)
#   - admin ownership proposed to ADMIN_ADDRESS (two-step transfer_admin)
#
# Real funds: this script refuses to run unless every input is explicit.
#
# Required environment:
#   DEPLOYER_KEY         stellar-cli key alias (never a secret key)
#   ADMIN_ADDRESS        multisig/timelock G-address that will own the contracts;
#                        must differ from the deployer
#   USDC_CONTRACT        mainnet USDC Stellar Asset Contract id (C...)
#   XLM_CONTRACT         mainnet native XLM Stellar Asset Contract id (C...)
#   MAX_DEPOSIT_STROOPS  initial per-deposit cap, in 7-decimal base units
#   MAINNET_RPC_URL      https Soroban RPC endpoint from your provider; SDF does not
#                        run a public mainnet RPC, so there is deliberately no default
#   TVL_CAP_STROOPS      initial cap on total vault assets, same units; bounds
#                        total exposure (MAX_DEPOSIT only bounds one deposit)
#
# Optional:
#   DRY_RUN=1 or --dry-run   print every action, execute nothing, skip the build
#   DEPLOYER_ADDRESS         only honoured with DRY_RUN, to avoid a key lookup
#
# Prerequisites:
#   cargo install stellar-cli --features opt
# =============================================================================
set -euo pipefail

NETWORK="mainnet"
NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
WASM_DIR="./target/wasm32-unknown-unknown/release"
OUTPUT_FILE="./scripts/deployed-mainnet.env"

die() { echo "ERROR: $*" >&2; exit 1; }

DRY_RUN="${DRY_RUN:-}"
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    S*) die "Pass a key alias via DEPLOYER_KEY, not a secret key." ;;
    *) die "Unknown argument: $arg" ;;
  esac
done

DEPLOYER_KEY="${DEPLOYER_KEY:-}"
[ -n "$DEPLOYER_KEY" ] || die "DEPLOYER_KEY (a stellar-cli key alias) is required."
ADMIN_ADDRESS="${ADMIN_ADDRESS:-}"
[ -n "$ADMIN_ADDRESS" ] || die "ADMIN_ADDRESS is required (multisig/timelock, never the deployer)."
[[ "$ADMIN_ADDRESS" == G* ]] || die "ADMIN_ADDRESS must be a G... account address."
USDC_CONTRACT="${USDC_CONTRACT:-}"
[[ "$USDC_CONTRACT" == C* ]] || die "USDC_CONTRACT must be the mainnet USDC contract id (C...)."
XLM_CONTRACT="${XLM_CONTRACT:-}"
[[ "$XLM_CONTRACT" == C* ]] || die "XLM_CONTRACT must be the mainnet XLM contract id (C...)."
MAX_DEPOSIT_STROOPS="${MAX_DEPOSIT_STROOPS:-}"
[[ "$MAX_DEPOSIT_STROOPS" =~ ^[1-9][0-9]*$ ]] || die "MAX_DEPOSIT_STROOPS must be a positive integer; an unbounded cap is not allowed on mainnet."
MAINNET_RPC_URL="${MAINNET_RPC_URL:-}"
[[ "$MAINNET_RPC_URL" == https://* ]] || die "MAINNET_RPC_URL must be an https:// Soroban RPC endpoint from your provider (there is no public default)."
RPC_URL="$MAINNET_RPC_URL"
TVL_CAP_STROOPS="${TVL_CAP_STROOPS:-}"
[[ "$TVL_CAP_STROOPS" =~ ^[1-9][0-9]*$ ]] || die "TVL_CAP_STROOPS must be a positive integer; unbounded total exposure is not allowed on mainnet."

if [ -n "$DRY_RUN" ]; then
  DEPLOYER="${DEPLOYER_ADDRESS:-$(stellar keys address "$DEPLOYER_KEY" 2>/dev/null || true)}"
else
  command -v stellar >/dev/null || die "stellar CLI not found."
  DEPLOYER="$(stellar keys address "$DEPLOYER_KEY" 2>/dev/null || true)"
fi
[ -n "$DEPLOYER" ] || die "Could not resolve the deployer address for key alias $DEPLOYER_KEY."
[ "$DEPLOYER" != "$ADMIN_ADDRESS" ] || die "admin must differ from deployer."

echo "Network:     $NETWORK ($NETWORK_PASSPHRASE)"
echo "Deployer:    $DEPLOYER"
echo "Admin:       $ADMIN_ADDRESS"
echo "Max deposit: $MAX_DEPOSIT_STROOPS"
echo "TVL cap:     $TVL_CAP_STROOPS"
[ -z "$DRY_RUN" ] || echo "DRY RUN: nothing will be sent to the network."

if [ -z "$DRY_RUN" ]; then
  read -r -p "Type DEPLOY MAINNET to continue: " ok
  [ "$ok" = "DEPLOY MAINNET" ] || die "Aborted."
  cd "$(dirname "$0")/.."
  cargo build --release --target wasm32-unknown-unknown -p vault-token-contract
  for pkg in vault-contract treasury-contract yield-registry-contract allocation-strategy-contract nester-contract; do
    cargo build --release --target wasm32-unknown-unknown -p "$pkg"
  done
  for wasm in vault_token vault_contract treasury_contract yield_registry_contract allocation_strategy_contract nester_contract; do
    stellar contract optimize --wasm "$WASM_DIR/${wasm}.wasm" --wasm-out "$WASM_DIR/${wasm}_opt.wasm"
  done
  # Refuse to deploy anything the network would reject for size.
  bash ./scripts/check-wasm-size.sh "$WASM_DIR" || die "A contract exceeds the mainnet size limit; nothing was deployed."
fi

STELLAR_ARGS=(--source-account "$DEPLOYER_KEY" --network "$NETWORK" --rpc-url "$RPC_URL" --network-passphrase "$NETWORK_PASSPHRASE")

deploy_contract() {
  local name="$1" wasm="$2"
  echo "Deploying $name" >&2
  if [ -n "$DRY_RUN" ]; then
    echo "  stellar contract deploy --wasm $wasm ${STELLAR_ARGS[*]}" >&2
    echo "C_DRYRUN_${name}"
    return
  fi
  local id
  id=$(stellar contract deploy --wasm "$wasm" "${STELLAR_ARGS[@]}" 2>/dev/null | tail -1)
  [[ "$id" == C* ]] || die "$name deployment failed"
  echo "  $id" >&2
  echo "$id"
}

invoke() {
  local contract_id="$1"
  shift
  if [ -n "$DRY_RUN" ]; then
    echo "  invoke $contract_id -- $*"
    return
  fi
  stellar contract invoke --id "$contract_id" "${STELLAR_ARGS[@]}" -- "$@"
}

VAULT_TOKEN_USDC_ID=$(deploy_contract "VaultToken_USDC" "$WASM_DIR/vault_token_opt.wasm")
VAULT_TOKEN_XLM_ID=$(deploy_contract "VaultToken_XLM" "$WASM_DIR/vault_token_opt.wasm")
VAULT_USDC_ID=$(deploy_contract "Vault_USDC" "$WASM_DIR/vault_contract_opt.wasm")
VAULT_XLM_ID=$(deploy_contract "Vault_XLM" "$WASM_DIR/vault_contract_opt.wasm")
TREASURY_ID=$(deploy_contract "Treasury" "$WASM_DIR/treasury_contract_opt.wasm")
YIELD_REGISTRY_ID=$(deploy_contract "YieldRegistry" "$WASM_DIR/yield_registry_contract_opt.wasm")
ALLOCATION_STRATEGY_ID=$(deploy_contract "AllocationStrategy" "$WASM_DIR/allocation_strategy_contract_opt.wasm")
NESTER_ID=$(deploy_contract "Nester" "$WASM_DIR/nester_contract_opt.wasm")

echo "Initializing"
invoke "$VAULT_TOKEN_USDC_ID" initialize --vault "$VAULT_USDC_ID" --name "Nester USDC Vault Token" --symbol "nUSDC" --decimals 7
invoke "$VAULT_TOKEN_XLM_ID" initialize --vault "$VAULT_XLM_ID" --name "Nester XLM Vault Token" --symbol "nXLM" --decimals 7
invoke "$TREASURY_ID" initialize --admin "$DEPLOYER" --vault "$VAULT_USDC_ID"
invoke "$VAULT_USDC_ID" initialize --admin "$DEPLOYER" --token_address "$USDC_CONTRACT" --vault_token_address "$VAULT_TOKEN_USDC_ID" --treasury "$TREASURY_ID"
invoke "$VAULT_XLM_ID" initialize --admin "$DEPLOYER" --token_address "$XLM_CONTRACT" --vault_token_address "$VAULT_TOKEN_XLM_ID" --treasury "$TREASURY_ID"
invoke "$YIELD_REGISTRY_ID" initialize --admin "$DEPLOYER"
invoke "$ALLOCATION_STRATEGY_ID" initialize --admin "$DEPLOYER" --registry_id "$YIELD_REGISTRY_ID"
NESTER_CONFIG="{\"vault_usdc\":\"$VAULT_USDC_ID\",\"vault_xlm\":\"$VAULT_XLM_ID\",\"vault_token_usdc\":\"$VAULT_TOKEN_USDC_ID\",\"vault_token_xlm\":\"$VAULT_TOKEN_XLM_ID\",\"treasury\":\"$TREASURY_ID\",\"yield_registry\":\"$YIELD_REGISTRY_ID\",\"allocation_strategy\":\"$ALLOCATION_STRATEGY_ID\"}"
invoke "$NESTER_ID" initialize --admin "$DEPLOYER" --config "$NESTER_CONFIG"

echo "Guarded-launch limits"
for vault in "$VAULT_USDC_ID" "$VAULT_XLM_ID"; do
  invoke "$vault" set_max_deposit --caller "$DEPLOYER" --amount "$MAX_DEPOSIT_STROOPS"
  invoke "$vault" set_tvl_cap --caller "$DEPLOYER" --amount "$TVL_CAP_STROOPS"
  invoke "$vault" set_deposit_allowlist_enabled --caller "$DEPLOYER" --enabled true
done

ADMIN_CONTRACTS=("$VAULT_USDC_ID" "$VAULT_XLM_ID" "$TREASURY_ID" "$YIELD_REGISTRY_ID" "$ALLOCATION_STRATEGY_ID" "$NESTER_ID")

echo "Proposing admin ownership to $ADMIN_ADDRESS (step 1 of 2)"
for contract in "${ADMIN_CONTRACTS[@]}"; do
  invoke "$contract" transfer_admin --current_admin "$DEPLOYER" --new_admin "$ADMIN_ADDRESS"
done

DEPLOY_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
DEPLOY_TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
wasm_sha() { sha256sum "$WASM_DIR/$1" 2>/dev/null | awk '{print $1}'; }

# The variable names are the canonical ones the dapp reads (see
# apps/dapp/frontend/lib/contracts.ts) and the testnet deploy script writes.
render_env() {
  cat <<ENV
# Nester Mainnet Contract IDs - generated by deploy-mainnet.sh
#
# Deployed from commit: $DEPLOY_COMMIT
# Deployed at (UTC):    $DEPLOY_TIMESTAMP
# Verify with packages/contracts/scripts/VERIFY.md.
#
# wasm sha256 (optimized build):
#   nester_contract_opt.wasm:              $(wasm_sha nester_contract_opt.wasm)
#   vault_contract_opt.wasm:               $(wasm_sha vault_contract_opt.wasm)
#   vault_token_opt.wasm:                  $(wasm_sha vault_token_opt.wasm)
#   treasury_contract_opt.wasm:            $(wasm_sha treasury_contract_opt.wasm)
#   yield_registry_contract_opt.wasm:      $(wasm_sha yield_registry_contract_opt.wasm)
#   allocation_strategy_contract_opt.wasm: $(wasm_sha allocation_strategy_contract_opt.wasm)

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
NEXT_PUBLIC_ADMIN_ADDRESS=$ADMIN_ADDRESS
ENV
}

if [ -n "$DRY_RUN" ]; then
  echo "Dry run: would write $OUTPUT_FILE:"
  render_env
  exit 0
fi

render_env > "$OUTPUT_FILE"

echo ""
echo "Deployment complete. IDs written to $OUTPUT_FILE"
echo "REQUIRED next step: $ADMIN_ADDRESS must accept ownership of each contract:"
for contract in "${ADMIN_CONTRACTS[@]}"; do
  echo "  stellar contract invoke --id $contract --network mainnet -- accept_admin --new_admin $ADMIN_ADDRESS"
done
echo "Until then the deployer still holds admin."
