"use client";

import { useEffect } from "react";
import { useParams, useRouter, notFound } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { AppShell } from "@/components/app-shell";
import Link from "next/link";
import Image from "next/image";
import { motion } from "framer-motion";
import { useState } from "react";
import { ArrowLeft, TrendingUp, Layers, BarChart3, Info } from "lucide-react";
import { AnimatePresence } from "framer-motion";
import { getVaultById, formatTvl, type MarketType } from "@/lib/mock-vaults";
import { APYChart } from "@/components/vaults/apy-chart";
import { AllocationDonut } from "@/components/vaults/allocation-donut";
import { UserPosition } from "@/components/vaults/user-position";
import { cn } from "@/lib/utils";

const MARKET_LABELS: Record<MarketType, string> = {
    single: "Single Token Market",
    pair:   "Token Pair Market",
    index:  "Index Fund",
};

function InfoTooltip({ text }: { text: string }) {
    const [show, setShow] = useState(false);
    return (
        <div className="relative" onMouseEnter={() => setShow(true)} onMouseLeave={() => setShow(false)}>
            <button
                className="flex h-4 w-4 items-center justify-center rounded-full border border-black/12 text-black/30 hover:border-black/25 hover:text-black/55 transition-colors"
                tabIndex={-1}
            >
                <Info className="h-2.5 w-2.5" />
            </button>
            <AnimatePresence>
                {show && (
                    <motion.div
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 4 }}
                        transition={{ duration: 0.13 }}
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-56 rounded-xl border border-black/8 bg-white px-3 py-2.5 shadow-lg text-xs text-black/50 leading-relaxed pointer-events-none"
                    >
                        {text}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function UtilizationBar({ value, large }: { value: number; large?: boolean }) {
    return (
        <div className="w-full">
            <div className={cn("w-full rounded-full bg-black/[0.06] overflow-hidden", large ? "h-3" : "h-1.5")}>
                <div
                    className={cn(
                        "h-full rounded-full transition-all",
                        value >= 80 ? "bg-black/70" : value >= 50 ? "bg-black/45" : "bg-black/25"
                    )}
                    style={{ width: `${value}%` }}
                />
            </div>
        </div>
    );
}

export default function VaultDetailPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const { id } = useParams();

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    const vault = getVaultById(id?.toString() ?? "");
    if (!vault) notFound();

    return (
        <AppShell>
                {/* Back */}
                <motion.div
                    initial={{ opacity: 0, x: -8 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.3 }}
                    className="mb-7"
                >
                    <Link
                        href="/vaults"
                        className="inline-flex items-center gap-1.5 text-xs text-black/40 hover:text-black transition-colors"
                    >
                        <ArrowLeft className="h-3.5 w-3.5" />
                        All Markets
                    </Link>
                </motion.div>

                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.35, delay: 0.05 }}
                    className="mb-8"
                >
                    <div className="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <div className="flex items-center gap-2 mb-2">
                                <div className="flex items-center">
                                    {vault.tokens.map((t, i) => (
                                        <Image
                                            key={t}
                                            src={`/${t.toLowerCase()}.png`}
                                            alt={t}
                                            width={28}
                                            height={28}
                                            className={cn("rounded-full border-2 border-white", i > 0 && "-ml-2")}
                                        />
                                    ))}
                                </div>
                                <span className="text-[10px] uppercase tracking-widest text-black/35">
                                    {MARKET_LABELS[vault.marketType]}
                                </span>
                            </div>
                            <h1 className="text-2xl text-black sm:text-3xl">{vault.name}</h1>
                            <p className="mt-2 max-w-xl text-sm leading-relaxed text-black/45">
                                {vault.description}
                            </p>
                        </div>
                        <div className="flex items-center gap-2 shrink-0 rounded-xl border border-black/8 px-4 py-2">
                            <TrendingUp className="h-3.5 w-3.5 text-black/35" />
                            <span className="text-xs text-black/45">Target APY</span>
                            <span className="font-mono text-sm text-black">{vault.apyRange}</span>
                        </div>
                    </div>
                </motion.div>

                {/* Key metrics strip */}
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, delay: 0.1 }}
                    className="mb-8 grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4"
                >
                    {[
                        { label: "Current APY", value: `${vault.currentApy.toFixed(1)}%`, tooltip: "The current annualized yield rate for supplying assets to this market." },
                        { label: "TVL", value: formatTvl(vault.tvl), tooltip: "Total Value Locked — the total amount of assets currently deposited in this market." },
                        { label: "Utilization", value: `${vault.utilization}%`, tooltip: "The percentage of supplied assets currently borrowed. Higher utilization often means higher yields for suppliers." },
                        { label: "APY Range", value: vault.apyRange, tooltip: "The historical range of APY this market has offered. Actual rates fluctuate based on supply and demand." },
                    ].map((m) => (
                        <div key={m.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                            <p className="font-mono text-xl text-black sm:text-2xl">{m.value}</p>
                            <div className="mt-0.5 flex items-center gap-1.5">
                                <span className="text-[11px] text-black/35">{m.label}</span>
                                <InfoTooltip text={m.tooltip} />
                            </div>
                        </div>
                    ))}
                </motion.div>

                {/* Two-column layout */}
                <div className="grid gap-5 lg:grid-cols-5">

                    {/* Left: Charts */}
                    <motion.div
                        initial={{ opacity: 0, y: 16 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.15 }}
                        className="space-y-5 lg:col-span-3"
                    >
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/35 uppercase tracking-widest">APY History</p>
                            <APYChart data={vault.apyHistory} />
                        </div>

                        {/* Utilization card */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/35 uppercase tracking-widest">Market Utilization</p>
                            <div className="space-y-4">
                                <div className="flex items-end justify-between">
                                    <div>
                                        <p className="font-mono text-3xl text-black">{vault.utilization}%</p>
                                        <p className="mt-1 text-xs text-black/35">of supplied assets are currently borrowed</p>
                                    </div>
                                    <div className="text-right">
                                        <p className="font-mono text-sm text-black">{formatTvl(vault.tvl * vault.utilization / 100)}</p>
                                        <p className="text-[11px] text-black/35">Borrowed</p>
                                    </div>
                                </div>
                                <UtilizationBar value={vault.utilization} large />
                                <div className="flex justify-between text-xs text-black/35">
                                    <span>Available: {formatTvl(vault.tvl * (1 - vault.utilization / 100))}</span>
                                    <span>Total supplied: {formatTvl(vault.tvl)}</span>
                                </div>
                            </div>
                        </div>

                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/35 uppercase tracking-widest">Allocation</p>
                            <AllocationDonut allocations={vault.allocations} />
                        </div>
                    </motion.div>

                    {/* Right: Info + actions */}
                    <motion.div
                        initial={{ opacity: 0, y: 16 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.2 }}
                        className="space-y-4 lg:col-span-2"
                    >
                        {/* Supported assets */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-3 text-xs text-black/35 uppercase tracking-widest">Supported Assets</p>
                            <div className="flex gap-2 flex-wrap">
                                {vault.supportedAssets
                                    .filter((a) => ["USDC", "XLM"].includes(a))
                                    .map((asset) => (
                                        <div key={asset} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1.5">
                                            <Image
                                                src={`/${asset.toLowerCase()}.png`}
                                                alt={asset}
                                                width={16}
                                                height={16}
                                                className="rounded-full"
                                            />
                                            <span className="text-xs text-black/60">{asset}</span>
                                        </div>
                                    ))}
                            </div>
                        </div>

                        {/* Market info */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5 space-y-3">
                            <p className="text-xs text-black/35 uppercase tracking-widest">Market Info</p>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Market type</span>
                                <span className="text-black">{MARKET_LABELS[vault.marketType]}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">TVL</span>
                                <span className="font-mono text-black">{formatTvl(vault.tvl)}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Utilization</span>
                                <span className="font-mono text-black">{vault.utilization}%</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Withdrawal</span>
                                <span className="text-black">{vault.maturityTerms}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Early exit penalty</span>
                                <span className="text-black">{vault.earlyWithdrawalPenalty}</span>
                            </div>
                        </div>

                        {/* Strategies */}
                        {vault.strategies.length > 0 && (
                            <div className="rounded-2xl border border-black/8 bg-white p-5">
                                <p className="mb-3 text-xs text-black/35 uppercase tracking-widest">Available Strategies</p>
                                <div className="space-y-2">
                                    {vault.strategies.map((strat) => (
                                        <div key={strat.id} className="rounded-xl border border-black/6 px-4 py-3">
                                            <div className="flex items-center justify-between">
                                                <span className="text-sm text-black">{strat.name}</span>
                                                <div className="flex items-center gap-2">
                                                    <span className={cn(
                                                        "rounded-full px-2 py-0.5 text-[10px] font-medium",
                                                        strat.risk === "low" ? "bg-emerald-50 text-emerald-600" :
                                                        strat.risk === "medium" ? "bg-amber-50 text-amber-600" :
                                                        "bg-red-50 text-red-500"
                                                    )}>
                                                        {strat.risk}
                                                    </span>
                                                    <span className="font-mono text-sm text-black">{strat.apy}%</span>
                                                </div>
                                            </div>
                                            <p className="mt-1 text-[11px] text-black/40 leading-relaxed">{strat.description}</p>
                                            <div className="mt-2 flex gap-3 text-[11px] text-black/35">
                                                <span>Lock: {strat.lockDays ? `${strat.lockDays}d` : "None"}</span>
                                                {strat.penaltyPct > 0 && <span>Penalty: {strat.penaltyPct}%</span>}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}

                        {/* User position */}
                        <UserPosition />

                        {/* Supply CTA */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <button
                                disabled
                                className="w-full rounded-xl bg-black py-3.5 text-sm text-white transition-opacity disabled:opacity-35 disabled:cursor-not-allowed"
                            >
                                Supply to {vault.name}
                            </button>
                            <p className="mt-2.5 text-center text-[11px] text-black/30">
                                Supply flow coming soon
                            </p>
                        </div>
                    </motion.div>
                </div>
        </AppShell>
    );
}
