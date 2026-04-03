"use client";

import { useEffect, Suspense, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import Image from "next/image";
import { motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { Navbar } from "@/components/navbar";
import { DepositModal } from "@/components/vault/depositModal";
import { useWallet } from "@/components/wallet-provider";
import { ArrowUpRight, Users, TrendingUp } from "lucide-react";
import { formatTvl, type Vault as VaultType, type RiskTier } from "@/lib/mock-vaults";
import { useVaultFilters } from "@/hooks/use-vault-filters";

// ── Risk config ───────────────────────────────────────────────────────────────

const RISK_STYLES: Record<RiskTier, { dot: string }> = {
    Conservative: { dot: "bg-black/40" },
    Balanced:     { dot: "bg-black/60" },
    Growth:       { dot: "bg-black" },
    DeFi500:      { dot: "bg-black" },
};

const RISK_LABELS: Record<RiskTier, string> = {
    Conservative: "Conservative",
    Balanced:     "Balanced",
    Growth:       "Growth",
    DeFi500:      "DeFi500 Index",
};

// ── Filter / sort strip ───────────────────────────────────────────────────────

const TIER_FILTERS: { label: string; value: RiskTier | "all" }[] = [
    { label: "All",           value: "all" },
    { label: "Conservative",  value: "Conservative" },
    { label: "Balanced",      value: "Balanced" },
    { label: "Growth",        value: "Growth" },
    { label: "DeFi500",       value: "DeFi500" },
];

function FilterBar() {
    const { filterTier, sortBy, setFilter, setSort } = useVaultFilters();

    return (
        <div className="mb-6 flex items-center justify-between gap-4 flex-wrap">
            {/* Tier tabs */}
            <div className="flex gap-1 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide">
                {TIER_FILTERS.map((f) => (
                    <button
                        key={f.value}
                        onClick={() => setFilter(f.value as RiskTier | "all")}
                        className={cn(
                            "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                            filterTier === f.value
                                ? "text-black"
                                : "text-black/35 hover:text-black/55"
                        )}
                    >
                        {f.label}
                        {filterTier === f.value && (
                            <motion.div
                                layoutId="vault-tab"
                                className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                            />
                        )}
                    </button>
                ))}
            </div>

            {/* Sort */}
            <div className="flex items-center gap-1 shrink-0">
                <span className="text-xs text-black/35 mr-1">Sort:</span>
                {(["apy", "tvl", "risk"] as const).map((key) => (
                    <button
                        key={key}
                        onClick={() => setSort(key)}
                        className={cn(
                            "rounded-lg border px-3 py-1.5 text-xs transition-colors",
                            sortBy === key
                                ? "border-black bg-black text-white"
                                : "border-black/10 text-black/45 hover:border-black/20 hover:text-black"
                        )}
                    >
                        {key === "apy" ? "APY" : key === "tvl" ? "TVL" : "Risk"}
                    </button>
                ))}
            </div>
        </div>
    );
}

// ── Vault row card ────────────────────────────────────────────────────────────

function VaultCard({
    vault,
    index,
    onSelect,
}: {
    vault: VaultType;
    index: number;
    onSelect: (v: VaultType) => void;
}) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, delay: index * 0.06 }}
            className="group grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm sm:grid-cols-[2fr_1fr_1fr_1fr_auto]"
        >
            {/* Name + risk */}
            <div className="flex items-center gap-3 min-w-0">
                <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/5 shrink-0">
                    <div className={cn("h-2 w-2 rounded-full", RISK_STYLES[vault.riskTier].dot)} />
                </div>
                <div className="min-w-0">
                    <p className="truncate text-sm text-black">{vault.name}</p>
                    <p className="text-[11px] text-black/35 mt-0.5">{RISK_LABELS[vault.riskTier]}</p>
                </div>
            </div>

            {/* APY */}
            <div className="hidden sm:block">
                <p className="font-mono text-lg text-black">{vault.currentApy.toFixed(1)}%</p>
                <p className="text-[11px] text-black/35">APY</p>
            </div>

            {/* TVL */}
            <div className="hidden sm:block">
                <p className="font-mono text-sm text-black">{formatTvl(vault.tvl)}</p>
                <p className="text-[11px] text-black/35">TVL</p>
            </div>

            {/* Users */}
            <div className="hidden sm:flex items-center gap-1.5">
                <Users className="h-3 w-3 text-black/30" />
                <span className="font-mono text-sm text-black/60">
                    {vault.userCount.toLocaleString()}
                </span>
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2 shrink-0">
                {/* Mobile APY badge */}
                <span className="sm:hidden font-mono text-sm text-black">
                    {vault.currentApy.toFixed(1)}%
                </span>

                <Link href={`/dashboard/vaults/${vault.id}`}>
                    <button className="h-8 rounded-lg border border-black/10 px-3 text-xs text-black/50 hover:border-black/20 hover:text-black transition-colors">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    className="flex h-8 items-center gap-1 rounded-lg bg-black px-3 text-xs text-white transition-opacity hover:opacity-75"
                >
                    Deposit
                    <ArrowUpRight className="h-3 w-3" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Savings vault row ─────────────────────────────────────────────────────────

function SavingsRow() {
    return (
        <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, delay: 0.35 }}
            className="grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm sm:grid-cols-[2fr_1fr_1fr_1fr_auto]"
        >
            <div className="flex items-center gap-3 min-w-0">
                <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/5 shrink-0">
                    <div className="h-2 w-2 rounded-full bg-black/30" />
                </div>
                <div className="min-w-0">
                    <p className="truncate text-sm text-black">Savings</p>
                    <p className="text-[11px] text-black/35 mt-0.5">Flexible & Goal-based</p>
                </div>
            </div>

            <div className="hidden sm:block">
                <p className="font-mono text-lg text-black">4–12%</p>
                <p className="text-[11px] text-black/35">APY range</p>
            </div>

            <div className="hidden sm:block">
                <p className="font-mono text-sm text-black">$1.2M</p>
                <p className="text-[11px] text-black/35">TVL</p>
            </div>

            <div className="hidden sm:flex items-center gap-1.5">
                <Users className="h-3 w-3 text-black/30" />
                <span className="font-mono text-sm text-black/60">800</span>
            </div>

            <div className="flex items-center gap-2 shrink-0">
                <span className="sm:hidden font-mono text-sm text-black">4–12%</span>
                <Link href="/dashboard/savings">
                    <button className="flex h-8 items-center gap-1 rounded-lg bg-black px-3 text-xs text-white transition-opacity hover:opacity-75">
                        View
                        <ArrowUpRight className="h-3 w-3" />
                    </button>
                </Link>
            </div>
        </motion.div>
    );
}

// ── Stats bar ─────────────────────────────────────────────────────────────────

function StatsBar({ vaults }: { vaults: VaultType[] }) {
    const totalTvl = vaults.reduce((s, v) => s + v.tvl, 0);
    const avgApy = vaults.reduce((s, v) => s + v.currentApy, 0) / vaults.length;
    const totalUsers = vaults.reduce((s, v) => s + v.userCount, 0);

    return (
        <div className="mb-8 grid grid-cols-3 gap-3 sm:gap-4">
            {[
                { label: "Total TVL",   value: formatTvl(totalTvl),              icon: TrendingUp },
                { label: "Avg APY",     value: `${avgApy.toFixed(1)}%`,          icon: TrendingUp },
                { label: "Total Users", value: totalUsers.toLocaleString(),       icon: Users },
            ].map((s) => (
                <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                    <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                    <p className="mt-0.5 text-[11px] text-black/35">{s.label}</p>
                </div>
            ))}
        </div>
    );
}

// ── Supported assets ──────────────────────────────────────────────────────────

function AssetPills({ assets }: { assets: string[] }) {
    return (
        <div className="mb-8 flex items-center gap-2 flex-wrap">
            <span className="text-xs text-black/35 mr-1">Accepted assets</span>
            {assets.map((a) => (
                <div key={a} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1">
                    <Image
                        src={`/${a.toLowerCase()}.png`}
                        alt={a}
                        width={16}
                        height={16}
                        className="rounded-full"
                    />
                    <span className="text-xs text-black/60">{a}</span>
                </div>
            ))}
        </div>
    );
}

// ── Empty state ───────────────────────────────────────────────────────────────

function EmptyState() {
    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex flex-col items-center justify-center py-20 text-center"
        >
            <p className="text-sm text-black/40">No vaults match this filter</p>
        </motion.div>
    );
}

// ── Main content ──────────────────────────────────────────────────────────────

function VaultsPageContent({ onSelect }: { onSelect: (v: VaultType) => void }) {
    const { filteredAndSorted } = useVaultFilters();

    return (
        <>
            <FilterBar />
            <div className="space-y-2.5">
                {filteredAndSorted.length === 0 ? (
                    <EmptyState />
                ) : (
                    filteredAndSorted.map((v, i) => (
                        <VaultCard key={v.id} vault={v} index={i} onSelect={onSelect} />
                    ))
                )}
                <SavingsRow />
            </div>
        </>
    );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function VaultsPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    // Derive all unique assets across vaults for the pill row
    const allAssets = Array.from(
        new Set(
            [
                { id: "conservative", supportedAssets: ["USDC", "USDT"] },
                { id: "balanced",     supportedAssets: ["USDC", "USDT"] },
            ].flatMap((v) => v.supportedAssets)
        )
    );

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-5xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-8"
                >
                    <h1 className="text-2xl text-black sm:text-3xl">Vaults</h1>
                    <p className="mt-1 text-sm text-black/40">
                        Deposit USDC and earn optimised yield across DeFi protocols.
                    </p>
                </motion.div>

                {/* Stats */}
                <Suspense>
                    <StatsBarWrapper />
                </Suspense>

                {/* Asset pills */}
                <AssetPills assets={["USDC", "USDT"]} />

                {/* Vault list */}
                <Suspense>
                    <VaultsPageContent onSelect={setSelectedVault} />
                </Suspense>
            </main>

            <DepositModal
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                vault={selectedVault}
            />
        </div>
    );
}

function StatsBarWrapper() {
    const { filteredAndSorted } = useVaultFilters();
    return <StatsBar vaults={filteredAndSorted} />;
}
