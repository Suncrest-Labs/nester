# [API-31] Add Stellar address format validation to bootstrap-admin

**Status:** Open  
**Priority:** Low  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

The `bootstrap-admin` CLI tool accepts a wallet address from the user but does not validate its format before querying the database. A typo in the address produces a confusing "no user found" error rather than an immediate "invalid address format" error.

**Related PRD claims:**
- [API-31] bootstrap-admin — validate Stellar address format before querying
- [P-02] bootstrap-admin accepts invalid Stellar addresses silently

## Acceptance Criteria

- [ ] Add basic Stellar address format validation before database query
- [ ] Validate: address starts with 'G' and has exactly 56 characters
- [ ] If invalid, print error: "Invalid Stellar address format. Expected: G<56 alphanumeric chars>"
- [ ] Exit with code 1 on invalid address
- [ ] Test with valid and invalid addresses

## Implementation

**File:** `apps/api/cmd/bootstrap-admin/main.go`

```go
func validateStellarAddress(addr string) error {
    if len(addr) != 56 || addr[0] != 'G' {
        return fmt.Errorf("invalid Stellar address format: expected G<56 alphanumeric chars>, got %s", addr)
    }
    return nil
}

// In main():
if err := validateStellarAddress(walletAddress); err != nil {
    log.Fatalf("Invalid address: %v", err)
}
```

## Testing

- Valid address (G prefix, 56 chars) → passes validation
- Invalid address (no G, wrong length, typos) → fails with clear error before DB query

## Evidence References

Once resolved:
- `file: apps/api/cmd/bootstrap-admin/main.go#<lines>` (validation code)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [P-02], [API-31]
- GitHub issue #1115
