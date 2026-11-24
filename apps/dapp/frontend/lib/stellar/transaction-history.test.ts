import { beforeEach, describe, expect, it } from "vitest";
import {
  MAX_STORED_TRANSACTIONS,
  getSubmittedTransactions,
  hasAmbiguousTransaction,
  isTransactionRetryable,
  recordSubmittedTransaction,
  updateSubmittedTransactionStatus,
} from "@/lib/stellar/transaction-history";

describe("submitted transaction history", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("persists pending submissions separately for each wallet and network", () => {
    recordSubmittedTransaction("GONE", "testnet", {
      txHash: "a".repeat(64),
      minTime: 10,
      maxTime: 20,
    });

    expect(getSubmittedTransactions("GONE", "testnet")[0].status).toBe("pending");
    expect(getSubmittedTransactions("GOTHER", "testnet")).toEqual([]);
    expect(getSubmittedTransactions("GONE", "mainnet")).toEqual([]);
  });

  it.each(["confirmed", "failed"] as const)(
    "transitions pending submissions to %s",
    (status) => {
      const { txHash } = recordSubmittedTransaction("GONE", "testnet", {
        txHash: "b".repeat(64),
        minTime: 10,
        maxTime: 20,
      });

      expect(
        updateSubmittedTransactionStatus("GONE", "testnet", txHash, status)
      ).toMatchObject({ txHash, status });
      expect(getSubmittedTransactions("GONE", "testnet")[0].status).toBe(status);
      expect(hasAmbiguousTransaction(getSubmittedTransactions("GONE", "testnet"))).toBe(false);
    }
  );

  it("archives only after the transaction time bounds have expired and then allows retry", () => {
    const { txHash } = recordSubmittedTransaction("GONE", "testnet", {
      txHash: "c".repeat(64),
      minTime: 10,
      maxTime: 20,
    });
    const nowBeforeExpiry = 20_000;

    expect(
      updateSubmittedTransactionStatus("GONE", "testnet", txHash, "archived", nowBeforeExpiry)
    ).toMatchObject({ status: "pending" });
    expect(
      updateSubmittedTransactionStatus("GONE", "testnet", txHash, "archived", 20_001)
    ).toMatchObject({ status: "archived" });
    expect(
      isTransactionRetryable(getSubmittedTransactions("GONE", "testnet")[0], 20_001)
    ).toBe(true);
    expect(hasAmbiguousTransaction(getSubmittedTransactions("GONE", "testnet"))).toBe(false);
  });

  it("does not regress a terminal status and keeps only the newest bounded history", () => {
    const first = recordSubmittedTransaction("GONE", "testnet", {
      txHash: "d".repeat(64),
      minTime: 1,
      maxTime: 2,
    });
    updateSubmittedTransactionStatus("GONE", "testnet", first.txHash, "confirmed");
    expect(
      updateSubmittedTransactionStatus("GONE", "testnet", first.txHash, "failed")
    ).toMatchObject({ status: "confirmed" });

    for (let index = 0; index < MAX_STORED_TRANSACTIONS + 1; index++) {
      const current = recordSubmittedTransaction("GONE", "testnet", {
        txHash: (index + 100).toString(16).padStart(64, "0"),
        minTime: 1,
        maxTime: 2,
      });
      updateSubmittedTransactionStatus("GONE", "testnet", current.txHash, "confirmed");
    }

    expect(getSubmittedTransactions("GONE", "testnet")).toHaveLength(MAX_STORED_TRANSACTIONS);
    expect(
      getSubmittedTransactions("GONE", "testnet").some((record) => record.txHash === first.txHash)
    ).toBe(false);
  });

  it("does not evict ambiguous submissions to make room for a new transaction", () => {
    for (let index = 0; index < MAX_STORED_TRANSACTIONS; index++) {
      recordSubmittedTransaction("GONE", "testnet", {
        txHash: index.toString(16).padStart(64, "0"),
        minTime: 1,
        maxTime: 2,
      });
    }

    expect(() =>
      recordSubmittedTransaction("GONE", "testnet", {
        txHash: "f".repeat(64),
        minTime: 1,
        maxTime: 2,
      })
    ).toThrow(/full of unresolved submissions/);
    expect(getSubmittedTransactions("GONE", "testnet")).toHaveLength(MAX_STORED_TRANSACTIONS);
  });
});
