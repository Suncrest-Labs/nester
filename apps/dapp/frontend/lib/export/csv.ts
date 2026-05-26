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

/**
 * Converts the active filtered transaction list to CSV and triggers a download.
 */
export function exportToCSV(transactions: ActivityTransaction[]): void {
    if (typeof window === "undefined" || !transactions.length) return;

    const headers = [
        "Date",
        "Time",
        "Type",
        "Currency",
        "Amount",
        "Status",
        "Transaction Hash",
        "Vault ID",
    ];

    const rows = transactions.map((tx) => {
        const d = new Date(tx.created_at);
        return [
            `"${d.toLocaleDateString("en-US", { year: "numeric", month: "short", day: "numeric" })}"`,
            `"${d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" })}"`,
            `"${TYPE_LABELS[tx.type] ?? tx.type}"`,
            `"${tx.currency}"`,
            `"${tx.amount}"`,
            `"${STATUS_LABELS[tx.status] ?? tx.status}"`,
            `"${tx.tx_hash ?? ""}"`,
            `"${tx.vault_id}"`,
        ];
    });

    const csvContent = [headers.join(","), ...rows.map(r => r.join(","))].join("\n");
    const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
    const url  = URL.createObjectURL(blob);
    const link = document.createElement("a");

    link.setAttribute("href", url);
    link.setAttribute("download", `nester_transactions_${new Date().toISOString().slice(0, 10)}.csv`);
    link.style.visibility = "hidden";
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
}
