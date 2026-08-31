# [API-26] SECURITY: Fix BOLA vulnerability in initiateSettlement

**Status:** Open  
**Priority:** Critical  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Security

## Issue

The `initiateSettlement` handler accepts `user_id` from the JSON request body instead of extracting it from the JWT token. This allows an authenticated attacker to create settlements on behalf of any other user (Broken Object-Level Authorization / BOLA attack).

**Related PRD claims:**
- [API-26] SECURITY: initiateSettlement — extract user_id from JWT, not request body (BOLA)
- [B-03] BOLA: initiateSettlement accepts user_id from request body

## Acceptance Criteria

- [ ] Modify `initiateSettlement` handler to ignore `user_id` in request body
- [ ] Extract `user_id` from JWT via `auth.GetUserFromContext(ctx)`
- [ ] Add unit test: `TestInitiateSettlement_BOLA_RejectsOtherUserID` that verifies attacker cannot create settlement for victim
- [ ] Verify existing settlement tests still pass
- [ ] Add comment in code explaining why `user_id` must come from auth context only
- [ ] Verify no other handlers have similar BOLA patterns (grep for `req.UserID`)

## Implementation

**File:** `apps/api/internal/handler/settlement_handler.go`

```go
// Before (vulnerable):
func (h *SettlementHandler) InitiateSettlement(w http.ResponseWriter, r *http.Request) {
    var req InitiateSettlementRequest
    json.NewDecoder(r.Body).Decode(&req)
    // ...
    settlement, err := h.svc.Initiate(ctx, req.UserID, req.Amount, ...) // ❌ BOLA!
}

// After (fixed):
func (h *SettlementHandler) InitiateSettlement(w http.ResponseWriter, r *http.Request) {
    var req InitiateSettlementRequest
    json.NewDecoder(r.Body).Decode(&req)
    // ...
    userID := auth.GetUserFromContext(ctx) // ✓ From JWT
    settlement, err := h.svc.Initiate(ctx, userID, req.Amount, ...) // ✓ Safe
}
```

## Testing

- Unit test with JWT containing user A, request body containing user B
- Assert error or settlement belongs to user A, not user B
- Verify audit log records attempted BOLA attack

## Evidence References

Once resolved:
- `file: apps/api/internal/handler/settlement_handler.go#<lines>` (fixed code)
- `test: apps/api/internal/handler/settlement_handler_test.go::TestInitiateSettlement_BOLA_RejectsOtherUserID`
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-03], [API-26]
- GitHub issue #1115
