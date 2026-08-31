/**
 * faucet.ts
 *
 * Testnet faucet interaction for funding smoke test wallets.
 *
 * This module provides helpers to request testnet funds (XLM) from the
 * Stellar Friendbot faucet, required to pay transaction fees during
 * smoke test execution.
 *
 * Only works on testnet. Mainnet tests should use pre-funded accounts
 * or skip the faucet step.
 */

/**
 * Fund a testnet Stellar account using Friendbot.
 *
 * Requests an initial amount of XLM from the Friendbot faucet.
 * Used to bootstrap the smoke test wallet with transaction fees.
 *
 * @param stellarAddress - Public key to fund (G...)
 * @param friendbotUrl - Friendbot endpoint (defaults to SDF public instance)
 * @returns Promise resolving to the transaction hash on success
 * @throws if the request fails or faucet is unavailable
 */
export async function fundFromFaucet(
  stellarAddress: string,
  friendbotUrl: string = "https://friendbot.stellar.org"
): Promise<string> {
  if (!stellarAddress.startsWith("G")) {
    throw new Error(`Invalid Stellar address: ${stellarAddress}`);
  }

  try {
    // Friendbot endpoint: GET /friendbot?addr=GABC...
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 30_000);

    const response = await fetch(`${friendbotUrl}?addr=${encodeURIComponent(stellarAddress)}`, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal: controller.signal,
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(
        `Friendbot failed with status ${response.status}: ${errorText.slice(0, 200)}`
      );
    }

    const result = (await response.json()) as Record<string, unknown>;

    // Friendbot returns the ledger result containing the tx hash
    if (
      result.hash &&
      typeof result.hash === "string"
    ) {
      return result.hash;
    }

    throw new Error("Faucet response missing transaction hash");
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(`Faucet funding failed: ${message}`);
  }
}

/**
 * Wait for faucet funds to be confirmed on-chain.
 *
 * After requesting funds, polls Horizon until the account has
 * the native XLM balance on the ledger.
 *
 * @param stellarAddress - Address that was funded
 * @param minBalance - Minimum XLM balance to confirm (default 10 XLM)
 * @param horizonUrl - Horizon endpoint (defaults to SDF public instance)
 * @param timeoutMs - Maximum wait time in milliseconds
 * @returns Promise resolving when balance is confirmed
 * @throws if timeout or balance confirmation fails
 */
export async function waitForFaucetFunding(
  stellarAddress: string,
  minBalance: number = 10,
  horizonUrl: string = "https://horizon-testnet.stellar.org",
  timeoutMs: number = 60_000
): Promise<{ balance: string; sequence: string }> {
  const startTime = Date.now();
  const pollIntervalMs = 2_000;

  while (Date.now() - startTime < timeoutMs) {
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 10_000);

      const response = await fetch(`${horizonUrl}/accounts/${stellarAddress}`, {
        headers: { Accept: "application/json" },
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        // 404 means account not yet created
        if (response.status === 404) {
          await new Promise((r) => setTimeout(r, pollIntervalMs));
          continue;
        }
        throw new Error(`Horizon status ${response.status}`);
      }

      const account = (await response.json()) as Record<string, unknown>;

      // Parse native XLM balance from balances array
      const balances = Array.isArray(account.balances)
        ? (account.balances as Array<Record<string, unknown>>)
        : [];

      const nativeBalance = balances.find(
        (b) => b.asset_type === "native"
      );

      if (
        nativeBalance &&
        typeof nativeBalance.balance === "string"
      ) {
        const balance = parseFloat(nativeBalance.balance);
        if (balance >= minBalance) {
          return {
            balance: nativeBalance.balance,
            sequence: String(account.sequence || "0"),
          };
        }
      }

      // Not ready yet, poll again
      await new Promise((r) => setTimeout(r, pollIntervalMs));
    } catch (err) {
      // Network error, retry
      console.error(`Faucet funding poll error: ${err}`);
      await new Promise((r) => setTimeout(r, pollIntervalMs));
    }
  }

  throw new Error(
    `Faucet funding confirmation timeout after ${timeoutMs}ms: ` +
      `balance did not reach ${minBalance} XLM`
  );
}

/**
 * Request testnet funding for a smoke test wallet.
 *
 * Combines faucet request and confirmation polling into a single call.
 *
 * @param stellarAddress - Public key to fund
 * @param options - Configuration options
 * @returns Promise resolving with account info once funded
 * @throws if funding fails or times out
 */
export async function requestSmokeTestFunding(
  stellarAddress: string,
  options?: {
    friendbotUrl?: string;
    horizonUrl?: string;
    minBalance?: number;
    timeoutMs?: number;
  }
): Promise<{ txHash: string; balance: string }> {
  const {
    friendbotUrl = "https://friendbot.stellar.org",
    horizonUrl = "https://horizon-testnet.stellar.org",
    minBalance = 10,
    timeoutMs = 60_000,
  } = options || {};

  // Step 1: Request funds from faucet
  const txHash = await fundFromFaucet(stellarAddress, friendbotUrl);
  console.log(`Faucet request sent, tx: ${txHash.slice(0, 8)}...`);

  // Step 2: Wait for confirmation
  const accountInfo = await waitForFaucetFunding(
    stellarAddress,
    minBalance,
    horizonUrl,
    timeoutMs
  );
  console.log(`Faucet funding confirmed: ${accountInfo.balance} XLM`);

  return {
    txHash,
    balance: accountInfo.balance,
  };
}

/**
 * Check if an account has sufficient XLM for smoke test operations.
 *
 * @param stellarAddress - Address to check
 * @param minBalance - Minimum required XLM (default 5 XLM for operations + fees)
 * @param horizonUrl - Horizon endpoint
 * @returns Promise resolving to true if account has sufficient balance
 */
export async function hasSufficientBalance(
  stellarAddress: string,
  minBalance: number = 5,
  horizonUrl: string = "https://horizon-testnet.stellar.org"
): Promise<boolean> {
  try {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 10_000);

    const response = await fetch(`${horizonUrl}/accounts/${stellarAddress}`, {
      headers: { Accept: "application/json" },
      signal: controller.signal,
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      return false;
    }

    const account = (await response.json()) as Record<string, unknown>;
    const balances = Array.isArray(account.balances)
      ? (account.balances as Array<Record<string, unknown>>)
      : [];

    const nativeBalance = balances.find(
      (b) => b.asset_type === "native"
    );

    if (nativeBalance && typeof nativeBalance.balance === "string") {
      const balance = parseFloat(nativeBalance.balance);
      return balance >= minBalance;
    }

    return false;
  } catch {
    return false;
  }
}
