/**
 * withdraw-flow.ts
 *
 * Smoke test flow for executing vault withdrawals.
 *
 * Orchestrates the withdraw UI flow:
 * 1. Navigate to withdraw modal or action
 * 2. Enter withdraw shares or amount
 * 3. Review exchange rate and fees
 * 4. Confirm transaction (wallet popup)
 * 5. Poll for on-chain confirmation
 */

import { type Page } from "@playwright/test";
import { waitForTransactionConfirmation } from "./tx-helpers";

export interface WithdrawResult {
  txHash: string;
  sharesWithdrawn: number;
  assetsReceived: number;
  asset: string;
  explorerUrl: string;
  durationMs: number;
}

export interface WithdrawParams {
  sharesRatio?: number; // 0.0 to 1.0, ratio of total shares to withdraw
  amount?: number; // Or specify exact amount instead of ratio
  asset?: string;
  timeout?: number;
}

/**
 * Execute a vault withdrawal transaction through the UI.
 *
 * @param page - Playwright page object
 * @param params - Withdrawal parameters
 * @returns WithdrawResult with transaction confirmation details
 * @throws if withdrawal fails or times out
 */
export async function performWithdraw(
  page: Page,
  params: WithdrawParams
): Promise<WithdrawResult> {
  const startTime = Date.now();
  const { sharesRatio = 1.0, timeout = 90_000 } = params;
  const asset = params.asset || "USDC";

  if (sharesRatio <= 0 || sharesRatio > 1) {
    throw new Error(`Invalid sharesRatio: ${sharesRatio} (must be 0 < ratio <= 1)`);
  }

  // Step 1: Navigate to withdraw action
  const withdrawButton = page
    .locator(
      'button:has-text("Withdraw"), button:has-text("Redeem"), [data-testid="withdraw-btn"]'
    )
    .first();

  await withdrawButton.click({ timeout: 10_000 });

  // Step 2: Wait for withdraw modal to appear
  const withdrawModal = page
    .locator('[role="dialog"]:has-text("Withdraw"), .withdraw-modal, [class*="modal"]:visible')
    .first();

  await withdrawModal.waitFor({ state: "visible", timeout: 15_000 });

  // Step 3: Determine withdrawal amount
  // Try to find current shares balance first
  let withdrawAmount: string | number = sharesRatio;

  const balanceDisplay = page
    .locator('[data-testid="shares-balance"], .shares-balance, [class*="balance"]')
    .first();

  try {
    const balanceText = await balanceDisplay.textContent({ timeout: 5_000 });
    const currentShares = parseFloat(balanceText?.match(/[\d.]+/)?.[0] || "0");

    if (currentShares > 0) {
      withdrawAmount = Math.round(currentShares * sharesRatio * 1000000) / 1000000; // 6 decimals
    }
  } catch {
    console.log("Could not extract current shares balance, using ratio");
  }

  // Step 4: Enter withdrawal amount
  const amountInput = page
    .locator('input[type="number"], input[placeholder*="amount" i], input[name*="amount"]')
    .first();

  await amountInput.fill(String(withdrawAmount), { timeout: 5_000 });

  // Step 5: Wait for exchange rate and fees to display
  await page.waitForTimeout(1500);

  // Extract expected assets to receive for later verification
  const assetsDisplay = page
    .locator(
      '[data-testid="assets-to-receive"], .assets-received, [class*="receive"], text=/you will receive/i'
    )
    .first();

  let assetsReceived = 0;
  try {
    const assetsText = await assetsDisplay.textContent({ timeout: 5_000 });
    assetsReceived = parseFloat(assetsText?.match(/[\d.]+/)?.[0] || "0");
  } catch {
    console.log("Could not extract assets to receive");
  }

  // Step 6: Confirm withdrawal
  const confirmButton = page
    .locator(
      'button:has-text("Confirm"), button:has-text("Withdraw"), button[type="submit"]:visible'
    )
    .first();

  await confirmButton.click({ timeout: 5_000 });

  // Step 7: Handle wallet popup
  await page.waitForTimeout(2000);

  // Step 8: Extract transaction hash
  const txHashElement = page
    .locator(
      '[data-testid="tx-hash"], .tx-hash, [class*="txHash"], [class*="transaction-id"]'
    )
    .first();

  let txHash: string | undefined;
  try {
    await txHashElement.waitFor({ state: "visible", timeout: 10_000 });
    const hashText = await txHashElement.textContent();
    txHash = hashText?.match(/[a-fA-F0-9]{64}/)?.[0];
  } catch {
    console.log("Transaction hash not visible in UI");
  }

  if (!txHash) {
    throw new Error("Could not extract transaction hash from withdrawal response");
  }

  // Step 9: Poll for confirmation
  await waitForTransactionConfirmation(txHash, {
    timeoutMs: timeout,
    pollIntervalMs: 2000,
  });

  // Step 10: Build result
  const explorerUrl = `https://stellar.expert/explorer/testnet/tx/${txHash}`;
  const durationMs = Date.now() - startTime;

  console.log(
    `Withdrawal completed: ${withdrawAmount} shares → ~${assetsReceived} ${asset} ` +
      `(tx: ${txHash.slice(0, 8)}..., ${durationMs}ms)`
  );

  return {
    txHash,
    sharesWithdrawn: Number(withdrawAmount),
    assetsReceived,
    asset,
    explorerUrl,
    durationMs,
  };
}

/**
 * Wait for a withdrawal to settle (become claimable or appear in pending balance).
 *
 * Withdrawals may not be immediately claimable; they may enter a settlement period.
 * This helper polls until the withdrawal is in a settled/claimable state.
 */
export async function waitForWithdrawalSettlement(
  page: Page,
  txHash: string,
  timeoutMs: number = 60_000
): Promise<void> {
  const startTime = Date.now();

  while (Date.now() - startTime < timeoutMs) {
    // Refresh balance and settlement status
    await page.reload({ waitUntil: "networkidle" });

    // Look for settlement status indicator
    const settlementStatus = page
      .locator(
        'text=/settlement|pending|claimable/i, [data-testid="settlement-status"], [class*="settlement"]'
      )
      .first();

    if (await settlementStatus.count()) {
      const statusText = await settlementStatus.textContent();

      if (statusText?.toLowerCase().includes("claimable")) {
        console.log("Withdrawal is now claimable");
        return;
      }

      if (statusText?.toLowerCase().includes("settled")) {
        console.log("Withdrawal has settled");
        return;
      }
    }

    // Check if withdrawal appears in pending or history
    const txElement = page.locator(`text=${txHash.slice(0, 8)}`);
    if (await txElement.count()) {
      console.log(`Withdrawal found in history: ${txHash.slice(0, 8)}...`);
      return;
    }

    await page.waitForTimeout(3000);
  }

  throw new Error(
    `Withdrawal settlement timeout after ${timeoutMs}ms: ${txHash}`
  );
}
