# Independent Third-Party Audit: Vault & Adapter Contracts (#1133, #1537)

## Status

**Not yet commissioned.** No third-party audit engagement exists today, no
auditor has been selected, and no RFP has gone out. This document defines
the process to follow once an audit is scheduled, the checklist to work
through before it starts, and the mandatory findings tracker and
fix-verification workflow.

**Launch gate ([`docs/LAUNCH_CHECKLIST.md`](../LAUNCH_CHECKLIST.md#checklist) Item 1)**:
Mainnet deployment is a hard blocker until a third-party audit has been
completed, every Critical and High finding is **Auditor-Verified Fixed**, and
every Medium/Low/Informational finding is either **Auditor-Verified Fixed** or
formally signed off as **Accepted Risk** in the table below.

---

## 1. Target Scope (Proposed)

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

---

## 2. Vendor Selection Process

1. **Shortlist criteria**: prior Soroban/Stellar or comparable Rust smart
   contract audit experience, published track record of findings on
   similar DeFi vault/adapter patterns, available start date compatible
   with the launch timeline, and a fixed-scope or capped-cost quote.
2. **RFP**: send the scope above (refined as needed) to the shortlisted
   firms, request a quote, timeline, and sample report from a past
   engagement.
3. **Selection**: record the chosen firm, the engagement terms, and the
   start/end dates here once a contract is signed.
4. **Kickoff**: share the target commit/tag being audited, build
   instructions, and any known risk areas with the auditor.

---

## 3. Audit Finding Lifecycle & Fix-Verification Process (#1537)

Every finding delivered by the auditor must follow this 4-step lifecycle so no
Medium or Low finding is ever lost:

### Step 1: One GitHub Issue per Finding
Open a dedicated GitHub issue for each finding immediately upon receiving the
draft audit report:
- **Title format**: `audit(<AuditID>): <short finding summary>` (e.g. `audit(AUD-01): rounding in lock break penalty dust`)
- **Required labels**: `security` and `severity:critical`, `severity:high`, `severity:medium`, `severity:low`, or `severity:informational`
- **Issue body template**:
  ```markdown
  ## Audit Finding Metadata
  - **Audit ID:** AUD-XX
  - **Severity:** Critical | High | Medium | Low | Informational
  - **Contract / File:** `packages/contracts/contracts/...`
  - **Audited Commit:** `<sha>`

  ## Auditor Description & Proof of Concept
  <paste verbatim from draft report>

  ## Remediation Plan / Accepted Risk Justification
  - [ ] Fix PR opened: #<PR_NUMBER>
  - [ ] Regression test added in `packages/contracts/tests/integration` or `src/test.rs`
  - [ ] Auditor fix-verification sign-off received at commit `<fix_sha>`
  ```

### Step 2: Fix Implementation & Regression Test
- Every fixed finding must include a deterministic unit or integration
  regression test that fails before the fix and passes after.
- The fix PR must reference `Fixes #<ISSUE_NUMBER>` and record the exact commit
  SHA sent back to the auditor for fix review.

### Step 3: Auditor Fix Verification
- Submit the fix PR commit SHA and test reference to the audit firm during the
  remediation review window.
- Record the auditor's verification status (`Verified Fixed`, `Partially Fixed`,
  or `Acknowledged / Accepted Risk`) and the final report commit SHA in §4.

### Step 4: Accepted-Risk Sign-Off Rules
- **Critical & High findings:** Cannot be accepted as risk. Must be `Verified Fixed` by the auditor before mainnet deployment.
- **Medium & Low findings:** May only be marked `Accepted Risk` with written justification in §4 signed off by both the **Security Lead** and **Lead Maintainer**.

---

## 4. Findings & Resolution Tracker

| Finding ID | Severity | Category | GitHub Issue (`security`, `severity:*`) | Fix PR & Commit SHA | Regression Test | Auditor Verification Status | Accepted-Risk Sign-Off (if not fixed) |
|---|---|---|---|---|---|---|---|
| `AUD-00` _(template row)_ | `severity:high` | Accounting / Rounding | `#0000` (`security`, `severity:high`) | `PR #0000` (`<commit_sha>`) | `packages/contracts/tests/integration/...` | `Verified Fixed` (Final Report §X.Y) | N/A — Fixed |

---

## 5. Pre-Launch Go/No-Go Verification Gate

Linked from [`docs/LAUNCH_CHECKLIST.md`](../LAUNCH_CHECKLIST.md) (**Item 1: Audit Complete**):
- [ ] Every finding from the audit report has a row in §4 and a matching GitHub issue with `security` + `severity:*` labels.
- [ ] 100% of `Critical` and `High` findings have status `Verified Fixed` confirmed by the auditor.
- [ ] 100% of `Medium` and `Low` findings are either `Verified Fixed` or carry dual sign-off in the Accepted-Risk column.
- [ ] `AUDITED_GIT_COMMIT` in `packages/contracts/scripts/deployed-mainnet.env` matches the final post-remediation commit verified by the auditor.
