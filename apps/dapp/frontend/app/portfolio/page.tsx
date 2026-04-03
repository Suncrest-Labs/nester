"use client";

import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState, useRef } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    Search,
    RefreshCw,
    Wallet,
    TrendingUp,
    Coins,
    AlertCircle,
    ChevronDown,
    ChevronUp,
    ExternalLink,
    ArrowUpRight,
    ArrowDownLeft,
    RefreshCcw,
    LineChart,
    Eye,
    EyeOff,
    Copy,
    Check,
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

function truncateAddress(addr: string, chars = 6): string {
    if (addr.length <= chars * 2 + 3) return addr;
    return `${addr.slice(0, chars)}…${addr.slice(-chars)}`;
}

function truncateIssuer(issuer: string, chars = 8): string {
    if (issuer.length <= chars * 2 + 3) return issuer;
    return `${issuer.slice(0, chars)}…${issuer.slice(-chars)}`;
}

// ── Asset Table Row ───────────────────────────────────────────────────────────

function AssetTableRow({ asset, index }: { asset: AssetRow; index: number }) {
    const pct =
        asset.limit && asset.limit > 0
            ? Math.min((asset.balance / asset.limit) * 100, 100)
            : null;

    return (
        <motion.tr
            initial={{ opacity: 0, x: -8 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.25, delay: index * 0.04 }}
            className="group border-b border-black/6 last:border-0 hover:bg-black/[0.02] transition-colors"
        >
            <td className="py-3 pr-4 pl-1">
                <div className="flex items-center gap-3">
                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-black/6 text-xs font-bold text-black/60 shrink-0">
                        {asset.code.slice(0, 2)}
                    </div>
                    <div>
                        <p className="text-sm font-medium text-black leading-tight">
                            {asset.code}
                        </p>
                        <p className="text-[10px] text-black/40 mt-0.5">
                            {asset.type === "native" ? "Stellar Native" : "Custom Token"}
                        </p>
                    </div>
                </div>
            </td>
            <td className="py-3 pr-4">
                <p className="font-mono text-sm text-black/80">
                    {asset.balance.toLocaleString("en-US", {
                        minimumFractionDigits: 2,
                        maximumFractionDigits: 4,
                    })}
                </p>
                {pct !== null && (
                    <div className="mt-1 flex items-center gap-2">
                        <div className="h-1 w-16 overflow-hidden rounded-full bg-black/8">
                            <div
                                className="h-full rounded-full bg-black/40 transition-all"
                                style={{ width: `${pct}%` }}
                            />
                        </div>
                        <span className="text-[10px] text-black/40">{pct.toFixed(0)}%</span>
                    </div>
                )}
            </td>
            <td className="py-3 pr-4 hidden sm:table-cell">
                {asset.issuer ? (
                    <div className="flex items-center gap-1.5">
                        <span className="font-mono text-xs text-black/40">
                            {truncateIssuer(asset.issuer, 6)}
                        </span>
                        <a
                            href={`https://stellar.expert/explorer/public/asset/${asset.code}-${asset.issuer}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-black/30 hover:text-black/70 transition-colors"
                        >
                            <ExternalLink className="h-3 w-3" />
                        </a>
                    </div>
                ) : (
                    <span className="text-xs text-black/30">—</span>
                )}
            </td>
            <td className="py-3 pr-1 text-right">
                {asset.limit !== null ? (
                    <span className="text-xs text-black/40">
                        /{asset.limit.toLocaleString()}
                    </span>
                ) : (
                    <span className="text-xs text-black/30">—</span>
                )}
            </td>
        </motion.tr>
    );
}

// ── Collapsible Section ───────────────────────────────────────────────────────

function CollapsibleSection({
    title,
    value,
    count,
    children,
    defaultOpen = true,
    icon,
}: {
    title: string;
    value?: string;
    count?: number;
    children: React.ReactNode;
    defaultOpen?: boolean;
    icon?: React.ReactNode;
}) {
    const [open, setOpen] = useState(defaultOpen);

    return (
        <div className="border border-black/8 rounded-2xl overflow-hidden">
            <button
                onClick={() => setOpen(!open)}
                className="w-full flex items-center justify-between px-5 py-4 hover:bg-black/[0.02] transition-colors"
            >
                <div className="flex items-center gap-3">
                    {icon && (
                        <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/6">
                            {icon}
                        </div>
                    )}
                    <span className="text-sm font-medium text-black">{title}</span>
                    {count !== undefined && (
                        <span className="text-xs text-black/40 bg-black/5 rounded-full px-2 py-0.5">
                            {count}
                        </span>
                    )}
                </div>
                <div className="flex items-center gap-3">
                    {value && (
                        <span className="font-mono text-sm font-medium text-black">{value}</span>
                    )}
                    {open ? (
                        <ChevronUp className="h-4 w-4 text-black/40" />
                    ) : (
                        <ChevronDown className="h-4 w-4 text-black/40" />
                    )}
                </div>
            </button>
            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: "easeInOut" }}
                        className="overflow-hidden"
                    >
                        <div className="border-t border-black/6 px-5 pb-4 pt-1">
                            {children}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// ── Transaction History ───────────────────────────────────────────────────────

const TYPE_ICONS = {
    Deposit: ArrowDownLeft,
    Withdrawal: ArrowUpRight,
    "Yield Accrual": LineChart,
    Rebalance: RefreshCcw,
};

const TYPE_LABELS: Record<string, string> = {
    Deposit: "Deposit",
    Withdrawal: "Withdraw",
    "Yield Accrual": "Yield",
    Rebalance: "Rebalance",
};

const STATUS_STYLES: Record<string, string> = {
    Confirmed: "text-black bg-black/6 border border-black/10",
    Pending: "text-black/60 bg-black/4 border border-black/8",
    Failed: "text-black/40 bg-black/4 border border-black/8 line-through",
};

function ActivityTab({ transactions }: { transactions: ReturnType<typeof usePortfolio>["transactions"] }) {
    const recent = transactions.slice(0, 20);

    if (recent.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-20 text-center">
                <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5">
                    <LineChart className="h-5 w-5 text-black/30" />
                </div>
                <p className="text-sm font-medium text-black/50">No activity yet</p>
                <p className="mt-1 text-xs text-black/30">
                    Deposits, withdrawals, and yield events will appear here.
                </p>
            </div>
        );
    }

    return (
        <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse min-w-[520px]">
                <thead>
                    <tr className="text-[11px] uppercase tracking-wide text-black/40 border-b border-black/8">
                        <th className="pb-3 pr-4 font-medium">Type</th>
                        <th className="pb-3 pr-4 font-medium">Amount</th>
                        <th className="pb-3 pr-4 font-medium">Asset</th>
                        <th className="pb-3 pr-4 font-medium hidden sm:table-cell">Vault</th>
                        <th className="pb-3 pr-4 font-medium">Status</th>
                        <th className="pb-3 pr-4 font-medium hidden md:table-cell">Time</th>
                        <th className="pb-3 font-medium text-right">Tx</th>
                    </tr>
                </thead>
                <tbody>
                    {recent.map((tx, i) => {
                        const Icon = TYPE_ICONS[tx.type as keyof typeof TYPE_ICONS] || ArrowDownLeft;
                        const isPositive = tx.type === "Deposit" || tx.type === "Yield Accrual";
                        return (
                            <motion.tr
                                key={tx.id}
                                initial={{ opacity: 0, y: 4 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ delay: i * 0.03 }}
                                className="border-b border-black/5 last:border-0 hover:bg-black/[0.015] transition-colors text-sm"
                            >
                                <td className="py-3 pr-4">
                                    <div className="flex items-center gap-2">
                                        <div className="flex h-6 w-6 items-center justify-center rounded-lg bg-black/5 shrink-0">
                                            <Icon className="h-3 w-3 text-black/50" />
                                        </div>
                                        <span className="text-black/70 text-xs font-medium">
                                            {TYPE_LABELS[tx.type] || tx.type}
                                        </span>
                                    </div>
                                </td>
                                <td className="py-3 pr-4">
                                    <span className={cn(
                                        "font-mono text-sm font-medium",
                                        isPositive ? "text-black" : "text-black/60"
                                    )}>
                                        {tx.amount}
                                    </span>
                                </td>
                                <td className="py-3 pr-4">
                                    <span className="text-xs font-medium text-black/60 bg-black/5 rounded-md px-2 py-1">
                                        {tx.asset}
                                    </span>
                                </td>
                                <td className="py-3 pr-4 hidden sm:table-cell">
                                    <span className="text-xs text-black/50 truncate max-w-[120px] block">
                                        {tx.vaultName}
                                    </span>
                                </td>
                                <td className="py-3 pr-4">
                                    <span className={cn(
                                        "inline-block rounded-full px-2 py-0.5 text-[10px] font-medium",
                                        STATUS_STYLES[tx.status]
                                    )}>
                                        {tx.status}
                                    </span>
                                </td>
                                <td className="py-3 pr-4 hidden md:table-cell">
                                    <span className="text-xs text-black/40">
                                        {new Date(tx.timestamp).toLocaleString("en-US", {
                                            month: "short",
                                            day: "numeric",
                                            hour: "2-digit",
                                            minute: "2-digit",
                                        })}
                                    </span>
                                </td>
                                <td className="py-3 text-right">
                                    <a
                                        href={`https://stellar.expert/explorer/public/tx/${tx.txHash}`}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="inline-flex items-center gap-1 text-[11px] text-black/30 hover:text-black/70 transition-colors"
                                    >
                                        <ExternalLink className="h-3 w-3" />
                                    </a>
                                </td>
                            </motion.tr>
                        );
                    })}
                </tbody>
            </table>
        </div>
    );
}

// ── Vault Positions Tab ───────────────────────────────────────────────────────

function VaultPositionsSection({ positions }: { positions: ReturnType<typeof usePortfolio>["positions"] }) {
    if (positions.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-center">
                <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5">
                    <Coins className="h-5 w-5 text-black/30" />
                </div>
                <p className="text-sm font-medium text-black/50">No vault positions</p>
                <p className="mt-1 text-xs text-black/30">
                    Deposit into a vault to see your positions here.
                </p>
            </div>
        );
    }

    return (
        <div className="space-y-2">
            {positions.map((pos, i) => {
                const daysTotal = Math.round(
                    (new Date(pos.maturityAt).getTime() - new Date(pos.depositedAt).getTime()) /
                    (1000 * 60 * 60 * 24)
                );
                const daysElapsed = daysTotal - pos.daysRemaining;
                const maturityPct = daysTotal > 0 ? Math.min(100, (daysElapsed / daysTotal) * 100) : 100;

                return (
                    <motion.div
                        key={pos.id}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: i * 0.05 }}
                        className="rounded-xl border border-black/8 p-4 hover:border-black/15 transition-colors"
                    >
                        <div className="flex items-start justify-between gap-4">
                            <div className="flex-1 min-w-0">
                                <div className="flex items-center gap-2 flex-wrap">
                                    <span className="text-sm font-medium text-black">{pos.vaultName}</span>
                                    {pos.isMatured ? (
                                        <span className="text-[10px] font-medium bg-black text-white rounded-full px-2 py-0.5">
                                            Matured
                                        </span>
                                    ) : (
                                        <span className="text-[10px] font-medium bg-black/6 text-black/50 rounded-full px-2 py-0.5">
                                            {pos.daysRemaining}d left
                                        </span>
                                    )}
                                </div>
                                <div className="mt-1 flex items-center gap-3">
                                    <span className="text-xs text-black/40">{pos.asset}</span>
                                    <span className="text-xs text-black/40">
                                        APY {(pos.apy * 100).toFixed(1)}%
                                    </span>
                                </div>
                                <div className="mt-2.5">
                                    <div className="flex justify-between text-[10px] text-black/40 mb-1">
                                        <span>Maturity progress</span>
                                        <span>{maturityPct.toFixed(0)}%</span>
                                    </div>
                                    <div className="h-1 w-full overflow-hidden rounded-full bg-black/8">
                                        <div
                                            className="h-full rounded-full bg-black/50 transition-all"
                                            style={{ width: `${maturityPct}%` }}
                                        />
                                    </div>
                                </div>
                            </div>
                            <div className="text-right shrink-0">
                                <p className="font-mono text-base font-semibold text-black">
                                    {pos.currentValue.toLocaleString("en-US", {
                                        minimumFractionDigits: 2,
                                        maximumFractionDigits: 2,
                                    })}
                                </p>
                                <p className="text-[11px] text-black/40 mt-0.5">
                                    Principal: {pos.principal.toFixed(2)}
                                </p>
                                <p className="text-[11px] text-black/60 font-medium mt-0.5">
                                    +{pos.yieldEarned.toFixed(4)} yield
                                </p>
                            </div>
                        </div>
                    </motion.div>
                );
            })}
        </div>
    );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function PortfolioPage() {
    const { isConnected, address } = useWallet();
    const { transactions, positions, balances } = usePortfolio();
    const router = useRouter();

    const [searchInput, setSearchInput] = useState("");
    const [activeAddress, setActiveAddress] = useState<string | null>(null);
    const [assets, setAssets] = useState<AssetRow[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);
    const [activeTab, setActiveTab] = useState<"positions" | "activity">("positions");
    const [hideBalances, setHideBalances] = useState(false);
    const [copied, setCopied] = useState(false);
    const [searchOpen, setSearchOpen] = useState(false);
    const searchRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

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
        setSearchOpen(false);
    };

    const handleCopyAddress = () => {
        if (activeAddress) {
            navigator.clipboard.writeText(activeAddress);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        }
    };

    const xlmBalance = assets.find((a) => a.code === "XLM")?.balance ?? 0;
    const totalAssets = assets.length;
    const customTokens = assets.filter((a) => a.type === "token").length;
    const vaultTotal = positions.reduce((sum, p) => sum + p.currentValue, 0);
    const totalYield = positions.reduce((sum, p) => sum + p.yieldEarned, 0);

    const displayValue = (val: number | string) =>
        hideBalances ? "••••••" : typeof val === "number" ? val.toFixed(2) : val;

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-7xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">

                {/* ── Address Bar ───────────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-6 flex items-center justify-between gap-4"
                >
                    <div className="flex items-center gap-3">
                        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-black text-white text-xs font-bold shrink-0">
                            {address ? address.slice(1, 3).toUpperCase() : "??"}
                        </div>
                        <div className="flex items-center gap-2">
                            <span className="font-mono text-sm text-black/70">
                                {activeAddress
                                    ? truncateAddress(activeAddress, 8)
                                    : "No address"}
                            </span>
                            {activeAddress === address && (
                                <span className="text-[10px] font-medium bg-black text-white rounded-full px-2 py-0.5">
                                    Connected
                                </span>
                            )}
                            {activeAddress && (
                                <button
                                    onClick={handleCopyAddress}
                                    className="text-black/30 hover:text-black/70 transition-colors"
                                    title="Copy address"
                                >
                                    {copied ? (
                                        <Check className="h-3.5 w-3.5" />
                                    ) : (
                                        <Copy className="h-3.5 w-3.5" />
                                    )}
                                </button>
                            )}
                        </div>
                    </div>

                    <div className="flex items-center gap-2">
                        <button
                            onClick={() => setHideBalances(!hideBalances)}
                            className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/10 text-black/40 hover:text-black/70 hover:border-black/20 transition-all"
                            title={hideBalances ? "Show balances" : "Hide balances"}
                        >
                            {hideBalances ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        </button>
                        <button
                            onClick={() => setSearchOpen(!searchOpen)}
                            className="flex h-8 items-center gap-2 rounded-lg border border-black/10 px-3 text-xs font-medium text-black/60 hover:border-black/20 hover:text-black transition-all"
                        >
                            <Search className="h-3.5 w-3.5" />
                            <span className="hidden sm:inline">Search address</span>
                        </button>
                        {activeAddress && (
                            <button
                                onClick={() => loadAssets(activeAddress)}
                                disabled={loading}
                                className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/10 text-black/40 hover:text-black/70 hover:border-black/20 transition-all disabled:opacity-40"
                                title="Refresh"
                            >
                                <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} />
                            </button>
                        )}
                    </div>
                </motion.div>

                {/* ── Search Dropdown ───────────────────────────────────────── */}
                <AnimatePresence>
                    {searchOpen && (
                        <motion.div
                            initial={{ opacity: 0, height: 0 }}
                            animate={{ opacity: 1, height: "auto" }}
                            exit={{ opacity: 0, height: 0 }}
                            className="mb-6 overflow-hidden"
                        >
                            <div className="rounded-2xl border border-black/10 bg-white p-4 shadow-sm">
                                <div className="flex flex-col gap-2 sm:flex-row">
                                    <div className="relative flex-1">
                                        <Search className="absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-black/30" />
                                        <input
                                            ref={searchRef}
                                            type="text"
                                            value={searchInput}
                                            onChange={(e) => setSearchInput(e.target.value)}
                                            onKeyDown={(e) => e.key === "Enter" && handleSearch()}
                                            placeholder="Enter a Stellar address (G…)"
                                            className="h-10 w-full rounded-xl border border-black/10 bg-black/[0.02] pl-10 pr-4 text-sm text-black placeholder:text-black/30 outline-none transition-colors focus:border-black/25 focus:bg-white"
                                            autoFocus
                                        />
                                    </div>
                                    <div className="flex gap-2">
                                        <button
                                            onClick={handleSearch}
                                            disabled={!searchInput.trim() || loading}
                                            className="flex h-10 flex-1 sm:flex-none items-center justify-center gap-1.5 rounded-xl bg-black px-4 text-sm font-medium text-white transition-opacity disabled:opacity-40"
                                        >
                                            Search
                                        </button>
                                        {address && (
                                            <button
                                                onClick={() => {
                                                    setSearchInput("");
                                                    loadAssets(address);
                                                    setSearchOpen(false);
                                                }}
                                                disabled={loading}
                                                className="flex h-10 flex-1 sm:flex-none items-center justify-center gap-1.5 rounded-xl border border-black/10 px-4 text-sm font-medium text-black/70 hover:border-black/20 transition-all"
                                            >
                                                <Wallet className="h-3.5 w-3.5" />
                                                My Wallet
                                            </button>
                                        )}
                                    </div>
                                </div>
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>

                {/* ── Error ─────────────────────────────────────────────────── */}
                <AnimatePresence>
                    {error && (
                        <motion.div
                            initial={{ opacity: 0, y: -4 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0 }}
                            className="mb-5 flex items-center gap-3 rounded-2xl border border-black/10 bg-black/[0.03] px-4 py-3 text-sm text-black/70"
                        >
                            <AlertCircle className="h-4 w-4 shrink-0 text-black/40" />
                            {error}
                            <button
                                onClick={() => setError(null)}
                                className="ml-auto text-black/30 hover:text-black/60 transition-colors text-xs"
                            >
                                Dismiss
                            </button>
                        </motion.div>
                    )}
                </AnimatePresence>

                {/* ── Hero: Net Worth ────────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: 16 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.45 }}
                    className="mb-5 rounded-2xl border border-black/8 bg-white p-6 sm:p-7"
                >
                    <div className="flex flex-col gap-6 lg:flex-row lg:items-stretch lg:gap-0 lg:divide-x lg:divide-black/6">
                        {/* Left: Net Worth */}
                        <div className="flex-1 lg:pr-8">
                            <p className="text-xs font-medium uppercase tracking-widest text-black/40 mb-3">
                                Portfolio Overview
                            </p>
                            <div className="flex items-baseline gap-3 flex-wrap">
                                <span className="font-heading text-4xl font-light text-black sm:text-5xl">
                                    {hideBalances ? "••••••" : xlmBalance.toLocaleString("en-US", {
                                        minimumFractionDigits: 2,
                                        maximumFractionDigits: 2,
                                    })}
                                </span>
                                <span className="text-lg text-black/40 font-light">XLM</span>
                            </div>
                            {lastRefreshed && (
                                <p className="mt-2 text-xs text-black/30">
                                    Updated {lastRefreshed.toLocaleTimeString()}
                                </p>
                            )}

                            {/* Sub-metrics */}
                            <div className="mt-5 grid grid-cols-3 gap-4">
                                <div>
                                    <p className="text-[10px] uppercase tracking-widest text-black/30 mb-1">
                                        Wallet Assets
                                    </p>
                                    <p className="font-mono text-base font-medium text-black">
                                        {hideBalances ? "••" : totalAssets}
                                    </p>
                                </div>
                                <div>
                                    <p className="text-[10px] uppercase tracking-widest text-black/30 mb-1">
                                        Custom Tokens
                                    </p>
                                    <p className="font-mono text-base font-medium text-black">
                                        {hideBalances ? "••" : customTokens}
                                    </p>
                                </div>
                                <div>
                                    <p className="text-[10px] uppercase tracking-widest text-black/30 mb-1">
                                        Vault Positions
                                    </p>
                                    <p className="font-mono text-base font-medium text-black">
                                        {hideBalances ? "••" : positions.length}
                                    </p>
                                </div>
                            </div>
                        </div>

                        {/* Right: Vault + Yield Stats */}
                        <div className="flex-1 lg:pl-8">
                            <p className="text-xs font-medium uppercase tracking-widest text-black/40 mb-3">
                                Vault Performance
                            </p>
                            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                <div className="rounded-xl bg-black/[0.025] p-4">
                                    <div className="flex items-center gap-2 mb-2">
                                        <TrendingUp className="h-3.5 w-3.5 text-black/40" />
                                        <span className="text-[11px] text-black/40 uppercase tracking-wide">
                                            Total Deposited
                                        </span>
                                    </div>
                                    <p className="font-mono text-xl font-semibold text-black">
                                        {displayValue(vaultTotal)}
                                    </p>
                                    <p className="text-[10px] text-black/30 mt-0.5">USDC</p>
                                </div>
                                <div className="rounded-xl bg-black/[0.025] p-4">
                                    <div className="flex items-center gap-2 mb-2">
                                        <Coins className="h-3.5 w-3.5 text-black/40" />
                                        <span className="text-[11px] text-black/40 uppercase tracking-wide">
                                            Total Yield
                                        </span>
                                    </div>
                                    <p className="font-mono text-xl font-semibold text-black">
                                        +{displayValue(totalYield)}
                                    </p>
                                    <p className="text-[10px] text-black/30 mt-0.5">USDC earned</p>
                                </div>
                            </div>

                            {/* USDC balance from portfolio context */}
                            {balances.USDC !== undefined && (
                                <div className="mt-3 flex items-center justify-between rounded-xl border border-black/6 px-4 py-3">
                                    <div className="flex items-center gap-2">
                                        <div className="h-6 w-6 rounded-full bg-black/6 flex items-center justify-center text-[10px] font-bold text-black/50">
                                            $
                                        </div>
                                        <span className="text-sm text-black/60">USDC Available</span>
                                    </div>
                                    <span className="font-mono text-sm font-medium text-black">
                                        {displayValue(balances.USDC)}
                                    </span>
                                </div>
                            )}
                        </div>
                    </div>
                </motion.div>

                {/* ── Protocol Tabs Strip ────────────────────────────────────── */}
                {assets.length > 0 && (
                    <motion.div
                        initial={{ opacity: 0, x: -12 }}
                        animate={{ opacity: 1, x: 0 }}
                        transition={{ duration: 0.35, delay: 0.1 }}
                        className="mb-5 overflow-x-auto scrollbar-hide"
                    >
                        <div className="flex gap-2 pb-1 min-w-max">
                            <div className="flex items-center gap-2 rounded-xl border border-black/10 bg-black/[0.025] px-4 py-2">
                                <Wallet className="h-3.5 w-3.5 text-black/50" />
                                <span className="text-xs font-medium text-black/70">Wallet</span>
                                <span className="font-mono text-xs font-semibold text-black ml-1">
                                    {hideBalances ? "•••" : `${xlmBalance.toFixed(2)} XLM`}
                                </span>
                            </div>
                            {positions.length > 0 && (
                                <div className="flex items-center gap-2 rounded-xl border border-black/10 bg-black/[0.025] px-4 py-2">
                                    <TrendingUp className="h-3.5 w-3.5 text-black/50" />
                                    <span className="text-xs font-medium text-black/70">Vaults</span>
                                    <span className="font-mono text-xs font-semibold text-black ml-1">
                                        {hideBalances ? "•••" : `${vaultTotal.toFixed(2)} USDC`}
                                    </span>
                                </div>
                            )}
                            {customTokens > 0 && assets.filter(a => a.type === "token").map(tok => (
                                <div key={tok.code} className="flex items-center gap-2 rounded-xl border border-black/8 px-4 py-2">
                                    <div className="h-5 w-5 rounded-md bg-black/6 flex items-center justify-center text-[9px] font-bold text-black/50">
                                        {tok.code.slice(0, 2)}
                                    </div>
                                    <span className="text-xs font-medium text-black/70">{tok.code}</span>
                                    <span className="font-mono text-xs font-semibold text-black ml-1">
                                        {hideBalances ? "•••" : tok.balance.toFixed(2)}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </motion.div>
                )}

                {/* ── Main Tab Navigation ────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ delay: 0.15 }}
                    className="mb-5 flex items-center gap-1 border-b border-black/8"
                >
                    {(["positions", "activity"] as const).map((tab) => (
                        <button
                            key={tab}
                            onClick={() => setActiveTab(tab)}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm font-medium capitalize transition-colors",
                                activeTab === tab
                                    ? "text-black"
                                    : "text-black/40 hover:text-black/60"
                            )}
                        >
                            {tab}
                            {activeTab === tab && (
                                <motion.div
                                    layoutId="tab-indicator"
                                    className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                                />
                            )}
                            {tab === "activity" && transactions.length > 0 && (
                                <span className="ml-1.5 inline-block text-[10px] font-medium bg-black/8 text-black/50 rounded-full px-1.5 py-0.5">
                                    {transactions.length}
                                </span>
                            )}
                        </button>
                    ))}
                </motion.div>

                {/* ── Tab Content ────────────────────────────────────────────── */}
                <AnimatePresence mode="wait">
                    {activeTab === "positions" && (
                        <motion.div
                            key="positions"
                            initial={{ opacity: 0, y: 8 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -8 }}
                            transition={{ duration: 0.2 }}
                            className="space-y-3"
                        >
                            {/* Loading skeletons */}
                            {loading && (
                                <div className="space-y-3">
                                    {Array.from({ length: 4 }).map((_, i) => (
                                        <div
                                            key={i}
                                            className="h-16 animate-pulse rounded-2xl border border-black/6 bg-black/[0.02]"
                                        />
                                    ))}
                                </div>
                            )}

                            {/* Wallet Holdings */}
                            {!loading && assets.length > 0 && (
                                <CollapsibleSection
                                    title="Wallet Holdings"
                                    value={hideBalances ? "••••••" : `${xlmBalance.toFixed(2)} XLM`}
                                    count={totalAssets}
                                    icon={<Wallet className="h-3.5 w-3.5 text-black/50" />}
                                >
                                    <div className="mt-2">
                                        <table className="w-full">
                                            <thead>
                                                <tr className="text-[11px] uppercase tracking-wide text-black/35">
                                                    <th className="pb-2.5 pr-4 text-left font-medium">Asset</th>
                                                    <th className="pb-2.5 pr-4 text-left font-medium">Balance</th>
                                                    <th className="pb-2.5 pr-4 text-left font-medium hidden sm:table-cell">Issuer</th>
                                                    <th className="pb-2.5 text-right font-medium">Limit</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {assets.map((asset, i) => (
                                                    <AssetTableRow
                                                        key={`${asset.code}-${asset.issuer ?? "native"}`}
                                                        asset={asset}
                                                        index={i}
                                                    />
                                                ))}
                                            </tbody>
                                        </table>
                                    </div>
                                </CollapsibleSection>
                            )}

                            {/* Vault Positions */}
                            <CollapsibleSection
                                title="Vault Positions"
                                value={positions.length > 0
                                    ? (hideBalances ? "••••••" : `${vaultTotal.toFixed(2)} USDC`)
                                    : undefined
                                }
                                count={positions.length}
                                icon={<TrendingUp className="h-3.5 w-3.5 text-black/50" />}
                                defaultOpen={positions.length > 0}
                            >
                                <div className="mt-2">
                                    <VaultPositionsSection positions={positions} />
                                </div>
                            </CollapsibleSection>

                            {/* Empty state when no data at all */}
                            {!loading && assets.length === 0 && positions.length === 0 && (
                                <div className="flex flex-col items-center justify-center py-20 text-center">
                                    <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-black/5">
                                        <Wallet className="h-6 w-6 text-black/25" />
                                    </div>
                                    <p className="text-sm font-medium text-black/50">Nothing to show yet</p>
                                    <p className="mt-1 max-w-xs text-xs leading-relaxed text-black/30">
                                        Search a Stellar address or use the "My Wallet" button to track your assets.
                                    </p>
                                    <button
                                        onClick={() => setSearchOpen(true)}
                                        className="mt-5 flex items-center gap-2 rounded-xl bg-black px-5 py-2.5 text-sm font-medium text-white"
                                    >
                                        <Search className="h-4 w-4" />
                                        Search an address
                                    </button>
                                </div>
                            )}
                        </motion.div>
                    )}

                    {activeTab === "activity" && (
                        <motion.div
                            key="activity"
                            initial={{ opacity: 0, y: 8 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -8 }}
                            transition={{ duration: 0.2 }}
                            className="rounded-2xl border border-black/8 bg-white p-5 sm:p-6"
                        >
                            <ActivityTab transactions={transactions} />
                        </motion.div>
                    )}
                </AnimatePresence>
            </main>
        </div>
    );
}
