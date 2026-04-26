"use client";

import { useState } from "react";
import { ProtectedRoute } from "@/components/protected-route";
import Image from "next/image";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { AppShell } from "@/components/app-shell";
import { PositionCards } from "@/components/position-cards";
import { usePortfolio } from "@/components/portfolio-provider";
import {
    ArrowUpRight,
    ArrowDownRight,
    TrendingUp,
    Search,
    X,
    Info,
} from "lucide-react";

// ── Types ────────────────────────────────────────────────────────────────────

interface TokenizedStock {
    id: string;
    ticker: string;
    name: string;
    category: "equity" | "etf" | "commodity";
    price: number;
    change24h: number;
    marketCap: string;
    volume24h: string;
    yieldApy: number;
    description: string;
}

type CategoryFilter = "all" | "equity" | "etf" | "commodity";

// ── Mock data ────────────────────────────────────────────────────────────────

const STOCKS: TokenizedStock[] = [
    {
        id: "sp500",
        ticker: "nSPY",
        name: "S&P 500 Index",
        category: "etf",
        price: 542.18,
        change24h: 1.24,
        marketCap: "$4.2M",
        volume24h: "$380K",
        yieldApy: 8.2,
        description: "Tokenized S&P 500 index tracking the 500 largest US public companies. Rebalanced quarterly.",
    },
    {
        id: "tech-etf",
        ticker: "nQQQ",
        name: "Tech ETF",
        category: "etf",
        price: 487.35,
        change24h: 2.15,
        marketCap: "$2.8M",
        volume24h: "$245K",
        yieldApy: 9.1,
        description: "Tokenized Nasdaq-100 exposure. Heavy weighting toward big tech and AI companies.",
    },
    {
        id: "aapl",
        ticker: "nAAPL",
        name: "Apple Inc.",
        category: "equity",
        price: 198.52,
        change24h: -0.34,
        marketCap: "$1.1M",
        volume24h: "$95K",
        yieldApy: 5.8,
        description: "Tokenized Apple stock. Dividends auto-converted to USDC and distributed to holders.",
    },
    {
        id: "tsla",
        ticker: "nTSLA",
        name: "Tesla Inc.",
        category: "equity",
        price: 245.80,
        change24h: 3.42,
        marketCap: "$890K",
        volume24h: "$142K",
        yieldApy: 0,
        description: "Tokenized Tesla stock. Price tracks the underlying equity via oracle feeds.",
    },
    {
        id: "msft",
        ticker: "nMSFT",
        name: "Microsoft Corp.",
        category: "equity",
        price: 425.60,
        change24h: 0.87,
        marketCap: "$1.5M",
        volume24h: "$118K",
        yieldApy: 4.2,
        description: "Tokenized Microsoft stock with auto-distributed dividends.",
    },
    {
        id: "gold",
        ticker: "nGLD",
        name: "Gold",
        category: "commodity",
        price: 2_342.50,
        change24h: 0.15,
        marketCap: "$3.1M",
        volume24h: "$210K",
        yieldApy: 3.5,
        description: "Tokenized gold exposure. 1 nGLD tracks 1 troy ounce of gold via oracle price feeds.",
    },
    {
        id: "div-basket",
        ticker: "nDIV",
        name: "Dividend Basket",
        category: "etf",
        price: 112.40,
        change24h: 0.52,
        marketCap: "$1.8M",
        volume24h: "$78K",
        yieldApy: 7.4,
        description: "A basket of high-dividend US equities. Dividends auto-compounded for higher yield.",
    },
];

const CATEGORY_LABELS: Record<CategoryFilter, string> = {
    all: "All Assets",
    equity: "Equities",
    etf: "ETFs & Indexes",
    commodity: "Commodities",
};

// ── Buy modal ────────────────────────────────────────────────────────────────

function BuyModal({ stock, onClose }: { stock: TokenizedStock; onClose: () => void }) {
    const [amount, setAmount] = useState("");
    const parsedAmount = parseFloat(amount) || 0;
    const estimatedShares = parsedAmount / stock.price;

    return (
        <AnimatePresence>
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="fixed inset-0 z-50 bg-black/25 backdrop-blur-sm"
                onClick={onClose}
            />
            <motion.div
                initial={{ opacity: 0, y: 32 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 32 }}
                transition={{ duration: 0.22 }}
                className="fixed inset-x-0 bottom-0 z-50 rounded-t-3xl bg-white shadow-2xl
                           sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2
                           sm:rounded-3xl sm:w-[480px]"
            >
                <div className="p-6 sm:p-8">
                    {/* Header */}
                    <div className="flex items-center justify-between mb-6">
                        <div>
                            <p className="text-lg font-medium text-black">Buy {stock.ticker}</p>
                            <p className="text-xs text-black/40 mt-0.5">{stock.name}</p>
                        </div>
                        <button onClick={onClose} className="flex h-8 w-8 items-center justify-center rounded-full border border-black/8 text-black/40 hover:text-black transition-colors">
                            <X className="h-4 w-4" />
                        </button>
                    </div>

                    {/* Price */}
                    <div className="mb-6 rounded-xl bg-black/[0.025] px-4 py-3 flex items-center justify-between">
                        <span className="text-xs text-black/40">Current Price</span>
                        <span className="font-mono text-sm text-black">${stock.price.toLocaleString("en-US", { minimumFractionDigits: 2 })}</span>
                    </div>

                    {/* Amount input */}
                    <div className="mb-4">
                        <label className="text-xs text-black/40 mb-2 block">Amount (USDC)</label>
                        <div className="relative">
                            <span className="absolute left-4 top-1/2 -translate-y-1/2 text-black/30 text-lg">$</span>
                            <input
                                type="number"
                                value={amount}
                                onChange={(e) => setAmount(e.target.value)}
                                placeholder="0.00"
                                className="h-14 w-full rounded-xl border border-black/10 bg-black/[0.02] pl-8 pr-4
                                           font-mono text-xl text-black outline-none transition-colors
                                           focus:border-black/25 focus:bg-white
                                           [appearance:textfield]
                                           [&::-webkit-outer-spin-button]:appearance-none
                                           [&::-webkit-inner-spin-button]:appearance-none"
                            />
                        </div>
                    </div>

                    {/* Quick amounts */}
                    <div className="flex gap-2 mb-6">
                        {[50, 100, 250, 500].map((v) => (
                            <button
                                key={v}
                                onClick={() => setAmount(String(v))}
                                className="flex-1 rounded-lg border border-black/8 py-2 text-xs text-black/50 hover:border-black/20 hover:text-black transition-colors"
                            >
                                ${v}
                            </button>
                        ))}
                    </div>

                    {/* Estimate */}
                    {parsedAmount > 0 && (
                        <div className="mb-6 rounded-xl border border-black/8 px-4 py-3 space-y-2">
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">You receive (est.)</span>
                                <span className="font-mono text-black">{estimatedShares.toFixed(6)} {stock.ticker}</span>
                            </div>
                            {stock.yieldApy > 0 && (
                                <div className="flex justify-between text-xs">
                                    <span className="text-black/40">Yield APY</span>
                                    <span className="font-mono text-black">{stock.yieldApy}%</span>
                                </div>
                            )}
                        </div>
                    )}

                    {/* CTA */}
                    <button
                        disabled={parsedAmount <= 0}
                        className="w-full rounded-xl bg-black py-3.5 text-sm text-white transition-opacity disabled:opacity-35 disabled:cursor-not-allowed hover:opacity-80"
                    >
                        Buy {stock.ticker}
                    </button>
                    <p className="mt-2.5 text-center text-[11px] text-black/30">
                        Paid with USDC from your wallet
                    </p>
                </div>
            </motion.div>
        </AnimatePresence>
    );
}

// ── Page ─────────────────────────────────────────────────────────────────────

export default function StocksPage() {
    const { positions } = usePortfolio();
    const [filter, setFilter] = useState<CategoryFilter>("all");
    const [search, setSearch] = useState("");
    const [selectedStock, setSelectedStock] = useState<TokenizedStock | null>(null);

    const filtered = STOCKS.filter((s) => {
        if (filter !== "all" && s.category !== filter) return false;
        if (search) {
            const q = search.toLowerCase();
            return s.ticker.toLowerCase().includes(q) || s.name.toLowerCase().includes(q);
        }
        return true;
    });

    const totalMarketCap = STOCKS.reduce((sum, s) => {
        const raw = s.marketCap.replace(/[$KMB,]/g, "");
        const num = parseFloat(raw);
        if (s.marketCap.includes("M")) return sum + num * 1_000_000;
        if (s.marketCap.includes("K")) return sum + num * 1_000;
        return sum + num;
    }, 0);

    return (
        <ProtectedRoute>
            <AppShell>
            {/* Header */}
            <motion.div
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-7"
            >
                <h1 className="text-2xl text-black sm:text-3xl">Stocks</h1>
                <p className="mt-1 text-sm text-black/40">
                    Buy tokenized equities, ETFs, and commodities with USDC. Earn dividends and price appreciation on-chain.
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
                    { label: "Assets Listed", value: STOCKS.length.toString() },
                    { label: "Total Market Cap", value: totalMarketCap >= 1_000_000 ? `$${(totalMarketCap / 1_000_000).toFixed(1)}M` : `$${(totalMarketCap / 1_000).toFixed(0)}K` },
                    { label: "Avg Yield", value: `${(STOCKS.reduce((s, x) => s + x.yieldApy, 0) / STOCKS.length).toFixed(1)}%` },
                ].map((s) => (
                    <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                        <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                        <p className="mt-0.5 text-[11px] text-black/35">{s.label}</p>
                    </div>
                ))}
            </motion.div>

            {/* Search + Filters */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.1 }}
                className="mb-6 space-y-3"
            >
                {/* Search */}
                <div className="relative w-full max-w-sm">
                    <Search className="absolute left-3.5 top-1/2 h-[15px] w-[15px] -translate-y-1/2 text-black/25" />
                    <input
                        type="text"
                        placeholder="Search by name or ticker..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        className="w-full rounded-xl border border-black/[0.08] bg-transparent py-2.5 pl-10 pr-4 text-[14px] text-black placeholder:text-black/30 outline-none transition-colors focus:border-black/20"
                    />
                </div>

                {/* Category tabs */}
                <div className="flex gap-1 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide">
                    {(["all", "equity", "etf", "commodity"] as const).map((cat) => (
                        <button
                            key={cat}
                            onClick={() => setFilter(cat)}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                                filter === cat ? "text-black" : "text-black/35 hover:text-black/55"
                            )}
                        >
                            {CATEGORY_LABELS[cat]}
                            {filter === cat && (
                                <motion.div
                                    layoutId="stock-tab"
                                    className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                                />
                            )}
                        </button>
                    ))}
                </div>
            </motion.div>

            {/* Stock list */}
            <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: 0.15 }}
                className="space-y-2"
            >
                {/* Table header */}
                <div className="hidden sm:grid grid-cols-[2fr_1fr_1fr_1fr_1fr_auto] gap-4 px-5 py-2 text-[11px] text-black/35">
                    <span>Asset</span>
                    <span className="text-right">Price</span>
                    <span className="text-right">24h</span>
                    <span className="text-right">Market Cap</span>
                    <span className="text-right">Yield APY</span>
                    <span className="w-20"></span>
                </div>

                {filtered.length === 0 ? (
                    <div className="flex flex-col items-center justify-center py-20 text-center">
                        <p className="text-sm text-black/40">No assets match your search</p>
                    </div>
                ) : (
                    filtered.map((stock, i) => (
                        <motion.div
                            key={stock.id}
                            initial={{ opacity: 0, y: 8 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ duration: 0.2, delay: i * 0.04 }}
                            className="grid grid-cols-[1fr_auto] sm:grid-cols-[2fr_1fr_1fr_1fr_1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm"
                        >
                            {/* Name + ticker */}
                            <div className="flex items-center gap-3 min-w-0">
                                <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.04] shrink-0">
                                    <span className="text-xs font-semibold text-black/60">{stock.ticker.slice(1, 3)}</span>
                                </div>
                                <div className="min-w-0">
                                    <p className="truncate text-sm text-black">{stock.name}</p>
                                    <p className="text-[11px] text-black/35 mt-0.5 font-mono">{stock.ticker}</p>
                                </div>
                            </div>

                            {/* Price */}
                            <div className="hidden sm:block text-right">
                                <p className="font-mono text-sm text-black">
                                    ${stock.price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                                </p>
                            </div>

                            {/* 24h change */}
                            <div className="hidden sm:block text-right">
                                <span className={cn(
                                    "inline-flex items-center gap-0.5 font-mono text-sm",
                                    stock.change24h >= 0 ? "text-emerald-600" : "text-red-500"
                                )}>
                                    {stock.change24h >= 0 ? (
                                        <ArrowUpRight className="h-3 w-3" />
                                    ) : (
                                        <ArrowDownRight className="h-3 w-3" />
                                    )}
                                    {Math.abs(stock.change24h).toFixed(2)}%
                                </span>
                            </div>

                            {/* Market Cap */}
                            <div className="hidden sm:block text-right">
                                <p className="font-mono text-sm text-black/55">{stock.marketCap}</p>
                            </div>

                            {/* Yield */}
                            <div className="hidden sm:block text-right">
                                {stock.yieldApy > 0 ? (
                                    <p className="font-mono text-sm text-black">{stock.yieldApy}%</p>
                                ) : (
                                    <p className="text-xs text-black/30">-</p>
                                )}
                            </div>

                            {/* Actions */}
                            <div className="flex items-center gap-2 shrink-0">
                                <span className="sm:hidden font-mono text-sm text-black">
                                    ${stock.price.toLocaleString("en-US", { minimumFractionDigits: 2 })}
                                </span>
                                <button
                                    onClick={() => setSelectedStock(stock)}
                                    className="flex h-8 items-center gap-1 rounded-lg bg-black px-3 text-xs text-white transition-opacity hover:opacity-75"
                                >
                                    Buy <ArrowUpRight className="h-3 w-3" />
                                </button>
                            </div>
                        </motion.div>
                    ))
                )}
            </motion.div>

            {/* Open positions */}
            {(() => {
                const stockPositions = positions.filter((p) => p.vaultId === "stocks");
                if (stockPositions.length === 0) return null;
                return (
                    <motion.div
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: 0.2 }}
                        className="mt-8"
                    >
                        <h2 className="text-sm text-black mb-3">Your Stock Positions</h2>
                        <PositionCards positions={stockPositions} />
                    </motion.div>
                );
            })()}

            {/* Buy modal */}
            {selectedStock && (
                <BuyModal stock={selectedStock} onClose={() => setSelectedStock(null)} />
            )}
        </AppShell>
        </ProtectedRoute>
    );
}
