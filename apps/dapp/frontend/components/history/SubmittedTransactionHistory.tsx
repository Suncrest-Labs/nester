"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, ExternalLink, RefreshCw } from "lucide-react";
import { getExplorerTxUrl } from "@/utils/explorer";
import { refreshSubmittedTransactions } from "@/lib/stellar/transaction";
import {
  getSubmittedTransactions,
  isTransactionRetryable,
  type SubmittedTransaction,
} from "@/lib/stellar/transaction-history";
import { useNetwork } from "@/hooks/useNetwork";

const statusStyles = {
  pending: "bg-yellow-100 text-yellow-800",
  confirmed: "bg-green-100 text-green-800",
  failed: "bg-red-100 text-red-800",
  archived: "bg-gray-100 text-gray-700",
} satisfies Record<SubmittedTransaction["status"], string>;

const statusLabels = {
  pending: "Pending",
  confirmed: "Confirmed",
  failed: "Failed",
  archived: "Archived",
} satisfies Record<SubmittedTransaction["status"], string>;

export default function SubmittedTransactionHistory({
  walletAddress,
}: {
  walletAddress: string;
}) {
  const { currentNetwork } = useNetwork();
  const [transactions, setTransactions] = useState<SubmittedTransaction[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const refreshingRef = useRef(false);

  const refresh = useCallback(async () => {
    if (refreshingRef.current) return;
    refreshingRef.current = true;
    setRefreshing(true);
    setError(null);
    try {
      const latest = await refreshSubmittedTransactions(walletAddress, currentNetwork.id);
      setTransactions(latest);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Unable to refresh transaction statuses.");
      setTransactions(getSubmittedTransactions(walletAddress, currentNetwork.id));
    } finally {
      refreshingRef.current = false;
      setRefreshing(false);
    }
  }, [walletAddress, currentNetwork.id]);

  useEffect(() => {
    const initialRefresh = window.setTimeout(() => void refresh(), 0);
    const interval = window.setInterval(() => void refresh(), 10_000);
    return () => {
      window.clearTimeout(initialRefresh);
      window.clearInterval(interval);
    };
  }, [refresh]);

  return (
    <section
      aria-labelledby="submitted-transactions-title"
      className="mb-6 rounded-xl border border-black/10 bg-white p-4 dark:border-white/10 dark:bg-[#100F0F]"
    >
      <div className="mb-3 flex items-center justify-between gap-3">
        <div>
          <h2 id="submitted-transactions-title" className="font-semibold text-black dark:text-white">
            Recent on-chain submissions
          </h2>
          <p className="mt-1 text-sm text-gray-500">
            Pending submissions are checked on {currentNetwork.name}. A retry is safe only after an
            archived transaction has passed its time bounds.
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          disabled={refreshing}
          aria-label="Refresh submitted transaction statuses"
          className="inline-flex shrink-0 items-center gap-2 rounded-md border px-3 py-2 text-sm disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${refreshing ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </div>

      {error && (
        <p role="alert" className="mb-3 flex items-center gap-2 text-sm text-red-600">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          {error}
        </p>
      )}

      {transactions.length === 0 ? (
        <p className="py-5 text-center text-sm text-gray-500">No recent on-chain submissions.</p>
      ) : (
        <ul className="divide-y divide-black/5 dark:divide-white/5">
          {transactions.map((transaction) => {
            const explorerUrl = getExplorerTxUrl(transaction.txHash);
            return (
              <li
                key={transaction.txHash}
                className="flex flex-wrap items-center justify-between gap-3 py-3"
              >
                <div className="min-w-0">
                  <a
                    href={explorerUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex max-w-full items-center gap-2 font-mono text-sm text-blue-600 hover:underline"
                  >
                    <span className="truncate">{transaction.txHash}</span>
                    <ExternalLink className="h-3.5 w-3.5 shrink-0" />
                    <span className="sr-only">View on Stellar Explorer</span>
                  </a>
                  <p className="mt-1 text-xs text-gray-500">
                    Submitted {new Date(transaction.submittedAt).toLocaleString()}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <span
                    className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${statusStyles[transaction.status]}`}
                  >
                    {statusLabels[transaction.status]}
                  </span>
                  {isTransactionRetryable(transaction) && (
                    <span className="text-xs font-medium text-green-700">Retry is safe</span>
                  )}
                  {transaction.status === "pending" && (
                    <span className="text-xs text-gray-500">Do not retry yet</span>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
