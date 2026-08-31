# [SC-12] Add preview_withdraw_net to vault contract

**Status:** Open  
**Priority:** High  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

The current `preview_withdraw` function returns the gross amount before deducting management, early-withdrawal, and performance fees. This breaks slippage protection in the DApp: when users pass `preview_withdraw` output as `min_assets_out` to a withdrawal, they hit `SlippageExceeded` because the actual net amount is lower than promised.

**Related PRD claims:**
- [SC-12] preview_withdraw — fix or document pre-fee gross return vs EIP-4626 previewRedeem semantics
- [E-01] Add preview_withdraw_net to vault contract
- [P-10] preview_withdraw returns gross pre-fee amount (breaks slippage protection)

## Acceptance Criteria

- [ ] Implement `preview_withdraw_net(shares: u128) -> u128` in vault contract that returns net amount **after** all applicable fees
- [ ] Ensure fee calculation matches the actual withdrawal fee logic in `execute_withdrawal`
- [ ] Add test: `test_preview_withdraw_net_matches_actual_withdrawal` that verifies 100 test withdrawals produce net amounts within 1 basis point of predicted
- [ ] Update DApp code to use `preview_withdraw_net` instead of `preview_withdraw` for slippage protection
- [ ] Document in vault contract comments the difference between `preview_withdraw` (gross) and `preview_withdraw_net` (net)
- [ ] Verify backward compatibility: ensure `preview_withdraw` still exists (though gross amount may be unexpected)

## Implementation Notes

- Fee types to include in net calculation: management fee, early-withdrawal penalty (if applicable), performance fee
- Use `Decimal` type for precision; avoid rounding errors on large amounts
- Consider edge cases: zero shares, shares causing zero net amount, fees exceeding gross amount
- Test with property-based testing to ensure idempotency and monotonicity

## Evidence References

Once resolved:
- `file: packages/contracts/contracts/vault/src/lib.rs#<lines>` (preview_withdraw_net implementation)
- `test: packages/contracts/contracts/vault/src/test.rs::test_preview_withdraw_net_matches_actual_withdrawal`
- `pr: #<number>` (merged PR with commit hash)

## Related Issues

- PRD [E-01], [P-10]
- GitHub issue #1115
