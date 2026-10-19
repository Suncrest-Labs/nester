#!/usr/bin/env bash
# Guard tests for deploy-mainnet.sh. Needs no network or funds: every case
# either fails a guard before touching the chain, or runs with --dry-run.
set -uo pipefail
cd "$(dirname "$0")"

SCRIPT=./deploy-mainnet.sh
DEPLOYER=GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
ADMIN=GBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
USDC=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC
XLM=CDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD
BASE_ENV=(DEPLOYER_KEY=mainnet-deployer DEPLOYER_ADDRESS="$DEPLOYER" USDC_CONTRACT="$USDC" XLM_CONTRACT="$XLM" MAX_DEPOSIT_STROOPS=1000000000 TVL_CAP_STROOPS=100000000000 MAINNET_RPC_URL=https://rpc.example.test)

fail=0
check() {
  local name="$1" want_rc="$2" want_text="$3"; shift 3
  local out rc
  out=$(env "$@" bash "$SCRIPT" 2>&1); rc=$?
  if [ "$rc" -ne "$want_rc" ] || ! grep -qF -- "$want_text" <<<"$out"; then
    echo "FAIL: $name (rc=$rc, wanted rc=$want_rc and text: $want_text)"; echo "$out" | tail -5
    fail=1
  else
    echo "ok:   $name"
  fi
}

check "rejects a secret key argument" 1 "key alias" \
  "${BASE_ENV[@]}" ADMIN_ADDRESS="$ADMIN" bash "$SCRIPT" SNOT-A-REAL-KEY
check "requires ADMIN_ADDRESS" 1 "ADMIN_ADDRESS" \
  "${BASE_ENV[@]}" ADMIN_ADDRESS= DRY_RUN=1
check "refuses admin equal to deployer" 1 "admin must differ from deployer" \
  "${BASE_ENV[@]}" ADMIN_ADDRESS="$DEPLOYER" DRY_RUN=1
check "requires MAX_DEPOSIT_STROOPS" 1 "MAX_DEPOSIT_STROOPS" \
  DEPLOYER_KEY=mainnet-deployer DEPLOYER_ADDRESS="$DEPLOYER" USDC_CONTRACT="$USDC" XLM_CONTRACT="$XLM" ADMIN_ADDRESS="$ADMIN" DRY_RUN=1
check "requires MAINNET_RPC_URL" 1 "MAINNET_RPC_URL" \
  DEPLOYER_KEY=mainnet-deployer DEPLOYER_ADDRESS="$DEPLOYER" USDC_CONTRACT="$USDC" XLM_CONTRACT="$XLM" ADMIN_ADDRESS="$ADMIN" MAX_DEPOSIT_STROOPS=1000000000 TVL_CAP_STROOPS=100000000000 DRY_RUN=1
check "rejects a non-https MAINNET_RPC_URL" 1 "https" \
  "${BASE_ENV[@]}" MAINNET_RPC_URL=http://rpc.example.test ADMIN_ADDRESS="$ADMIN" DRY_RUN=1
check "requires TVL_CAP_STROOPS" 1 "TVL_CAP_STROOPS" \
  DEPLOYER_KEY=mainnet-deployer DEPLOYER_ADDRESS="$DEPLOYER" USDC_CONTRACT="$USDC" XLM_CONTRACT="$XLM" ADMIN_ADDRESS="$ADMIN" MAX_DEPOSIT_STROOPS=1000000000 MAINNET_RPC_URL=https://rpc.example.test DRY_RUN=1
check "refuses a non-contract USDC id" 1 "USDC_CONTRACT" \
  "${BASE_ENV[@]}" USDC_CONTRACT=GNOTACONTRACT ADMIN_ADDRESS="$ADMIN" DRY_RUN=1

out=$(env "${BASE_ENV[@]}" ADMIN_ADDRESS="$ADMIN" DRY_RUN=1 bash "$SCRIPT" 2>&1); rc=$?
[ "$rc" -eq 0 ] || { echo "FAIL: dry run rc=$rc"; echo "$out" | tail -8; fail=1; }
for needle in \
  "Public Global Stellar Network ; September 2015" \
  "set_max_deposit" \
  "set_tvl_cap" \
  "--rpc-url https://rpc.example.test" \
  "set_deposit_allowlist_enabled" \
  "transfer_admin" \
  "--new_admin $ADMIN" \
  '--config {"vault_usdc":"C_DRYRUN_Vault_USDC"' \
  "invoke C_DRYRUN_Treasury -- transfer_admin" \
  "NEXT_PUBLIC_VAULT_CONTRACT_ID=C_DRYRUN_Vault_USDC" \
  "NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID=C_DRYRUN_Vault_XLM" \
  "NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ID=C_DRYRUN_VaultToken_USDC" \
  "NEXT_PUBLIC_USDC_CONTRACT_ID=$USDC" \
  "NEXT_PUBLIC_ADMIN_ADDRESS=$ADMIN"; do
  grep -qF -- "$needle" <<<"$out" && echo "ok:   dry run contains: $needle" \
    || { echo "FAIL: dry run missing: $needle"; fail=1; }
done
grep -q "NEXT_PUBLIC_MAINNET_" <<<"$out" && { echo "FAIL: env uses non-canonical NEXT_PUBLIC_MAINNET_* names the dapp does not read"; fail=1; } || echo "ok:   env uses the canonical names the dapp reads"
grep -qE "testnet|friendbot" <<<"$out" && { echo "FAIL: dry run mentions testnet/friendbot"; fail=1; } || echo "ok:   dry run never touches testnet"

exit "$fail"
