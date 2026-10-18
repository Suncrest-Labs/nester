# Verifying Deployed Testnet & Mainnet Contracts

`deploy-testnet.sh` and `deploy-mainnet.sh` record the audited git commit,
contract IDs, deployment transaction hashes, and SHA-256 digests of each
optimized `.wasm` artifact in `deployed-testnet.env` and `deployed-mainnet.env`.
Anyone outside the team can use these manifests to independently verify that
every on-chain contract runs the exact audited source code.

---

## 1. Verify a Mainnet Contract (`deployed-mainnet.env`)

### Step 1: Load the Mainnet Manifest and Check Out the Audited Commit

```bash
source packages/contracts/scripts/deployed-mainnet.env
git checkout "$AUDITED_GIT_COMMIT"
```

Confirm `git rev-parse HEAD` equals `$AUDITED_GIT_COMMIT`.

### Step 2: Run a Reproducible Build of All Soroban Contracts

Using the pinned Rust toolchain (`1.84.1`, `wasm32-unknown-unknown` target) and `stellar contract optimize`:

```bash
cd packages/contracts
make build
```

For each contract crate (`vault_token_contract`, `vault_contract`, `yield_registry_contract`, `allocation_strategy_contract`, `treasury_contract`, `nester_access_control`), optimize the `.wasm`:

```bash
for wasm in target/wasm32-unknown-unknown/release/*.wasm; do
  stellar contract optimize --wasm "$wasm"
done
```

### Step 3: Compare Local Rebuilt WASM Hashes Against `deployed-mainnet.env`

```bash
sha256sum target/wasm32-unknown-unknown/release/vault_contract.optimized.wasm
# Must equal $VAULT_WASM_SHA256

sha256sum target/wasm32-unknown-unknown/release/vault_token_contract.optimized.wasm
# Must equal $VAULT_TOKEN_WASM_SHA256

sha256sum target/wasm32-unknown-unknown/release/yield_registry_contract.optimized.wasm
# Must equal $YIELD_REGISTRY_WASM_SHA256

sha256sum target/wasm32-unknown-unknown/release/allocation_strategy_contract.optimized.wasm
# Must equal $ALLOCATION_STRATEGY_WASM_SHA256

sha256sum target/wasm32-unknown-unknown/release/treasury_contract.optimized.wasm
# Must equal $TREASURY_WASM_SHA256
```

### Step 4: Fetch On-Chain Mainnet Bytecode and Compare Digests

Fetch the live WASM binary directly from Stellar Mainnet for each contract ID in `deployed-mainnet.env` and hash it:

```bash
mkdir -p /tmp/nester-mainnet-verify

for pair in \
  "VAULT:$NEXT_PUBLIC_MAINNET_VAULT_CONTRACT:$VAULT_WASM_SHA256" \
  "VAULT_TOKEN:$NEXT_PUBLIC_MAINNET_VAULT_TOKEN_CONTRACT:$VAULT_TOKEN_WASM_SHA256" \
  "YIELD_REGISTRY:$NEXT_PUBLIC_MAINNET_YIELD_REGISTRY_CONTRACT:$YIELD_REGISTRY_WASM_SHA256" \
  "ALLOCATION_STRATEGY:$NEXT_PUBLIC_MAINNET_ALLOCATION_STRATEGY_CONTRACT:$ALLOCATION_STRATEGY_WASM_SHA256" \
  "TREASURY:$NEXT_PUBLIC_MAINNET_TREASURY_CONTRACT:$TREASURY_WASM_SHA256" \
  "ACCESS_CONTROL:$NEXT_PUBLIC_MAINNET_ACCESS_CONTROL_CONTRACT:$ACCESS_CONTROL_WASM_SHA256"
do
  IFS=":" read -r name contract_id expected_sha <<< "$pair"
  stellar contract fetch --id "$contract_id" --network mainnet --out-file "/tmp/nester-mainnet-verify/${name}.wasm"
  actual_sha=$(sha256sum "/tmp/nester-mainnet-verify/${name}.wasm" | awk '{print $1}')
  echo "${name} (${contract_id}): onchain=${actual_sha} expected=${expected_sha}"
  test "$actual_sha" = "$expected_sha"
done
```

### Step 5: Verify Deployment Transaction Provenance on Horizon

```bash
curl -sf "https://horizon.stellar.org/transactions/${VAULT_DEPLOY_TX_HASH}" | jq '{hash, successful, ledger_attr, created_at, source_account}'
```

---

## 2. Verify a Testnet Contract (`deployed-testnet.env`)

1. Check out the recorded commit:
   ```bash
   git checkout <commit-from-deployed-testnet.env>
   ```
2. Rebuild the same way the deploy script did:
   ```bash
   make build
   ```
3. Hash the rebuilt wasm and compare to the recorded value:
   ```bash
   sha256sum target/wasm32-unknown-unknown/release/nester_contract_opt.wasm
   ```
4. Compare against the on-chain wasm hash for the deployed contract ID:
   ```bash
   stellar contract fetch --id <CONTRACT_ID> --network testnet --out-file /tmp/onchain.wasm
   sha256sum /tmp/onchain.wasm
   ```
   The two sha256 values (rebuilt-from-source vs. fetched-from-chain) must match.

## 3. Redeploying vs Upgrading

Redeploying via `deploy-testnet.sh` deploys **new** contract instances with
new contract IDs — it does not upgrade the existing ones in place. Existing
deployed contracts and their on-chain state are untouched; only
`deployed-testnet.env` is overwritten with the new IDs, so anything still
pointing at the old IDs (a running frontend, an indexer, a user's saved
contract reference) keeps working against the old deployment until it's
manually updated to the new one.

To upgrade an *existing* deployed contract in place instead of deploying a
new one, use the `propose_upgrade`/`execute_upgrade` flow printed at the end
of `deploy-testnet.sh`'s output, not a redeploy.
