# [DB-10] Fix scripts/seed.sql schema mismatch with migration 007

**Status:** Open  
**Priority:** High  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Bug Fix

## Issue

The `scripts/seed.sql` users INSERT block references `email` and `name` columns that were removed in migration 007. Running `make dev` on a fresh clone fails during database seeding because the schema post-migration 007 has `wallet_address` and `display_name`, not `email` and `name`.

**Related PRD claims:**
- [DB-10] Fix scripts/seed.sql — users INSERT still references old email/name columns
- [B-07] seed.sql schema mismatch (dev environment setup fails)

## Acceptance Criteria

- [ ] Update `scripts/seed.sql` users INSERT to match post-migration 007 schema
- [ ] Remove references to deprecated `email` column
- [ ] Remove references to deprecated `name` column
- [ ] Use correct columns: `wallet_address`, `display_name`
- [ ] Insert test user with valid Stellar address (G-prefix, 56 chars)
- [ ] Test: `make dev-reset` should complete without schema errors
- [ ] Verify seeded test user can be queried via API

## Implementation

**File:** `scripts/seed.sql`

```sql
-- Before (broken):
INSERT INTO users (id, name, email) VALUES
    ('550e8400-e29b-41d4-a716-446655440001', 'Test User', 'test@nester.dev');

-- After (fixed):
INSERT INTO users (id, wallet_address, display_name) VALUES
    ('550e8400-e29b-41d4-a716-446655440001', 'GBRPYHIL2CI3FV4BSXVEQ7JZ47ZQFLQOJHGS4OVRI7OFDIYWERWLVUO', 'Test User');
```

## Testing

- Run `make dev-reset` → seeding should complete without error
- Query seeded user via API → should return user with wallet_address and display_name
- Verify no references to `email` or `name` in seed output

## Evidence References

Once resolved:
- `file: scripts/seed.sql#<lines>` (fixed INSERT statement)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [B-07], [DB-10]
- GitHub issue #1115
