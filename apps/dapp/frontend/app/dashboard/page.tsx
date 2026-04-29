"use client";

import Link from "next/link";
import Image from "next/image";
import { useMemo, useState } from "react";
import { ProtectedRoute } from "@/components/protected-route";
import { motion } from "framer-motion";
import {
    ArrowDownToLine,
    ArrowUpRight,
    BarChart3,
    Layers,
    PiggyBank,
    Shield,
    TrendingUp,
    Vault,
    Wallet,
} from "lucide-react";
import {
    usePortfolio,
    type PortfolioPosition,
} from "@/components/portfolio-provider";
import { WithdrawModal } from "@/components/vault-action-modals";
import { cn } from "@/lib/utils";
import { GuidedTour } from "@/components/onboarding/GuidedTour";
import { useTokenPrices } from "@/hooks/useTokenPrices";
import { useNetwork } from "@/hooks/useNetwork";
import { AppShell } from "@/components/app-shell";
import { PrometheusInsightsCard } from "@/components/ai/prometheusInsightsCard";
import { MarketSentimentWidget } from "@/components/ai/marketSentiment";

const CHART_PERIODS = ["1D", "1W", "1M", "6M", "1Y", "All"] as const;

function getVaultIcon(vaultName: string) {
    const name = vaultName.toLowerCase();
    if (name.includes("saving") || name.includes("flex"))
        return <PiggyBank className="h-4 w-4" />;
    if (name.includes("defi") || name.includes("index"))
        return <Layers className="h-4 w-4" />;
    if (name.includes("conservative") || name.includes("stable"))
        return <Shield className="h-4 w-4" />;
    if (name.includes("growth") || name.includes("aggressive"))
        return <TrendingUp className="h-4 w-4" />;
    if (name.includes("balanced"))
        return <BarChart3 className="h-4 w-4" />;
    return <Vault className="h-4 w-4" />;
}

export default function Dashboard() {
    const { positions, transactions, balances } = usePortfolio();
    const { prices: tokenPrices } = useTokenPrices();
    const { currentNetwork } = useNetwork();
    const [selectedPosition, setSelectedPosition] = useState<PortfolioPosition | null>(null);
    const [chartPeriod, setChartPeriod] = useState<(typeof CHART_PERIODS)[number]>("1W");

    const { protocolBalanceUsd, totalYield, avgApy } = useMemo(() => {
        const vaultUsd = positions.reduce((sum, p) => sum + p.currentValue, 0);
        const yield_ = positions.reduce((sum, p) => sum + p.yieldEarned, 0);
        const apy = positions.length
            ? positions.reduce((sum, p) => sum + (p.apy ?? 0), 0) / positions.length
            : 0;
        return { protocolBalanceUsd: vaultUsd, totalYield: yield_, avgApy: apy };
    }, [positions]);

    const greeting = useMemo(() => {
        const hour = new Date().getHours();
        if (hour < 12) return "Good morning.";
        if (hour < 18) return "Good afternoon.";
        return "Good evening.";
    }, []);

    const recentTransactions = transactions.slice(0, 5);

    return (
        <ProtectedRoute>
            <AppShell>
            {/* ── Greeting + action buttons ── */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3 }}
                className="my-4 flex flex-wrap items-center justify-between gap-4"
            >
                <h1 className="text-[30px] font-semibold text-black tracking-[-0.02em]">
                    {greeting}
                </h1>
                <div className="flex items-center gap-2.5">
                    <Link
                        href="/vaults"
                        className="flex min-h-[var(--touch-target)] items-center justify-center gap-2 rounded-full border border-black/[0.1] bg-white px-6 sm:px-5 py-2.5 text-[14px] sm:text-[13px] font-medium text-black/65 transition-all hover:border-black/20 hover:shadow-sm active:bg-black/5"
                    >
                        <ArrowDownToLine className="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                        Deposit
                    </Link>
                    <Link
                        href="/savings"
                        className="flex min-h-[var(--touch-target)] items-center justify-center gap-2 rounded-full border border-black/[0.1] bg-white px-6 sm:px-5 py-2.5 text-[14px] sm:text-[13px] font-medium text-black/65 transition-all hover:border-black/20 hover:shadow-sm active:bg-black/5"
                    >
                        <PiggyBank className="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                        Save
                    </Link>
                </div>
            </motion.div>

            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.08 }}
                className="mb-8 grid grid-cols-1 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)] gap-4"
            >
                <PrometheusInsightsCard />
                <MarketSentimentWidget />
            </motion.div>

            {/* ── Balance + Chart row ── */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.05 }}
                className="mb-10 grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] gap-0 rounded-2xl border border-black/[0.06] bg-white overflow-hidden"
            >
                {/* Left — balance + stats */}
                <div className="p-8 lg:p-10 flex flex-col justify-between">
                    <div>
                        <p className="text-[32px] sm:text-[42px] font-light leading-none text-black tracking-[-0.02em] break-all">
                            ${protocolBalanceUsd.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                        </p>
                        <p className="mt-2 text-[12px] text-black/35 tracking-wide">Protocol Balance</p>
                    </div>
                    <div className="mt-8 space-y-5">
                        <div className="flex flex-col gap-1">
                            <div className="flex items-center justify-between">
                                <span className="text-[13px] text-black/40">Position APY</span>
                                <span className="text-[13px] font-medium text-black">
                                    {(avgApy * 100).toFixed(2)}%
                                </span>
                            </div>
                            <p className="text-[9px] text-black/30 text-left">APY is variable and based on recent performance. Past performance is not indicative of future results.</p>
                        </div>
                        <div className="flex items-center justify-between">
                            <span className="text-[13px] text-black/40">Total earnings</span>
                            <span className="text-[13px] font-medium text-black">
                                ${totalYield.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                            </span>
                        </div>
                    </div>
                </div>

                {/* Right — chart */}
                <div className="border-t lg:border-t-0 lg:border-l border-black/[0.06] p-8 lg:p-10 flex flex-col">
                    <div className="flex items-center justify-end gap-0.5 mb-6">
                        {CHART_PERIODS.map((period) => (
                            <button
                                key={period}
                                onClick={() => setChartPeriod(period)}
                                className={cn(
                                    "rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors",
                                    chartPeriod === period
                                        ? "bg-black/[0.06] text-black"
                                        : "text-black/30 hover:text-black/55"
                                )}
                            >
                                {period}
                            </button>
                        ))}
                    </div>
                    <div className="flex-1 min-h-[160px] flex items-end">
                        <svg viewBox="0 0 400 120" className="w-full h-full" preserveAspectRatio="none">
                            <defs>
                                <linearGradient id="chartGrad" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="0%" stopColor="rgb(99, 102, 241)" stopOpacity="0.12" />
                                    <stop offset="100%" stopColor="rgb(99, 102, 241)" stopOpacity="0" />
                                </linearGradient>
                            </defs>
                            <path d="M0,95 C40,90 70,80 110,75 C150,70 190,82 240,55 C290,28 340,38 370,32 L400,28 L400,120 L0,120Z" fill="url(#chartGrad)" />
                            <path d="M0,95 C40,90 70,80 110,75 C150,70 190,82 240,55 C290,28 340,38 370,32 L400,28" fill="none" stroke="rgb(99, 102, 241)" strokeWidth="2" />
                        </svg>
                    </div>
                    <div className="flex items-center gap-2 mt-4">
                        <span className="h-2 w-2 rounded-full bg-indigo-500" />
                        <span className="text-[11px] text-black/35">Balance</span>
                    </div>
                </div>
            </motion.div>

            {/* ── Positions ── */}
            <motion.div
                data-tour="vault-list"
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.1 }}
                className="mb-8 rounded-2xl dash-border bg-white"
            >
                <div className="flex items-center justify-between px-8 pt-7 pb-0">
                    <h2 className="text-[16px] font-semibold text-black">Positions</h2>
                    <Link href="/vaults" data-tour="deposit-cta" className="flex min-h-[var(--touch-target)] items-center px-2 -mr-2 text-[14px] sm:text-[12px] text-black/35 transition-colors hover:text-black">
                        + New Position
                    </Link>
                </div>
                <div className="px-8 pb-8 pt-6">
                    {positions.length === 0 ? (
                        <div className="flex flex-col items-center justify-center py-14 text-center">
                            <p className="text-[14px] font-medium text-black/50">No Positions</p>
                            <p className="mt-1.5 text-[13px] text-black/30">
                                Create a position by depositing an asset from your wallet.
                            </p>
                        </div>
                    ) : (
                        <div className="overflow-x-auto pb-2">
                            <table className="w-full text-left min-w-[600px]">
                                <caption className="sr-only">Your active positions</caption>
                                <thead>
                                    <tr className="border-b border-black/[0.05] text-[11px] text-black/35">
                                        <th scope="col" className="pb-3.5 pr-6 font-medium">Vault</th>
                                        <th scope="col" className="pb-3.5 pr-6 font-medium">Balance</th>
                                        <th scope="col" className="pb-3.5 pr-6 font-medium">APY</th>
                                        <th scope="col" className="pb-3.5 pr-6 font-medium">Yield</th>
                                        <th scope="col" className="pb-3.5 pr-6 font-medium">Status</th>
                                        <th scope="col" className="pb-3.5 font-medium"><span className="sr-only">Actions</span></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {positions.map((position) => (
                                        <tr key={position.id} className="border-b border-black/[0.04] last:border-0">
                                            <td className="py-4 pr-6">
                                                <div className="flex items-center gap-3">
                                                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-black/[0.04] text-black/40" aria-hidden="true">
                                                        {getVaultIcon(position.vaultName)}
                                                    </div>
                                                    <div>
                                                        <p className="text-[14px] text-black">{position.vaultName}</p>
                                                        <p className="text-[11px] text-black/30 mt-0.5">{position.asset}</p>
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="py-4 pr-6 font-mono text-[14px] text-black">
                                                ${position.currentValue.toFixed(2)}
                                            </td>
                                            <td className="py-4 pr-6">
                                                <div className="text-[14px] text-black">
                                                    {((position.apy ?? 0) * 100).toFixed(1)}%
                                                </div>
                                                <div className="text-[9px] text-black/30 mt-1 max-w-[120px]">
                                                    APY is variable. Past performance is not indicative of future results.
                                                </div>
                                            </td>
                                            <td className="py-4 pr-6 font-mono text-[14px] text-black/60">
                                                +${position.yieldEarned.toFixed(4)}
                                            </td>
                                            <td className="py-4 pr-6">
                                                <span className="inline-flex items-center rounded-full bg-black/[0.04] px-2.5 py-1 text-[11px] font-medium text-black/50">
                                                    {position.isMatured ? "Matured" : `${position.daysRemaining}d left`}
                                                </span>
                                            </td>
                                            <td className="py-4">
                                                <button
                                                    onClick={() => setSelectedPosition(position)}
                                                    aria-label={`Withdraw from ${position.vaultName}`}
                                                    className="flex min-h-[var(--touch-target)] sm:min-h-0 sm:py-1.5 items-center justify-center rounded-lg border border-black/[0.08] px-4 sm:px-3.5 text-[14px] sm:text-[12px] text-black/50 transition-colors hover:border-black/20 hover:text-black active:bg-black/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
                                                >
                                                    Withdraw
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            </motion.div>

            {/* ── Wallet balance ── */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.15 }}
                className="rounded-2xl dash-border bg-white"
            >
                <div className="px-8 pt-7">
                    <h2 className="text-[16px] font-semibold text-black">Wallet balance</h2>
                </div>
                <div className="px-8 pb-8 pt-6">
                    <WalletBalanceTable balances={balances} tokenPrices={tokenPrices} />
                </div>
            </motion.div>

            {/* ── Recent Activity ── */}
            {recentTransactions.length > 0 && (
                <motion.div
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, delay: 0.2 }}
                    className="mt-8 rounded-2xl dash-border bg-white"
                >
                    <div className="px-8 pt-7">
                        <h2 className="text-[16px] font-semibold text-black">Recent Activity</h2>
                    </div>
                    <div className="px-8 pb-8 pt-6 space-y-2">
                        {recentTransactions.map((tx) => (
                            <div key={tx.id} className="flex items-center justify-between rounded-xl bg-black/[0.015] px-5 py-3.5">
                                <div className="flex items-center gap-3">
                                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-black/[0.04] text-black/40">
                                        {tx.type === "Deposit" ? (
                                            <ArrowDownToLine className="h-4 w-4" />
                                        ) : tx.type === "Withdrawal" ? (
                                            <ArrowUpRight className="h-4 w-4" />
                                        ) : (
                                            <Wallet className="h-4 w-4" />
                                        )}
                                    </div>
                                    <div>
                                        <p className="text-[14px] text-black">{tx.type}</p>
                                        <p className="mt-0.5 text-[11px] text-black/30">
                                            {tx.vaultName} · {new Date(tx.timestamp).toLocaleString()}
                                        </p>
                                    </div>
                                </div>
                                <div className="flex items-center gap-4">
                                    <div className="text-right">
                                        <p className="font-mono text-[14px] text-black">{tx.amount} {tx.asset}</p>
                                        <span className={cn(
                                            "inline-block mt-0.5 text-[11px] font-medium",
                                            tx.status === "Confirmed" ? "text-black/40" : tx.status === "Pending" ? "text-amber-500/70" : "text-red-400/70"
                                        )}>
                                            {tx.status}
                                        </span>
                                    </div>
                                    {tx.txHash && (
                                        <a
                                            href={`${currentNetwork.explorerUrl}/tx/${tx.txHash}`}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="flex h-7 w-7 items-center justify-center rounded-md text-black/25 transition-colors hover:bg-black/[0.04] hover:text-black/50"
                                            title="View on explorer"
                                        >
                                            <ArrowUpRight className="h-3.5 w-3.5" />
                                        </a>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                </motion.div>
            )}
            <WithdrawModal
                open={!!selectedPosition}
                onClose={() => setSelectedPosition(null)}
                position={selectedPosition}
            />
            <GuidedTour />
            </AppShell>
        </ProtectedRoute>
    );
}

// ── Wallet Balance Table ─────────────────────────────────────────────────────

function WalletBalanceTable({
    balances,
    tokenPrices,
}: {
    balances: Record<string, number>;
    tokenPrices: { XLM: number; USDC: number };
}) {
    const assets = [
        { code: "XLM", name: "Stellar Lumens", logo: "/xlm.png", balance: balances.XLM ?? 0, price: tokenPrices.XLM },
        { code: "USDC", name: "USD Coin", logo: "/usdc.png", balance: balances.USDC ?? 0, price: tokenPrices.USDC },
    ];

    const hasBalance = assets.some((a) => a.balance > 0);

    if (!hasBalance) {
        return (
            <div className="flex flex-col items-center justify-center py-14 text-center">
                <p className="text-[14px] font-medium text-black/50">No Wallet Balance</p>
                <p className="mt-1.5 text-[13px] text-black/30">
                    Fund your wallet to start depositing into vaults.
                </p>
            </div>
        );
    }

    return (
        <div className="overflow-x-auto pb-2">
            <table className="w-full text-left min-w-[400px]">
                <caption className="sr-only">Wallet Balance Details</caption>
                <thead>
                    <tr className="border-b border-black/[0.05] text-[11px] text-black/35">
                    <th scope="col" className="pb-3.5 pr-6 font-medium">Asset</th>
                    <th scope="col" className="pb-3.5 pr-6 font-medium text-right">Balance</th>
                    <th scope="col" className="pb-3.5 pr-6 font-medium text-right">Price</th>
                    <th scope="col" className="pb-3.5 font-medium text-right">USD Value</th>
                </tr>
            </thead>
            <tbody>
                {assets.map((asset) => (
                    <tr key={asset.code} className="border-b border-black/[0.04] last:border-0">
                        <td className="py-4 pr-6">
                            <div className="flex items-center gap-3">
                                <Image
                                    src={asset.logo}
                                    alt={asset.code}
                                    width={32}
                                    height={32}
                                    className="rounded-full"
                                />
                                <div>
                                    <p className="text-[14px] text-black">{asset.code}</p>
                                    <p className="text-[11px] text-black/30 mt-0.5">{asset.name}</p>
                                </div>
                            </div>
                        </td>
                        <td className="py-4 pr-6 text-right font-mono text-[14px] text-black">
                            {asset.balance.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 7 })}
                        </td>
                        <td className="py-4 pr-6 text-right text-[13px] text-black/40">
                            ${asset.price.toFixed(4)}
                        </td>
                        <td className="py-4 text-right font-mono text-[14px] text-black">
                            ${(asset.balance * asset.price).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
        </div>
    );
}
