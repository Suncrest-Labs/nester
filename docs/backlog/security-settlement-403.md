# [API-28] SECURITY: Fix existence oracle in PATCH /settlements/{id}

**Status:** Open  
**Priority:** High  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Security

## Issue

The `PATCH /settlements/{id}` endpoint returns 403 for non-owned UUIDs and 404 for non-existent UUIDs. This leaks information: attackers can distinguish live settlements from dead ones, enabling existence oracle attacks that compound other vulnerabilities.

**Related PRD claims:**
- [API-28] SECURITY: PATCH /settlements/{id} — return 404 (not 403) for non-owned UUIDs to prevent existence oracle
- [B-05] PATCH /settlements/{id} ownership 403 confirms settlement existence

## Acceptance Criteria

- [ ] Modify PATCH handler to return 404 for both non-existent and non-owned settlements
- [ ] Add unit test: `TestPatchSettlement_NonOwner_Returns404` that verifies non-owner gets 404
- [ ] Add unit test: `TestPatchSettlement_NonExistent_Returns404` that verifies missing settlement also returns 404
- [ ] Verify that attacker cannot distinguish live from dead via status code
- [ ] Ensure audit logs record all access attempts
- [ ] Update API documentation

## Implementation

**File:** `apps/api/internal/service/settlement_service.go` (or handler)

Same pattern as [API-27]: both 404 and non-owned should return 404 (not 403).

## Testing

- Non-owner attempts to patch → 404
- Non-existent UUID → 404
- Owner patches own settlement → 200 or appropriate success status

## Evidence References

Once resolved:
- `file: apps/api/internal/handler/settlement_handler.go#<lines>` (fixed code)
- `test: apps/api/internal/handler/settlement_handler_test.go::TestPatchSettlement_NonOwner_Returns404`
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-05], [API-28]
- GitHub issue #1115
