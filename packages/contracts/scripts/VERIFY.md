# Verifying a deployed testnet contract

`deploy-testnet.sh` records, at the top of `deployed-testnet.env`, the git
commit and the wasm sha256 of each optimized `.wasm` it deployed. Use those
to verify a deployed contract still matches source.

## Verify a contract's on-chain wasm matches this commit

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

## Redeploying

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
