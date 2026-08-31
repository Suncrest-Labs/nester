# [DAPP-22] Add E2E test suite (Playwright)

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Testing

## Issue

The DApp lacks end-to-end tests. Critical user flows (wallet connect → create vault → deposit → view portfolio) are not automatically tested, making regressions easy to miss.

**Related PRD claims:**
- [DAPP-22] E2E tests (Playwright or Cypress)
- [E-14] DApp: E2E test suite (Playwright)

## Acceptance Criteria

- [ ] Set up Playwright test framework in `apps/dapp/frontend/e2e/`
- [ ] Test golden paths: wallet connect, vault creation, deposit, offramp initiation
- [ ] Test error cases: invalid input, network errors, auth failures
- [ ] Add to CI: run E2E tests on every PR via `playwright test`
- [ ] E2E tests must run against staging environment or local dev server
- [ ] Document how to run locally: `npm run e2e`

## Implementation

**File:** `apps/dapp/frontend/e2e/auth.spec.ts`

```typescript
import { test, expect } from '@playwright/test';

test('user can connect wallet and authenticate', async ({ page }) => {
  await page.goto('http://localhost:3001');
  await page.click('button:has-text("Connect Wallet")');
  // Mock Freighter wallet...
  await expect(page).toHaveURL('/dashboard');
});

test('user can create vault', async ({ page }) => {
  // ... setup authenticated session
  await page.click('button:has-text("Create Vault")');
  await page.fill('input[name="name"]', 'Test Vault');
  await page.click('button:has-text("Create")');
  await expect(page.locator('text=Test Vault')).toBeVisible();
});
```

## Testing

- Run: `npx playwright test`
- Verify all tests pass
- Add to CI workflow

## Evidence References

Once resolved:
- `file: apps/dapp/frontend/e2e/` (E2E tests)
- `file: .github/workflows/ci.yml#<lines>` (CI integration)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [E-14], [DAPP-22]
- GitHub issue #1115
