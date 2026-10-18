import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  MAX_TX_HISTORY_PER_WALLET,
  RPC_RETENTION_WINDOW_MS,
  canRetryVaultAction,
  getWalletTxHistory,
  recordSubmittedTransaction,
  refreshSubmittedTxStatus,
  resolveTxStatusFromRpc,
  updateSubmittedTxStatus,
} from "@/lib/stellar/tx-history";

const WALLET_A = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF";
const WALLET_B = "GBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBWGH";

describe("tx-history (#1557)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("persists the last N submitted tx hashes per wallet and evicts older entries", () => {
    for (let i = 1; i <= MAX_TX_HISTORY_PER_WALLET + 5; i++) {
      recordSubmittedTransaction({
        txHash: `tx-hash-${i}`,
        walletAddress: WALLET_A,
        vaultId: "vault-1",
        action: "deposit",
        amount: i * 10,
        asset: "USDC",
        submittedAt: 1_700_000_000_000 + i * 1000,
        expiresAt: 1_700_000_030_000 + i * 1000,
      });
    }

    const historyA = getWalletTxHistory(WALLET_A);
    expect(historyA).toHaveLength(MAX_TX_HISTORY_PER_WALLET);
    // Newest first
    expect(historyA[0].txHash).toBe(`tx-hash-${MAX_TX_HISTORY_PER_WALLET + 5}`);
    expect(historyA[historyA.length - 1].txHash).toBe("tx-hash-6");
    expect(historyA[0].explorerUrl).toContain(`/tx/tx-hash-${MAX_TX_HISTORY_PER_WALLET + 5}`);
    expect(historyA[0].horizonUrl).toContain(`/transactions/tx-hash-${MAX_TX_HISTORY_PER_WALLET + 5}`);

    // Isolated per wallet
    expect(getWalletTxHistory(WALLET_B)).toHaveLength(0);
  });

  it("transitions status across pending, ambiguous, success, failed, not_found_expired, and archived", () => {
    const submittedAt = 1_700_000_000_000;
    const expiresAt = submittedAt + 30_000;

    const record = recordSubmittedTransaction({
      txHash: "tx-lifecycle-1",
      walletAddress: WALLET_A,
      vaultId: "vault-1",
      action: "deposit",
      amount: 250,
      asset: "USDC",
      submittedAt,
      expiresAt,
    });

    expect(record.status).toBe("pending");

    // 1. NOT_FOUND before expiry during active polling remains pending
    const stillPending = resolveTxStatusFromRpc(
      record,
      { status: "NOT_FOUND" },
      { nowMs: submittedAt + 10_000, pollTimedOut: false },
    );
    expect(stillPending.status).toBe("pending");

    // 2. NOT_FOUND when poll window times out BEFORE timebound expiry -> ambiguous
    const ambiguous = resolveTxStatusFromRpc(
      record,
      { status: "NOT_FOUND" },
      { nowMs: submittedAt + 20_000, pollTimedOut: true },
    );
    expect(ambiguous.status).toBe("ambiguous");

    // 3. NOT_FOUND AFTER timebound expiry -> not_found_expired
    const expiredNotFound = resolveTxStatusFromRpc(
      record,
      { status: "NOT_FOUND" },
      { nowMs: expiresAt + 1_000 },
    );
    expect(expiredNotFound.status).toBe("not_found_expired");

    // 4. SUCCESS -> success with ledger number
    const confirmed = resolveTxStatusFromRpc(
      record,
      { status: "SUCCESS", ledger: 54321 },
      { nowMs: submittedAt + 6_000 },
    );
    expect(confirmed.status).toBe("success");
    expect(confirmed.ledger).toBe(54321);

    // 5. FAILED -> failed with reason
    const failed = resolveTxStatusFromRpc(
      record,
      { status: "FAILED", errorReason: "HostError: Error(Contract, #3)" },
      { nowMs: submittedAt + 6_000 },
    );
    expect(failed.status).toBe("failed");
    expect(failed.errorReason).toBe("HostError: Error(Contract, #3)");

    // 6. Older than RPC retention window -> archived
    const archivedByAge = resolveTxStatusFromRpc(
      record,
      { status: "NOT_FOUND" },
      { nowMs: submittedAt + RPC_RETENTION_WINDOW_MS + 1 },
    );
    expect(archivedByAge.status).toBe("archived");
  });

  it("blocks retry while a submission is pending or ambiguous, and unblocks once NOT_FOUND past timebounds or FAILED", () => {
    const submittedAt = 1_700_000_000_000;
    const expiresAt = submittedAt + 30_000;

    recordSubmittedTransaction({
      txHash: "tx-in-flight",
      walletAddress: WALLET_A,
      vaultId: "vault-1",
      action: "deposit",
      amount: 500,
      asset: "USDC",
      submittedAt,
      expiresAt,
    });

    // Blocked while pending
    const checkPending = canRetryVaultAction(WALLET_A, "vault-1", "deposit");
    expect(checkPending.canRetry).toBe(false);
    expect(checkPending.blockingTx?.txHash).toBe("tx-in-flight");

    // Blocked while ambiguous (poll timed out before transaction maxTime expired)
    updateSubmittedTxStatus(WALLET_A, "tx-in-flight", { status: "ambiguous" });
    const checkAmbiguous = canRetryVaultAction(WALLET_A, "vault-1", "deposit");
    expect(checkAmbiguous.canRetry).toBe(false);
    expect(checkAmbiguous.blockingTx?.status).toBe("ambiguous");

    // Different vault or action is not blocked
    expect(canRetryVaultAction(WALLET_A, "vault-2", "deposit").canRetry).toBe(true);
    expect(canRetryVaultAction(WALLET_A, "vault-1", "withdraw").canRetry).toBe(true);

    // Unblocked once getTransaction confirms NOT_FOUND past the transaction's timebounds
    updateSubmittedTxStatus(WALLET_A, "tx-in-flight", { status: "not_found_expired" });
    const checkExpired = canRetryVaultAction(WALLET_A, "vault-1", "deposit");
    expect(checkExpired.canRetry).toBe(true);
    expect(checkExpired.blockingTx).toBeNull();

    // Also unblocked when status is failed
    updateSubmittedTxStatus(WALLET_A, "tx-in-flight", { status: "failed" });
    expect(canRetryVaultAction(WALLET_A, "vault-1", "deposit").canRetry).toBe(true);
  });

  it("refreshes stored transaction status via RPC getTransaction and handles archived ledger errors", async () => {
    const submittedAt = 1_700_000_000_000;
    const expiresAt = submittedAt + 30_000;

    const rec = recordSubmittedTransaction({
      txHash: "tx-rpc-refresh",
      walletAddress: WALLET_A,
      vaultId: "vault-1",
      action: "withdraw",
      amount: 100,
      asset: "USDC",
      submittedAt,
      expiresAt,
    });

    // RPC returns NOT_FOUND before expiry -> stays pending
    const rpcNotFoundEarly = {
      getTransaction: vi.fn().mockResolvedValue({ status: "NOT_FOUND" }),
    };
    const afterEarly = await refreshSubmittedTxStatus(
      rec,
      rpcNotFoundEarly as never,
      submittedAt + 5_000,
    );
    expect(afterEarly.status).toBe("pending");
    expect(canRetryVaultAction(WALLET_A, "vault-1", "withdraw").canRetry).toBe(false);

    // RPC returns NOT_FOUND after expiry -> transitions to not_found_expired & unblocks retry
    const afterExpiry = await refreshSubmittedTxStatus(
      afterEarly,
      rpcNotFoundEarly as never,
      expiresAt + 2_000,
    );
    expect(afterExpiry.status).toBe("not_found_expired");
    expect(canRetryVaultAction(WALLET_A, "vault-1", "withdraw").canRetry).toBe(true);

    // RPC throws ledger retention / archived error -> transitions to archived
    const rpcArchived = {
      getTransaction: vi
        .fn()
        .mockRejectedValue(new Error("ledger range out of retention window (archived)")),
    };
    const afterArchived = await refreshSubmittedTxStatus(
      afterExpiry,
      rpcArchived as never,
      expiresAt + 100_000,
    );
    expect(afterArchived.status).toBe("archived");
    expect(afterArchived.horizonUrl).toContain("/transactions/tx-rpc-refresh");
  });
});
