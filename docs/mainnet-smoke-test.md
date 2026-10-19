# Mainnet smoke test (manual, real funds)

Run once after the mainnet deploy and again after any contract or config
change, before raising the deposit cap. It moves real money, so use a small
amount (1 USDC) from an account you control. The automated testnet smoke
(`docs/smoke-test-runbook.md`) stays the regression gate; this checks that the
mainnet deployment itself is wired correctly.

Stop at the first step that does not match and treat it as a failed launch
check. Do not raise caps or announce anything until every step passes.

## 0. Preconditions

- `packages/contracts/scripts/deployed-mainnet.env` is committed and its commit
  hash is the audited one.
- Ownership handover is complete: for every contract in that file the multisig
  has run `accept_admin`, and the deployer account no longer holds admin.
  The treasury exposes `has_role`: `stellar contract invoke --network mainnet --id <TREASURY> -- has_role --account <DEPLOYER> --role Admin`
  must return `false` and the same call for the multisig `true`. The vault has
  no read entrypoint for roles, so check it by simulation: an admin-only call
  such as `set_max_deposit --caller <DEPLOYER> --amount 1` with `--send=no`
  must fail with `Error(Contract, #3)` (Unauthorized), because the deployer no
  longer holds Admin.
- The vault caps are set and the allowlist is on:
  - `get_max_deposit` and `get_tvl_cap` return your intended numbers, not
    `170141183460469231731687303715884105727` (that is "unlimited").
  - `deposit_allowlist_enabled` returns `true`, and your test account is
    allowlisted (`is_deposit_allowlisted`).
- The API starts against mainnet config. It refuses to start if the passphrase
  and RPC/Horizon URLs disagree, or if `STELLAR_MAINNET_VAULT_TVL_CAP` is not
  positive. `GET /health` returns 200.
- The dapp build has `NEXT_PUBLIC_STELLAR_NETWORK` set to the mainnet
  passphrase and the canonical `NEXT_PUBLIC_*_CONTRACT_ID` values from
  `deployed-mainnet.env`.

## 1. Read-only checks (no funds)

| Check | Expected |
| --- | --- |
| `GET /api/v1/money-path/status` | both operations `paused: false` |
| Dapp loads on mainnet, wallet connects, network is Mainnet | no network-mismatch error |
| Select Testnet in the network selector | deposit refuses with "Testnet is not available in this deployment" |
| Indexer lag metric (`nester_indexer_lag_ledgers`) | present and not climbing |

## 2. Deposit

1. Deposit 1 USDC from the allowlisted test account.
2. The quoted shares match the shares received (the quote comes from the
   contract's `preview_deposit`).
3. The vault's `total_assets` rises by exactly the amount deposited.
4. The API shows the position within the indexer's staleness budget (5 minutes).

## 3. Rejections work (no funds lost)

| Attempt | Expected |
| --- | --- |
| Deposit from an account that is not allowlisted | refused with the early-access message |
| Deposit above `get_max_deposit` | refused with the deposit-limit message |
| Deposit that would push total assets past `get_tvl_cap` | refused with the deposit-limit message |

## 4. Pause drill

1. Engage the global deposit pause (`PUT /api/v1/admin/money-path/switches/deposit`
   with `{"paused": true, "reason": "smoke test"}`).
2. Within 30 seconds the dapp shows "Deposits are temporarily paused: smoke test"
   and Confirm is disabled. Withdrawals are unaffected.
3. Release it (`"paused": false`) and confirm the notice clears.
4. The audit log has two entries naming the actor.

## 5. Withdraw

1. Withdraw the position. An early-exit fee applies inside the vault's minimum
   lock period; the quote must show it before you sign.
2. Net received equals the quoted net amount, to the stroop.
3. Balances reconcile: vault `total_assets`, the indexer's view, and the API
   position all agree.

## 6. Record the result

Paste the transaction hashes, the cap values read in step 0, and the pass/fail
of each step into the launch-readiness register as evidence for the mainnet
rows in `docs/LAUNCH_GO_NO_GO.md`.
