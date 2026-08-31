# [API-27] SECURITY: Fix UUID enumeration in GET /settlements/{id}

**Status:** Open  
**Priority:** High  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Security

## Issue

The `GET /settlements/{id}` endpoint returns HTTP 403 when a user tries to fetch a settlement they don't own. This allows attackers to enumerate valid settlement UUIDs: if a UUID returns 403, it exists; if it returns 404, it doesn't. The endpoint should return 404 for both cases to prevent UUID enumeration.

**Related PRD claims:**
- [API-27] SECURITY: GET /settlements/{id} — add ownership check (return 404 for non-owner, not 403)
- [B-04] GET /settlements/{id} has no ownership check

## Acceptance Criteria

- [ ] Modify settlement handler to return 404 instead of 403 for non-owned settlements
- [ ] Add unit test: `TestGetSettlement_NonOwner_Returns404` that verifies non-owner gets 404
- [ ] Add unit test: `TestGetSettlement_NonExistent_Returns404` that verifies missing settlement also returns 404
- [ ] Verify that attacker cannot distinguish live UUID from dead UUID based on status code
- [ ] Ensure audit logs still record all access attempts (including failed)
- [ ] Update API documentation to clarify 404 response

## Implementation

**File:** `apps/api/internal/handler/settlement_handler.go`

```go
// Before (vulnerable):
func (h *SettlementHandler) GetSettlement(w http.ResponseWriter, r *http.Request) {
    settlementID := chi.URLParam(r, "id")
    settlement, err := h.svc.GetByID(ctx, uuid.MustParse(settlementID))
    if err == settlement.ErrNotFound {
        http.Error(w, "Not found", http.StatusNotFound)
        return
    }
    if settlement.UserID != userID {
        http.Error(w, "Forbidden", http.StatusForbidden) // ❌ Enumeration!
        return
    }
    // ...
}

// After (fixed):
func (h *SettlementHandler) GetSettlement(w http.ResponseWriter, r *http.Request) {
    settlementID := chi.URLParam(r, "id")
    settlement, err := h.svc.GetByID(ctx, uuid.MustParse(settlementID))
    if err == settlement.ErrNotFound || settlement.UserID != userID {
        http.Error(w, "Not found", http.StatusNotFound) // ✓ Safe
        return
    }
    // ...
}
```

## Testing

- Non-owner attempts to fetch settlement → 404
- Non-existent UUID → 404
- Owner fetches own settlement → 200 with data

## Evidence References

Once resolved:
- `file: apps/api/internal/handler/settlement_handler.go#<lines>` (fixed code)
- `test: apps/api/internal/handler/settlement_handler_test.go::TestGetSettlement_NonOwner_Returns404`
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-04], [API-27]
- GitHub issue #1115
