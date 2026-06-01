"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import {
    ArrowUpRight,
    ArrowDownRight,
    TrendingUp,
    Search,
    X,
    Bookmark,
    BookmarkCheck,
    RefreshCw,
    Zap,
    AlertTriangle,
} from "lucide-react";

// ── Types ────────────────────────────────────────────────────────────────────

interface YieldPool {
    pool: string;
    project: string;
    symbol: string;
    apy: number;
    apyBase: number;
    apyReward: number;
    tvlUsd: number;
    apyPct7d: number | null;
    chain: string;
    riskScore: number;
}

interface AIPick {
    protocol: string;
    symbol: string;
    apy: number;
    tvl_usd: number;
    rationale: string;
    risk_level: "low" | "medium" | "high";
    confidence: number;
}

interface WatchlistItem {
    id: string;
    pool_id: string;
    pool_symbol: string;
    pool_project: string;
    pool_chain: string;
    apy_at_save: number;
    tvl_usd: number;
    added_at: string;
}

// ── API helpers ───────────────────────────────────────────────────────────────

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

async function fetchYieldOpportunities(chain = "Stellar", limit = 20): Promise<YieldPool[]> {
    const res = await fetch(`${API_BASE}/api/v1/yield-opportunities?chain=${chain}&limit=${limit}`);
    if (!res.ok) throw new Error(`yield-opportunities: ${res.status}`);
    const json = await res.json();
    return json.data ?? [];
}

async function fetchAIPick(): Promise<AIPick | null> {
    try {
        const res = await fetch(`/api/v1/recommend/vault`);
        if (!res.ok) return null;
        return res.json();
    } catch {
        return null;
    }
}

async function fetchWatchlist(token: string): Promise<WatchlistItem[]> {
    const res = await fetch(`${API_BASE}/api/v1/users/watchlist`, {
        headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) return [];
    const json = await res.json();
    return json.data ?? [];
}

async function addToWatchlist(pool: YieldPool, token: string): Promise<WatchlistItem | null> {
    const res = await fetch(`${API_BASE}/api/v1/users/watchlist`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify({
            pool_id: pool.pool,
            pool_symbol: pool.symbol,
            pool_project: pool.project,
            pool_chain: pool.chain,
            apy_at_save: pool.apy,
            tvl_usd: pool.tvlUsd,
        }),
    });
    if (!res.ok) return null;
    const json = await res.json();
    return json.data ?? null;
}

async function removeFromWatchlist(itemId: string, token: string): Promise<boolean> {
    const res = await fetch(`${API_BASE}/api/v1/users/watchlist/${itemId}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
    });
    return res.ok || res.status === 204;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function riskLabel(score: number): { text: string; color: string } {
    if (score <= 25) return { text: "Low", color: "text-emerald-600" };
    if (score <= 55) return { text: "Med", color: "text-amber-500" };
    return { text: "High", color: "text-red-500" };
}

function fmtTVL(usd: number): string {
    if (usd >= 1_000_000_000) return `$${(usd / 1_000_000_000).toFixed(1)}B`;
    if (usd >= 1_000_000) return `$${(usd / 1_000_000).toFixed(1)}M`;
    if (usd >= 1_000) return `$${(usd / 1_000).toFixed(0)}K`;
    return `$${usd.toFixed(0)}`;
}

// ── Loading skeleton ──────────────────────────────────────────────────────────

function SkeletonRow() {
    return (
        <div className="animate-pulse flex items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4">
            <div className="h-10 w-10 rounded-xl bg-black/[0.06] shrink-0" />
            <div className="flex-1 space-y-2">
                <div className="h-3.5 w-36 rounded bg-black/[0.06]" />
                <div className="h-3 w-20 rounded bg-black/[0.04]" />
            </div>
            <div className="h-4 w-16 rounded bg-black/[0.06]" />
            <div className="h-4 w-12 rounded bg-black/[0.06]" />
            <div className="h-8 w-16 rounded-lg bg-black/[0.06]" />
        </div>
    );
}

// ── Pool row ──────────────────────────────────────────────────────────────────

function PoolRow({
    pool,
    watchlistId,
    onWatch,
    onUnwatch,
}: {
    pool: YieldPool;
    watchlistId: string | undefined;
    onWatch: (pool: YieldPool) => void;
    onUnwatch: (id: string) => void;
}) {
    const risk = riskLabel(pool.riskScore);
    const saved = Boolean(watchlistId);

    return (
        <motion.div
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.18 }}
            className="grid grid-cols-[1fr_auto] sm:grid-cols-[2fr_1fr_1fr_1fr_1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm"
        >
            {/* Name */}
            <div className="flex items-center gap-3 min-w-0">
                <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.04] shrink-0">
                    <span className="text-xs font-semibold text-black/60 uppercase">
                        {pool.symbol.slice(0, 2)}
                    </span>
                </div>
                <div className="min-w-0">
                    <p className="truncate text-sm text-black">{pool.symbol}</p>
                    <p className="text-[11px] text-black/35 mt-0.5 font-mono capitalize">{pool.project}</p>
                </div>
            </div>

            {/* APY */}
            <div className="hidden sm:block text-right">
                <p className="font-mono text-sm text-black">{pool.apy.toFixed(2)}%</p>
                <p className="text-[10px] text-black/30 mt-0.5">APY</p>
            </div>

            {/* 7d trend */}
            <div className="hidden sm:block text-right">
                {pool.apyPct7d != null ? (
                    <span className={cn(
                        "inline-flex items-center gap-0.5 font-mono text-sm",
                        pool.apyPct7d >= 0 ? "text-emerald-600" : "text-red-500"
                    )}>
                        {pool.apyPct7d >= 0
                            ? <ArrowUpRight className="h-3 w-3" />
                            : <ArrowDownRight className="h-3 w-3" />}
                        {Math.abs(pool.apyPct7d).toFixed(1)}%
                    </span>
                ) : (
                    <span className="text-xs text-black/30">-</span>
                )}
            </div>

            {/* TVL */}
            <div className="hidden sm:block text-right">
                <p className="font-mono text-sm text-black/55">{fmtTVL(pool.tvlUsd)}</p>
            </div>

            {/* Risk */}
            <div className="hidden sm:block text-right">
                <span className={cn("text-xs font-medium", risk.color)}>{risk.text}</span>
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2 shrink-0">
                <span className="sm:hidden font-mono text-sm text-black">{pool.apy.toFixed(1)}%</span>
                <button
                    onClick={() => saved ? onUnwatch(watchlistId!) : onWatch(pool)}
                    title={saved ? "Remove from watchlist" : "Add to watchlist"}
                    className={cn(
                        "flex h-8 w-8 items-center justify-center rounded-lg border transition-colors",
                        saved
                            ? "border-black/20 text-black hover:bg-black/5"
                            : "border-black/8 text-black/35 hover:border-black/20 hover:text-black"
                    )}
                >
                    {saved
                        ? <BookmarkCheck className="h-3.5 w-3.5" />
                        : <Bookmark className="h-3.5 w-3.5" />}
                </button>
            </div>
        </motion.div>
    );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function StocksPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const [search, setSearch] = useState("");

    const [pools, setPools] = useState<YieldPool[]>([]);
    const [aiPick, setAIPick] = useState<AIPick | null>(null);
    const [watchlist, setWatchlist] = useState<WatchlistItem[]>([]);
    const [loadingPools, setLoadingPools] = useState(true);
    const [loadingAI, setLoadingAI] = useState(true);
    const [errorPools, setErrorPools] = useState(false);

    // ── auth token helper ─────────────────────────────────────────────────────
    // Attempt to read JWT from storage; falls back to empty string so watchlist
    // calls silently fail and the user sees an empty list without an error.
    const getToken = useCallback((): string => {
        if (typeof window === "undefined") return "";
        return localStorage.getItem("nester_token") ?? sessionStorage.getItem("nester_token") ?? "";
    }, []);

    // ── fetch pools ───────────────────────────────────────────────────────────
    useEffect(() => {
        if (!isConnected) return;
        setLoadingPools(true);
        setErrorPools(false);
        fetchYieldOpportunities("Stellar", 20)
            .then(setPools)
            .catch(() => setErrorPools(true))
            .finally(() => setLoadingPools(false));
    }, [isConnected]);

    // ── fetch AI pick ─────────────────────────────────────────────────────────
    useEffect(() => {
        if (!isConnected) return;
        fetchAIPick()
            .then(setAIPick)
            .finally(() => setLoadingAI(false));
    }, [isConnected]);

    // ── fetch watchlist ───────────────────────────────────────────────────────
    useEffect(() => {
        if (!isConnected) return;
        const token = getToken();
        if (!token) return;
        fetchWatchlist(token).then(setWatchlist);
    }, [isConnected, getToken]);

    // ── redirect if not connected ─────────────────────────────────────────────
    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    // ── derived data ──────────────────────────────────────────────────────────
    const watchedPoolIds = new Map(watchlist.map((w) => [w.pool_id, w.id]));

    const filtered = pools.filter((p) => {
        if (!search) return true;
        const q = search.toLowerCase();
        return p.symbol.toLowerCase().includes(q) || p.project.toLowerCase().includes(q);
    });

    const trending = [...pools]
        .filter((p) => p.apyPct7d != null)
        .sort((a, b) => (b.apyPct7d ?? 0) - (a.apyPct7d ?? 0))
        .slice(0, 3);

    // ── watchlist actions ─────────────────────────────────────────────────────
    const handleWatch = async (pool: YieldPool) => {
        const token = getToken();
        if (!token) return;
        const item = await addToWatchlist(pool, token);
        if (item) setWatchlist((prev) => [item, ...prev]);
    };

    const handleUnwatch = async (id: string) => {
        const token = getToken();
        if (!token) return;
        const ok = await removeFromWatchlist(id, token);
        if (ok) setWatchlist((prev) => prev.filter((w) => w.id !== id));
    };

    const avgAPY = pools.length > 0
        ? pools.reduce((s, p) => s + p.apy, 0) / pools.length
        : 0;

    return (
        <AppShell>
            {/* Header */}
            <motion.div initial={{ opacity: 0, y: -8 }} animate={{ opacity: 1, y: 0 }} className="mb-7">
                <h1 className="text-2xl text-black sm:text-3xl">Yield Opportunities</h1>
                <p className="mt-1 text-sm text-black/40">
                    Live yield-bearing digital assets on Stellar, ranked by risk-adjusted APY.
                </p>
            </motion.div>

            {/* Stats */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.05 }}
                className="mb-7 grid grid-cols-3 gap-3 sm:gap-4"
            >
                {[
                    { label: "Pools Listed", value: loadingPools ? "—" : pools.length.toString() },
                    { label: "Avg APY", value: loadingPools ? "—" : `${avgAPY.toFixed(1)}%` },
                    { label: "Watchlist", value: watchlist.length.toString() },
                ].map((s) => (
                    <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                        <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                        <p className="mt-0.5 text-[11px] text-black/35">{s.label}</p>
                    </div>
                ))}
            </motion.div>

            {/* AI Pick of the Day */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.08 }}
                className="mb-7"
            >
                <h2 className="text-sm text-black mb-3 flex items-center gap-2">
                    <Zap className="h-3.5 w-3.5" />
                    AI Pick of the Day
                </h2>
                {loadingAI ? (
                    <div className="animate-pulse rounded-2xl border border-black/8 bg-white p-5 h-24" />
                ) : aiPick && aiPick.confidence > 0 ? (
                    <div className="rounded-2xl border border-black/8 bg-white p-5">
                        <div className="flex items-start justify-between gap-4">
                            <div className="min-w-0">
                                <p className="text-sm font-medium text-black">
                                    {aiPick.symbol || aiPick.protocol}
                                    {aiPick.symbol && aiPick.protocol && (
                                        <span className="ml-1.5 text-xs text-black/40 font-normal capitalize">
                                            via {aiPick.protocol}
                                        </span>
                                    )}
                                </p>
                                <p className="mt-1 text-xs text-black/50 leading-relaxed">{aiPick.rationale}</p>
                            </div>
                            <div className="text-right shrink-0">
                                <p className="font-mono text-lg text-black">{aiPick.apy.toFixed(2)}%</p>
                                <p className="text-[10px] text-black/35 mt-0.5 capitalize">{aiPick.risk_level} risk</p>
                            </div>
                        </div>
                        <div className="mt-3 flex items-center gap-1.5">
                            <div className="h-1 flex-1 rounded-full bg-black/[0.06]">
                                <div
                                    className="h-1 rounded-full bg-black/30"
                                    style={{ width: `${(aiPick.confidence * 100).toFixed(0)}%` }}
                                />
                            </div>
                            <span className="text-[10px] text-black/35 shrink-0">
                                {(aiPick.confidence * 100).toFixed(0)}% confidence
                            </span>
                        </div>
                    </div>
                ) : (
                    <div className="rounded-2xl border border-black/8 bg-white p-5 text-sm text-black/35">
                        AI recommendation temporarily unavailable.
                    </div>
                )}
            </motion.div>

            {/* Trending in DeFi */}
            {!loadingPools && trending.length > 0 && (
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.1 }}
                    className="mb-7"
                >
                    <h2 className="text-sm text-black mb-3 flex items-center gap-2">
                        <TrendingUp className="h-3.5 w-3.5" />
                        Trending in DeFi
                    </h2>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                        {trending.map((pool) => (
                            <div
                                key={pool.pool}
                                className="rounded-2xl border border-black/8 bg-white px-4 py-3"
                            >
                                <div className="flex items-center justify-between mb-1">
                                    <p className="text-sm text-black truncate">{pool.symbol}</p>
                                    <span className={cn(
                                        "inline-flex items-center gap-0.5 font-mono text-sm",
                                        (pool.apyPct7d ?? 0) >= 0 ? "text-emerald-600" : "text-red-500"
                                    )}>
                                        {(pool.apyPct7d ?? 0) >= 0
                                            ? <ArrowUpRight className="h-3 w-3" />
                                            : <ArrowDownRight className="h-3 w-3" />}
                                        {Math.abs(pool.apyPct7d ?? 0).toFixed(1)}% 7d
                                    </span>
                                </div>
                                <p className="text-xs text-black/40 capitalize">{pool.project}</p>
                                <p className="font-mono text-base text-black mt-1">{pool.apy.toFixed(2)}% APY</p>
                            </div>
                        ))}
                    </div>
                </motion.div>
            )}

            {/* Search */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.12 }}
                className="mb-5"
            >
                <div className="relative w-full max-w-sm">
                    <Search className="absolute left-3.5 top-1/2 h-[15px] w-[15px] -translate-y-1/2 text-black/25" />
                    <input
                        type="text"
                        placeholder="Search by symbol or protocol..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        className="w-full rounded-xl border border-black/[0.08] bg-transparent py-2.5 pl-10 pr-4 text-[14px] text-black placeholder:text-black/30 outline-none transition-colors focus:border-black/20"
                    />
                </div>
            </motion.div>

            {/* Top Yield Opportunities */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.15 }}
                className="mb-8"
            >
                <h2 className="text-sm text-black mb-3">Top Yield Opportunities</h2>

                {/* Table header */}
                <div className="hidden sm:grid grid-cols-[2fr_1fr_1fr_1fr_1fr_auto] gap-4 px-5 py-2 text-[11px] text-black/35">
                    <span>Asset</span>
                    <span className="text-right">APY</span>
                    <span className="text-right">7d</span>
                    <span className="text-right">TVL</span>
                    <span className="text-right">Risk</span>
                    <span className="w-8" />
                </div>

                <div className="space-y-2">
                    {loadingPools ? (
                        Array.from({ length: 5 }).map((_, i) => <SkeletonRow key={i} />)
                    ) : errorPools ? (
                        <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
                            <AlertTriangle className="h-6 w-6 text-black/25" />
                            <p className="text-sm text-black/40">Yield data unavailable right now.</p>
                            <button
                                onClick={() => {
                                    setLoadingPools(true);
                                    setErrorPools(false);
                                    fetchYieldOpportunities("Stellar", 20)
                                        .then(setPools)
                                        .catch(() => setErrorPools(true))
                                        .finally(() => setLoadingPools(false));
                                }}
                                className="flex items-center gap-1.5 rounded-lg border border-black/10 px-3 py-1.5 text-xs text-black/50 hover:text-black transition-colors"
                            >
                                <RefreshCw className="h-3 w-3" />
                                Retry
                            </button>
                        </div>
                    ) : filtered.length === 0 ? (
                        <div className="flex flex-col items-center justify-center py-16 text-center">
                            <p className="text-sm text-black/40">No pools match your search.</p>
                        </div>
                    ) : (
                        filtered.map((pool) => (
                            <PoolRow
                                key={pool.pool}
                                pool={pool}
                                watchlistId={watchedPoolIds.get(pool.pool)}
                                onWatch={handleWatch}
                                onUnwatch={handleUnwatch}
                            />
                        ))
                    )}
                </div>
            </motion.div>

            {/* Your Watchlist */}
            {watchlist.length > 0 && (
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.2 }}
                    className="mb-8"
                >
                    <h2 className="text-sm text-black mb-3 flex items-center gap-2">
                        <BookmarkCheck className="h-3.5 w-3.5" />
                        Your Watchlist
                    </h2>
                    <div className="space-y-2">
                        {watchlist.map((item) => (
                            <div
                                key={item.id}
                                className="grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4"
                            >
                                <div className="flex items-center gap-3 min-w-0">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.04] shrink-0">
                                        <span className="text-xs font-semibold text-black/60 uppercase">
                                            {item.pool_symbol.slice(0, 2)}
                                        </span>
                                    </div>
                                    <div className="min-w-0">
                                        <p className="truncate text-sm text-black">{item.pool_symbol}</p>
                                        <p className="text-[11px] text-black/35 mt-0.5 capitalize">{item.pool_project}</p>
                                    </div>
                                </div>
                                <div className="flex items-center gap-3 shrink-0">
                                    <div className="text-right hidden sm:block">
                                        <p className="font-mono text-sm text-black">{item.apy_at_save.toFixed(2)}%</p>
                                        <p className="text-[10px] text-black/30">at save</p>
                                    </div>
                                    <button
                                        onClick={() => handleUnwatch(item.id)}
                                        className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/8 text-black/35 hover:border-black/20 hover:text-black transition-colors"
                                    >
                                        <X className="h-3.5 w-3.5" />
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>
                </motion.div>
            )}
        </AppShell>
    );
}
