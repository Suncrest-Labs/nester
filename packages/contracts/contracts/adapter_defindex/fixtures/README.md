# DeFindex fixtures

| File | Contents | SHA-256 |
|---|---|---|
| `defindex_vault.spec.xdr` | The raw `contractspecv0` custom section (concatenated `ScSpecEntry` XDR) of the deployed testnet DeFindex vault, and nothing else | `2047f6aed99cdc57a95efc0416fd1bc8068a936ab14748b607d9c12b190c89b8` |

## Original

- Contract: testnet vault `CBMVK2JK6NTOT2O4HNQAIQFJY232BHKGLIMXDVQVHIIZKDACXDFZDWHN`
  (`usdc_paltalabs_vault`), fetched with `stellar contract fetch --network testnet`
  on 2026-10-30.
- WASM SHA-256: `f345228dca59c6605789620e9ec62ff4847a0927c33dac7581a955fe746016be`. This
  equals the `defindex_vault` hash in
  [defindex-io/stellar-contracts](https://github.com/defindex-io/stellar-contracts)
  `public/testnet.contracts.json` at commit `529f7c5006387e8c59c8d68014ff87761b8a3490`.
  The source is `vault/` in that repo. It is built for protocol 22 (`rssdkver` 22.0.6).
- DeFindex is GPL-3.0. The WASM itself is not vendored. Only its interface description
  is kept here.

`src/spec_test.rs` parses this file with `soroban_spec::read::parse_raw`. It checks that
`MockDefindexVault`'s `deposit`, `withdraw`, `get_asset_amounts_per_shares`, `balance`
and `total_supply`, and the structs in `deposit`'s return type, match the vault exactly.

The vault's single asset is Blend's testnet USDC
`CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU`
(`USDC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56`). On testnet, the
savings core is deployed with this asset (R22).

## Refresh

1. Fetch the WASM outside the repo:
   `stellar contract fetch --id <vault> --network testnet -o /tmp/vault.wasm`.
2. Copy the bytes of its `contractspecv0` custom section into `defindex_vault.spec.xdr`.
   `soroban_spec::read::raw_from_wasm` returns exactly these bytes.
3. Update both checksums above.
4. Run `cargo test -p adapter-defindex-contract spec_test`.
