import { describe, expect, it, vi } from "vitest";
import type { Transaction } from "@stellar/stellar-sdk";
import {
  DEFAULT_TRANSACTION_TIMEOUT_SECONDS,
  TransactionExpiredError,
  isBuiltTransactionExpired,
  isTransactionTooLateError,
  signWithRebuildOnExpiry,
  type BuiltTransaction,
} from "@/lib/stellar/transaction";

describe("transaction timeout & delayed signer rebuild (#1510)", () => {
  it("sets default transaction timeout to 180 seconds for hardware/mobile wallets", () => {
    expect(DEFAULT_TRANSACTION_TIMEOUT_SECONDS).toBe(180);
  });

  it("rebuilds and re-simulates when a delayed signer completes after transaction expiry", async () => {
    let clockMs = 1_000_000;
    let buildCount = 0;

    const buildFn = vi.fn(async (): Promise<BuiltTransaction> => {
      buildCount += 1;
      return {
        xdr: `unsigned-xdr-${buildCount}`,
        transaction: {
          timeBounds: {
            minTime: "0",
            maxTime: String(
              Math.floor(clockMs / 1000) + DEFAULT_TRANSACTION_TIMEOUT_SECONDS
            ),
          },
        } as unknown as Transaction,
        expiresAtMs: clockMs + DEFAULT_TRANSACTION_TIMEOUT_SECONDS * 1000,
      };
    });

    const signFn = vi.fn(async (xdr: string): Promise<string> => {
      if (xdr === "unsigned-xdr-1") {
        // First signature on Ledger takes 195 seconds -> exceeds 180s window
        clockMs += 195_000;
      } else {
        // Second signature after rebuild completes in 15 seconds
        clockMs += 15_000;
      }
      return `signed:${xdr}`;
    });

    const result = await signWithRebuildOnExpiry(buildFn, signFn, () => clockMs);

    expect(buildFn).toHaveBeenCalledTimes(2);
    expect(signFn).toHaveBeenCalledTimes(2);
    expect(signFn).toHaveBeenNthCalledWith(1, "unsigned-xdr-1");
    expect(signFn).toHaveBeenNthCalledWith(2, "unsigned-xdr-2");
    expect(result).toBe("signed:unsigned-xdr-2");
  });

  it("throws TransactionExpiredError with 'Signing took too long, try again.' if rebuilt transaction also expires", async () => {
    let clockMs = 1_000_000;

    const buildFn = vi.fn(async (): Promise<BuiltTransaction> => ({
      xdr: "unsigned-xdr",
      transaction: {} as unknown as Transaction,
      expiresAtMs: clockMs + DEFAULT_TRANSACTION_TIMEOUT_SECONDS * 1000,
    }));

    const signFn = vi.fn(async (xdr: string): Promise<string> => {
      clockMs += 200_000;
      return `signed:${xdr}`;
    });

    await expect(
      signWithRebuildOnExpiry(buildFn, signFn, () => clockMs)
    ).rejects.toThrowError(TransactionExpiredError);

    await expect(
      signWithRebuildOnExpiry(buildFn, signFn, () => clockMs)
    ).rejects.toThrow("Signing took too long, try again.");
  });

  it("detects txTOO_LATE errors and expired timeBounds", () => {
    expect(isTransactionTooLateError("txTOO_LATE")).toBe(true);
    expect(isTransactionTooLateError("Transaction failed: tx_too_late")).toBe(
      true
    );
    expect(isTransactionTooLateError("txBAD_SEQ")).toBe(false);

    const built: BuiltTransaction = {
      xdr: "xdr",
      transaction: {
        timeBounds: { minTime: "0", maxTime: "200" },
      } as unknown as Transaction,
    };
    expect(isBuiltTransactionExpired(built, 199_000)).toBe(false);
    expect(isBuiltTransactionExpired(built, 200_000)).toBe(true);
  });
});
