# [API-33] Document re-auth requirement after migration 009 deployment

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Documentation

## Issue

Migration 009 adds the `user_roles` table and introduces a `Roles` claim to JWT tokens. All JWT tokens issued before this migration lack the `Roles` field. When `migration 009` is deployed to production, existing admin users with old tokens will suddenly fail authorization checks because their tokens lack the required claim, causing a silent service outage for admins.

**Related PRD claims:**
- [API-33] Document re-auth requirement after migration 009 deployment (existing admin tokens lack Roles claim)
- [P-01] Auth tokens for existing admins lack Roles claim after migration 009

## Acceptance Criteria

- [ ] Create `docs/DEPLOYMENT.md` with deployment runbook
- [ ] Document migration 009 explicitly: "After migration 009, all admin users must re-authenticate to issue new tokens with Roles claim"
- [ ] Include step: "Before deploying migration 009 to production, notify admins of required re-authentication"
- [ ] Provide admin recovery procedure: if all admins are locked out, provide manual token recovery steps or bootstrap command
- [ ] Recommend: run migration 009 on staging first; test admin token refresh workflow end-to-end
- [ ] Add warning to migration 009 file itself (SQL comment) explaining the re-auth requirement

## Implementation

**File:** `docs/DEPLOYMENT.md`

```markdown
## Migration 009: User Roles and JWT Roles Claim

**⚠️ BREAKING CHANGE:** This migration introduces a `Roles` claim to JWT tokens.

### Before Deployment

- [ ] Notify all admins of required re-authentication
- [ ] Test token refresh on staging environment
- [ ] Prepare recovery plan (backup admin credentials, bootstrap command)

### After Deployment

All admin users MUST re-authenticate to issue new tokens with the `Roles` claim.
Existing tokens will be rejected by role-based authorization checks.

### Admin Recovery (if all admins locked out)

Use the `bootstrap-admin` tool to re-grant admin role to a user:
```bash
./bootstrap-admin <user-wallet-address> admin
```

### Verification

- [ ] Test admin login workflow
- [ ] Verify new tokens contain `Roles` claim (decode JWT and check)
- [ ] Verify admin endpoints return 200 (not 403) after re-auth
```

**File:** `apps/api/migrations/009_create_user_roles_table.up.sql`

```sql
-- ⚠️ NOTE: This migration introduces a new Roles claim in JWT tokens.
-- All existing admin tokens will be invalid after this migration.
-- Action required: All admins must re-authenticate post-deployment.
-- See docs/DEPLOYMENT.md for details and recovery procedures.

CREATE TABLE user_roles (
    ...
);
```

## Testing

- Deploy migration 009 to staging
- Test old token → authorization fails
- Test new token (post re-auth) → authorization succeeds
- Verify audit logs record all failed + successful auth attempts

## Evidence References

Once resolved:
- `file: docs/DEPLOYMENT.md#<section>` (deployment runbook)
- `file: apps/api/migrations/009_create_user_roles_table.up.sql#<lines>` (migration comment)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [P-01], [API-33]
- GitHub issue #1115
