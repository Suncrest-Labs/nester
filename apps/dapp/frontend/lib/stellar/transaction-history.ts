import { safeStorage } from "@/lib/storage";
import type { NetworkId } from "@/lib/storageKeys";

export const MAX_STORED_TRANSACTIONS = 20;

export type SubmittedTransactionStatus = "pending" | "confirmed" | "failed" | "archived";

export interface SubmittedTransaction {
  txHash: string;
  status: SubmittedTransactionStatus;
  submittedAt: number;
  minTime: number | null;
  maxTime: number | null;
}

function storageKey(walletAddress: string, networkId: NetworkId) {
  return `nester_submitted_transactions_v1:${networkId}:${walletAddress}`;
}

function isSubmittedTransaction(value: unknown): value is SubmittedTransaction {
  if (!value || typeof value !== "object") return false;
  const record = value as Partial<SubmittedTransaction>;
  return (
    typeof record.txHash === "string" &&
    typeof record.submittedAt === "number" &&
    (record.status === "pending" ||
      record.status === "confirmed" ||
      record.status === "failed" ||
      record.status === "archived") &&
    (record.minTime === null || typeof record.minTime === "number") &&
    (record.maxTime === null || typeof record.maxTime === "number")
  );
}

export function getSubmittedTransactions(
  walletAddress: string,
  networkId: NetworkId
): SubmittedTransaction[] {
  if (!walletAddress) return [];
  const records = safeStorage.get<unknown>(storageKey(walletAddress, networkId), []);
  if (!Array.isArray(records)) return [];
  return records.filter(isSubmittedTransaction).slice(0, MAX_STORED_TRANSACTIONS);
}

export function recordSubmittedTransaction(
  walletAddress: string,
  networkId: NetworkId,
  transaction: Omit<SubmittedTransaction, "status" | "submittedAt">
): SubmittedTransaction {
  if (!walletAddress) throw new Error("Cannot record a transaction without a wallet address.");
  const record: SubmittedTransaction = {
    ...transaction,
    status: "pending",
    submittedAt: Date.now(),
  };
  const records = [
    record,
    ...getSubmittedTransactions(walletAddress, networkId).filter(
      (existing) => existing.txHash !== record.txHash
    ),
  ];
  if (records.length > MAX_STORED_TRANSACTIONS) {
    let oldestResolved: SubmittedTransaction | null = null;
    for (const existing of records.slice(1).reverse()) {
      if (existing.status !== "pending") {
        oldestResolved = existing;
        break;
      }
    }
    if (!oldestResolved) {
      throw new Error(
        "Transaction history is full of unresolved submissions. Resolve one before submitting another."
      );
    }
    records.splice(records.indexOf(oldestResolved), 1);
  }
  if (!safeStorage.set(storageKey(walletAddress, networkId), records)) {
    throw new Error("Unable to safely save this transaction before submission. Please try again.");
  }
  return record;
}

export function updateSubmittedTransactionStatus(
  walletAddress: string,
  networkId: NetworkId,
  txHash: string,
  status: Exclude<SubmittedTransactionStatus, "pending">,
  now = Date.now()
): SubmittedTransaction | null {
  const records = getSubmittedTransactions(walletAddress, networkId);
  const current = records.find((record) => record.txHash === txHash);
  if (!current) return null;
  if (current.status !== "pending") return current;
  if (status === "archived" && (current.maxTime === null || now / 1000 <= current.maxTime)) {
    return current;
  }

  const updated = { ...current, status };
  const next = records.map((record) => (record.txHash === txHash ? updated : record));
  safeStorage.set(storageKey(walletAddress, networkId), next);
  return updated;
}

export function hasAmbiguousTransaction(records: SubmittedTransaction[]): boolean {
  return records.some((record) => record.status === "pending");
}

export function isTransactionRetryable(record: SubmittedTransaction, now = Date.now()): boolean {
  return record.status === "archived" && record.maxTime !== null && now / 1000 > record.maxTime;
}
