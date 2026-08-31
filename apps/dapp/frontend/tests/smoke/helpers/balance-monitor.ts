/**
 * balance-monitor.ts
 *
 * Balance monitoring and verification helpers for smoke tests.
 *
 * Verifies that UI balance displays and API responses accurately reflect
 * deposited and withdrawn amounts, allowing for small fee variations.
 */

import { type Page } from "@playwright/test";

export interface BalanceInfo {
  usdc: number;
  xlm: number;
  vaultShares: number;
  usdValue: number;
  lastUpdated: number;
}

/**
 * Extract current balance from the UI.
 *
 * Looks for balance displays on dashboard or portfolio page.
 * Returns main asset balances (USDC, XLM, vault shares).
 */
export async function getUIBalance(page: Page): Promise<BalanceInfo> {
  const balances: Record<string, number> = {
    USDC: 0,
    XLM: 0,
    shares: 0,
    usd: 0,
  };

  try {
    // Look for USDC balance
    const usdcElement = page
      .locator('[data-testid="usdc-balance"], text=/USDC/i, [class*="usdc"]')
      .first();

    if (await usdcElement.count()) {
      const usdcText = await usdcElement.textContent();
      balances.USDC = parseFloat(usdcText?.match(/[\d.]+/)?.[0] || "0");
    }

    // Look for XLM balance
    const xlmElement = page
      .locator('[data-testid="xlm-balance"], text=/XLM/i, [class*="xlm"]')
      .first();

    if (await xlmElement.count()) {
      const xlmText = await xlmElement.textContent();
      balances.XLM = parseFloat(xlmText?.match(/[\d.]+/)?.[0] || "0");
    }

    // Look for vault shares balance
    const sharesElement = page
      .locator(
        '[data-testid="vault-shares"], text=/shares/i, [class*="shares"]'
      )
      .first();

    if (await sharesElement.count()) {
      const sharesText = await sharesElement.textContent();
      balances.shares = parseFloat(sharesText?.match(/[\d.]+/)?.[0] || "0");
    }

    // Look for total USD value
    const totalElement = page
      .locator('[data-testid="total-value"], text=/total|USD/i, [class*="total"]')
      .first();

    if (await totalElement.count()) {
      const totalText = await totalElement.textContent();
      balances.usd = parseFloat(totalText?.match(/[\d.]+/)?.[0] || "0");
    }
  } catch (err) {
    console.log(`Error extracting UI balance: ${err}`);
  }

  return {
    usdc: balances.USDC,
    xlm: balances.XLM,
    vaultShares: balances.shares,
    usdValue: balances.usd,
    lastUpdated: Date.now(),
  };
}

/**
 * Query the API for current account balance.
 *
 * Makes a direct API call to get the authoritative balance from the backend.
 */
export async function getAPIBalance(
  page: Page,
  apiUrl: string = "http://localhost:8080/api/v1"
): Promise<BalanceInfo> {
  try {
    // Get current user/account context from page
    const userContext = await page.evaluate(() => {
      // Try to read from localStorage or sessionStorage
      const token = localStorage.getItem("auth_token") || sessionStorage.getItem("auth_token");
      const userId = localStorage.getItem("user_id");
      return { token, userId };
    });

    if (!userContext.token) {
      throw new Error("No auth token available");
    }

    // Fetch balances from API
    const response = await page.evaluate(
      async ({ apiUrl, token }) => {
        const res = await fetch(`${apiUrl}/portfolio/balance`, {
          headers: {
            Authorization: `Bearer ${token}`,
            Accept: "application/json",
          },
        });

        if (!res.ok) {
          throw new Error(`API returned ${res.status}`);
        }

        return res.json();
      },
      { apiUrl, token: userContext.token }
    );

    return {
      usdc: response.usdc || 0,
      xlm: response.xlm || 0,
      vaultShares: response.vault_shares || 0,
      usdValue: response.usd_value || 0,
      lastUpdated: Date.now(),
    };
  } catch (err) {
    console.log(`Error fetching API balance: ${err}`);
    return {
      usdc: 0,
      xlm: 0,
      vaultShares: 0,
      usdValue: 0,
      lastUpdated: Date.now(),
    };
  }
}

/**
 * Wait for balance to update after a transaction.
 *
 * Polls the UI (or API) until the balance reflects the expected change.
 * Allows for small variations due to fees.
 *
 * @param page - Playwright page object
 * @param params - Configuration for balance check
 * @returns BalanceInfo once target is reached
 * @throws if timeout or balance doesn't reach target
 */
export async function waitForBalanceUpdate(
  page: Page,
  params?: {
    expectedMinimumBalance?: number;
    expectedAsset?: string;
    timeout?: number;
    pollIntervalMs?: number;
    useAPI?: boolean;
  }
): Promise<BalanceInfo> {
  const {
    expectedMinimumBalance = 0,
    expectedAsset = "USDC",
    timeout = 60_000,
    pollIntervalMs = 3000,
    useAPI = false,
  } = params || {};

  const startTime = Date.now();

  while (Date.now() - startTime < timeout) {
    const balance = useAPI
      ? await getAPIBalance(page)
      : await getUIBalance(page);

    const currentBalance =
      expectedAsset === "USDC"
        ? balance.usdc
        : expectedAsset === "XLM"
          ? balance.xlm
          : balance.vaultShares;

    if (currentBalance >= expectedMinimumBalance) {
      console.log(
        `Balance updated: ${expectedAsset} ${currentBalance} ` +
          `(expected >= ${expectedMinimumBalance})`
      );
      return balance;
    }

    console.log(
      `Waiting for balance update: ${currentBalance}/${expectedMinimumBalance} ${expectedAsset}...`
    );

    await page.waitForTimeout(pollIntervalMs);
  }

  throw new Error(
    `Balance did not reach expected minimum of ${expectedMinimumBalance} ${expectedAsset} ` +
      `within ${timeout}ms`
  );
}

/**
 * Verify balance reconciliation between UI and API.
 *
 * Fetches both UI and API balances and checks they match within tolerance.
 * Helps detect UI-API sync issues.
 */
export async function verifyBalanceReconciliation(
  page: Page,
  tolerancePercent: number = 5
): Promise<{ ui: BalanceInfo; api: BalanceInfo; reconciled: boolean }> {
  const ui = await getUIBalance(page);
  const api = await getAPIBalance(page);

  const tolerance = (value: number) => (value * tolerancePercent) / 100;

  const reconciled =
    Math.abs(ui.usdc - api.usdc) <= tolerance(api.usdc) &&
    Math.abs(ui.xlm - api.xlm) <= tolerance(api.xlm) &&
    Math.abs(ui.vaultShares - api.vaultShares) <= tolerance(api.vaultShares);

  console.log(
    `Balance reconciliation: ${reconciled ? "OK" : "MISMATCH"} ` +
      `(UI: ${ui.usdc} USDC, API: ${api.usdc} USDC)`
  );

  return { ui, api, reconciled };
}

/**
 * Wait for balance reconciliation between UI and API.
 *
 * Polls until balances match, indicating the UI has synced with backend.
 */
export async function waitForBalanceReconciliation(
  page: Page,
  params?: {
    timeout?: number;
    pollIntervalMs?: number;
    tolerancePercent?: number;
  }
): Promise<{ ui: BalanceInfo; api: BalanceInfo }> {
  const {
    timeout = 30_000,
    pollIntervalMs = 2000,
    tolerancePercent = 5,
  } = params || {};

  const startTime = Date.now();

  while (Date.now() - startTime < timeout) {
    const { ui, api, reconciled } = await verifyBalanceReconciliation(
      page,
      tolerancePercent
    );

    if (reconciled) {
      console.log("Balance reconciliation verified");
      return { ui, api };
    }

    await page.waitForTimeout(pollIntervalMs);
  }

  throw new Error(
    `Balance reconciliation timeout after ${timeout}ms: UI and API balances did not match`
  );
}
