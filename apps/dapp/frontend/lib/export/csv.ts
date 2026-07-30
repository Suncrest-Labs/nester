// apps/dapp/frontend/lib/export/csv.ts

/**
 * Export an array of transaction objects to CSV and trigger a download.
 * The transaction type should match the columns displayed in the TransactionTable.
 */
export interface Transaction {
  date: string; // ISO date string
  type: string;
  vault: string;
  amount: string; // already formatted string e.g. "123.45"
  status: string;
  txHash?: string;
}

export const exportCsv = (transactions: Transaction[], fileName = 'transactions.csv') => {
  const headers = ['Date', 'Type', 'Vault', 'Amount', 'Status', 'TX Hash'];
  const rows = transactions.map((tx) => [
    tx.date,
    tx.type,
    tx.vault,
    tx.amount,
    tx.status,
    tx.txHash ?? ''
  ]);

  const csvContent = [headers, ...rows]
    .map((row) => row.map((cell) => `"${cell.replace(/"/g, '""')}"`).join(','))
    .join('\n');

  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.setAttribute('href', url);
  link.setAttribute('download', fileName);
  link.style.visibility = 'hidden';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
};
