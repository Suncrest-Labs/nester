"use client";

import React from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    ArrowDownLeft, ArrowUpRight, RefreshCw, Zap, Landmark,
    ExternalLink, ChevronLeft, ChevronRight, FileText, Loader2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { ActivityTransaction } from "@/lib/api/activity";

// ── Mappings ──────────────────────────────────────────────────────────────────

const TX_ICONS: Record<string, React.ElementType> = {
    deposit:      ArrowDownLeft,
    withdrawal:   ArrowUpRight,
    rebalance:    RefreshCw,
    settlement:   Landmark,
    yield_earned: Zap,
};

const TYPE_LABELS: Record<string, string> = {
    deposit:      "Deposit",
    withdrawal:   "Withdrawal",
    rebalance:    "Rebalance",
    settlement:   "Settlement",
    yield_earned: "Yield Earned",
};

const STATUS_STYLES: Record<string, string> = {
    completed: "bg-emerald-50 text-emerald-600 border-emerald-100/60",
    pending:   "bg-amber-50  text-amber-600  border-amber-100/60",
    failed:    "bg-red-50    text-red-500    border-red-100/60",
};

const STATUS_LABELS: Record<string, string> = {
    completed: "Confirmed",
    pending:   "Pending",
    failed:    "Failed",
};

const AMOUNT_COLOR: Record<string, string> = {
    deposit:      "text-emerald-600",
    withdrawal:   "text-black/80",
    rebalance:    "text-black/55",
    settlement:   "text-black/55",
    yield_earned: "text-emerald-500",
};

const AMOUNT_PREFIX: Record<string, string> = {
    deposit:      "+",
    withdrawal:   "−",
    rebalance:    "↔",
    settlement:   "↔",
    yield_earned: "+",
};

// ── Props ─────────────────────────────────────────────────────────────────────

interface TransactionTableProps {
    transactions: ActivityTransaction[];
    total:        number;
    nextCursor:   string;
    prevCursor:   string;
    onNext:       () => void;
    onPrev:       () => void;
    loading:      boolean;
    page:         number;   // 1-based display count for "Page N"
    pageSize:     number;
    vaultsMap:    Record<string, string>;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function TransactionTable({
    transactions,
    total,
    nextCursor,
    prevCursor,
    onNext,
    onPrev,
    loading,
    page,
    pageSize,
    vaultsMap,
}: TransactionTableProps) {
    const start = (page - 1) * pageSize + 1;
    const end   = Math.min(page * pageSize, total);

    // Empty state
    if (!loading && transactions.length === 0) {
        return (
            <motion.div
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                className="flex flex-col items-center justify-center rounded-2xl border border-black/[0.06] bg-white py-24 text-center"
            >
                <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/[0.03]">
                    <FileText className="h-5 w-5 text-black/25" />
                </div>
                <p className="text-sm text-black/55 font-medium">No transactions found</p>
                <p className="mt-1 text-xs text-black/35">Try adjusting your filters or date range.</p>
            </motion.div>
        );
    }

    return (
        <div className="flex flex-col gap-4">
            {/* Table card */}
            <div className="relative overflow-hidden rounded-2xl border border-black/[0.06] bg-white shadow-sm">

                {/* Loading overlay */}
                <AnimatePresence>
                    {loading && (
                        <motion.div
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            className="absolute inset-0 z-10 flex items-center justify-center bg-white/70 backdrop-blur-[2px]"
                        >
                            <Loader2 className="h-6 w-6 animate-spin text-black/40" />
                        </motion.div>
                    )}
                </AnimatePresence>

                {/* Header row */}
                <div className="grid grid-cols-[1.4fr_1.1fr_1.4fr_1fr_0.9fr_1.2fr] items-center gap-4 border-b border-black/[0.05] px-6 py-3.5 bg-black/[0.005]">
                    {["Date", "Type", "Vault", "Amount", "Status", "TX Hash"].map(h => (
                        <span key={h} className="text-[10px] font-bold uppercase tracking-widest text-black/30">
                            {h}
                        </span>
                    ))}
                </div>

                {/* Data rows */}
                <AnimatePresence mode="popLayout">
                    <div className="divide-y divide-black/[0.04]">
                        {transactions.map((tx, i) => {
                            const Icon       = TX_ICONS[tx.type] ?? ArrowDownLeft;
                            const label      = TYPE_LABELS[tx.type] ?? tx.type;
                            const explorerUrl = tx.tx_hash ? `https://stellar.expert/explorer/public/tx/${tx.tx_hash}` : null;
                            const date       = new Date(tx.created_at);
                            const vaultName  = vaultsMap[tx.vault_id] ?? `Vault (${tx.vault_id.slice(0, 6)})`;

                            return (
                                <motion.div
                                    key={tx.id}
                                    initial={{ opacity: 0, y: 4 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ delay: i * 0.015 }}
                                    className="grid grid-cols-[1.4fr_1.1fr_1.4fr_1fr_0.9fr_1.2fr] items-center gap-4 px-6 py-3.5 transition-colors hover:bg-black/[0.01]"
                                >
                                    {/* Date */}
                                    <div>
                                        <p className="text-xs font-medium text-black/75">
                                            {date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })}
                                        </p>
                                        <p className="text-[10px] text-black/35 font-mono mt-0.5">
                                            {date.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" })}
                                        </p>
                                    </div>

                                    {/* Type */}
                                    <div className="flex items-center gap-2 min-w-0">
                                        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-black/[0.03] text-black/45">
                                            <Icon className="h-3.5 w-3.5" />
                                        </div>
                                        <span className="truncate text-xs font-medium text-black/80">{label}</span>
                                    </div>

                                    {/* Vault */}
                                    <span className="truncate text-xs font-semibold text-black/70">{vaultName}</span>

                                    {/* Amount */}
                                    <span className={cn(
                                        "font-mono text-xs font-bold tabular-nums",
                                        AMOUNT_COLOR[tx.type] ?? "text-black"
                                    )}>
                                        {AMOUNT_PREFIX[tx.type] ?? ""}{tx.amount} <span className="text-[9px] text-black/35 font-sans font-medium">{tx.currency}</span>
                                    </span>

                                    {/* Status badge */}
                                    <span className={cn(
                                        "inline-flex w-fit items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold tracking-wide",
                                        STATUS_STYLES[tx.status] ?? "bg-black/5 text-black/50 border-black/10"
                                    )}>
                                        {STATUS_LABELS[tx.status] ?? tx.status}
                                    </span>

                                    {/* TX Hash */}
                                    <div className="flex items-center gap-1.5 text-xs text-black/45">
                                        {tx.tx_hash ? (
                                            <>
                                                <span className="font-mono text-[11px] text-black/55">{tx.tx_hash.slice(0, 4)}…{tx.tx_hash.slice(-4)}</span>
                                                <a
                                                    href={explorerUrl!}
                                                    target="_blank"
                                                    rel="noreferrer"
                                                    title={`View on Stellar Expert`}
                                                    className="flex h-5 w-5 items-center justify-center rounded-md text-black/25 transition-colors hover:bg-black/[0.06] hover:text-black/60"
                                                >
                                                    <ExternalLink className="h-3 w-3" />
                                                </a>
                                            </>
                                        ) : (
                                            <span className="text-black/20 font-mono">—</span>
                                        )}
                                    </div>
                                </motion.div>
                            );
                        })}
                    </div>
                </AnimatePresence>
            </div>

            {/* Pagination bar */}
            {total > 0 && (
                <div className="flex items-center justify-between mt-2 px-1">
                    <p className="text-xs text-black/40">
                        Showing{" "}
                        <strong className="text-black/60 font-semibold">{start}–{end}</strong>{" "}
                        of <strong className="text-black/60 font-semibold">{total}</strong> transactions
                    </p>
                    <div className="flex items-center gap-2">
                        <button
                            onClick={onPrev}
                            disabled={!prevCursor && page === 1}
                            className="flex h-8 items-center gap-1.5 rounded-lg border border-black/8 px-3 text-xs font-medium text-black/55 transition-colors hover:border-black/15 hover:text-black disabled:opacity-30 disabled:cursor-not-allowed bg-white"
                        >
                            <ChevronLeft className="h-3.5 w-3.5" />
                            Prev
                        </button>
                        <span className="rounded-lg border border-black/8 px-3 py-1 text-xs font-semibold text-black/55 bg-white">
                            Page {page}
                        </span>
                        <button
                            onClick={onNext}
                            disabled={!nextCursor}
                            className="flex h-8 items-center gap-1.5 rounded-lg border border-black/8 px-3 text-xs font-medium text-black/55 transition-colors hover:border-black/15 hover:text-black disabled:opacity-30 disabled:cursor-not-allowed bg-white"
                        >
                            Next
                            <ChevronRight className="h-3.5 w-3.5" />
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}
