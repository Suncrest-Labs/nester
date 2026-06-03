"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { AppShell } from "@/components/app-shell";
import Link from "next/link";
import Image from "next/image";
import { motion } from "framer-motion";
import { ArrowLeft, TrendingUp, Info } from "lucide-react";
import { AnimatePresence } from "framer-motion";
import { type MarketType } from "@/lib/types/vault";
import { formatTvl } from "@/app/vaults/page";
import { APYChart } from "@/components/vaults/apy-chart";
import { useVault } from "@/hooks/useVault";
import { AllocationDonut } from "@/components/vaults/allocation-donut";
import { UserPosition } from "@/components/vaults/user-position";
import { DepositModal } from "@/components/vault/depositModal";
import { useBlendApy } from "@/hooks/useBlendApy";
import { cn } from "@/lib/utils";
import { RiskGauge } from "@/components/vaults/risk-gauge";

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
                className="flex h-4 w-4 items-center justify-center rounded-full border border-black/12 text-black/40 hover:border-black/25 hover:text-black/70 transition-colors focus-visible:ring-2 focus-visible:ring-black"
                aria-label="More info"
                onFocus={() => setShow(true)}
                onBlur={() => setShow(false)}
            >
                <Info className="h-2.5 w-2.5" aria-hidden="true" />
            </button>
            <AnimatePresence>
                {show && (
                    <motion.div
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 4 }}
                        transition={{ duration: 0.13 }}
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-56 rounded-xl border border-black/8 bg-white px-3 py-2.5 shadow-lg text-xs text-black/70 leading-relaxed pointer-events-none"
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
        <div className="w-full" role="meter" aria-label="Utilization" aria-valuenow={value} aria-valuemin={0} aria-valuemax={100}>
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
    const [depositOpen, setDepositOpen] = useState(false);
    const blendApy = useBlendApy();

    const { vault, isLoading } = useVault(id?.toString() ?? "");

    // Use live Blend APY when available, fall back to vault's static value
    const liveApy = vault?.id === "usdc"
        ? (blendApy.usdcSupplyApy ?? vault?.currentApy)
        : vault?.id === "xlm"
            ? (blendApy.xlmSupplyApy ?? vault?.currentApy)
            : vault?.currentApy;

    useEffect(() => {
        if (!isConnected) router.push("/");
        else if (!vault) router.replace("/vaults");
    }, [isConnected, vault, router]);

    if (!isConnected) return null;
    if (isLoading) return <AppShell><div className="p-8 text-center text-sm text-black/50">Loading...</div></AppShell>;
    if (!vault) return null;

    return (
        <>
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
                        className="inline-flex items-center gap-1.5 text-xs text-black/60 hover:text-black transition-colors focus-visible:ring-2 focus-visible:ring-black"
                    >
                        <ArrowLeft className="h-3.5 w-3.5" aria-hidden="true" />
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
                                <div className="flex items-center" aria-label={`Assets: ${vault.tokens.join(", ")}`}>
                                    {vault.tokens.map((t, i) => (
                                        <Image
                                            key={t}
                                            src={`/${t.toLowerCase()}.png`}
                                            alt=""
                                            width={28}
                                            height={28}
                                            className={cn("rounded-full border-2 border-white", i > 0 && "-ml-2")}
                                        />
                                    ))}
                                </div>
                                <span className="text-[10px] uppercase tracking-widest text-black/60 font-medium">
                                    {MARKET_LABELS[vault.marketType]}
                                </span>
                            </div>
                            <h1 className="text-2xl text-black sm:text-3xl font-semibold">{vault.name}</h1>
                            <p className="mt-2 max-w-xl text-sm leading-relaxed text-black/60">
                                {vault.description}
                            </p>
                        </div>
                        <div className="flex items-center gap-2 shrink-0 rounded-xl border border-black/8 px-4 py-2">
                            <TrendingUp className="h-3.5 w-3.5 text-black/40" aria-hidden="true" />
                            <span className="text-xs text-black/60">Target APY</span>
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
                        { label: "Current APY", value: `${(liveApy ?? vault.currentApy).toFixed(1)}%`, tooltip: blendApy.usdcSupplyApy || blendApy.xlmSupplyApy ? "Live APY fetched from Blend Protocol on-chain." : "The current annualized yield rate for supplying assets to this market." },
                        { label: "TVL", value: formatTvl(vault.tvl), tooltip: "Total Value Locked — the total amount of assets currently deposited in this market." },
                        { label: "Utilization", value: `${vault.utilization}%`, tooltip: "The percentage of supplied assets currently borrowed. Higher utilization often means higher yields for suppliers." },
                        { label: "APY Range", value: vault.apyRange, tooltip: "The historical range of APY this market has offered. Actual rates fluctuate based on supply and demand." },
                    ].map((m) => (
                        <div key={m.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                            <p className="font-mono text-xl text-black sm:text-2xl">{m.value}</p>
                            <div className="mt-0.5 flex items-center gap-1.5">
                                <span className="text-[11px] text-black/60">{m.label}</span>
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
                        <div className="rounded-2xl border border-black/8 bg-white p-5" role="img" aria-label="APY History Chart">
                            <p className="mb-4 text-xs text-black/60 font-semibold uppercase tracking-widest">APY History</p>
                            <APYChart data={vault.apyHistory} />
                        </div>

                        {/* Utilization card */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/60 font-semibold uppercase tracking-widest">Market Utilization</p>
                            <div className="space-y-4">
                                <div className="flex items-end justify-between">
                                    <div>
                                        <p className="font-mono text-3xl text-black">{vault.utilization}%</p>
                                        <p className="mt-1 text-xs text-black/60">of supplied assets are currently borrowed</p>
                                    </div>
                                    <div className="text-right">
                                        <p className="font-mono text-sm text-black">{formatTvl(vault.tvl * vault.utilization / 100)}</p>
                                        <p className="text-[11px] text-black/60">Borrowed</p>
                                    </div>
                                </div>
                                <UtilizationBar value={vault.utilization} large />
                                <div className="flex justify-between text-xs text-black/60">
                                    <span>Available: {formatTvl(vault.tvl * (1 - vault.utilization / 100))}</span>
                                    <span>Total supplied: {formatTvl(vault.tvl)}</span>
                                </div>
                            </div>
                        </div>

                        <div className="rounded-2xl border border-black/8 bg-white p-5" role="img" aria-label="Asset Allocation Donut Chart">
                            <p className="mb-4 text-xs text-black/60 font-semibold uppercase tracking-widest">Allocation</p>
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
                             <p className="mb-3 text-xs text-black/60 font-semibold uppercase tracking-widest">Supported Assets</p>
                             <div className="flex gap-2 flex-wrap">
                                 {vault.supportedAssets
                                     .filter((a) => ["USDC", "XLM"].includes(a))
                                     .map((asset) => (
                                         <div key={asset} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1.5">
                                             <Image
                                                 src={`/${asset.toLowerCase()}.png`}
                                                 alt=""
                                                 width={16}
                                                 height={16}
                                                 className="rounded-full"
                                             />
                                             <span className="text-xs text-black/70 font-medium">{asset}</span>
                                         </div>
                                     ))}
                             </div>
                         </div>

                         {/* Market info */}
                         <div className="rounded-2xl border border-black/8 bg-white p-5 space-y-3">
                             <p className="text-xs text-black/60 font-semibold uppercase tracking-widest">Market Info</p>
                             <div className="flex justify-between text-xs">
                                 <span className="text-black/60">Market type</span>
                                 <span className="text-black font-medium">{MARKET_LABELS[vault.marketType]}</span>
                             </div>
                             <div className="flex justify-between text-xs">
                                 <span className="text-black/60">TVL</span>
                                 <span className="font-mono text-black font-medium">{formatTvl(vault.tvl)}</span>
                             </div>
                             <div className="flex justify-between text-xs">
                                 <span className="text-black/60">Utilization</span>
                                 <span className="font-mono text-black font-medium">{vault.utilization}%</span>
                             </div>
                             <div className="flex justify-between text-xs">
                                 <span className="text-black/60">Withdrawal</span>
                                 <span className="text-black font-medium">{vault.maturityTerms}</span>
                             </div>
                             <div className="flex justify-between text-xs">
                                 <span className="text-black/60">Early exit penalty</span>
                                 <span className="text-black font-medium">{vault.earlyWithdrawalPenalty}</span>
                             </div>
                         </div>

                         {/* Strategies */}
                         {vault.strategies.length > 0 && (
                             <div className="rounded-2xl border border-black/8 bg-white p-5">
                                 <p className="mb-3 text-xs text-black/60 font-semibold uppercase tracking-widest">Available Strategies</p>
                                 <div className="space-y-2">
                                     {vault.strategies.map((strat) => (
                                         <div key={strat.id} className="rounded-xl border border-black/6 px-4 py-3">
                                             <div className="flex items-center justify-between">
                                                 <span className="text-sm text-black font-medium">{strat.name}</span>
                                                 <div className="flex items-center gap-2">
                                                     <span className={cn(
                                                         "rounded-full px-2 py-0.5 text-[10px] font-semibold",
                                                         strat.risk === "low" ? "bg-emerald-50 text-emerald-700" :
                                                         strat.risk === "medium" ? "bg-amber-50 text-amber-700" :
                                                         "bg-red-50 text-red-700"
                                                     )}>
                                                         {strat.risk}
                                                     </span>
                                                     <span className="font-mono text-sm text-black">{strat.apy}%</span>
                                                 </div>
                                             </div>
                                             <p className="mt-1 text-[11px] text-black/60 leading-relaxed">{strat.description}</p>
                                             <div className="mt-2 flex gap-3 text-[11px] text-black/60 font-medium">
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
                             {vault.contractAddress ? (
                                 <button
                                     onClick={() => setDepositOpen(true)}
                                     className="w-full rounded-xl bg-black py-3.5 text-sm text-white font-medium transition-opacity hover:opacity-85 focus-visible:ring-2 focus-visible:ring-black"
                                 >
                                     Supply to {vault.name}
                                 </button>
                             ) : (
                                 <div className="w-full rounded-xl border border-black/8 bg-black/3 py-3.5 text-center text-sm text-black/60 font-medium">
                                     Coming Soon — not yet deployed on testnet
                                 </div>
                             )}
                         </div>
                     </motion.div>
                 </div>

                 {/* Risk Section */}
                 <motion.div
                     initial={{ opacity: 0, y: 16 }}
                     animate={{ opacity: 1, y: 0 }}
                     transition={{ duration: 0.4, delay: 0.25 }}
                     className="mt-8"
                 >
                     <RiskGauge vaultId={id as string} />
                 </motion.div>
             </AppShell>

        <DepositModal
            open={depositOpen}
            onClose={() => setDepositOpen(false)}
            vault={vault}
        />
        </>
    );
}
