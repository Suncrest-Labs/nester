# [API-32] Wire golang-migrate into API startup behind RUN_MIGRATIONS flag

**Status:** Open  
**Priority:** High  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

New database migrations only apply via Docker `initdb` scripts or manual `make dev-reset` (which wipes all data). There is no migration runner wired into the API startup. This means: (1) incremental schema changes require data loss, (2) fresh deployments may not apply all pending migrations automatically, and (3) developers must remember to run migrations manually.

**Related PRD claims:**
- [API-32] Migration runner — wire golang-migrate (or equivalent) into API startup behind RUN_MIGRATIONS=true flag
- [B-12] No migration runner in API startup (new migrations silently skipped in dev)
- [E-03] Structured RUN_MIGRATIONS flag in API

## Acceptance Criteria

- [ ] Add environment variable `RUN_MIGRATIONS` (default: false) to config
- [ ] If `RUN_MIGRATIONS=true` at startup, run all pending migrations before starting API
- [ ] Use `github.com/golang-migrate/migrate/v4` library (already in go.mod)
- [ ] Log migration results (applied migrations, version, any errors)
- [ ] If migration fails, exit API with error (do not start with stale schema)
- [ ] Add unit test: `TestStartupAppliesPendingMigrations` that verifies migrations run
- [ ] Document in README: "Set RUN_MIGRATIONS=true to apply migrations automatically; set to false for manual control"
- [ ] Verify existing `make dev` flow works with flag enabled by default in Dockerfile

## Implementation

**File:** `apps/api/cmd/api/main.go`

```go
func runMigrations(migrationPath string, dbURL string) error {
    m, err := migrate.New("file://"+migrationPath, dbURL)
    if err != nil {
        return fmt.Errorf("failed to create migrator: %w", err)
    }
    defer m.Close()

    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return fmt.Errorf("migration failed: %w", err)
    }

    version, _, _ := m.Version()
    log.Printf("Migrations applied. Current version: %d", version)
    return nil
}

// In main():
if config.RunMigrations {
    if err := runMigrations("./migrations", config.DatabaseURL); err != nil {
        log.Fatalf("Database migration error: %v", err)
    }
}
```

## Configuration

Add to `.env.example`:
```
RUN_MIGRATIONS=false
```

Add to `Dockerfile.dev`:
```
ENV RUN_MIGRATIONS=true
```

## Testing

- Fresh deployment with migrations: all pending migrations apply
- Subsequent restarts: no migrations re-applied (idempotent)
- Bad migration: API fails to start with clear error
- `RUN_MIGRATIONS=false`: API starts without touching schema

## Evidence References

Once resolved:
- `file: apps/api/cmd/api/main.go#<lines>` (migration runner code)
- `file: .env.example` (config)
- `file: Dockerfile.dev` (RUN_MIGRATIONS env var)
- `test: apps/api/cmd/api/main_test.go::TestStartupAppliesPendingMigrations`
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-12], [E-03], [API-32]
- GitHub issue #1115
