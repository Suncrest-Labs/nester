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
- [ ] Audit scope includes: vault, vault_token, allocation_strategy, yield_registry, access_control, nester, treasury, timelock contracts
- [ ] Provide audit report with findings, severity levels, and remediation status
- [ ] All **Critical** and **High** findings must be resolved before mainnet deployment
- [ ] All **Medium** findings must have documented risk acceptance or remediation
- [ ] Publish audit report (or redacted version) in `SECURITY.md` or linked from `docs/audits/`
- [ ] Add audit report link and completion date to `LAUNCH_READINESS_REGISTER.md` [SC-14] Evidence field

## Implementation Notes

- **Timeline:** Scheduled for Q3 2026 (Aug–Sep)
- **Budget:** Allocate 15–30 ETH (~$30k–$60k USD) for professional audit
- **Scope:** Include both code review and property-based testing verification
- **Remediation:** Plan 2–4 weeks for fixes post-audit before mainnet deploy
- **Follow-up:** Schedule retesting of any critical fixes

## Pre-Audit Checklist

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
