# [API-29] Fix GetRoles to pass raw uuid.UUID instead of id.String()

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Bug Fix

## Issue

The `GetRoles` function in the user repository passes `id.String()` (a string) to pgx instead of the native `uuid.UUID` type. pgx v5 handles `uuid.UUID` natively with built-in type conversion; passing `.String()` forces an implicit server-side cast and breaks type safety guarantees. This inconsistency can surface as unexpected query errors under stricter PostgreSQL configurations.

**Related PRD claims:**
- [API-29] GetRoles — pass raw uuid.UUID to pgx, not id.String()
- [B-08] GetRoles passes id.String() instead of uuid.UUID to pgx

## Acceptance Criteria

- [ ] Find `GetRoles` function in `apps/api/internal/repository/postgres/user_repository.go`
- [ ] Change parameter from `id.String()` to the raw `uuid.UUID` value
- [ ] Verify pgx query accepts `uuid.UUID` natively (should be automatic in v5)
- [ ] Run existing unit and integration tests for user repository
- [ ] Verify no other functions have similar pattern (audit other queries)
- [ ] Add comment explaining why `uuid.UUID` is preferred over `.String()`

## Implementation

**File:** `apps/api/internal/repository/postgres/user_repository.go`

```go
// Before:
func (r *UserRepository) GetRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
    row := r.db.QueryRow(ctx, `SELECT roles FROM user_roles WHERE user_id = $1`, id.String()) // ❌
    // ...
}

// After:
func (r *UserRepository) GetRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
    row := r.db.QueryRow(ctx, `SELECT roles FROM user_roles WHERE user_id = $1`, id) // ✓
    // ...
}
```

## Testing

- Run `go test ./apps/api/internal/repository/...` to verify tests still pass
- Verify with `go vet` that no type mismatches are reported

## Evidence References

Once resolved:
- `file: apps/api/internal/repository/postgres/user_repository.go#<lines>` (fixed code)
- `test: apps/api/internal/repository/postgres/user_repository_test.go` (passing tests)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-08], [API-29]
- GitHub issue #1115
