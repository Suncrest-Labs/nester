/**
 * deposit-flow.ts
 *
 * Smoke test flow for executing vault deposits.
 *
 * Orchestrates the deposit UI flow:
 * 1. Navigate to deposit modal or action
 * 2. Enter deposit amount
 * 3. Confirm transaction (wallet popup)
 * 4. Poll for on-chain confirmation
 * 5. Verify receipt
 */

import { type Page } from "@playwright/test";
import { pollTransactionConfirmation } from "./tx-helpers";

export interface DepositResult {
  txHash: string;
  amount: number;
  asset: string;
  explorerUrl: string;
  ledger: number;
  durationMs: number;
}

export interface DepositParams {
  amount: number;
  walletAddress: string;
  asset?: string;
  timeout?: number;
}

/**
 * Execute a vault deposit transaction through the UI.
 *
 * @param page - Playwright page object
 * @param params - Deposit parameters (amount, wallet address, etc.)
 * @returns DepositResult with transaction confirmation details
 * @throws if deposit fails or times out
 */
export async function performDeposit(
  page: Page,
  params: DepositParams
): Promise<DepositResult> {
  const startTime = Date.now();
  const { amount, timeout = 90_000 } = params;
  const asset = params.asset || "USDC";

  // Step 1: Navigate to dashboard if not already there
  if (!page.url().includes("/dashboard")) {
    await page.goto("/dashboard", { waitUntil: "networkidle" });
  }

  // Step 2: Find and click the deposit button
  const depositButton = page
    .locator('button:has-text("Deposit"), button:has-text("Add Funds"), [data-testid="deposit-btn"], button:has-text("Start Earning")')
    .first();

  await depositButton.waitFor({ state: "visible", timeout: 10_000 });
  await depositButton.click({ timeout: 10_000 });

  // Step 3: Wait for deposit modal to appear
  const depositModal = page
    .locator('[role="dialog"]:has-text("Deposit"), .deposit-modal, [class*="modal"]:visible')
    .first();

  await depositModal.waitFor({ state: "visible", timeout: 15_000 });

  // Step 4: Enter deposit amount
  const amountInput = page
    .locator('input[type="number"], input[placeholder*="amount" i], input[name*="amount"]')
    .first();

  await amountInput.fill(String(amount), { timeout: 5_000 });

  // Step 5: Review and confirm
  // Wait for balance/fee display to update
  await page.waitForTimeout(1000);

  // Click confirm/deposit button
  const confirmButton = page
    .locator(
      'button:has-text("Confirm"), button:has-text("Deposit"), button[type="submit"]:visible'
    )
    .first();

  await confirmButton.click({ timeout: 5_000 });

  // Step 6: Handle wallet popup (if it appears)
  // The wallet provider should auto-sign for test wallets
  // Wait for the modal to close or transaction to start
  await page.waitForTimeout(2000);

  // Step 7: Extract transaction hash from UI
  // Look for transaction receipt or confirmation message
  const txHashElement = page
    .locator(
      '[data-testid="tx-hash"], .tx-hash, [class*="txHash"], [class*="transaction-id"]'
    )
    .first();

  let txHash: string | undefined;
  try {
    await txHashElement.waitFor({ state: "visible", timeout: 10_000 });
    const hashText = await txHashElement.textContent();
    // Extract hex string from text (could be "Hash: abc123..." or just "abc123...")
    txHash = hashText?.match(/[a-fA-F0-9]{64}/)?.[0];
  } catch {
    // Deliberately no fallback. A smoke test that invents a transaction hash
    // reports PASS for a completely broken deposit path and promotes the
    // deploy, which is worse than having no gate at all.
    throw new Error(
      "Deposit did not surface a transaction hash in the UI. The deposit flow " +
        "did not complete; failing rather than substituting a placeholder."
    );
  }

  if (!txHash) {
    throw new Error("Could not extract transaction hash from deposit response");
  }

  // Confirm the transaction actually landed on-chain. Reading a hash out of
  // the DOM only proves the UI rendered something.
  const confirmation = await pollTransactionConfirmation(txHash);
  if (confirmation.status !== "success") {
    throw new Error(
      `Deposit transaction ${txHash} did not succeed on-chain: ` +
        `${confirmation.errorReason ?? confirmation.status}`
    );
  }

  const explorerUrl = `https://stellar.expert/explorer/testnet/tx/${txHash}`;

  const durationMs = Date.now() - startTime;

  console.log(
    `✓ Deposit confirmed: ${amount} ${asset} (tx: ${txHash.slice(0, 8)}..., ledger ${confirmation.ledger ?? "unknown"}, ${durationMs}ms)`
  );

  return {
    txHash,
    amount,
    asset,
    explorerUrl,
    ledger: confirmation.ledger ?? 0,
    durationMs,
  };
}

/**
 * Wait for a deposit transaction to appear in the UI transaction list.
 *
 * Used to verify that the backend has processed the deposit after
 * on-chain confirmation.
 */
export async function waitForDepositInTransactionList(
  page: Page,
  txHash: string,
  timeoutMs: number = 30_000
): Promise<void> {
  const startTime = Date.now();

  while (Date.now() - startTime < timeoutMs) {
    // Navigate to transactions page if needed
    if (!page.url().includes("/transactions")) {
      await page.goto("/transactions", { waitUntil: "networkidle" });
    }

    // Look for transaction in list
    const txElement = page.locator(`text=${txHash.slice(0, 8)}`);

    if (await txElement.count()) {
      console.log(`Deposit transaction found in list: ${txHash.slice(0, 8)}...`);
      return;
    }

    // Refresh and retry
    await page.reload({ waitUntil: "networkidle" });
    await page.waitForTimeout(2000);
  }

  throw new Error(
    `Deposit transaction not found in list after ${timeoutMs}ms: ${txHash}`
  );
}
