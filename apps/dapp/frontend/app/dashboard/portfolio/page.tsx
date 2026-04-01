"use client";

import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { motion } from "framer-motion";
import {
    Search,
    RefreshCw,
    Wallet,
    TrendingUp,
    Coins,
    AlertCircle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";

// ── Types ─────────────────────────────────────────────────────────────────────

interface StellarBalance {
    asset_type: "native" | "credit_alphanum4" | "credit_alphanum12";
    asset_code?: string;
    asset_issuer?: string;
    balance: string;
    limit?: string;
    buying_liabilities: string;
    selling_liabilities: string;
    is_authorized?: boolean;
}

interface AssetRow {
    code: string;
    issuer: string | null;
    balance: number;
    type: "native" | "token";
    limit: number | null;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function getHorizonUrl(): string {
    if (typeof window !== "undefined") {
        const saved = localStorage.getItem("nester_network_id");
        if (saved === "mainnet") return NETWORKS.mainnet.horizonUrl;
        if (saved === "testnet") return NETWORKS.testnet.horizonUrl;
    }
    return DEFAULT_NETWORK.horizonUrl;
}

function isValidStellarAddress(addr: string): boolean {
    return /^G[A-Z2-7]{55}$/.test(addr);
}

async function fetchAccountAssets(address: string): Promise<AssetRow[]> {
    const horizonUrl = getHorizonUrl();
    const res = await fetch(`${horizonUrl}/accounts/${address}`);
    if (res.status === 404) throw new Error("Account not found on this network.");
    if (!res.ok) throw new Error(`Horizon error: ${res.status}`);
    const data = await res.json();
    const balances: StellarBalance[] = data.balances ?? [];

    return balances.map((b) => ({
        code: b.asset_type === "native" ? "XLM" : (b.asset_code ?? "?"),
        issuer: b.asset_issuer ?? null,
        balance: parseFloat(b.balance),
        type: b.asset_type === "native" ? "native" : "token",
        limit: b.limit ? parseFloat(b.limit) : null,
    }));
}

function truncateIssuer(issuer: string, chars = 8): string {
    if (issuer.length <= chars * 2 + 3) return issuer;
    return `${issuer.slice(0, chars)}…${issuer.slice(-chars)}`;
}

// ── Sub-components ────────────────────────────────────────────────────────────

function AssetCard({ asset, index }: { asset: AssetRow; index: number }) {
    const pct =
        asset.limit && asset.limit > 0
            ? Math.min((asset.balance / asset.limit) * 100, 100)
            : null;

    return (
        <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, delay: index * 0.05 }}
            className="rounded-2xl border border-border bg-white p-4 transition-all hover:border-black/15 hover:shadow-sm sm:p-5"
        >
            <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                    <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-secondary text-sm font-semibold text-foreground/70">
                        {asset.code.slice(0, 2)}
                    </div>
                    <div>
                        <p className="text-sm font-medium text-foreground">
                            {asset.code}
                        </p>
                        <p className="mt-0.5 text-[10px] text-muted-foreground">
                            {asset.type === "native" ? "Stellar Native" : "Custom Token"}
                        </p>
                    </div>
                </div>

                <div className="text-right">
                    <p className="font-mono text-sm font-medium text-foreground">
                        {asset.balance.toLocaleString("en-US", {
                            minimumFractionDigits: 2,
                            maximumFractionDigits: 7,
                        })}
                    </p>
                    {asset.limit !== null && (
                        <p className="mt-0.5 text-[10px] text-muted-foreground">
                            Limit: {asset.limit.toLocaleString()}
                        </p>
                    )}
                </div>
            </div>

            {asset.issuer && (
                <div className="mt-3 flex items-center gap-1.5 rounded-xl bg-secondary/30 px-3 py-2">
                    <span className="text-[10px] font-medium text-muted-foreground">
                        Issuer:
                    </span>
                    <span className="font-mono text-[10px] text-foreground/70">
                        {truncateIssuer(asset.issuer)}
                    </span>
                    <a
                        href={`https://stellar.expert/explorer/public/asset/${asset.code}-${asset.issuer}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="ml-auto text-muted-foreground transition-colors hover:text-foreground"
                        title="View on Stellar Expert"
                    >
                        <ExternalLink className="h-3 w-3" />
                    </a>
                </div>
            )}

            {pct !== null && (
                <div className="mt-3">
                    <div className="mb-1 flex justify-between text-[10px] text-muted-foreground">
                        <span>Trustline usage</span>
                        <span>{pct.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-secondary">
                        <div
                            className="h-full rounded-full bg-foreground/60 transition-all"
                            style={{ width: `${pct}%` }}
                        />
                    </div>
                </div>
            )}
        </motion.div>
    );
}

function EmptyState({ searched }: { searched: boolean }) {
    return (
        <div className="col-span-full flex flex-col items-center justify-center py-16 text-center">
            <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-secondary">
                <Coins className="h-6 w-6 text-muted-foreground" />
            </div>
            <p className="text-sm font-medium text-foreground/80">
                {searched ? "No assets found" : "No assets to display"}
            </p>
            <p className="mt-1 max-w-xs text-xs leading-relaxed text-muted-foreground">
                {searched
                    ? "This account holds no assets on the selected network."
                    : "Connect your wallet or search a Stellar address to track assets."}
            </p>
        </div>
    );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function PortfolioPage() {
    const { isConnected, address } = useWallet();
    const router = useRouter();

    const [searchInput, setSearchInput] = useState("");
    const [activeAddress, setActiveAddress] = useState<string | null>(null);
    const [assets, setAssets] = useState<AssetRow[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    // Auto-load connected wallet on mount
    useEffect(() => {
        if (address && !activeAddress) {
            loadAssets(address);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [address]);

    const loadAssets = useCallback(async (addr: string) => {
        setError(null);
        setLoading(true);
        setActiveAddress(addr);
        try {
            const rows = await fetchAccountAssets(addr);
            // Sort: native first, then by balance desc
            rows.sort((a, b) => {
                if (a.type === "native") return -1;
                if (b.type === "native") return 1;
                return b.balance - a.balance;
            });
            setAssets(rows);
            setLastRefreshed(new Date());
        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to fetch assets.");
            setAssets([]);
        } finally {
            setLoading(false);
        }
    }, []);

    const handleSearch = () => {
        const trimmed = searchInput.trim();
        if (!trimmed) return;
        if (!isValidStellarAddress(trimmed)) {
            setError("Invalid Stellar address. Must start with G and be 56 characters.");
            return;
        }
        loadAssets(trimmed);
    };

    const handleUseConnected = () => {
        if (address) {
            setSearchInput("");
            loadAssets(address);
        }
    };

    const totalTokens = assets.length;
    const hasNonNative = assets.some((a) => a.type === "token");

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-background">
            <Navbar />

            <main className="mx-auto max-w-384 px-4 pb-24 pt-32 md:px-8 md:pb-16 md:pt-36 lg:px-12 xl:px-16">
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, x: -20 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.5 }}
                    className="mb-8"
                >
                    <h1 className="font-heading text-xl font-light text-foreground sm:text-2xl md:text-3xl">
                        Portfolio Tracker
                    </h1>
                    <p className="mt-1 text-xs text-muted-foreground sm:text-sm">
                        Track every asset in any Stellar wallet
                    </p>
                </motion.div>

                {/* Search bar */}
                <motion.div
                    initial={{ opacity: 0, y: 16 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4, delay: 0.1 }}
                    className="mb-6 rounded-2xl border border-border bg-white p-4 sm:p-5"
                >
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                        <div className="relative flex-1">
                            <Search className="absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                            <input
                                type="text"
                                value={searchInput}
                                onChange={(e) => setSearchInput(e.target.value)}
                                onKeyDown={(e) => e.key === "Enter" && handleSearch()}
                                placeholder="Search any Stellar address (G…)"
                                className="h-11 w-full rounded-xl border border-border bg-secondary/20 pl-10 pr-4 text-sm text-foreground placeholder:text-muted-foreground outline-none transition-colors focus:border-black/20 focus:bg-white"
                            />
                        </div>
                        <div className="flex gap-2">
                            <button
                                onClick={handleSearch}
                                disabled={!searchInput.trim() || loading}
                                className="flex h-11 items-center gap-2 rounded-xl bg-foreground px-5 text-sm font-medium text-background transition-opacity disabled:opacity-40"
                            >
                                <Search className="h-4 w-4" />
                                Search
                            </button>
                            {address && (
                                <button
                                    onClick={handleUseConnected}
                                    disabled={loading || activeAddress === address}
                                    className={cn(
                                        "flex h-11 items-center gap-2 rounded-xl border border-border px-4 text-sm font-medium text-foreground transition-all hover:border-black/20",
                                        activeAddress === address &&
                                            "border-black/20 bg-secondary/40"
                                    )}
                                >
                                    <Wallet className="h-4 w-4" />
                                    My Wallet
                                </button>
                            )}
                        </div>
                    </div>

                    {activeAddress && (
                        <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                            <p className="font-mono text-[11px] text-muted-foreground">
                                Viewing:{" "}
                                <span className="text-foreground/60">
                                    {truncateIssuer(activeAddress, 12)}
                                </span>
                                {activeAddress === address && (
                                    <span className="ml-2 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
                                        Connected
                                    </span>
                                )}
                            </p>
                            <button
                                onClick={() => loadAssets(activeAddress)}
                                disabled={loading}
                                className="flex items-center gap-1.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
                            >
                                <RefreshCw
                                    className={cn("h-3 w-3", loading && "animate-spin")}
                                />
                                {lastRefreshed
                                    ? `Updated ${lastRefreshed.toLocaleTimeString()}`
                                    : "Refresh"}
                            </button>
                        </div>
                    )}
                </motion.div>

                {/* Error banner */}
                {error && (
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        className="mb-6 flex items-center gap-3 rounded-2xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700"
                    >
                        <AlertCircle className="h-4 w-4 shrink-0" />
                        {error}
                    </motion.div>
                )}

                {/* Stats row */}
                {assets.length > 0 && (
                    <motion.div
                        initial={{ opacity: 0, y: 12 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.15 }}
                        className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4"
                    >
                        {[
                            {
                                label: "Total Assets",
                                value: String(totalTokens),
                                icon: Coins,
                            },
                            {
                                label: "XLM Balance",
                                value: `${(assets.find((a) => a.code === "XLM")?.balance ?? 0).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} XLM`,
                                icon: TrendingUp,
                            },
                            {
                                label: "Custom Tokens",
                                value: hasNonNative
                                    ? String(assets.filter((a) => a.type === "token").length)
                                    : "0",
                                icon: Wallet,
                            },
                        ].map((stat) => (
                            <div
                                key={stat.label}
                                className="rounded-2xl border border-border bg-white p-4 sm:p-5"
                            >
                                <div className="mb-3 flex h-8 w-8 items-center justify-center rounded-xl bg-secondary">
                                    <stat.icon className="h-3.5 w-3.5 text-foreground/50" />
                                </div>
                                <p className="font-heading text-xl font-light text-foreground sm:text-2xl">
                                    {stat.value}
                                </p>
                                <p className="mt-1 text-[10px] text-muted-foreground sm:text-xs">
                                    {stat.label}
                                </p>
                            </div>
                        ))}
                    </motion.div>
                )}

                {/* Loading skeleton */}
                {loading && (
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 lg:grid-cols-3">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <div
                                key={i}
                                className="h-28 animate-pulse rounded-2xl border border-border bg-secondary/30"
                            />
                        ))}
                    </div>
                )}

                {/* Asset grid */}
                {!loading && (
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 lg:grid-cols-3">
                        {assets.length === 0 ? (
                            <EmptyState searched={!!activeAddress} />
                        ) : (
                            assets.map((asset, i) => (
                                <AssetCard key={`${asset.code}-${asset.issuer ?? "native"}`} asset={asset} index={i} />
                            ))
                        )}
                    </div>
                )}
                {/* Recent Activity (Transaction History) */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5, delay: 0.2 }}
                    className="mt-10 rounded-2xl border border-border bg-white p-5 sm:p-6"
                >
                    <h2 className="mb-4 font-heading text-base font-light text-foreground sm:text-lg">
                        Recent Activity
                    </h2>
                    <TransactionHistory />
                </motion.div>
            </main>
        </div>
    );
}

import { usePortfolio } from "@/components/portfolio-provider";
import { ArrowUpRight, ArrowDownLeft, RefreshCcw, LineChart, ExternalLink } from "lucide-react";

const TYPE_ICONS = {
    Deposit: ArrowDownLeft,
    Withdrawal: ArrowUpRight,
    "Yield Accrual": LineChart,
    Rebalance: RefreshCcw,
};

const STATUS_COLORS = {
    Confirmed: "text-emerald-600 bg-emerald-50 border-emerald-100",
    Pending: "text-amber-600 bg-amber-50 border-amber-100",
    Failed: "text-rose-600 bg-rose-50 border-rose-100",
};

function TransactionHistory() {
    const { transactions } = usePortfolio();
    const recent = transactions.slice(0, 10);
    if (recent.length === 0) {
        return (
            <div className="flex items-center justify-center py-8 sm:py-10">
                <p className="text-sm text-muted-foreground">No recent activity</p>
            </div>
        );
    }
    return (
        <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse min-w-200">
                <thead>
                    <tr className="text-xs text-muted-foreground border-b border-border">
                        <th className="py-2 pr-2 font-medium">Type</th>
                        <th className="py-2 pr-2 font-medium">Amount</th>
                        <th className="py-2 pr-2 font-medium">Asset</th>
                        <th className="py-2 pr-2 font-medium">Vault</th>
                        <th className="py-2 pr-2 font-medium">Status</th>
                        <th className="py-2 pr-2 font-medium">Time</th>
                        <th className="py-2 pr-2 font-medium">Tx</th>
                    </tr>
                </thead>
                <tbody>
                    {recent.map((tx) => {
                        const Icon = TYPE_ICONS[tx.type] || ArrowDownLeft;
                        return (
                            <tr key={tx.id} className="border-b border-border last:border-0 text-sm">
                                <td className="py-2 pr-2">
                                    <span className="inline-flex items-center gap-1">
                                        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
                                        {tx.type}
                                    </span>
                                </td>
                                <td className="py-2 pr-2 font-mono">{tx.amount}</td>
                                <td className="py-2 pr-2">{tx.asset}</td>
                                <td className="py-2 pr-2">{tx.vaultName}</td>
                                <td className="py-2 pr-2">
                                    <span className={`inline-block rounded-full border px-2 py-0.5 text-xs font-medium ${STATUS_COLORS[tx.status]}`}>{tx.status}</span>
                                </td>
                                <td className="py-2 pr-2">
                                    {new Date(tx.timestamp).toLocaleString()}
                                </td>
                                <td className="py-2 pr-2">
                                    <a
                                        href={`https://stellar.expert/explorer/public/tx/${tx.txHash}`}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                                    >
                                        <ExternalLink className="h-3 w-3" />
                                        View
                                    </a>
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        </div>
    );
}
