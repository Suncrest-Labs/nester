'use client';

import React from 'react';
import { Download, FileText, AlertTriangle } from 'lucide-react';
import { cn } from '@/lib/utils';
import { SkeletonTable, LoadingRegion } from '@/components/ui/skeleton/skeleton';

export interface Transaction {
  id: string;
  timestamp: string; // ISO string
  type: 'Deposit' | 'Withdrawal' | 'Rebalance' | 'Yield Earned';
  vaultName: string;
  amount: string; // exact decimal string from the API, e.g. "123.45" (#1223)
  asset: string;
  status: 'Confirmed' | 'Pending' | 'Failed';
  txHash?: string;
}

interface Props {
  transactions: Transaction[];
  loading: boolean;
  error?: string;
  onExportCsv: () => void;
  onExportPdf: () => void;
}

const TransactionTable: React.FC<Props> = ({ transactions, loading, error, onExportCsv, onExportPdf }) => {
  const renderStatus = (status: Transaction['status']) => {
    const colors = {
      Confirmed: 'bg-green-100 text-green-800',
      Pending: 'bg-yellow-100 text-yellow-800',
      Failed: 'bg-red-100 text-red-800',
    };
    return (
      <span className={cn('inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium', colors[status])}>
        {status}
      </span>
    );
  };

  // The skeleton mirrors the real table so the header and column widths do not
  // shift once rows arrive.
  if (loading) {
    return (
      <LoadingRegion
        label="Loading your transactions"
        className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/10 bg-white dark:bg-[#100F0F] p-4"
      >
        <SkeletonTable rows={6} columns={6} />
      </LoadingRegion>
    );
  }

  if (error) {
    return (
      <div role="alert" data-testid="history-error" className="flex items-center justify-center py-12 text-red-500">
        <AlertTriangle className="mr-2 h-5 w-5" /> {error}
      </div>
    );
  }

  if (transactions.length === 0) {
    return (
      <div data-testid="history-empty-state" className="flex items-center justify-center py-12 text-gray-500">
        No transactions match the selected filters.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/10 bg-white dark:bg-[#100F0F]">
      <table className="w-full text-left">
        <thead className="border-b border-black/10 dark:border-white/10 bg-gray-50 text-xs text-gray-600">
          <tr>
            <th className="px-4 py-2">Date</th>
            <th className="px-4 py-2">Type</th>
            <th className="px-4 py-2">Vault</th>
            <th className="px-4 py-2">Amount</th>
            <th className="px-4 py-2">Status</th>
            <th className="px-4 py-2">TX Hash</th>
          </tr>
        </thead>
        <tbody>
          {transactions.map((tx) => (
            <tr key={tx.id} className="border-b border-black/5 dark:border-white/5 last:border-0 hover:bg-black/5 dark:hover:bg-white/5">
              <td className="px-4 py-2 text-sm text-gray-700">{new Date(tx.timestamp).toLocaleString()}</td>
              <td className="px-4 py-2 text-sm text-gray-700">{tx.type}</td>
              <td className="px-4 py-2 text-sm text-gray-700">{tx.vaultName}</td>
              <td className="px-4 py-2 font-mono text-sm text-gray-700">
                {Number(tx.amount).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 8 })} {tx.asset}
              </td>
              <td className="px-4 py-2">{renderStatus(tx.status)}</td>
              <td className="px-4 py-2">
                {tx.txHash ? (
                  <a
                    href={`https://stellar.expert/explorer/public/tx/${tx.txHash}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center text-blue-600 hover:underline"
                    title="View on Stellar Explorer"
                  >
                    <FileText className="h-4 w-4" />
                  </a>
                ) : (
                  <span className="text-gray-400">—</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="flex items-center justify-end space-x-2 p-2">
        <button onClick={onExportCsv} className="inline-flex items-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700">
          <Download className="mr-1 h-4 w-4" /> CSV
        </button>
        <button onClick={onExportPdf} className="inline-flex items-center rounded-md bg-gray-800 px-3 py-1.5 text-sm font-medium text-white hover:bg-gray-900">
          <FileText className="mr-1 h-4 w-4" /> PDF
        </button>
      </div>
    </div>
  );
};

export default TransactionTable;
