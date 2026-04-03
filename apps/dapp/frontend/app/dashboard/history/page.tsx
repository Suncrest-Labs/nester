"use client";

import Link from "next/link";
import { useWallet } from "@/components/wallet-provider";
import {
    usePortfolio,
    type PortfolioTransactionType,
} from "@/components/portfolio-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import { Fragment, useEffect, useState, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { 
    History, 
    Search, 
    ArrowUpRight, 
    ArrowDownLeft, 
    RefreshCcw, 
    LineChart,
    ExternalLink,
    SearchX,
    ChevronLeft,
    ChevronRight,
    Download,
    Calendar,
    Filter,
    Vault as VaultIcon
} from "lucide-react";
import { cn, truncateAddress } from "@/lib/utils";
import { getExplorerTxUrl } from "@/utils/explorer";

const TYPE_ICONS = {
    "Deposit": ArrowDownLeft,
    "Withdrawal": ArrowUpRight,
    "Yield Accrual": LineChart,
    "Rebalance": RefreshCcw
};

const STATUS_COLORS = {
    "Confirmed": "text-emerald-600 bg-emerald-50 border-emerald-100",
    "Pending": "text-amber-600 bg-amber-50 border-amber-100",
    "Failed": "text-rose-600 bg-rose-50 border-rose-100"
};

const PAGE_SIZE = 10;

export default function HistoryPage() {
    const { isConnected, isInitializing } = useWallet();
    const { transactions } = usePortfolio();
    const router = useRouter();
    
    const [searchQuery, setSearchQuery] = useState("");
    const [filterType, setFilterType] = useState<PortfolioTransactionType | "All">("All");
    const [filterVault, setFilterVault] = useState("All");
    const [startDate, setStartDate] = useState("");
    const [endDate, setEndDate] = useState("");
    const [currentPage, setCurrentPage] = useState(1);

    useEffect(() => {
        if (!isInitializing && !isConnected) {
            router.push("/");
        }
    }, [isConnected, isInitializing, router]);

    const filteredTransactions = useMemo(() => {
        return transactions.filter(tx => {
            const matchesType = filterType === "All" || tx.type === filterType;
            const matchesVault = filterVault === "All" || tx.vaultName === filterVault;
            const matchesSearch = tx.txHash.toLowerCase().includes(searchQuery.toLowerCase()) || 
                                 tx.amount.includes(searchQuery);
            
            let matchesDate = true;
            if (startDate) matchesDate = matchesDate && new Date(tx.timestamp) >= new Date(startDate);
            if (endDate) {
                const end = new Date(endDate);
                end.setHours(23, 59, 59, 999);
                matchesDate = matchesDate && new Date(tx.timestamp) <= end;
            }
            
            return matchesType && matchesVault && matchesSearch && matchesDate;
        });
    }, [filterType, filterVault, searchQuery, startDate, endDate, transactions]);

    const totalPages = Math.ceil(filteredTransactions.length / PAGE_SIZE);
    const paginatedTransactions = filteredTransactions.slice(
        (currentPage - 1) * PAGE_SIZE,
        currentPage * PAGE_SIZE
    );

    const uniqueVaults = Array.from(new Set(transactions.map(tx => tx.vaultName)));

    const exportToCSV = () => {
        const headers = ["ID", "Type", "Amount", "Asset", "Vault", "Timestamp", "Status", "Hash"];
        const rows = filteredTransactions.map(tx => [
            tx.id, tx.type, tx.amount, tx.asset, tx.vaultName, tx.timestamp, tx.status, tx.txHash
        ]);
        
        const csvContent = [headers, ...rows].map(row => 
            row.map(field => `"${String(field).replace(/"/g, '""')}"`).join(",")
        ).join("\n");
        
        const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.setAttribute("href", url);
        link.setAttribute("download", `nester_history_${new Date().toISOString().split('T')[0]}.csv`);
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
    };

    if (isInitializing || !isConnected) return null;

    const isInitiallyEmpty = transactions.length === 0;

    return (
        <div className="min-h-screen bg-background">
            <Navbar />

            <main className="mx-auto max-w-[1536px] px-4 md:px-8 lg:px-12 xl:px-16 pt-28 pb-16">
                {/* Header Section */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5 }}
                    className="mb-8 flex flex-col md:flex-row md:items-end justify-between gap-4"
                >
                    <div className="flex-1">
                        <div className="flex items-center gap-2 text-primary mb-2">
                            <History className="h-4 w-4" />
                            <span className="text-xs font-mono font-medium uppercase tracking-wider">Transaction Ledger</span>
                        </div>
                        <h1 className="font-heading text-3xl font-light text-foreground sm:text-4xl">
                            Activity History
                        </h1>
                    </div>
                    
                    {!isInitiallyEmpty && (
                        <button 
                            onClick={exportToCSV}
                            className="flex items-center gap-2 px-4 py-2.5 rounded-full border border-border bg-white text-sm font-medium hover:bg-secondary transition-all"
                        >
                            <Download className="h-4 w-4" />
                            Export CSV
                        </button>
                    )}
                </motion.div>

                {isInitiallyEmpty ? (
                    /* Initial Empty State */
                    <motion.div
                        initial={{ opacity: 0, scale: 0.95 }}
                        animate={{ opacity: 1, scale: 1 }}
                        className="mt-12 rounded-3xl border border-dashed border-border py-32 text-center"
                    >
                        <div className="flex flex-col items-center max-w-sm mx-auto">
                            <div className="mb-6 h-20 w-20 rounded-3xl bg-secondary flex items-center justify-center">
                                <VaultIcon className="h-10 w-10 text-muted-foreground" />
                            </div>
                            <h2 className="font-heading text-2xl font-light text-foreground mb-3">No transactions yet</h2>
                            <p className="text-muted-foreground leading-relaxed mb-8">
                                You haven&apos;t made any transactions. Browse our vaults to start earning optimized yield.
                            </p>
                            <div className="p-[3px] rounded-full border border-black/15 shadow-lg bg-white inline-block">
                                <Link href="/dashboard/vaults">
                                    <button className="rounded-full bg-brand-dark px-8 py-3 text-sm font-medium text-white hover:bg-black transition-all">
                                        Browse Vaults
                                    </button>
                                </Link>
                            </div>
                        </div>
                    </motion.div>
                ) : (
                    <>
                        {/* Filters & Search */}
                        <div className="mb-6 space-y-4">
                            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
                                <div className="lg:col-span-2 relative">
                                    <Search className="absolute left-4 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                                    <input
                                        type="text"
                                        placeholder="Search by hash or amount..."
                                        value={searchQuery}
                                        onChange={(e) => {
                                            setSearchQuery(e.target.value);
                                            setCurrentPage(1);
                                        }}
                                        className="w-full rounded-2xl border border-border bg-white pl-11 pr-4 py-3 text-sm transition-all focus:border-black/20 focus:outline-none"
                                    />
                                </div>
                                <div className="relative">
                                    <Filter className="absolute left-4 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground z-10" />
                                    <select
                                        value={filterType}
                                        onChange={(e) => {
                                            setFilterType(e.target.value as PortfolioTransactionType | "All");
                                            setCurrentPage(1);
                                        }}
                                        className="w-full rounded-2xl border border-border bg-white pl-11 pr-4 py-3 text-sm appearance-none cursor-pointer focus:border-black/20 focus:outline-none"
                                    >
                                        <option value="All">All Types</option>
                                        <option value="Deposit">Deposits</option>
                                        <option value="Withdrawal">Withdrawals</option>
                                        <option value="Yield Accrual">Yield Accruals</option>
                                        <option value="Rebalance">Rebalancing</option>
                                    </select>
                                </div>
                                <div className="relative">
                                    <VaultIcon className="absolute left-4 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground z-10" />
                                    <select
                                        value={filterVault}
                                        onChange={(e) => {
                                            setFilterVault(e.target.value);
                                            setCurrentPage(1);
                                        }}
                                        className="w-full rounded-2xl border border-border bg-white pl-11 pr-4 py-3 text-sm appearance-none cursor-pointer focus:border-black/20 focus:outline-none"
                                    >
                                        <option value="All">All Vaults</option>
                                        {uniqueVaults.map(vault => (
                                            <option key={vault} value={vault}>{vault}</option>
                                        ))}
                                    </select>
                                </div>
                            </div>
                            
                            {/* Date Range Row */}
                            <div className="flex flex-wrap items-center gap-4 bg-secondary/30 p-4 rounded-2xl border border-border/50">
                                <span className="text-[10px] font-mono font-medium uppercase tracking-widest text-muted-foreground flex items-center gap-2">
                                    <Calendar className="h-3 w-3" /> Date Range
                                </span>
                                <div className="flex items-center gap-2">
                                    <input 
                                        type="date"
                                        value={startDate}
                                        onChange={(e) => {
                                            setStartDate(e.target.value);
                                            setCurrentPage(1);
                                        }}
                                        className="h-8 rounded-lg border border-border bg-white px-3 text-xs focus:outline-none focus:border-black/20"
                                    />
                                    <span className="text-muted-foreground text-xs">—</span>
                                    <input 
                                        type="date"
                                        value={endDate}
                                        onChange={(e) => {
                                            setEndDate(e.target.value);
                                            setCurrentPage(1);
                                        }}
                                        className="h-8 rounded-lg border border-border bg-white px-3 text-xs focus:outline-none focus:border-black/20"
                                    />
                                </div>
                                {(startDate || endDate) && (
                                    <button 
                                        onClick={() => { 
                                            setStartDate(""); 
                                            setEndDate(""); 
                                            setCurrentPage(1);
                                        }}
                                        className="text-[10px] font-medium text-primary hover:underline"
                                    >
                                        Clear Range
                                    </button>
                                )}
                            </div>
                        </div>

                        {/* Transaction Table */}
                        <motion.div
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ duration: 0.5, delay: 0.2 }}
                            className="rounded-3xl border border-border bg-white overflow-hidden shadow-sm hover:shadow-md transition-shadow"
                        >
                            <div className="overflow-x-auto scrollbar-hide">
                                <table className="w-full text-left border-collapse min-w-[800px]">
                                    <thead>
                                        <tr className="border-b border-border bg-secondary/10">
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">Type</th>
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">Vault</th>
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">Date & Time</th>
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider text-right">Amount</th>
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">Status</th>
                                            <th className="px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider text-right">Hash</th>
                                        </tr>
                                    </thead>
                                    <tbody className="divide-y divide-border">
                                        <AnimatePresence mode="popLayout" initial={false}>
                                            {paginatedTransactions.length > 0 ? (
                                                paginatedTransactions.map((tx) => {
                                                    const Icon = TYPE_ICONS[tx.type];
                                                    return (
                                                        <motion.tr 
                                                            key={tx.id}
                                                            layout
                                                            initial={{ opacity: 0 }}
                                                            animate={{ opacity: 1 }}
                                                            exit={{ opacity: 0, scale: 0.98 }}
                                                            transition={{ duration: 0.2 }}
                                                            className="group hover:bg-secondary/20 transition-colors"
                                                        >
                                                            <td className="px-6 py-4">
                                                                <div className="flex items-center gap-3">
                                                                    <div className="h-9 w-9 rounded-xl bg-secondary flex items-center justify-center text-foreground/50 group-hover:scale-110 transition-transform">
                                                                        <Icon className="h-4 w-4" />
                                                                    </div>
                                                                    <span className="text-sm font-medium">{tx.type}</span>
                                                                </div>
                                                            </td>
                                                            <td className="px-6 py-4">
                                                                <span className="text-sm text-foreground/70">{tx.vaultName}</span>
                                                            </td>
                                                            <td className="px-6 py-4">
                                                                <div className="flex flex-col">
                                                                    <span className="text-sm text-foreground/80 font-medium">
                                                                        {new Date(tx.timestamp).toLocaleDateString([], { month: "short", day: "numeric" })}
                                                                    </span>
                                                                    <span className="text-[10px] text-muted-foreground font-mono">
                                                                        {new Date(tx.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                                                                    </span>
                                                                </div>
                                                            </td>
                                                            <td className="px-6 py-4 text-right">
                                                                <div className={cn(
                                                                    "text-sm font-semibold",
                                                                    tx.amount.startsWith("+") ? "text-emerald-600" : 
                                                                    tx.amount.startsWith("-") ? "text-rose-600" : "text-foreground"
                                                                )}>
                                                                    {tx.amount} <span className="text-[10px] opacity-70 ml-0.5">{tx.asset}</span>
                                                                </div>
                                                            </td>
                                                            <td className="px-6 py-4">
                                                                <span className={cn(
                                                                    "px-2.5 py-0.5 rounded-full text-[10px] font-bold border uppercase tracking-wide",
                                                                    STATUS_COLORS[tx.status]
                                                                )}>
                                                                    {tx.status}
                                                                </span>
                                                            </td>
                                                            <td className="px-6 py-4 text-right">
                                                                <a 
                                                                    href={getExplorerTxUrl(tx.txHash)} 
                                                                    target="_blank" 
                                                                    rel="noopener noreferrer"
                                                                    className="inline-flex items-center gap-1.5 text-xs text-primary hover:text-primary-foreground hover:bg-primary px-2 py-1 rounded-lg transition-all group/link"
                                                                >
                                                                    <span className="font-mono">{truncateAddress(tx.txHash)}</span>
                                                                    <ExternalLink className="h-3 w-3 opacity-50 group-hover/link:opacity-100" />
                                                                </a>
                                                            </td>
                                                        </motion.tr>
                                                    );
                                                })
                                            ) : (
                                                <motion.tr initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
                                                    <td colSpan={6} className="px-6 py-24 text-center">
                                                        <div className="flex flex-col items-center justify-center">
                                                            <div className="h-16 w-16 bg-secondary rounded-3xl flex items-center justify-center mb-4">
                                                                <SearchX className="h-8 w-8 text-muted-foreground" />
                                                            </div>
                                                            <h3 className="font-heading text-lg font-light text-foreground">No matching results</h3>
                                                            <p className="text-sm text-muted-foreground max-w-xs mt-1">
                                                                No transactions met your search or filter criteria. Try clearing them to see all activity.
                                                            </p>
                                                        </div>
                                                    </td>
                                                </motion.tr>
                                            )}
                                        </AnimatePresence>
                                    </tbody>
                                </table>
                            </div>

                            {/* Pagination */}
                            <div className="px-6 py-5 border-t border-border flex flex-col sm:flex-row items-center justify-between gap-4 bg-secondary/5">
                                <p className="text-xs text-muted-foreground select-none">
                                    Displaying <span className="font-medium text-foreground">{paginatedTransactions.length}</span> of <span className="font-medium text-foreground">{filteredTransactions.length}</span> transactions
                                </p>
                                
                                <div className="flex items-center gap-3">
                                    <button 
                                        disabled={currentPage === 1}
                                        onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
                                        className="p-1.5 rounded-xl border border-border bg-white text-foreground hover:bg-secondary disabled:opacity-30 disabled:hover:bg-white transition-colors shadow-sm"
                                    >
                                        <ChevronLeft className="h-4 w-4" />
                                    </button>
                                    
                                    <div className="flex items-center gap-1.5">
                                        {(() => {
                                            let pages: number[] = [];
                                            if (totalPages <= 5) {
                                                pages = Array.from({ length: totalPages }, (_, i) => i + 1);
                                            } else {
                                                if (currentPage <= 3) {
                                                    pages = [1, 2, 3, 4, totalPages];
                                                } else if (currentPage >= totalPages - 2) {
                                                    pages = [1, totalPages - 3, totalPages - 2, totalPages - 1, totalPages];
                                                } else {
                                                    pages = [1, currentPage - 1, currentPage, currentPage + 1, totalPages];
                                                }
                                            }
                                            return pages.map((page, index, array) => {
                                                const isGap = index > 0 && page - array[index - 1] > 1;
                                                return (
                                                    <Fragment key={page}>
                                                        {isGap && (
                                                            <span className="mx-0.5 text-muted-foreground tracking-widest text-xs">
                                                                ...
                                                            </span>
                                                        )}
                                                        <button 
                                                            onClick={() => setCurrentPage(page)}
                                                            className={cn(
                                                                "h-8 w-8 rounded-xl text-xs font-medium transition-all",
                                                                currentPage === page 
                                                                ? "bg-brand-dark text-white shadow-md shadow-black/10" 
                                                                : "bg-white border border-border text-foreground hover:border-black/20"
                                                            )}
                                                        >
                                                            {page}
                                                        </button>
                                                    </Fragment>
                                                );
                                            });
                                        })()}
                                    </div>

                                    <button 
                                        disabled={currentPage === totalPages || totalPages === 0}
                                        onClick={() => setCurrentPage(p => Math.min(totalPages, p + 1))}
                                        className="p-1.5 rounded-xl border border-border bg-white text-foreground hover:bg-secondary disabled:opacity-30 disabled:hover:bg-white transition-colors shadow-sm"
                                    >
                                        <ChevronRight className="h-4 w-4" />
                                    </button>
                                </div>
                            </div>
                        </motion.div>
                    </>
                )}
            </main>
        </div>
    );
}
