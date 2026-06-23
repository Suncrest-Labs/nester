import { jsPDF } from 'jspdf';
import 'jspdf-autotable';

export interface Transaction {
  date: string;
  type: string;
  vault: string;
  amount: string;
  status: string;
  txHash?: string;
}

/**
 * Generate a PDF statement for the given transactions.
 * The PDF includes Nester branding, a summary, and a table of transactions.
 */
export const exportPdf = (transactions: Transaction[], summary: { totalYield: string; totalDeposited: string; totalWithdrawn: string }) => {
  const doc = new jsPDF();

  // Header with branding
  doc.setFontSize(20);
  doc.text('Nester Transaction Statement', 14, 20);
  doc.setFontSize(12);
  doc.text(`Generated: ${new Date().toLocaleString()}`, 14, 30);

  // Summary section
  doc.text('Summary', 14, 45);
  doc.text(`Total Yield Earned: ${summary.totalYield}`, 14, 55);
  doc.text(`Total Deposited: ${summary.totalDeposited}`, 14, 65);
  doc.text(`Total Withdrawn: ${summary.totalWithdrawn}`, 14, 75);

  // Table of transactions
  const tableBody = transactions.map((tx) => [
    tx.date,
    tx.type,
    tx.vault,
    tx.amount,
    tx.status,
    tx.txHash ?? '-',
  ]);

  // @ts-ignore – jsPDF type definitions may not include autotable by default
  // eslint-disable-next-line @typescript-eslint/no-unsafe-call
  (doc as any).autoTable({
    startY: 85,
    head: [['Date', 'Type', 'Vault', 'Amount', 'Status', 'TX Hash']],
    body: tableBody,
    theme: 'grid',
    headStyles: { fillColor: [50, 50, 50] },
    styles: { fontSize: 9 },
  });

  doc.save('transaction_statement.pdf');
};
