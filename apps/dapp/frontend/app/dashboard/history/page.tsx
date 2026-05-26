"use client";

import { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import { ArrowLeft, TrendingUp, ArrowDownLeft, ArrowUpRight, Zap, Landmark, Loader2 } from "lucide-react";
import Link from "next/link";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { FilterBar, FilterState } from "@/components/history/FilterBar";
import { TransactionTable } from "@/components/history/TransactionTable";
import { fetchActivity } from "@/lib/api/activity";
import { exportToCSV } from "@/lib/export/csv";
import { exportToPDF } from "@/lib/export/pdf";
import { vaultDefinitions } from "@/lib/vault-data";

// ── Yield Summary Cards ──────────────────────────────────────────────────────

interface YieldSummaryProps {
    totalDeposited: string;
    totalWithdrawn: string;
    totalYield:     string;
    loading:        boolean;
}

function YieldSummary({ totalDeposited, totalWithdrawn, totalYield, loading }: YieldSummaryProps) {
    const dep = parseFloat(totalDeposited || "0");
    const wit = parseFloat(totalWithdrawn || "0");
    const yld = parseFloat(totalYield || "0");
    const net = dep - wit + yld;

    const stats = [
        {
            label: "Total Deposited",
            value: dep.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
            icon: ArrowDownLeft,
            color: "text-emerald-600",
            prefix: "+",
        },
        {
            label: "Total Withdrawn",
            value: wit.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
            icon: ArrowUpRight,
            color: "text-black/70",
            prefix: "−",
        },
        {
            label: "Yield Earned",
            value: yld.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 4 }),
            icon: Zap,
            color: "text-amber-500",
            prefix: "+",
        },
        {
            label: "Net Position",
            value: net.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
            icon: TrendingUp,
            color: net >= 0 ? "text-emerald-600" : "text-red-500",
            prefix: net >= 0 ? "+" : "",
        },
    ];

    return (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {stats.map((stat, i) => (
                <motion.div
                    key={stat.label}
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: i * 0.04 }}
                    className="relative overflow-hidden rounded-2xl border border-black/[0.06] bg-white px-5 py-4 shadow-sm"
                >
                    <div className="mb-3 flex h-7 w-7 items-center justify-center rounded-lg bg-black/[0.03]">
                        <stat.icon className="h-3.5 w-3.5 text-black/35" />
                    </div>
                    <p className={`font-mono text-lg font-bold leading-none tabular-nums ${stat.color}`}>
                        {stat.prefix}{stat.value}
                    </p>
                    <p className="mt-1.5 text-[11px] font-medium text-black/35">{stat.label}</p>
                </motion.div>
            ))}
        </div>
    );
}

// ── Page Component ────────────────────────────────────────────────────────────

export default function TransactionHistoryPage() {
    const { address, isConnected } = useWallet();

    // Vault mappings loaded dynamically from API matching contract addresses to definitions
    const [vaultsOptions, setVaultsOptions] = useState<{ id: string; label: string }[]>([]);
    const [vaultsMap, setVaultsMap] = useState<Record<string, string>>({});

    // Filter bar state
    const [filters, setFilters] = useState<FilterState>({
        search: "",
        types: [],
        status: "",
        from: "",
        to: "",
        vaultId: "",
    });

    // Pagination keyset cursor tracking stack
    const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([undefined]);
    const [page, setPage] = useState(1);
    const pageSize = 25;

    // API Response State
    const [loading, setLoading] = useState(false);
    const [pageData, setPageData] = useState<any>(null);
    const [error, setError] = useState<string | null>(null);

    // Export progress states
    const [exportingCSV, setExportingCSV] = useState(false);
    const [exportingPDF, setExportingPDF] = useState(false);

    const hasActiveFilters = useMemo(() => {
        return (
            filters.search !== "" ||
            filters.types.length > 0 ||
            filters.status !== "" ||
            filters.from !== "" ||
            filters.to !== "" ||
            filters.vaultId !== ""
        );
    }, [filters]);

    // Load vaults to construct human-readable mappings
    useEffect(() => {
        if (!address) return;

        const loadVaults = async () => {
            try {
                const token = localStorage.getItem("nester_token") ?? "";
                const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/api/v1/vaults?userId=${address}`, {
                    headers: {
                        Authorization: `Bearer ${token}`
                    }
                });
                if (res.ok) {
                    const payload = await res.json();
                    const list = payload.data || [];

                    const options = list.map((v: any) => {
                        const def = vaultDefinitions.find(
                            d => d.contractAddress.toLowerCase() === v.contract_address.toLowerCase() ||
                                 (d.contractXlmAddress && d.contractXlmAddress.toLowerCase() === v.contract_address.toLowerCase())
                        );
                        return {
                            id: v.id,
                            label: def ? def.name : `Vault (${v.contract_address.slice(0, 8)})`
                        };
                    });

                    const map: Record<string, string> = {};
                    list.forEach((v: any) => {
                        const def = vaultDefinitions.find(
                            d => d.contractAddress.toLowerCase() === v.contract_address.toLowerCase() ||
                                 (d.contractXlmAddress && d.contractXlmAddress.toLowerCase() === v.contract_address.toLowerCase())
                        );
                        map[v.id] = def ? def.name : `Vault (${v.contract_address.slice(0, 8)})`;
                    });

                    setVaultsOptions(options);
                    setVaultsMap(map);
                }
            } catch (err) {
                console.error("Failed to load user vaults", err);
            }
        };

        loadVaults();
    }, [address]);

    // Load paginated list activity
    useEffect(() => {
        if (!address) return;

        const loadActivity = async () => {
            try {
                setLoading(true);
                setError(null);
                const currentCursor = cursorStack[page - 1];

                const data = await fetchActivity({
                    userId: address,
                    types: filters.types.length ? filters.types : undefined,
                    status: filters.status || undefined,
                    from: filters.from ? new Date(filters.from).toISOString() : undefined,
                    to: filters.to ? new Date(filters.to).toISOString() : undefined,
                    cursor: currentCursor,
                    limit: pageSize,
                    vaultId: filters.vaultId || undefined,
                    search: filters.search || undefined,
                });

                setPageData(data);
            } catch (err: any) {
                console.error("Failed to load activity details", err);
                setError(err.message || "Failed to load transaction history.");
            } finally {
                setLoading(false);
            }
        };

        loadActivity();
    }, [address, filters, page, cursorStack]);

    const handleFilterChange = (next: Partial<FilterState>) => {
        setFilters(prev => ({ ...prev, ...next }));
        setPage(1);
        setCursorStack([undefined]);
    };

    const handleClear = () => {
        setFilters({
            search: "",
            types: [],
            status: "",
            from: "",
            to: "",
            vaultId: "",
        });
        setPage(1);
        setCursorStack([undefined]);
    };

    const handleNextPage = () => {
        if (pageData?.next_cursor) {
            setCursorStack(prev => {
                const next = [...prev];
                next[page] = pageData.next_cursor;
                return next;
            });
            setPage(prev => prev + 1);
        }
    };

    const handlePrevPage = () => {
        if (page > 1) {
            setPage(prev => prev - 1);
        }
    };

    const handleExportCSV = async () => {
        if (!address) return;
        try {
            setExportingCSV(true);
            const data = await fetchActivity({
                userId: address,
                types: filters.types.length ? filters.types : undefined,
                status: filters.status || undefined,
                from: filters.from ? new Date(filters.from).toISOString() : undefined,
                to: filters.to ? new Date(filters.to).toISOString() : undefined,
                vaultId: filters.vaultId || undefined,
                search: filters.search || undefined,
                limit: 1000,
            });
            exportToCSV(data.items);
        } catch (err) {
            console.error("CSV export failed", err);
        } finally {
            setExportingCSV(false);
        }
    };

    const handleExportPDF = async () => {
        if (!address) return;
        try {
            setExportingPDF(true);
            const data = await fetchActivity({
                userId: address,
                types: filters.types.length ? filters.types : undefined,
                status: filters.status || undefined,
                from: filters.from ? new Date(filters.from).toISOString() : undefined,
                to: filters.to ? new Date(filters.to).toISOString() : undefined,
                vaultId: filters.vaultId || undefined,
                search: filters.search || undefined,
                limit: 1000,
            });
            await exportToPDF({
                transactions: data.items,
                from: filters.from || undefined,
                to: filters.to || undefined,
                walletAddress: address,
            });
        } catch (err) {
            console.error("PDF export failed", err);
        } finally {
            setExportingPDF(false);
        }
    };

    if (!isConnected || !address) {
        return (
            <AppShell>
                <div className="flex flex-col items-center justify-center rounded-2xl border border-black/[0.06] bg-white py-24 text-center shadow-sm">
                    <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/[0.03]">
                        <Landmark className="h-5 w-5 text-black/25" />
                    </div>
                    <p className="text-sm font-semibold text-black/55">Wallet connection required</p>
                    <p className="mt-1 text-xs text-black/35">Please connect your Stellar wallet to view your transaction history.</p>
                </div>
            </AppShell>
        );
    }

    return (
        <AppShell>
            {/* Back nav */}
            <motion.div
                initial={{ opacity: 0, y: -6 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-6"
            >
                <Link
                    href="/dashboard"
                    className="inline-flex items-center gap-1.5 text-xs font-semibold text-black/40 transition-colors hover:text-black/70"
                >
                    <ArrowLeft className="h-3.5 w-3.5" />
                    Back to Dashboard
                </Link>
            </motion.div>

            {/* Page header */}
            <motion.div
                initial={{ opacity: 0, y: -6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.02 }}
                className="mb-7"
            >
                <h1 className="text-2xl font-bold text-black sm:text-3xl tracking-tight">Transaction History</h1>
                <p className="mt-1 text-sm text-black/45">
                    A full record of your deposits, withdrawals, rebalances, and yield accruals.
                </p>
            </motion.div>

            {/* Yield summary stats cards */}
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ delay: 0.05 }}
                className="mb-6"
            >
                <YieldSummary
                    totalDeposited={pageData?.total_deposited || "0"}
                    totalWithdrawn={pageData?.total_withdrawn || "0"}
                    totalYield={pageData?.total_yield_earned || "0"}
                    loading={loading}
                />
            </motion.div>

            {/* Filter bar */}
            <motion.div
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.08 }}
                className="mb-5"
            >
                <FilterBar
                    filters={filters}
                    vaultOptions={vaultsOptions}
                    totalResults={pageData?.total || 0}
                    hasActiveFilters={hasActiveFilters}
                    onFilterChange={handleFilterChange}
                    onClear={handleClear}
                    onExportCSV={handleExportCSV}
                    onExportPDF={handleExportPDF}
                />
            </motion.div>

            {/* Error messaging */}
            {error && (
                <div className="mb-4 rounded-xl border border-red-100 bg-red-50 p-4 text-xs text-red-600 font-medium">
                    {error}
                </div>
            )}

            {/* Export loading alerts */}
            {(exportingCSV || exportingPDF) && (
                <div className="mb-4 flex items-center gap-2 rounded-xl border border-black/5 bg-black/[0.01] px-4 py-3 text-xs text-black/50 font-medium">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    <span>Preparing statement export, this may take a moment…</span>
                </div>
            )}

            {/* Transaction table */}
            <motion.div
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.12 }}
            >
                <TransactionTable
                    transactions={pageData?.items || []}
                    total={pageData?.total || 0}
                    nextCursor={pageData?.next_cursor || ""}
                    prevCursor={page > 1 ? "prev" : ""}
                    onNext={handleNextPage}
                    onPrev={handlePrevPage}
                    loading={loading}
                    page={page}
                    pageSize={pageSize}
                    vaultsMap={vaultsMap}
                />
            </motion.div>
        </AppShell>
    );
}
