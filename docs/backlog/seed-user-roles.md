# [DB-11] Add user_roles seed row (test user → admin) after migration 009

**Status:** Open  
**Priority:** Low  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

After migration 009 creates the `user_roles` table, the seed script should also seed a test admin role for the test user (550e8400-e29b-41d4-a716-446655440001). Without this, the seeded test user has no roles and cannot perform admin operations in dev.

**Related PRD claims:**
- [DB-11] Add user_roles seed row (test user → admin) after PR #270 merges
- Dependency: [API-29] GetRoles — pass raw uuid.UUID to pgx

## Acceptance Criteria

- [ ] Add INSERT into `user_roles` in `scripts/seed.sql` after migration 009 is applied
- [ ] Seed test user (550e8400-e29b-41d4-a716-446655440001) with 'admin' role
- [ ] Test: seeded user can be queried via admin endpoints
- [ ] Verify `make dev-reset` completes successfully with new seed row

## Implementation

**File:** `scripts/seed.sql` (add after migration 009 creates table)

```sql
INSERT INTO user_roles (id, user_id, role, created_at) VALUES
    (gen_random_uuid(), '550e8400-e29b-41d4-a716-446655440001', 'admin', NOW());
```

## Testing

- Run `make dev-reset`
- Query test user roles via API: `GET /admin/users/550e8400-e29b-41d4-a716-446655440001/roles`
- Should return ['admin']

## Evidence References

Once resolved:
- `file: scripts/seed.sql#<lines>` (seed INSERT)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [DB-11]
- GitHub issue #1115
- Dependency: issue [API-29]
