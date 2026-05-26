"use client";

import { motion, AnimatePresence } from "framer-motion";
import { Search, Download, FileText, RotateCcw, SlidersHorizontal, Calendar } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ActivityTransactionType, ActivityTransactionStatus } from "@/lib/api/activity";

const ALL_TYPES: { value: ActivityTransactionType; label: string }[] = [
    { value: "deposit",      label: "Deposit" },
    { value: "withdrawal",   label: "Withdrawal" },
    { value: "rebalance",    label: "Rebalance" },
    { value: "settlement",   label: "Settlement" },
    { value: "yield_earned", label: "Yield Earned" },
];

const ALL_STATUSES: { value: ActivityTransactionStatus | ""; label: string }[] = [
    { value: "",          label: "All" },
    { value: "completed", label: "Confirmed" },
    { value: "pending",   label: "Pending" },
    { value: "failed",    label: "Failed" },
];

export interface FilterState {
    search:  string;
    types:   ActivityTransactionType[];
    status:  ActivityTransactionStatus | "";
    from:    string;
    to:      string;
    vaultId: string;
}

interface FilterBarProps {
    filters:        FilterState;
    vaultOptions:   { id: string; label: string }[];
    totalResults:   number;
    hasActiveFilters: boolean;
    onFilterChange: (next: Partial<FilterState>) => void;
    onClear:        () => void;
    onExportCSV:    () => void;
    onExportPDF:    () => void;
}

export function FilterBar({
    filters,
    vaultOptions,
    totalResults,
    hasActiveFilters,
    onFilterChange,
    onClear,
    onExportCSV,
    onExportPDF,
}: FilterBarProps) {
    const toggleType = (type: ActivityTransactionType) => {
        const next = filters.types.includes(type)
            ? filters.types.filter(t => t !== type)
            : [...filters.types, type];
        onFilterChange({ types: next });
    };

    return (
        <div className="flex flex-col gap-5 rounded-2xl border border-black/[0.06] bg-white p-6 shadow-sm">

            {/* Row 1 — Search + results count */}
            <div className="flex flex-wrap items-center gap-4">
                <div className="relative min-w-[260px] flex-1">
                    <Search className="absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-black/30" />
                    <input
                        type="text"
                        value={filters.search}
                        onChange={e => onFilterChange({ search: e.target.value })}
                        placeholder="Search by TX hash or amount…"
                        className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.01] pl-10 pr-4 text-[13px] text-black placeholder:text-black/30 outline-none transition-all focus:border-black/25 focus:bg-white"
                    />
                </div>
                <div className="flex items-center gap-1.5 text-xs text-black/40">
                    <SlidersHorizontal className="h-3.5 w-3.5" />
                    <span>
                        <strong className="text-black/70">{totalResults}</strong> result{totalResults !== 1 ? "s" : ""}
                    </span>
                </div>
            </div>

            {/* Row 2 — Type checkboxes */}
            <div>
                <p className="mb-2.5 text-[11px] font-medium uppercase tracking-widest text-black/30">Type</p>
                <div className="flex flex-wrap gap-2">
                    {ALL_TYPES.map(({ value, label }) => {
                        const active = filters.types.includes(value);
                        return (
                            <button
                                key={value}
                                onClick={() => toggleType(value)}
                                className={cn(
                                    "flex items-center gap-2 rounded-full border px-3.5 py-1.5 text-xs transition-all",
                                    active
                                        ? "border-black bg-black text-white"
                                        : "border-black/10 bg-white text-black/55 hover:border-black/20 hover:text-black"
                                )}
                            >
                                <span className={cn(
                                    "h-1.5 w-1.5 rounded-full",
                                    active ? "bg-white" : "bg-black/20"
                                )} />
                                {label}
                            </button>
                        );
                    })}
                </div>
            </div>

            {/* Row 3 — Date range + Status + Clear + Export */}
            <div className="flex flex-wrap items-end justify-between gap-4 border-t border-black/[0.05] pt-4">

                {/* Date range */}
                <div className="flex flex-wrap items-center gap-3">
                    <div className="flex flex-col gap-1">
                        <label className="flex items-center gap-1 text-[11px] text-black/35">
                            <Calendar className="h-3 w-3" /> From
                        </label>
                        <input
                            type="date"
                            value={filters.from}
                            onChange={e => onFilterChange({ from: e.target.value })}
                            className="h-9 rounded-lg border border-black/10 bg-white px-3 text-xs text-black outline-none transition-colors focus:border-black/25"
                        />
                    </div>
                    <div className="flex flex-col gap-1">
                        <label className="flex items-center gap-1 text-[11px] text-black/35">
                            <Calendar className="h-3 w-3" /> To
                        </label>
                        <input
                            type="date"
                            value={filters.to}
                            onChange={e => onFilterChange({ to: e.target.value })}
                            className="h-9 rounded-lg border border-black/10 bg-white px-3 text-xs text-black outline-none transition-colors focus:border-black/25"
                        />
                    </div>

                    {/* Vault Select Dropdown */}
                    <div className="flex flex-col gap-1">
                        <label className="text-[11px] text-black/35">Vault</label>
                        <select
                            value={filters.vaultId}
                            onChange={e => onFilterChange({ vaultId: e.target.value })}
                            className="h-9 rounded-lg border border-black/10 bg-white px-3 text-xs text-black outline-none transition-colors focus:border-black/25"
                        >
                            <option value="">All Vaults</option>
                            {vaultOptions.map(option => (
                                <option key={option.id} value={option.id}>
                                    {option.label}
                                </option>
                            ))}
                        </select>
                    </div>

                    {/* Status segmented toggle */}
                    <div className="flex flex-col gap-1">
                        <label className="text-[11px] text-black/35">Status</label>
                        <div className="flex h-9 overflow-hidden rounded-lg border border-black/10 bg-black/[0.02]">
                            {ALL_STATUSES.map(({ value, label }) => (
                                <button
                                    key={label}
                                    onClick={() => onFilterChange({ status: value })}
                                    className={cn(
                                        "px-3.5 text-xs transition-colors",
                                        filters.status === value
                                            ? "bg-black text-white"
                                            : "text-black/50 hover:text-black"
                                    )}
                                >
                                    {label}
                                </button>
                            ))}
                        </div>
                    </div>

                    {/* Clear */}
                    <AnimatePresence>
                        {hasActiveFilters && (
                            <motion.button
                                initial={{ opacity: 0, scale: 0.9 }}
                                animate={{ opacity: 1, scale: 1 }}
                                exit={{ opacity: 0, scale: 0.9 }}
                                onClick={onClear}
                                className="mt-4 flex h-9 items-center gap-1.5 rounded-lg border border-red-200 bg-red-50 px-3.5 text-xs font-medium text-red-600 transition-colors hover:bg-red-100"
                            >
                                <RotateCcw className="h-3 w-3" />
                                Reset
                            </motion.button>
                        )}
                    </AnimatePresence>
                </div>

                {/* Export buttons */}
                <div className="flex items-center gap-2">
                    <motion.button
                        whileHover={{ scale: 1.02 }} whileTap={{ scale: 0.97 }}
                        onClick={onExportCSV}
                        className="flex h-9 items-center gap-2 rounded-lg border border-black/10 bg-white px-4 text-xs font-medium text-black/60 transition-colors hover:border-black/20 hover:text-black"
                    >
                        <Download className="h-3.5 w-3.5" />
                        CSV
                    </motion.button>
                    <motion.button
                        whileHover={{ scale: 1.02 }} whileTap={{ scale: 0.97 }}
                        onClick={onExportPDF}
                        className="flex h-9 items-center gap-2 rounded-lg bg-black px-4 text-xs font-medium text-white transition-opacity hover:opacity-85"
                    >
                        <FileText className="h-3.5 w-3.5" />
                        PDF Statement
                    </motion.button>
                </div>
            </div>
        </div>
    );
}
