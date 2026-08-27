/**
 * settlement-monitor.ts
 *
 * Settlement and withdrawal finalization monitoring for smoke tests.
 *
 * Verifies that withdrawals settle properly and final balances reconcile.
 * Handles settlement delays and multi-step withdrawal flows (request → pending → settled).
 */

import { type Page } from "@playwright/test";
import { getUIBalance, getAPIBalance } from "./balance-monitor";

export interface SettlementStatus {
  isSettled: boolean;
  isPending: boolean;
  isClaimable: boolean;
  settledAmount: number;
  pendingAmount: number;
  claimableAmount: number;
  lastUpdated: number;
}

/**
 * Get current settlement status of pending withdrawals.
 *
 * Checks the UI for pending/settled withdrawal indicators.
 */
export async function getSettlementStatus(page: Page): Promise<SettlementStatus> {
  try {
    // Look for settlement status display
    const statusElement = page
      .locator(
        '[data-testid="settlement-status"], [class*="settlement"], text=/settlement|pending|settled/i'
      )
      .first();

    let statusText = "";
    if (await statusElement.count()) {
      statusText = (await statusElement.textContent()) || "";
    }

    // Look for pending amount
    let pendingAmount = 0;
    const pendingElement = page
      .locator('[data-testid="pending-amount"], text=/pending.*amount/i')
      .first();

    if (await pendingElement.count()) {
      const pendingText = await pendingElement.textContent();
      pendingAmount = parseFloat(pendingText?.match(/[\d.]+/)?.[0] || "0");
    }

    // Look for claimable amount
    let claimableAmount = 0;
    const claimableElement = page
      .locator('[data-testid="claimable-amount"], text=/claimable.*amount/i')
      .first();

    if (await claimableElement.count()) {
      const claimableText = await claimableElement.textContent();
      claimableAmount = parseFloat(claimableText?.match(/[\d.]+/)?.[0] || "0");
    }

    return {
      isSettled: statusText.toLowerCase().includes("settled") || claimableAmount > 0,
      isPending: statusText.toLowerCase().includes("pending") || pendingAmount > 0,
      isClaimable: statusText.toLowerCase().includes("claimable") || claimableAmount > 0,
      settledAmount: claimableAmount,
      pendingAmount,
      claimableAmount,
      lastUpdated: Date.now(),
    };
  } catch (err) {
    console.log(`Error getting settlement status: ${err}`);
    return {
      isSettled: false,
      isPending: false,
      isClaimable: false,
      settledAmount: 0,
      pendingAmount: 0,
      claimableAmount: 0,
      lastUpdated: Date.now(),
    };
  }
}

/**
 * Wait for a withdrawal to fully settle.
 *
 * Polls until the withdrawal is in a settled or claimable state,
 * indicating the backend has processed the on-chain transaction.
 *
 * @param page - Playwright page object
 * @param params - Configuration for settlement check
 * @returns SettlementStatus once settled
 * @throws if timeout or settlement fails
 */
export async function waitForSettlement(
  page: Page,
  params?: {
    timeout?: number;
    pollIntervalMs?: number;
    expectedAmount?: number;
  }
): Promise<SettlementStatus> {
  const {
    timeout = 120_000,
    pollIntervalMs = 5000,
    expectedAmount = 0,
  } = params || {};

  const startTime = Date.now();

  while (Date.now() - startTime < timeout) {
    // Reload to get fresh state from backend
    await page.reload({ waitUntil: "networkidle" });

    const status = await getSettlementStatus(page);

    if (status.isSettled || status.isClaimable) {
      console.log(
        `Settlement confirmed: claimable=${status.claimableAmount}, pending=${status.pendingAmount}`
      );
      return status;
    }

    if (status.isPending) {
      const elapsed = Math.round((Date.now() - startTime) / 1000);
      console.log(
        `Withdrawal pending (${elapsed}s): ${status.pendingAmount} awaiting settlement...`
      );
    }

    await page.waitForTimeout(pollIntervalMs);
  }

  throw new Error(
    `Settlement timeout after ${timeout}ms: withdrawal did not complete settlement`
  );
}

/**
 * Verify full settlement flow: request → pending → settled → claimed.
 *
 * Optionally claims the withdrawal if a "Claim" button is available.
 *
 * @param page - Playwright page object
 * @param params - Configuration
 * @throws if verification fails or settlement incomplete
 */
export async function verifySettlement(
  page: Page,
  params?: {
    expectedBalance?: number;
    timeout?: number;
    shouldClaim?: boolean;
  }
): Promise<void> {
  const {
    expectedBalance = 0,
    timeout = 120_000,
    shouldClaim = false,
  } = params || {};

  // Step 1: Wait for settlement
  const status = await waitForSettlement(page, { timeout });

  if (!status.isSettled && !status.isClaimable) {
    throw new Error(
      "Withdrawal failed to settle within timeout"
    );
  }

  // Step 2: Optionally claim the withdrawal
  if (shouldClaim && status.isClaimable) {
    const claimButton = page
      .locator('button:has-text("Claim"), button:has-text("Withdraw Funds")')
      .first();

    if (await claimButton.count()) {
      await claimButton.click({ timeout: 5_000 });
      console.log("Claim button clicked");

      // Wait for confirmation
      await page.waitForTimeout(2000);
    }
  }

  // Step 3: Verify final balance matches expected
  if (expectedBalance > 0) {
    const finalBalance = await getUIBalance(page);
    const tolerance = expectedBalance * 0.01; // 1% tolerance for rounding

    if (Math.abs(finalBalance.usdc - expectedBalance) > tolerance) {
      console.warn(
        `Final balance mismatch: expected ${expectedBalance}, got ${finalBalance.usdc} ` +
          `(diff: ${finalBalance.usdc - expectedBalance})`
      );

      // Check API balance as well
      const apiBalance = await getAPIBalance(page);
      console.log(`API balance: ${apiBalance.usdc} USDC`);

      if (Math.abs(apiBalance.usdc - expectedBalance) > tolerance) {
        throw new Error(
          `Settlement verification failed: balance ${apiBalance.usdc} does not match ` +
            `expected ${expectedBalance}`
        );
      }
    } else {
      console.log(
        `Settlement verified: final balance ${finalBalance.usdc} ≈ ${expectedBalance} USDC`
      );
    }
  }
}

/**
 * Check if there are pending withdrawals.
 *
 * Returns true if the account has any withdrawals awaiting settlement.
 */
export async function hasPendingWithdrawals(page: Page): Promise<boolean> {
  try {
    const pendingBadge = page
      .locator('[data-testid="pending-badge"], text=/pending/i, [class*="pending"]')
      .first();

    return await pendingBadge.count() > 0;
  } catch {
    return false;
  }
}

/**
 * Wait for all pending withdrawals to settle.
 *
 * Polls until the account has no pending withdrawals.
 */
export async function waitForAllWithdrawalsSettled(
  page: Page,
  timeoutMs: number = 120_000
): Promise<void> {
  const startTime = Date.now();

  while (Date.now() - startTime < timeoutMs) {
    const hasPending = await hasPendingWithdrawals(page);

    if (!hasPending) {
      console.log("All withdrawals settled");
      return;
    }

    console.log("Waiting for pending withdrawals to settle...");
    await page.reload({ waitUntil: "networkidle" });
    await page.waitForTimeout(5000);
  }

  throw new Error(
    `Pending withdrawals did not settle within ${timeoutMs}ms`
  );
}
