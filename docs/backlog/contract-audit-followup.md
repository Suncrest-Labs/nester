# [SC-14] External smart contract security audit

**Status:** Open  
**Priority:** Critical  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Security

## Issue

Nester smart contracts have not yet undergone professional security audit. This is a blocker for production deployment in emerging markets where regulatory scrutiny is high and user losses directly impact livelihoods.

**Related PRD claims:**
- [SC-14] Contract security audit (pending — link when complete)

## Acceptance Criteria

- [ ] Engage external audit firm (e.g., Trail of Bits, Certora, Zellic, Immunefi)
- [ ] Audit scope includes: vault, vault_token, allocation_strategy, yield_registry, access_control, nester, treasury, timelock contracts, plus every adapter that ships (at minimum `adapter_blend`; it has tests against Blend's real v1.0.0 pool WASM, but not against a live mainnet pool)
- [ ] Provide audit report with findings, severity levels, and remediation status
- [ ] All **Critical** and **High** findings must be resolved before mainnet deployment
- [ ] All **Medium** findings must have documented risk acceptance or remediation
- [ ] Publish audit report (or redacted version) in `SECURITY.md` or linked from `docs/audits/`
- [ ] Add audit report link and completion date to `docs/launch-readiness-register.md` [SC-14] Evidence field

## Implementation Notes

- **Timeline:** Scheduled for Q3 2026 (Aug–Sep)
- **Budget:** Allocate 15–30 ETH (~$30k–$60k USD) for professional audit
- **Scope:** Include both code review and property-based testing verification
- **Remediation:** Plan 2–4 weeks for fixes post-audit before mainnet deploy
- **Follow-up:** Schedule retesting of any critical fixes

## Recent changes the auditor should know about

Added for the guarded mainnet launch and not yet reviewed externally:

- `vault.set_tvl_cap` / `get_tvl_cap`, enforced in `deposit` against total assets (the existing `set_max_deposit` only bounds a single deposit and defaults to unlimited).
- `treasury.transfer_admin` / `accept_admin`: the treasury previously had no way to hand admin to a multisig.
- `packages/contracts/scripts/deploy-mainnet.sh`: sets the caps, enables the deposit allowlist and proposes admin ownership to the multisig. The multisig must still call `accept_admin` on each contract.

## Pre-Audit Checklist

**Known blocker (size):** the optimized vault is 150,037 bytes (169,061 before this branch's doc-comment change), over mainnet's 131,072-byte contract limit, so it cannot be deployed as is. Resolve before the audit candidate is tagged, since the fix changes the audited code.

**Known blocker (test):** `vault-contract` test `test_harvest_rate_limit_is_per_user` fails on clean `dev` (observed 2026-10-12). Fix it or document it as an accepted, explained failure before tagging an audit candidate.

- [ ] All contracts pass local `cargo test`
- [ ] All contracts pass property-based testing via `contracts-property-nightly.yml`
- [ ] Code follows Soroban best practices and naming conventions
- [ ] All public functions have doc comments
- [ ] No use of unsafe patterns or deprecated SDK methods
- [ ] Contract ABIs are stable and documented

## Evidence References

Once resolved:
- `file: SECURITY.md` (audit report link)
- `file: docs/audits/nester-<firm>-<date>.pdf` (audit report)
- `pr: #<number>` (PR documenting audit completion)

## Related Issues

- PRD [SC-14]
- GitHub issue #1115
