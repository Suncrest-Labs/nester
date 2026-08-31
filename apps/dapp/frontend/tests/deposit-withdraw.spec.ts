import { test, expect } from '@playwright/test';

/**
 * End-to-end deposit/withdraw journey test.
 *
 * This test exercises the full user flow:
 *   1. Connect wallet
 *   2. Deposit into a vault
 *   3. Verify balance update
 *   4. Withdraw from the vault
 *   5. Verify settlement
 *
 * Environment variables (for testnet runs):
 *   TESTNET_WALLET_ADDRESS — funded Stellar testnet address
 *   VAULT_ID              — deployed vault contract ID on testnet
 *
 * When these are not set the test skips with a clear message, keeping
 * CI green until testnet infrastructure is provisioned.
 */

const TESTNET_WALLET = process.env.TESTNET_WALLET_ADDRESS ?? '';
const VAULT_ID = process.env.VAULT_ID ?? '';

test.describe('Deposit / Withdraw journey', () => {
  test.skip(!TESTNET_WALLET || !VAULT_ID,
    'Set TESTNET_WALLET_ADDRESS and VAULT_ID env vars to enable testnet E2E');

  test('connect wallet → deposit → balance updates → withdraw → settles', async ({ page }) => {
    // ── 1. Navigate to the vault detail page ───────────────────────────
    await test.step('navigate to vault', async () => {
      await page.goto(`/vaults/${VAULT_ID}`);
      await expect(page.getByRole('heading', { name: /vault/i })).toBeVisible();
    });

    // ── 2. Connect wallet ──────────────────────────────────────────────
    await test.step('connect wallet', async () => {
      const connectBtn = page.getByRole('button', { name: /connect/i });
      if (await connectBtn.isVisible()) {
        await connectBtn.click();
        // Freighter popup or in-page wallet selector
        await page.getByText(TESTNET_WALLET.slice(0, 8)).click();
        await expect(page.getByText(/connected/i).or(page.getByText(TESTNET_WALLET.slice(0, 8)))).toBeVisible();
      }
    });

    // ── 3. Deposit ─────────────────────────────────────────────────────
    const depositAmount = '1';
    await test.step(`deposit ${depositAmount} XLM`, async () => {
      await page.getByRole('button', { name: /deposit/i }).first().click();

      const modal = page.getByRole('dialog');
      await expect(modal).toBeVisible();

      await modal.getByPlaceholder('0.00').fill(depositAmount);
      await modal.getByRole('button', { name: /confirm/i }).click();

      // Wait for transaction confirmation
      await expect(page.getByText(/deposit successful/i).or(page.getByText(/confirmed/i))).toBeVisible({ timeout: 30_000 });
    });

    // ── 4. Verify balance updated ──────────────────────────────────────
    await test.step('verify balance update', async () => {
      // The position card should reflect the deposited amount
      await expect(page.getByText(depositAmount)).toBeVisible({ timeout: 15_000 });
    });

    // ── 5. Withdraw ────────────────────────────────────────────────────
    await test.step(`withdraw ${depositAmount} XLM`, async () => {
      await page.getByRole('button', { name: /withdraw/i }).first().click();

      const modal = page.getByRole('dialog');
      await expect(modal).toBeVisible();

      await modal.getByPlaceholder('0.00').fill(depositAmount);
      await modal.getByRole('button', { name: /confirm/i }).click();

      await expect(page.getByText(/withdrawal successful/i).or(page.getByText(/confirmed/i))).toBeVisible({ timeout: 30_000 });
    });

    // ── 6. Verify settlement ───────────────────────────────────────────
    await test.step('verify settlement', async () => {
      // The position should return to zero or reflect the withdrawal
      await expect(page.getByText(/settled/i).or(page.getByText('0.00'))).toBeVisible({ timeout: 30_000 });
    });
  });

  test('deposit shows error for zero amount', async ({ page }) => {
    await page.goto(`/vaults/${VAULT_ID}`);

    // Connect wallet first
    const connectBtn = page.getByRole('button', { name: /connect/i });
    if (await connectBtn.isVisible()) {
      await connectBtn.click();
      await page.getByText(TESTNET_WALLET.slice(0, 8)).click();
    }

    await page.getByRole('button', { name: /deposit/i }).first().click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByPlaceholder('0.00').fill('0');
    await expect(modal.getByText(/must be greater than zero/i)).toBeVisible();
  });

  test('withdraw shows error for amount exceeding balance', async ({ page }) => {
    await page.goto(`/vaults/${VAULT_ID}`);

    // Connect wallet
    const connectBtn = page.getByRole('button', { name: /connect/i });
    if (await connectBtn.isVisible()) {
      await connectBtn.click();
      await page.getByText(TESTNET_WALLET.slice(0, 8)).click();
    }

    await page.getByRole('button', { name: /withdraw/i }).first().click();
    const modal = page.getByRole('dialog');
    await expect(modal).toBeVisible();

    await modal.getByPlaceholder('0.00').fill('999999999');
    await expect(modal.getByText(/insufficient balance/i)).toBeVisible();
  });
});
