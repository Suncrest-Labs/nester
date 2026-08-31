# [API-30] Add db.Ping() to bootstrap-admin after sql.Open()

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Bug Fix

## Issue

The `bootstrap-admin` CLI tool opens a database connection via `sql.Open()` but does not call `db.Ping()`. The `sql.Open` function only validates the DSN syntax; it does not actually connect to the database. A bad DSN or unreachable PostgreSQL host surfaces as a confusing query error deep in the code rather than a clear connection failure at startup.

**Related PRD claims:**
- [API-30] bootstrap-admin — add db.Ping() after sql.Open()
- [B-09] bootstrap-admin does not call db.Ping() after sql.Open()

## Acceptance Criteria

- [ ] Locate `bootstrap-admin` main function in `apps/api/cmd/bootstrap-admin/main.go`
- [ ] Add `db.Ping(ctx)` call immediately after `sql.Open()` with appropriate timeout
- [ ] If `Ping()` fails, print clear error message: "Failed to connect to database: <error>" and exit with code 1
- [ ] Verify error messages are user-friendly and actionable (e.g., "connection refused" vs. obscure query error)
- [ ] Test locally by providing bad DSN and verifying early, clear error
- [ ] Test with good DSN and verifying Ping succeeds

## Implementation

**File:** `apps/api/cmd/bootstrap-admin/main.go`

```go
// Before:
db, err := sql.Open("postgres", dsn)
if err != nil {
    log.Fatalf("Failed to open database: %v", err) // May not actually connect
}
defer db.Close()

// After:
db, err := sql.Open("postgres", dsn)
if err != nil {
    log.Fatalf("Failed to open database: %v", err)
}
defer db.Close()

// Verify connection is actually working
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := db.PingContext(ctx); err != nil {
    log.Fatalf("Failed to connect to database: %v", err) // ✓ Clear error
}
```

## Testing

- Run bootstrap-admin with valid DSN → should proceed
- Run bootstrap-admin with invalid DSN → should fail with clear "Failed to connect" message

## Evidence References

Once resolved:
- `file: apps/api/cmd/bootstrap-admin/main.go#<lines>` (fixed code)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-09], [API-30]
- GitHub issue #1115
