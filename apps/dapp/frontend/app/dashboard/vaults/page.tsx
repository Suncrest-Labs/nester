"use client";

import { useEffect, Suspense, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import Image from "next/image";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { Navbar } from "@/components/navbar";
import { DepositModal } from "@/components/vault/depositModal";
import { useWallet } from "@/components/wallet-provider";
import { ArrowUpRight, Users, LayoutList, LayoutGrid, Info } from "lucide-react";
import { formatTvl, type Vault as VaultType, type RiskTier } from "@/lib/mock-vaults";
import { useVaultFilters } from "@/hooks/use-vault-filters";

// ── Risk config ───────────────────────────────────────────────────────────────

const RISK_DOT: Record<RiskTier, string> = {
    Conservative: "bg-black/35",
    Balanced:     "bg-black/55",
    Growth:       "bg-black/80",
    DeFi500:      "bg-black",
};

const RISK_LABELS: Record<RiskTier, string> = {
    Conservative: "Conservative",
    Balanced:     "Balanced",
    Growth:       "Growth",
    DeFi500:      "DeFi500 Index",
};

// ── Filter / sort + view toggle strip ────────────────────────────────────────

const TIER_FILTERS: { label: string; value: RiskTier | "all" }[] = [
    { label: "All",          value: "all" },
    { label: "Conservative", value: "Conservative" },
    { label: "Balanced",     value: "Balanced" },
    { label: "Growth",       value: "Growth" },
    { label: "DeFi500",      value: "DeFi500" },
];

function FilterBar({ view, onViewChange }: { view: "list" | "grid"; onViewChange: (v: "list" | "grid") => void }) {
    const { filterTier, sortBy, setFilter, setSort } = useVaultFilters();

    return (
        <div className="mb-6 space-y-3">
            {/* Tabs row */}
            <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex gap-1 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide">
                    {TIER_FILTERS.map((f) => (
                        <button
                            key={f.value}
                            onClick={() => setFilter(f.value as RiskTier | "all")}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                                filterTier === f.value ? "text-black" : "text-black/35 hover:text-black/55"
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

                {/* Sort + view toggle */}
                <div className="flex items-center gap-2 shrink-0">
                    <span className="text-xs text-black/35">Sort:</span>
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

                    <div className="ml-1 flex items-center rounded-lg border border-black/10 overflow-hidden">
                        <button
                            onClick={() => onViewChange("list")}
                            className={cn(
                                "flex h-8 w-8 items-center justify-center transition-colors",
                                view === "list" ? "bg-black text-white" : "text-black/35 hover:text-black"
                            )}
                            title="List view"
                        >
                            <LayoutList className="h-3.5 w-3.5" />
                        </button>
                        <button
                            onClick={() => onViewChange("grid")}
                            className={cn(
                                "flex h-8 w-8 items-center justify-center transition-colors",
                                view === "grid" ? "bg-black text-white" : "text-black/35 hover:text-black"
                            )}
                            title="Grid view"
                        >
                            <LayoutGrid className="h-3.5 w-3.5" />
                        </button>
                    </div>
                </div>
            </div>
        </div>
    );
}

// ── Info tooltip ──────────────────────────────────────────────────────────────

function InfoTooltip({ text }: { text: string }) {
    const [show, setShow] = useState(false);
    return (
        <div className="relative" onMouseEnter={() => setShow(true)} onMouseLeave={() => setShow(false)}>
            <button
                className="flex h-5 w-5 items-center justify-center rounded-full border border-black/12 text-black/30 hover:border-black/25 hover:text-black/55 transition-colors"
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
                        <div className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-black/8" />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// ── List row card ─────────────────────────────────────────────────────────────

function VaultRow({ vault, index, onSelect }: { vault: VaultType; index: number; onSelect: (v: VaultType) => void }) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, delay: index * 0.05 }}
            className="grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm sm:grid-cols-[2fr_1fr_1fr_1fr_auto]"
        >
            {/* Name */}
            <div className="flex items-center gap-3 min-w-0">
                <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/5 shrink-0">
                    <div className={cn("h-2 w-2 rounded-full", RISK_DOT[vault.riskTier])} />
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
                <Users className="h-3 w-3 text-black/25" />
                <span className="font-mono text-sm text-black/55">{vault.userCount.toLocaleString()}</span>
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2 shrink-0">
                <span className="sm:hidden font-mono text-sm text-black">{vault.currentApy.toFixed(1)}%</span>
                <Link href={`/dashboard/vaults/${vault.id}`}>
                    <button className="h-8 rounded-lg border border-black/10 px-3 text-xs text-black/45 hover:border-black/20 hover:text-black transition-colors">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    className="flex h-8 items-center gap-1 rounded-lg bg-black px-3 text-xs text-white transition-opacity hover:opacity-75"
                >
                    Deposit <ArrowUpRight className="h-3 w-3" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Grid card ─────────────────────────────────────────────────────────────────

function VaultGridCard({ vault, index, onSelect }: { vault: VaultType; index: number; onSelect: (v: VaultType) => void }) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 14 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, delay: index * 0.06 }}
            className="flex flex-col rounded-2xl border border-black/8 bg-white px-6 py-5 transition-all hover:border-black/18 hover:shadow-md"
        >
            {/* Header */}
            <div className="mb-5 flex items-start justify-between gap-2">
                <div className="flex items-center gap-3">
                    <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-black/5 shrink-0">
                        <div className={cn("h-2 w-2 rounded-full", RISK_DOT[vault.riskTier])} />
                    </div>
                    <div>
                        <p className="text-sm text-black">{vault.name}</p>
                        <p className="text-[11px] text-black/35 mt-0.5">{RISK_LABELS[vault.riskTier]}</p>
                    </div>
                </div>
                <InfoTooltip text={vault.description} />
            </div>

            {/* APY + TVL */}
            <div className="mb-5 flex items-end gap-6">
                <div>
                    <p className="text-[10px] text-black/35 uppercase tracking-wide mb-1">APY</p>
                    <p className="font-mono text-2xl text-black">{vault.currentApy.toFixed(1)}%</p>
                    <p className="text-[11px] text-black/35 mt-0.5">{vault.apyRange} range</p>
                </div>
                <div>
                    <p className="text-[10px] text-black/35 uppercase tracking-wide mb-1">TVL</p>
                    <p className="font-mono text-2xl text-black">{formatTvl(vault.tvl)}</p>
                </div>
            </div>

            {/* Supported assets */}
            <div className="mb-5 flex items-center gap-1.5">
                {vault.supportedAssets
                    .filter((a) => ["USDC", "XLM"].includes(a))
                    .map((a) => (
                        <Image
                            key={a}
                            src={`/${a.toLowerCase()}.png`}
                            alt={a}
                            width={22}
                            height={22}
                            className="rounded-full"
                        />
                    ))}
                <div className="flex items-center gap-1 ml-1">
                    <Users className="h-3 w-3 text-black/25" />
                    <span className="font-mono text-xs text-black/45">{vault.userCount.toLocaleString()}</span>
                </div>
            </div>

            {/* Maturity / penalty */}
            <p className="mb-5 text-xs text-black/40 leading-relaxed flex-1">{vault.maturityTerms}</p>

            {/* Actions */}
            <div className="flex gap-2">
                <Link href={`/dashboard/vaults/${vault.id}`} className="flex-1">
                    <button className="h-9 w-full rounded-xl border border-black/10 text-xs text-black/45 hover:border-black/20 hover:text-black transition-colors">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    className="flex flex-1 h-9 items-center justify-center gap-1 rounded-xl bg-black text-xs text-white transition-opacity hover:opacity-75"
                >
                    Deposit <ArrowUpRight className="h-3.5 w-3.5" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Stats bar ─────────────────────────────────────────────────────────────────

function StatsBar({ vaults }: { vaults: VaultType[] }) {
    const totalTvl = vaults.reduce((s, v) => s + v.tvl, 0);
    const avgApy = vaults.length ? vaults.reduce((s, v) => s + v.currentApy, 0) / vaults.length : 0;
    const totalUsers = vaults.reduce((s, v) => s + v.userCount, 0);

    return (
        <div className="mb-7 grid grid-cols-3 gap-3 sm:gap-4">
            {[
                { label: "Total TVL",   value: formatTvl(totalTvl) },
                { label: "Avg APY",     value: `${avgApy.toFixed(1)}%` },
                { label: "Total Users", value: totalUsers.toLocaleString() },
            ].map((s) => (
                <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                    <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                    <p className="mt-0.5 text-[11px] text-black/35">{s.label}</p>
                </div>
            ))}
        </div>
    );
}

// ── Empty state ───────────────────────────────────────────────────────────────

function EmptyState() {
    return (
        <div className="flex flex-col items-center justify-center py-20 text-center">
            <p className="text-sm text-black/40">No vaults match this filter</p>
        </div>
    );
}

// ── Main content ──────────────────────────────────────────────────────────────

function VaultsPageContent({ view, onSelect }: { view: "list" | "grid"; onSelect: (v: VaultType) => void }) {
    const { filteredAndSorted } = useVaultFilters();

    if (filteredAndSorted.length === 0) return <EmptyState />;

    if (view === "grid") {
        return (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                {filteredAndSorted.map((v, i) => (
                    <VaultGridCard key={v.id} vault={v} index={i} onSelect={onSelect} />
                ))}
            </div>
        );
    }

    return (
        <div className="space-y-2.5">
            {filteredAndSorted.map((v, i) => (
                <VaultRow key={v.id} vault={v} index={i} onSelect={onSelect} />
            ))}
        </div>
    );
}

function StatsBarWrapper() {
    const { filteredAndSorted } = useVaultFilters();
    return <StatsBar vaults={filteredAndSorted} />;
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function VaultsPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);
    const [view, setView] = useState<"list" | "grid">("list");

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-5xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">

                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7"
                >
                    <h1 className="text-2xl text-black sm:text-3xl">Vaults</h1>
                    <p className="mt-1 text-sm text-black/40">
                        Deposit USDC or XLM and earn optimised yield across DeFi protocols.
                    </p>
                </motion.div>

                {/* Stats */}
                <Suspense>
                    <StatsBarWrapper />
                </Suspense>

                {/* Accepted assets */}
                <div className="mb-7 flex items-center gap-2 flex-wrap">
                    <span className="text-xs text-black/35 mr-1">Accepted assets</span>
                    {["USDC", "XLM"].map((a) => (
                        <div key={a} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1">
                            <Image
                                src={`/${a.toLowerCase()}.png`}
                                alt={a}
                                width={16}
                                height={16}
                                className="rounded-full"
                            />
                            <span className="text-xs text-black/55">{a}</span>
                        </div>
                    ))}
                </div>

                {/* Filter bar with view toggle */}
                <Suspense>
                    <FilterBar view={view} onViewChange={setView} />
                </Suspense>

                {/* Vault list / grid */}
                <Suspense>
                    <VaultsPageContent view={view} onSelect={setSelectedVault} />
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
