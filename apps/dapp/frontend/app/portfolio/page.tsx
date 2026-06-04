"use client";

import { useWallet } from "@/components/wallet-provider";
import { usePortfolio, type PortfolioPosition } from "@/components/portfolio-provider";
import { AppShell } from "@/components/app-shell";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    RefreshCw,
    Wallet as WalletIcon,
    TrendingUp,
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
import { TransferModal } from "@/components/vault-action-modals";
import { WithdrawModal } from "@/components/vault-action-modals";
import { useTokenPrices } from "@/hooks/useTokenPrices";
import { useNetwork } from "@/hooks/useNetwork";
import { YieldComparisonChart, type ProtocolApyPoint, type ProtocolSnapshot } from "@/components/analytics/YieldComparisonChart";

// ── Helpers ──────────────────────────────────────────────────────────────────

function getHorizonUrl(): string {
    if (typeof window !== "undefined") {
        const saved = localStorage.getItem("nester_network_id");
        if (saved === "mainnet") return NETWORKS.mainnet.horizonUrl;
        if (saved === "testnet") return NETWORKS.testnet.horizonUrl;
    }
    return DEFAULT_NETWORK.horizonUrl;
}

interface StellarBalance {
    asset_type: "native" | "credit_alphanum4" | "credit_alphanum12";
    asset_code?: string;
    balance: string;
}

interface WalletAsset {
    code: string;
    balance: number;
}

async function fetchWalletAssets(address: string): Promise<WalletAsset[]> {
    const res = await fetch(`${getHorizonUrl()}/accounts/${address}`);
    if (res.status === 404) return [];
    if (!res.ok) throw new Error(`Horizon error: ${res.status}`);
    const data = await res.json();
    return (data.balances ?? []).map((b: StellarBalance) => ({
        code: b.asset_type === "native" ? "XLM" : (b.asset_code ?? "?"),
        balance: parseFloat(b.balance),
    })).sort((a: WalletAsset, b: WalletAsset) => b.balance - a.balance);
}

function truncAddr(addr: string) {
    return `${addr.slice(0, 6)}…${addr.slice(-6)}`;
}

// ── Transaction type config ──────────────────────────────────────────────────

const TX_ICONS = {
    Deposit: ArrowDownLeft,
    Withdrawal: ArrowUpRight,
    "Yield Accrual": LineChart,
    Rebalance: RefreshCcw,
};

// ── Page ─────────────────────────────────────────────────────────────────────

export default function PortfolioPage() {
    const { isConnected, address } = useWallet();
    const { transactions, positions } = usePortfolio();
    const { prices: tokenPrices } = useTokenPrices();
    const { currentNetwork } = useNetwork();
    const router = useRouter();

    const [walletAssets, setWalletAssets] = useState<WalletAsset[]>([]);
    const [loading, setLoading] = useState(false);
    const [hideBalances, setHideBalances] = useState(false);
    const [copied, setCopied] = useState(false);
    const [activeTab, setActiveTab] = useState<"positions" | "activity" | "compare">("positions");
    const [withdrawPos, setWithdrawPos] = useState<PortfolioPosition | null>(null);
    const [transferPos, setTransferPos] = useState<PortfolioPosition | null>(null);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    const loadAssets = useCallback(async () => {
        if (!address) return;
        setLoading(true);
        try {
            setWalletAssets(await fetchWalletAssets(address));
        } catch { /* ignore */ }
        finally { setLoading(false); }
    }, [address]);

    useEffect(() => { loadAssets(); }, [loadAssets]);

    const copyAddress = () => {
        if (!address) return;
        navigator.clipboard.writeText(address);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    };

    // ── Yield comparison data derived from positions ──────────────────────────
    const { compareHistory, compareSnapshots } = useMemo((): {
        compareHistory: ProtocolApyPoint[];
        compareSnapshots: ProtocolSnapshot[];
    } => {
        const protocols = ["Blend", "Aave", "Compound", "Nester"];
        const totalVault = positions.reduce((s, p) => s + p.currentValue, 0);

        // Build 30 days of synthetic APY history seeded from position APYs
        const today = new Date();
        const nesterApy = positions.length
            ? positions.reduce((s, p) => s + (p.apy ?? 0), 0) / positions.length
            : 0.12;

        const baseApys: Record<string, number> = {
            Blend: 0.124,
            Aave: 0.098,
            Compound: 0.085,
            Nester: nesterApy,
        };

        const history: ProtocolApyPoint[] = Array.from({ length: 30 }, (_, i) => {
            const d = new Date(today);
            d.setDate(d.getDate() - (29 - i));
            const point: ProtocolApyPoint = { date: d.toISOString().slice(0, 10) };
            protocols.forEach((p) => {
                const jitter = (Math.sin(i * 0.4 + p.length) * 0.015);
                point[p] = parseFloat(((baseApys[p] + jitter) * 100).toFixed(2));
            });
            return point;
        });

        const snapshots: ProtocolSnapshot[] = protocols.map((protocol) => {
            const base = baseApys[protocol] * 100;
            const posAlloc = protocol === "Nester" && totalVault > 0
                ? 100
                : undefined;
            return {
                protocol,
                currentApy: parseFloat(base.toFixed(1)),
                avg30d: parseFloat((base * 0.97).toFixed(1)),
                trend7d: parseFloat(((Math.random() - 0.45) * 2).toFixed(1)),
                allocationPct: posAlloc,
            };
        });

        return { compareHistory: history, compareSnapshots: snapshots };
    }, [positions]);

    if (!isConnected) return null;

    const xlmBal = walletAssets.find(a => a.code === "XLM")?.balance ?? 0;
    const usdcBal = walletAssets.find(a => a.code === "USDC")?.balance ?? 0;
    const walletUsd = xlmBal * tokenPrices.XLM + usdcBal * tokenPrices.USDC;
    const vaultUsd = positions.reduce((s, p) => s + p.currentValue, 0);
    const totalYield = positions.reduce((s, p) => s + p.yieldEarned, 0);
    const totalUsd = walletUsd + vaultUsd;

    const hide = (v: string) => hideBalances ? "••••••" : v;
    const fmtUsd = (n: number) => `$${n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

    const recentTx = transactions.slice(0, 15);

    return (
        <AppShell>
            {/* Header row */}
            <motion.div
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-8 flex items-center justify-between gap-4"
            >
                <div>
                    <h1 className="text-2xl text-black sm:text-3xl font-semibold">Portfolio</h1>
                    <div className="mt-1.5 flex items-center gap-2">
                        <span className="text-sm text-black/60 font-medium">{address ? truncAddr(address) : ""}</span>
                        {address && (
                            <button
                                onClick={copyAddress}
                                className="text-black/40 hover:text-black/70 transition-colors focus-visible:ring-2 focus-visible:ring-black"
                                aria-label="Copy wallet address"
                            >
                                {copied ? <Check className="h-3.5 w-3.5" aria-hidden="true" /> : <Copy className="h-3.5 w-3.5" aria-hidden="true" />}
                            </button>
                        )}
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <button
                        onClick={() => setHideBalances(!hideBalances)}
                        className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/10 text-black/40 hover:text-black/70 transition-all focus-visible:ring-2 focus-visible:ring-black"
                        aria-label={hideBalances ? "Show balances" : "Hide balances"}
                        aria-pressed={hideBalances}
                    >
                        {hideBalances ? <EyeOff className="h-3.5 w-3.5" aria-hidden="true" /> : <Eye className="h-3.5 w-3.5" aria-hidden="true" />}
                    </button>
                    <button
                        onClick={loadAssets}
                        disabled={loading}
                        className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/10 text-black/40 hover:text-black/70 transition-all disabled:opacity-40 focus-visible:ring-2 focus-visible:ring-black"
                        aria-label="Refresh balances"
                    >
                        <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} aria-hidden="true" />
                    </button>
                </div>
            </motion.div>

            {/* Net worth card */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.05 }}
                className="mb-6 rounded-2xl border border-black/[0.06] bg-white overflow-hidden"
            >
                <div className="p-8">
                    <p className="text-[12px] text-black/60 font-semibold tracking-wide mb-2 uppercase">Net Worth</p>
                    <p className="text-[42px] font-extralight leading-none text-black tracking-[-0.02em]" aria-live="polite">
                        {hide(fmtUsd(totalUsd))}
                    </p>
                </div>
                <div className="border-t border-black/[0.06] grid grid-cols-2 sm:grid-cols-4 divide-x divide-black/[0.06]">
                    {[
                        { label: "Wallet", value: fmtUsd(walletUsd), icon: WalletIcon },
                        { label: "In Markets", value: fmtUsd(vaultUsd), icon: TrendingUp },
                        { label: "Total Yield", value: `+${fmtUsd(totalYield)}`, icon: LineChart },
                        { label: "Positions", value: String(positions.length), icon: TrendingUp },
                    ].map((item) => (
                        <div key={item.label} className="px-5 py-4">
                            <div className="flex items-center gap-1.5 mb-1.5">
                                <item.icon className="h-3 w-3 text-black/40" aria-hidden="true" />
                                <span className="text-[11px] text-black/60 font-medium">{item.label}</span>
                            </div>
                            <p className="text-sm text-black font-semibold">{hide(item.value)}</p>
                        </div>
                    ))}
                </div>
            </motion.div>

            {/* Wallet balances — compact row */}
            {walletAssets.length > 0 && (
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.1 }}
                    className="mb-6 flex items-center gap-2 overflow-x-auto scrollbar-hide pb-1"
                    aria-label="Wallet asset balances"
                >
                    {walletAssets.filter(a => a.balance > 0).map((asset) => (
                        <div
                            key={asset.code}
                            className="flex items-center gap-2 rounded-xl border border-black/8 bg-white px-4 py-2.5 shrink-0"
                        >
                            <div className="flex h-6 w-6 items-center justify-center rounded-md bg-black/[0.04] text-[10px] text-black/60 font-bold" aria-hidden="true">
                                {asset.code.slice(0, 2)}
                            </div>
                            <span className="text-xs text-black/70 font-semibold">{asset.code}</span>
                            <span className="text-sm text-black font-medium">
                                {hide(asset.balance.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 }))}
                            </span>
                        </div>
                    ))}
                </motion.div>
            )}

            {/* Tab navigation */}
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ delay: 0.15 }}
                className="mb-5 flex items-center gap-1 border-b border-black/8"
                role="tablist"
                aria-label="Portfolio sections"
            >
                {(["positions", "activity", "compare"] as const).map((tab) => (
                    <button
                        key={tab}
                        role="tab"
                        aria-selected={activeTab === tab}
                        aria-controls={`${tab}-panel`}
                        onClick={() => setActiveTab(tab)}
                        className={cn(
                            "relative pb-3 px-1 mr-4 text-sm capitalize transition-colors focus-visible:ring-2 focus-visible:ring-black",
                            activeTab === tab ? "text-black font-semibold" : "text-black/60 hover:text-black/80 font-medium"
                        )}
                    >
                        {tab}
                        {activeTab === tab && (
                            <motion.div layoutId="tab-indicator" className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full" aria-hidden="true" />
                        )}
                    </button>
                ))}
            </motion.div>

            {/* Tab content */}
            <AnimatePresence mode="wait">
                {activeTab === "positions" && (
                    <motion.div
                        key="positions"
                        id="positions-panel"
                        role="tabpanel"
                        aria-labelledby="positions-tab"
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -8 }}
                    >
                        {positions.length === 0 ? (
                            <div className="flex flex-col items-center justify-center py-20 text-center rounded-2xl border border-black/8 bg-white">
                                <p className="text-sm text-black/60 font-medium">No positions yet</p>
                                <p className="mt-1 text-xs text-black/50">
                                    Supply assets to a market to see your positions here.
                                </p>
                            </div>
                        ) : (
                            <div className="space-y-2">
                                {positions.map((pos, i) => (
                                    <motion.div
                                        key={pos.id}
                                        initial={{ opacity: 0, y: 6 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        transition={{ delay: i * 0.04 }}
                                        className="flex items-center justify-between gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4"
                                    >
                                        <div className="min-w-0">
                                            <div className="flex items-center gap-2">
                                                <p className="text-sm text-black font-semibold truncate">{pos.vaultName}</p>
                                                <span className="text-[11px] text-black/60 font-medium">{pos.asset}</span>
                                                {pos.isMatured ? (
                                                    <span className="text-[10px] bg-black text-white rounded-full px-2 py-0.5 font-bold">Matured</span>
                                                ) : (
                                                    <span className="text-[10px] bg-black/[0.04] text-black/70 rounded-full px-2 py-0.5 font-semibold">{pos.daysRemaining}d left</span>
                                                )}
                                            </div>
                                            <div className="mt-1 flex items-center gap-3 text-xs text-black/60 font-medium">
                                                <span>APY {(pos.apy * 100).toFixed(1)}%</span>
                                                <span className="text-emerald-700">Yield +{pos.yieldEarned.toFixed(4)}</span>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-3 shrink-0">
                                            <div className="text-right">
                                                <p className="text-base text-black font-bold">
                                                    {hide(pos.currentValue.toFixed(2))}
                                                </p>
                                                <p className="text-[11px] text-black/60 mt-0.5 font-medium">
                                                    Principal: {pos.principal.toFixed(2)}
                                                </p>
                                            </div>
                                            <div className="flex gap-1.5">
                                                <button
                                                    onClick={() => setTransferPos(pos)}
                                                    className="rounded-lg border border-black/10 px-3 py-1.5 text-[11px] text-black/60 font-semibold hover:border-black/20 hover:text-black transition-colors focus-visible:ring-2 focus-visible:ring-black"
                                                    aria-label={`Transfer ${pos.vaultName} position`}
                                                >
                                                    Transfer
                                                </button>
                                                <button
                                                    onClick={() => setWithdrawPos(pos)}
                                                    className="rounded-lg bg-black px-3 py-1.5 text-[11px] text-white font-semibold transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                                                    aria-label={`Withdraw from ${pos.vaultName}`}
                                                >
                                                    Withdraw
                                                </button>
                                            </div>
                                        </div>
                                    </motion.div>
                                ))}
                            </div>
                        )}
                    </motion.div>
                )}

                {activeTab === "activity" && (
                    <motion.div
                        key="activity"
                        id="activity-panel"
                        role="tabpanel"
                        aria-labelledby="activity-tab"
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -8 }}
                    >
                        {recentTx.length === 0 ? (
                            <div className="flex flex-col items-center justify-center py-20 text-center rounded-2xl border border-black/8 bg-white">
                                <p className="text-sm text-black/60 font-medium">No activity yet</p>
                                <p className="mt-1 text-xs text-black/50">
                                    Deposits, withdrawals, and yield events will appear here.
                                </p>
                            </div>
                        ) : (
                            <div className="space-y-1.5">
                                {recentTx.map((tx, i) => {
                                    const Icon = TX_ICONS[tx.type as keyof typeof TX_ICONS] || ArrowDownLeft;
                                    return (
                                        <motion.div
                                            key={tx.id}
                                            initial={{ opacity: 0, y: 4 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            transition={{ delay: i * 0.03 }}
                                            className="flex items-center justify-between gap-4 rounded-xl border border-black/8 bg-white px-5 py-3.5"
                                        >
                                            <div className="flex items-center gap-3">
                                                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/[0.04] text-black/60">
                                                    <Icon className="h-3.5 w-3.5" aria-hidden="true" />
                                                </div>
                                                <div>
                                                    <p className="text-sm text-black font-semibold">{tx.type}</p>
                                                    <p className="text-[11px] text-black/60 mt-0.5 font-medium">
                                                        {tx.vaultName} · {new Date(tx.timestamp).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
                                                    </p>
                                                </div>
                                            </div>
                                            <div className="flex items-center gap-3">
                                                <div className="text-right">
                                                    <p className="text-sm text-black font-bold">{tx.amount} {tx.asset}</p>
                                                    <span className={cn(
                                                        "text-[11px] font-semibold",
                                                        tx.status === "Confirmed" ? "text-black/60" :
                                                        tx.status === "Pending" ? "text-amber-700" : "text-red-700"
                                                    )}>{tx.status}</span>
                                                </div>
                                                {tx.isOnChain && tx.txHash && (
                                                    <a
                                                        href={`${currentNetwork.explorerUrl}/transactions/${tx.txHash}`}
                                                        target="_blank"
                                                        rel="noreferrer"
                                                        className="flex h-6 w-6 items-center justify-center rounded-md text-black/40 hover:bg-black/[0.04] hover:text-black/70 transition-colors focus-visible:ring-2 focus-visible:ring-black"
                                                        aria-label={`View transaction ${tx.txHash.slice(0, 8)} on explorer`}
                                                    >
                                                        <ExternalLink className="h-3 w-3" aria-hidden="true" />
                                                    </a>
                                                )}
                                            </div>
                                        </motion.div>
                                    );
                                })}
                            </div>
                        )}
                    </motion.div>
                )}

                {activeTab === "compare" && (
                    <motion.div
                        key="compare"
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -8 }}
                    >
                        <YieldComparisonChart
                            history={compareHistory}
                            snapshots={compareSnapshots}
                            loading={false}
                        />
                    </motion.div>
                )}
            </AnimatePresence>

            <WithdrawModal open={withdrawPos !== null} onClose={() => setWithdrawPos(null)} position={withdrawPos} />
            <TransferModal open={transferPos !== null} onClose={() => setTransferPos(null)} position={transferPos} />
        </AppShell>
    );
}
