"use client";

import React, { useState, useEffect, useMemo } from "react";
import { useRouter } from "next/navigation";
import { cn } from "@/lib/utils";
import FilterBar, { FilterState } from "@/components/history/FilterBar";
import TransactionTable from "@/components/history/TransactionTable";
import { exportCsv } from "@/lib/export/csv";
import { exportPdf } from "@/lib/export/pdf";
import { useWallet } from "@/components/wallet-provider";
import { useAuth } from "@/components/auth-provider";
import { useVaults } from "@/hooks/useVaults";
import { getStoredToken } from "@/lib/api/client";
import { AppShell } from "@/components/app-shell";
import { motion } from "framer-motion";

interface ActivityResponse {
  data: Transaction[];
  nextCursor?: string | null;
  prevCursor?: string | null;
}

interface Transaction {
  id: string;
  timestamp: string;
  type: "Deposit" | "Withdrawal" | "Rebalance" | "Settlement" | "Yield Earned";
  vaultName: string;
  amount: number;
  asset: string;
  status: "Confirmed" | "Pending" | "Failed";
  txHash?: string;
}

export default function HistoryPage() {
  const { isConnected, user } = useWallet();
  const { userId } = useAuth();
  const { vaults } = useVaults(userId ?? undefined);
  const router = useRouter();

  const vaultOptions = useMemo(
    () => vaults.map((v) => ({ id: v.id, name: `${v.currency} Vault` })),
    [vaults]
  );

  useEffect(() => {
    if (!isConnected) router.push("/");
  }, [isConnected, router]);

  const [filters, setFilters] = useState<FilterState>({
    types: {},
    status: "All",
    searchTerm: "",
  });

  const [cursor, setCursor] = useState<string | null>(null);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [prevCursor, setPrevCursor] = useState<string | null>(null);

  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const buildQuery = () => {
    const params = new URLSearchParams();
    if (user?.address) params.append("userId", user.address);
    const activeTypes = Object.keys(filters.types).filter((k) => filters.types[k]);
    if (activeTypes.length) params.append("type", activeTypes.join(","));
    if (filters.fromDate) params.append("from", filters.fromDate);
    if (filters.toDate) params.append("to", filters.toDate);
    if (filters.vaultId) params.append("vault", filters.vaultId);
    if (filters.status && filters.status !== "All") params.append("status", filters.status);
    if (filters.searchTerm) params.append("q", filters.searchTerm);
    if (cursor) params.append("cursor", cursor);
    params.append("limit", "25");
    return params.toString();
  };

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true);
      setError(null);
      try {
        const query = buildQuery();
        const res = await fetch(`/api/v1/activity?${query}`, {
          headers: { Authorization: `Bearer ${getStoredToken()}` },
        });
        if (!res.ok) throw new Error(`Error ${res.status}`);
        const json: ActivityResponse = await res.json();
        setTransactions(json.data);
        setNextCursor(json.nextCursor ?? null);
        setPrevCursor(json.prevCursor ?? null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unexpected error");
      } finally {
        setLoading(false);
      }
    };
    if (isConnected) fetchData();
  }, [isConnected, filters, cursor]);

  const filteredTransactions = useMemo(() => {
    if (!filters.searchTerm) return transactions;
    const term = filters.searchTerm.toLowerCase();
    return transactions.filter((tx) => {
      const hashMatch = tx.txHash?.toLowerCase().includes(term) ?? false;
      const amountMatch = tx.amount.toString().includes(term);
      return hashMatch || amountMatch;
    });
  }, [transactions, filters.searchTerm]);

  const summary = useMemo(() => {
    const totalDeposited = transactions
      .filter((t) => t.type === "Deposit")
      .reduce((sum, t) => sum + t.amount, 0);
    const totalWithdrawn = transactions
      .filter((t) => t.type === "Withdrawal")
      .reduce((sum, t) => sum + t.amount, 0);
    const totalYield = transactions
      .filter((t) => t.type === "Yield Earned")
      .reduce((sum, t) => sum + t.amount, 0);
    return { totalDeposited, totalWithdrawn, totalYield };
  }, [transactions]);

  const toExportTx = (txs: typeof filteredTransactions) =>
    txs.map((tx) => ({
      date: tx.timestamp,
      type: tx.type,
      vault: tx.vaultName,
      amount: tx.amount.toFixed(2),
      status: tx.status,
      txHash: tx.txHash,
    }));

  const handleCsvExport = () => {
    exportCsv(toExportTx(filteredTransactions), "transactions.csv");
  };
  const handlePdfExport = () => {
    exportPdf({
      transactions: toExportTx(filteredTransactions),
      summary,
      title: "Nester Transaction Statement",
    });
  };

  const goNext = () => setCursor(nextCursor);
  const goPrev = () => setCursor(prevCursor);

  if (!isConnected) return null;

  return (
    <AppShell>
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="my-6 flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between"
      >
        <div className="text-[28px] font-semibold text-black dark:text-white">Transaction History</div>
      </motion.div>

      {/* Yield Summary */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3 mb-6">
        <div className="rounded-xl border p-4 bg-white dark:bg-[#100F0F]">
          <p className="text-sm text-gray-500">Total Deposited</p>
          <p className="mt-1 text-lg font-medium text-black dark:text-white">
            {summary.totalDeposited.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
          </p>
        </div>
        <div className="rounded-xl border p-4 bg-white dark:bg-[#100F0F]">
          <p className="text-sm text-gray-500">Total Withdrawn</p>
          <p className="mt-1 text-lg font-medium text-black dark:text-white">
            {summary.totalWithdrawn.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
          </p>
        </div>
        <div className="rounded-xl border p-4 bg-white dark:bg-[#100F0F]">
          <p className="text-sm text-gray-500">Total Yield Earned</p>
          <p className="mt-1 text-lg font-medium text-black dark:text-white">
            {summary.totalYield.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
          </p>
        </div>
      </div>

      <FilterBar vaultOptions={vaultOptions} onChange={setFilters} />

      <TransactionTable
        transactions={filteredTransactions}
        loading={loading}
        error={error ?? undefined}
        onExportCsv={handleCsvExport}
        onExportPdf={handlePdfExport}
      />

      {/* Pagination Controls */}
      <div className="mt-6 flex justify-center gap-4">
        <button
          onClick={goPrev}
          disabled={!prevCursor}
          className={cn(
            "rounded-md px-4 py-2 border",
            prevCursor ? "bg-white dark:bg-[#100F0F] hover:bg-gray-100" : "bg-gray-100 text-gray-400 cursor-not-allowed"
          )}
        >
          Previous
        </button>
        <button
          onClick={goNext}
          disabled={!nextCursor}
          className={cn(
            "rounded-md px-4 py-2 border",
            nextCursor ? "bg-white dark:bg-[#100F0F] hover:bg-gray-100" : "bg-gray-100 text-gray-400 cursor-not-allowed"
          )}
        >
          Next
        </button>
      </div>
    </AppShell>
  );
}
