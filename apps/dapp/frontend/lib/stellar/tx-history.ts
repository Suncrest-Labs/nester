import { rpc as SorobanRpc, Transaction } from "@stellar/stellar-sdk";
import { safeStorage } from "@/lib/storage";
import { getCurrentNetwork } from "@/lib/stellar/transaction";

export const TX_HISTORY_STORAGE_PREFIX = "nester_tx_history_v1:";
export const MAX_TX_HISTORY_PER_WALLET = 25;
export const DEFAULT_TX_TIMEOUT_MS = 30_000;
export const RPC_RETENTION_WINDOW_MS = 7 * 24 * 60 * 60 * 1000; // 7 days default RPC retention

export type SubmittedTxStatus =
  | "pending"
  | "ambiguous"
  | "success"
  | "failed"
  | "not_found_expired"
  | "archived";

export type VaultTxAction = "deposit" | "withdraw";

export interface SubmittedTxRecord {
  txHash: string;
  walletAddress: string;
  vaultId: string;
  action: VaultTxAction;
  amount: number;
  asset: "USDC" | "XLM";
  submittedAt: number;
  /**
   * Unix timestamp in ms corresponding to the transaction's maxTime timebound.
   * Until the network clock passes this timestamp and RPC confirms NOT_FOUND,
   * the transaction could still land on-chain and retries must be blocked.
   */
  expiresAt: number;
  status: SubmittedTxStatus;
  explorerUrl: string;
  horizonUrl: string;
  ledger?: number;
  errorReason?: string;
}

function storageKey(walletAddress: string): string {
  return `${TX_HISTORY_STORAGE_PREFIX}${walletAddress.trim()}`;
}

export function buildTxExplorerLinks(txHash: string): {
  explorerUrl: string;
  horizonUrl: string;
} {
  const network = getCurrentNetwork();
  const baseExplorer = network.explorerUrl.replace(/\/+$/, "");
  const baseHorizon = network.horizonUrl.replace(/\/+$/, "");
  return {
    explorerUrl: `${baseExplorer}/tx/${txHash}`,
    horizonUrl: `${baseHorizon}/transactions/${txHash}`,
  };
}

/**
 * Extract the transaction's upper timebound (`maxTime`) in milliseconds from
 * a signed XDR or Transaction instance. Falls back to `nowMs + DEFAULT_TX_TIMEOUT_MS`
 * if no non-zero maxTime is set.
 */
export function extractTxExpiryMs(
  txOrXdr: Transaction | string,
  nowMs: number = Date.now(),
): number {
  try {
    const tx =
      typeof txOrXdr === "string"
        ? new Transaction(txOrXdr, getCurrentNetwork().networkPassphrase)
        : txOrXdr;
    const maxTimeRaw = tx.timeBounds?.maxTime;
    if (maxTimeRaw) {
      const maxTimeSec = Number(maxTimeRaw);
      if (Number.isFinite(maxTimeSec) && maxTimeSec > 0) {
        return maxTimeSec * 1000;
      }
    }
  } catch {
    // Fall back to default timeout window when XDR cannot be parsed in tests/mocks
  }
  return nowMs + DEFAULT_TX_TIMEOUT_MS;
}

/**
 * Read persisted transaction history for a wallet (newest first).
 */
export function getWalletTxHistory(walletAddress: string): SubmittedTxRecord[] {
  if (!walletAddress) return [];
  const records = safeStorage.get<SubmittedTxRecord[]>(storageKey(walletAddress), []);
  if (!Array.isArray(records)) return [];
  return records;
}

/**
 * Persist a new or updated transaction record for a wallet, capping the list
 * at `MAX_TX_HISTORY_PER_WALLET` entries (newest first).
 */
export function upsertWalletTxRecord(record: SubmittedTxRecord): SubmittedTxRecord[] {
  const key = storageKey(record.walletAddress);
  const existing = getWalletTxHistory(record.walletAddress);
  const filtered = existing.filter((item) => item.txHash !== record.txHash);
  const next = [record, ...filtered].slice(0, MAX_TX_HISTORY_PER_WALLET);
  safeStorage.set(key, next);
  return next;
}

/**
 * Record a newly submitted transaction before polling starts.
 */
export function recordSubmittedTransaction(params: {
  txHash: string;
  walletAddress: string;
  vaultId: string;
  action: VaultTxAction;
  amount: number;
  asset: "USDC" | "XLM";
  submittedAt?: number;
  expiresAt?: number;
}): SubmittedTxRecord {
  const now = params.submittedAt ?? Date.now();
  const expiresAt = params.expiresAt ?? now + DEFAULT_TX_TIMEOUT_MS;
  const { explorerUrl, horizonUrl } = buildTxExplorerLinks(params.txHash);

  const record: SubmittedTxRecord = {
    txHash: params.txHash,
    walletAddress: params.walletAddress,
    vaultId: params.vaultId,
    action: params.action,
    amount: params.amount,
    asset: params.asset,
    submittedAt: now,
    expiresAt,
    status: "pending",
    explorerUrl,
    horizonUrl,
  };

  upsertWalletTxRecord(record);
  return record;
}

/**
 * Update the status of an existing transaction record in storage.
 */
export function updateSubmittedTxStatus(
  walletAddress: string,
  txHash: string,
  patch: Partial<Pick<SubmittedTxRecord, "status" | "ledger" | "errorReason" | "expiresAt">>,
): SubmittedTxRecord | null {
  const existing = getWalletTxHistory(walletAddress);
  const target = existing.find((item) => item.txHash === txHash);
  if (!target) return null;

  const updated: SubmittedTxRecord = {
    ...target,
    ...patch,
  };
  upsertWalletTxRecord(updated);
  return updated;
}

/**
 * Determine the next status of a submitted transaction from an RPC `getTransaction` response.
 *
 * Key safety invariant (#1557):
 * - If RPC returns `NOT_FOUND` while `nowMs < record.expiresAt`, the transaction is still
 *   live in the network queue and may land in an upcoming ledger. When initial polling ends
 *   before expiry, it transitions to `"ambiguous"` so retries remain blocked.
 * - Only when RPC returns `NOT_FOUND` AND `nowMs >= record.expiresAt` does it transition to
 *   `"not_found_expired"`, proving the transaction's timebounds have elapsed without inclusion.
 * - If the transaction is older than the Soroban RPC retention window or RPC reports an
 *   archived ledger error, it transitions to `"archived"` and retains Explorer/Horizon links.
 */
export function resolveTxStatusFromRpc(
  record: Pick<SubmittedTxRecord, "submittedAt" | "expiresAt" | "status">,
  rpcResult:
    | {
        status: string;
        ledger?: number;
        errorReason?: string;
      }
    | { archived: true },
  options: { nowMs?: number; pollTimedOut?: boolean } = {},
): {
  status: SubmittedTxStatus;
  ledger?: number;
  errorReason?: string;
} {
  const nowMs = options.nowMs ?? Date.now();

  if ("archived" in rpcResult && rpcResult.archived) {
    return { status: "archived" };
  }

  const rawStatus = rpcResult.status;

  if (rawStatus === SorobanRpc.Api.GetTransactionStatus.SUCCESS || rawStatus === "SUCCESS") {
    return {
      status: "success",
      ledger: rpcResult.ledger,
    };
  }

  if (rawStatus === SorobanRpc.Api.GetTransactionStatus.FAILED || rawStatus === "FAILED") {
    return {
      status: "failed",
      ledger: rpcResult.ledger,
      errorReason: rpcResult.errorReason ?? "on-chain execution failed",
    };
  }

  if (rawStatus === "ARCHIVED") {
    return { status: "archived" };
  }

  // NOT_FOUND handling
  if (nowMs - record.submittedAt >= RPC_RETENTION_WINDOW_MS) {
    return { status: "archived" };
  }

  if (nowMs >= record.expiresAt) {
    return {
      status: "not_found_expired",
      errorReason: "Transaction expired past its timebounds without being included in a ledger.",
    };
  }

  if (options.pollTimedOut) {
    return {
      status: "ambiguous",
      errorReason:
        "Confirmation is taking longer than expected. Retry is paused until the transaction timebound expires.",
    };
  }

  return { status: "pending" };
}

/**
 * Returns the active ambiguous or pending submission for a wallet (optionally scoped
 * to a specific vaultId and action) that blocks new retries.
 */
export function getBlockingSubmission(
  walletAddress: string,
  vaultId?: string,
  action?: VaultTxAction,
): SubmittedTxRecord | null {
  const history = getWalletTxHistory(walletAddress);
  return (
    history.find((tx) => {
      if (vaultId && tx.vaultId !== vaultId) return false;
      if (action && tx.action !== action) return false;
      return tx.status === "pending" || tx.status === "ambiguous";
    }) ?? null
  );
}

/**
 * Check whether the user is allowed to retry a vault action.
 * Retries are blocked while an existing submission is `"pending"` or `"ambiguous"`.
 * Retries are allowed once the prior transaction is `"not_found_expired"`, `"failed"`,
 * `"success"`, or `"archived"`.
 */
export function canRetryVaultAction(
  walletAddress: string,
  vaultId?: string,
  action?: VaultTxAction,
): {
  canRetry: boolean;
  blockingTx: SubmittedTxRecord | null;
} {
  const blockingTx = getBlockingSubmission(walletAddress, vaultId, action);
  return {
    canRetry: blockingTx === null,
    blockingTx,
  };
}

/**
 * Re-check a single stored transaction against the Soroban RPC `getTransaction` endpoint
 * and persist the updated status.
 */
export async function refreshSubmittedTxStatus(
  record: SubmittedTxRecord,
  rpcServer: Pick<SorobanRpc.Server, "getTransaction">,
  nowMs: number = Date.now(),
): Promise<SubmittedTxRecord> {
  try {
    const res = await rpcServer.getTransaction(record.txHash);
    const errorReason =
      res.status === SorobanRpc.Api.GetTransactionStatus.FAILED
        ? res.resultMetaXdr?.toXDR("base64") ?? "on-chain execution failed"
        : undefined;

    const resolved = resolveTxStatusFromRpc(
      record,
      {
        status: res.status,
        ledger: "ledger" in res ? res.ledger : undefined,
        errorReason,
      },
      { nowMs, pollTimedOut: record.status === "ambiguous" },
    );

    return (
      updateSubmittedTxStatus(record.walletAddress, record.txHash, resolved) ?? {
        ...record,
        ...resolved,
      }
    );
  } catch (err) {
    const msg = (err instanceof Error ? err.message : String(err)).toLowerCase();
    if (msg.includes("archived") || msg.includes("retention") || msg.includes("ledger range")) {
      const resolved = resolveTxStatusFromRpc(record, { archived: true }, { nowMs });
      return (
        updateSubmittedTxStatus(record.walletAddress, record.txHash, resolved) ?? {
          ...record,
          ...resolved,
        }
      );
    }
    throw err;
  }
}
