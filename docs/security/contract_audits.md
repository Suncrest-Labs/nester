# Independent Third-Party Audit: Vault & Adapter Contracts (Issue #1133)

## Status

**Not yet commissioned.** No third-party audit engagement exists today, no
auditor has been selected, and no RFP has gone out. This document defines
the process to follow once an audit is scheduled, and the checklist to work
through before it starts. It is a readiness template, not a record of
completed work.

**Launch gate**: Mainnet deployment is a hard blocker until a third-party
audit has actually been completed and every critical/high finding has been
resolved and verified on testnet. That gate cannot be satisfied by this
document; it requires a real, completed engagement.

## Target scope (proposed)

The canonical list of contracts and adapters that ship on mainnet (along with
their WASM size budgets and audited commit hashes) is defined in
[`packages/contracts/launch-manifest.toml`](../../packages/contracts/launch-manifest.toml).
The mainnet deploy script, size budget gate, and audit pipeline all read from
that manifest and reject any crate not listed in its `[contracts]` or
`[adapters]` sections.

- **Core Mainnet Contracts**:
  - `vault` (`packages/contracts/contracts/vault`): deposit/withdrawal
    accounting, share pricing invariants, pause/emergency mechanics, fee
    collection, upgrade timelock integration.
  - `vault_token` (`packages/contracts/contracts/vault_token`): SEP-41 vault
    share token mint/burn, transfer, and allowance accounting.
  - `vault_factory` (`packages/contracts/contracts/vault_factory`): vault
    instance deployment and registry wiring.
  - `treasury` (`packages/contracts/contracts/treasury`): protocol fee custody
    and distribution.
  - `yield_registry` (`packages/contracts/contracts/yield_registry`) &
    `allocation_strategy` (`packages/contracts/contracts/allocation_strategy`):
    yield source registration and capital allocation weights.
  - `nester` (`packages/contracts/contracts/nester`): protocol orchestrator and
    address directory.
  - `access_control` (`packages/contracts/contracts/access_control`) &
    `timelock` (`packages/contracts/contracts/timelock`): role-based
    permissions (`Role::Upgrader`, `Role::Admin`), timelock delay enforcement,
    multi-sig governance wiring.
- **Mainnet Yield Adapters**:
  - `adapter_blend` (`packages/contracts/contracts/adapter_blend`): batch
    request formatting, reserve index positioning, bToken accounting.
  - `adapter_soroswap` (`packages/contracts/contracts/adapter_soroswap`): AMM
    integration, fee compounding, liquidity invariant protection.
- **Explicitly Excluded from Mainnet v1 Launch** (listed under `[excluded]` in
  [`packages/contracts/launch-manifest.toml`](../../packages/contracts/launch-manifest.toml)):
  `lp_aggregator`, `referral`, `goal_rewards`, `savings_goal`,
  `recurring_deposit`, `adapter_lending`, `adapter_pool`, and `rent_escrow`.

This scope is a starting proposal for the eventual RFP, not a commitment
any auditor has reviewed or accepted.

## Vendor selection process (to follow once this is scheduled)

1. **Shortlist criteria**: prior Soroban/Stellar or comparable Rust smart
   contract audit experience, published track record of findings on
   similar DeFi vault/adapter patterns, available start date compatible
   with the launch timeline, and a fixed-scope or capped-cost quote.
2. **RFP**: send the scope above (refined as needed) to the shortlisted
   firms, request a quote, timeline, and sample report from a past
   engagement.
3. **Selection**: record the chosen firm, the engagement terms, and the
   start/end dates here once a contract is signed — not before.
4. **Kickoff**: share the target commit/tag being audited, build
   instructions, and any known risk areas with the auditor.

## Findings & resolution tracking

No audit has started, so there are no findings yet. The table below is the
template to fill in once the auditor begins reporting; it is intentionally
empty.

| ID | Severity | Category | Description | Status | Resolution | Verified Commit / PR |
|---|---|---|---|---|---|---|
| _(none yet)_ | | | | | | |

## Verification requirements (once findings exist)

- Every reported finding (Critical, High, Medium, Low, Informational) must
  have a corresponding fix implemented in the respective contract crate.
- Regression tests must be added to `packages/contracts/tests/integration`
  or the contract-local `src/test.rs` suites.
- CI (`.github/workflows/contract-audit.yml` and `ci.yml`) must pass all
  integration tests and clippy checks with zero warnings before a finding
  is marked resolved.
