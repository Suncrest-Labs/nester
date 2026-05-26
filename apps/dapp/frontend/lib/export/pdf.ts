// lib/export/pdf.ts
// PDF statement generator using jsPDF + jsPDF-AutoTable
// Install: npm add jspdf jspdf-autotable

import type { ActivityTransaction } from "@/lib/api/activity";

const TYPE_LABELS: Record<string, string> = {
    deposit:      "Deposit",
    withdrawal:   "Withdrawal",
    settlement:   "Settlement",
    rebalance:    "Rebalance",
    yield_earned: "Yield Earned",
};

const STATUS_LABELS: Record<string, string> = {
    pending:   "Pending",
    completed: "Confirmed",
    failed:    "Failed",
};

function fmtDate(iso: string): string {
    return new Date(iso).toLocaleDateString("en-US", {
        year: "numeric", month: "short", day: "numeric",
    });
}

function fmtTime(iso: string): string {
    return new Date(iso).toLocaleTimeString("en-US", {
        hour: "2-digit", minute: "2-digit",
    });
}

export interface PDFExportOptions {
    transactions: ActivityTransaction[];
    from?: string;
    to?: string;
    walletAddress?: string;
}

export async function exportToPDF(options: PDFExportOptions): Promise<void> {
    if (typeof window === "undefined") return;

    // Dynamic import to avoid SSR issues
    const { default: jsPDF } = await import("jspdf");
    const { default: autoTable } = await import("jspdf-autotable");

    const doc = new jsPDF({ orientation: "landscape", unit: "mm", format: "a4" });
    const pageWidth = doc.internal.pageSize.getWidth();
    const now = new Date();

    // ── Header ────────────────────────────────────────────────────────────────
    // Logo / brand
    doc.setFillColor(10, 10, 10);
    doc.rect(0, 0, pageWidth, 22, "F");

    doc.setFont("helvetica", "bold");
    doc.setFontSize(16);
    doc.setTextColor(255, 255, 255);
    doc.text("NESTER", 14, 14);

    doc.setFont("helvetica", "normal");
    doc.setFontSize(9);
    doc.setTextColor(180, 180, 180);
    doc.text("Transaction Statement", 14, 19.5);

    // Meta: right side
    doc.setFontSize(8);
    doc.setTextColor(200, 200, 200);
    const generatedStr = `Generated: ${now.toLocaleDateString("en-US", { year: "numeric", month: "long", day: "numeric" })}`;
    doc.text(generatedStr, pageWidth - 14, 11, { align: "right" });

    if (options.walletAddress) {
        doc.text(`Wallet: ${options.walletAddress}`, pageWidth - 14, 16, { align: "right" });
    }

    if (options.from || options.to) {
        const range = [options.from ? `From: ${fmtDate(options.from)}` : "", options.to ? `To: ${fmtDate(options.to)}` : ""]
            .filter(Boolean).join("   ");
        doc.text(range, pageWidth - 14, 21, { align: "right" });
    }

    // ── Summary strip ─────────────────────────────────────────────────────────
    const txs = options.transactions;
    const totalDeposited = txs.filter(t => t.type === "deposit" && t.status === "completed")
        .reduce((s, t) => s + parseFloat(t.amount), 0);
    const totalWithdrawn = txs.filter(t => t.type === "withdrawal" && t.status === "completed")
        .reduce((s, t) => s + parseFloat(t.amount), 0);
    const totalYield = txs.filter(t => t.type === "yield_earned" && t.status === "completed")
        .reduce((s, t) => s + parseFloat(t.amount), 0);

    doc.setFillColor(245, 245, 245);
    doc.rect(0, 22, pageWidth, 16, "F");

    const summaryItems = [
        { label: "Total Deposited", value: `+${totalDeposited.toFixed(2)}` },
        { label: "Total Withdrawn", value: `-${totalWithdrawn.toFixed(2)}` },
        { label: "Yield Earned",    value: `+${totalYield.toFixed(4)}` },
        { label: "Transactions",    value: String(txs.length) },
    ];
    const colW = pageWidth / summaryItems.length;
    summaryItems.forEach((item, i) => {
        const x = colW * i + colW / 2;
        doc.setFont("helvetica", "bold");
        doc.setFontSize(11);
        doc.setTextColor(10, 10, 10);
        doc.text(item.value, x, 30, { align: "center" });
        doc.setFont("helvetica", "normal");
        doc.setFontSize(7.5);
        doc.setTextColor(100, 100, 100);
        doc.text(item.label, x, 35, { align: "center" });
    });

    // ── Table ─────────────────────────────────────────────────────────────────
    const rows = txs.map(tx => [
        fmtDate(tx.created_at) + "\n" + fmtTime(tx.created_at),
        TYPE_LABELS[tx.type]  ?? tx.type,
        tx.currency,
        tx.amount,
        STATUS_LABELS[tx.status] ?? tx.status,
        tx.tx_hash ? tx.tx_hash.slice(0, 16) + "…" : "—",
    ]);

    autoTable(doc, {
        startY: 42,
        head: [["Date & Time", "Type", "Currency", "Amount", "Status", "TX Hash (partial)"]],
        body: rows,
        styles: {
            fontSize: 8,
            cellPadding: 3,
            font: "helvetica",
            textColor: [30, 30, 30],
        },
        headStyles: {
            fillColor: [10, 10, 10],
            textColor: [255, 255, 255],
            fontStyle: "bold",
            fontSize: 8,
        },
        alternateRowStyles: {
            fillColor: [250, 250, 250],
        },
        columnStyles: {
            3: { halign: "right", fontStyle: "bold" },
            4: { halign: "center" },
            5: { fontSize: 7, textColor: [120, 120, 120] },
        },
        margin: { left: 14, right: 14 },
        didDrawPage: (data) => {
            // Footer on every page
            const pageCount = (doc.internal as unknown as { getNumberOfPages: () => number }).getNumberOfPages();
            doc.setFontSize(7);
            doc.setTextColor(160, 160, 160);
            doc.text(
                `Nester Financial Statement  ·  Page ${data.pageNumber} of ${pageCount}`,
                pageWidth / 2,
                doc.internal.pageSize.getHeight() - 6,
                { align: "center" }
            );
        },
    });

    const dateStr = now.toISOString().slice(0, 10);
    doc.save(`nester_statement_${dateStr}.pdf`);
}
