/**
 * tx-helpers.ts
 *
 * Transaction polling and confirmation helpers for smoke tests.
 *
 * Provides deterministic, timeout-aware polling for Stellar transaction
 * confirmation. Reuses the patterns from lib/stellar/transaction.ts but
 * tailored for smoke test needs with adjustable timeouts and retry logic.
 */

/**
 * Configuration for transaction polling behavior.
 */
export interface PollConfig {
  /** Poll interval in milliseconds. Default: 2000ms */
  pollIntervalMs?: number;
  /** Maximum total wait time before timeout. Default: 90000ms (90 seconds) */
  timeoutMs?: number;
  /** Maximum number of retries on transient errors. Default: 3 */
  maxRetries?: number;
}

/**
 * Result of a completed transaction poll.
 */
export interface TransactionStatus {
  /** Transaction hash (64-character hex string) */
  txHash: string;
  /** "success" if confirmed on-chain, "failed" if rejected, "pending" if still in progress */
  status: "success" | "failed" | "pending";
  /** Ledger number if confirmed, undefined if pending */
  ledger?: number;
  /** Error reason if status is "failed" */
  errorReason?: string;
  /** Total milliseconds spent polling */
  durationMs: number;
  /** Number of polls performed */
  pollCount: number;
}

/**
 * Poll Horizon API until a transaction is confirmed or timeout reached.
 *
 * Implements exponential backoff for transient errors and strict timeouts
 * for CI reliability. Returns success once the tx appears in a ledger.
 *
 * @param txHash - Transaction hash to poll for
 * @param config - Polling configuration
 * @param horizonUrl - Horizon endpoint URL
 * @returns TransactionStatus once settled (success, failed, or timeout)
 */
export async function pollTransactionConfirmation(
  txHash: string,
  config: PollConfig = {},
  horizonUrl: string = "https://horizon-testnet.stellar.org"
): Promise<TransactionStatus> {
  const {
    pollIntervalMs = 2000,
    timeoutMs = 90_000,
    maxRetries = 3,
  } = config;

  const startTime = Date.now();
  let pollCount = 0;
  let retryCount = 0;

  while (Date.now() - startTime < timeoutMs) {
    pollCount++;

    try {
      const response = await fetch(
        `${horizonUrl}/transactions/${txHash}`,
        {
          headers: { Accept: "application/json" },
          signal: AbortSignal.timeout(10_000),
        }
      );

      if (response.status === 404) {
        // Transaction not yet in ledger, continue polling
        await new Promise((r) => setTimeout(r, pollIntervalMs));
        continue;
      }

      if (!response.ok) {
        // Transient error, retry with backoff
        if (retryCount < maxRetries) {
          retryCount++;
          const backoffMs = Math.min(1000 * Math.pow(2, retryCount), 10_000);
          console.log(
            `Poll failed (${response.status}), retrying after ${backoffMs}ms...`
          );
          await new Promise((r) => setTimeout(r, backoffMs));
          continue;
        }

        return {
          txHash,
          status: "failed",
          errorReason: `HTTP ${response.status}`,
          durationMs: Date.now() - startTime,
          pollCount,
        };
      }

      // Transaction found in ledger
      const txData = (await response.json()) as Record<string, unknown>;

      if (txData.result_code && typeof txData.result_code === "string") {
        const resultCode = txData.result_code;

        // Result codes starting with "tx_" indicate success or specific failures
        if (resultCode === "tx_success" || resultCode.startsWith("tx_")) {
          // Check Soroban result for failures
          const resultXdr = txData.result_xdr;
          const isFailed =
            resultCode !== "tx_success" ||
            (typeof resultXdr === "string" && resultXdr.includes("Failure"));

          return {
            txHash,
            status: isFailed ? "failed" : "success",
            ledger: typeof txData.ledger_attr === "number" ? txData.ledger_attr : undefined,
            errorReason: isFailed ? resultCode : undefined,
            durationMs: Date.now() - startTime,
            pollCount,
          };
        }
      }

      // Default: assume success if we got a response with tx data
      return {
        txHash,
        status: "success",
        ledger: typeof txData.ledger_attr === "number" ? txData.ledger_attr : undefined,
        durationMs: Date.now() - startTime,
        pollCount,
      };
    } catch (err) {
      // Network error, retry
      if (retryCount < maxRetries) {
        retryCount++;
        const backoffMs = Math.min(500 * Math.pow(2, retryCount), 5000);
        console.log(`Poll network error: ${err}, retrying after ${backoffMs}ms...`);
        await new Promise((r) => setTimeout(r, backoffMs));
        continue;
      }

      // Max retries exceeded
      return {
        txHash,
        status: "failed",
        errorReason: `Network error after ${maxRetries} retries: ${err}`,
        durationMs: Date.now() - startTime,
        pollCount,
      };
    }
  }

  // Timeout reached
  return {
    txHash,
    status: "pending",
    errorReason: `Confirmation timeout after ${timeoutMs}ms (${pollCount} polls)`,
    durationMs: timeoutMs,
    pollCount,
  };
}

/**
 * Wait for a transaction to confirm or fail with a strict timeout.
 *
 * Throws an error if confirmation fails or times out.
 *
 * @param txHash - Transaction hash
 * @param config - Polling config with timeoutMs
 * @param horizonUrl - Horizon endpoint
 * @throws if transaction fails or timeout reached
 */
export async function waitForTransactionConfirmation(
  txHash: string,
  config: PollConfig = {},
  horizonUrl?: string
): Promise<void> {
  const result = await pollTransactionConfirmation(txHash, config, horizonUrl);

  if (result.status === "failed") {
    throw new Error(
      `Transaction failed: ${result.errorReason} (${result.pollCount} polls, ${result.durationMs}ms)`
    );
  }

  if (result.status === "pending") {
    throw new Error(
      `Transaction confirmation timeout: ${result.errorReason}`
    );
  }

  console.log(
    `Transaction confirmed: ${txHash.slice(0, 8)}... in ledger ${result.ledger} ` +
      `(${result.pollCount} polls, ${result.durationMs}ms)`
  );
}

/**
 * Get the current ledger height from Horizon.
 *
 * Useful for monitoring network health during smoke tests.
 */
export async function getCurrentLedgerHeight(
  horizonUrl: string = "https://horizon-testnet.stellar.org"
): Promise<number> {
  const response = await fetch(`${horizonUrl}/`, {
    headers: { Accept: "application/json" },
    signal: AbortSignal.timeout(5_000),
  });

  if (!response.ok) {
    throw new Error(`Failed to get ledger info: HTTP ${response.status}`);
  }

  const data = (await response.json()) as Record<string, unknown>;
  const ledger = typeof data.sequence === "number" ? data.sequence : 0;

  if (ledger === 0) {
    throw new Error("Horizon returned invalid ledger sequence");
  }

  return ledger;
}

/**
 * Calculate transaction fee in stroops for smoke test budgeting.
 *
 * Stellar base fee is 100 stroops per operation.
 * Soroban operations typically cost more.
 *
 * @param operationCount - Number of operations (default 1)
 * @returns Fee in stroops
 */
export function estimateSmokeTestFee(operationCount: number = 1): number {
  const BASE_FEE_STROOPS = 100;
  const SOROBAN_MULTIPLIER = 5; // Soroban ops are ~5x base fee

  return BASE_FEE_STROOPS * operationCount * SOROBAN_MULTIPLIER;
}

/**
 * Convert stroops to XLM for display.
 */
export function stroopsToXlm(stroops: number): number {
  return stroops / 10_000_000;
}

/**
 * Convert XLM to stroops.
 */
export function xlmToStroops(xlm: number): number {
  return xlm * 10_000_000;
}
