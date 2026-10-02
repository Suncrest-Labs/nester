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

- **Core Vault & Vault Factory** (`packages/contracts/contracts/vault`):
  deposit/withdrawal accounting, share pricing invariants, pause/emergency
  mechanics, fee collection, upgrade timelock integration.
- **Yield Adapters**:
  - `adapter_blend`: batch request formatting, reserve index positioning,
    bToken accounting.
  - `adapter_lending`: deposit/withdraw execution, slippage checks, interest
    accrual reading.
  - `adapter_pool`: pro-rata reserve valuation, derived APY window checks,
    checkpoint resets.
  - `adapter_soroswap`: AMM integration, fee compounding, liquidity
    invariant protection.
- **Access Control & Upgradability**: role-based permissions
  (`Role::Upgrader`, `Role::Admin`), timelock delay enforcement, multi-sig
  governance wiring.

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
