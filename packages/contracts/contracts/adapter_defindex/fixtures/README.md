# DeFindex fixtures

| File | Source | SHA-256 |
|---|---|---|
| `defindex_vault.wasm` | Deployed testnet vault `CBMVK2JK6NTOT2O4HNQAIQFJY232BHKGLIMXDVQVHIIZKDACXDFZDWHN` (`usdc_paltalabs_vault`), fetched with `stellar contract fetch --network testnet` on 2026-10-30 | `f345228dca59c6605789620e9ec62ff4847a0927c33dac7581a955fe746016be` |

The hash equals the `defindex_vault` hash in
[defindex-io/stellar-contracts](https://github.com/defindex-io/stellar-contracts)
`public/testnet.contracts.json` (commit `529f7c5006387e8c59c8d68014ff87761b8a3490`),
whose source is `vault/` in that repo. DeFindex is GPL-3.0; the WASM is
vendored unmodified and only its contract spec is read.

It is built for protocol 22 (`rssdkver` 22.0.6), so it cannot be registered
in this workspace's protocol-21 host. `src/spec_test.rs` reads its contract
spec and checks that `MockDefindexVault`'s `deposit`, `withdraw`,
`get_asset_amounts_per_shares`, `balance` and `total_supply`, and the structs
in `deposit`'s return type, match it exactly.

The testnet vault's single asset is Blend's testnet USDC
`CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU`
(`USDC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56`), not the
Circle testnet USDC SAC the rest of Nester's testnet deployment uses.

To refresh: `stellar contract fetch --id <vault> --network testnet -o defindex_vault.wasm`,
update the checksum above, and re-run `cargo test -p adapter-defindex-contract spec_test`.
