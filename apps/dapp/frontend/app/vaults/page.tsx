"use client";

import { Suspense, useState } from "react";
import { ProtectedRoute } from "@/components/protected-route";
import Link from "next/link";
import Image from "next/image";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { AppShell } from "@/components/app-shell";
import { DepositModal } from "@/components/vault/depositModal";
import { PositionCards } from "@/components/position-cards";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import {
    ArrowUpRight,
    LayoutList,
    LayoutGrid,
    Info,
    Layers,
    TrendingUp,
    BarChart3,
} from "lucide-react";
import { useVaults, type Vault as VaultType, formatTvl } from "@/hooks/useVaults";
import { useVaultFilters, type FilterType } from "@/hooks/use-vault-filters";

// ── Filter tabs ──────────────────────────────────────────────────────────────

const TYPE_FILTERS: { label: string; value: FilterType }[] = [
    { label: "All Markets",   value: "all" },
    { label: "Lending",  value: "Lending" },
    { label: "Optimized Yield",   value: "Optimized Yield" },
    { label: "Leveraged Lending",       value: "Leveraged Lending" },
];

// ── Token icons helper ───────────────────────────────────────────────────────

function TokenIcons({ vaultName, size = 24 }: { vaultName: string; size?: number }) {
    // Basic heuristic to guess token from vault name
    let token = "usdc";
    if (vaultName.toLowerCase().includes("xlm")) token = "xlm";
    return (
        <div className="flex items-center">
            <Image
                src={`/${token}.png`}
                alt={token}
                width={size}
                height={size}
                className="rounded-full border-2 border-white"
            />
        </div>
    );
}

// ── Filter bar ───────────────────────────────────────────────────────────────

function FilterBar({ view, onViewChange, vaults }: { view: "list" | "grid"; onViewChange: (v: "list" | "grid") => void; vaults: VaultType[] }) {
    const { filterType, sortBy, setFilter, setSort } = useVaultFilters(vaults);

    return (
        <div className="mb-6 space-y-3">
            <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex gap-1 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide">
                    {TYPE_FILTERS.map((f) => (
                        <button
                            key={f.value}
                            onClick={() => setFilter(f.value)}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                                filterType === f.value ? "text-black" : "text-black/35 hover:text-black/55"
                            )}
                        >
                            {f.label}
                            {filterType === f.value && (
                                <motion.div
                                    layoutId="vault-tab"
                                    className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                                />
                            )}
                        </button>
                    ))}
                </div>

                <div className="flex items-center gap-2 shrink-0">
                    <span className="text-xs text-black/35">Sort:</span>
                    {(["tvl", "apy"] as const).map((key) => (
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
                            {key === "apy" ? "APY" : "TVL"}
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

// ── List row ─────────────────────────────────────────────────────────────────

function VaultRow({ vault, index, onSelect }: { vault: VaultType; index: number; onSelect: (v: VaultType) => void }) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, delay: index * 0.05 }}
            className="grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm sm:grid-cols-[2fr_1fr_1fr_auto]"
        >
            {/* Market name + tokens */}
            <div className="flex items-center gap-3 min-w-0">
                <TokenIcons vaultName={vault.name} size={28} />
                <div className="min-w-0">
                    <p className="truncate text-sm text-black">{vault.name}</p>
                    <div className="flex items-center gap-1.5 mt-0.5">
                        <span className="text-[11px] text-black/35">{vault.strategy}</span>
                    </div>
                </div>
            </div>

            {/* APY */}
            <div className="hidden sm:block">
                <p className="font-mono text-lg text-black">
                    {vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : <span className="text-gray-400 text-sm">APY TBD</span>}
                </p>
                <div className="flex items-center gap-1">
                    <span className="text-[11px] text-black/35">APY</span>
                    <InfoTooltip text="Annual Percentage Yield — the projected yearly return on your supplied assets." />
                </div>
                {vault.apy !== undefined && (
                    <p className="text-[9px] text-black/40 mt-1">APY is variable and based on recent performance. Past performance is not indicative of future results.</p>
                )}
            </div>

            {/* TVL */}
            <div className="hidden sm:block">
                <p className="font-mono text-sm text-black">
                    {vault.tvl !== undefined ? formatTvl(vault.tvl) : <span className="text-gray-400">TVL unavailable</span>}
                </p>
                <div className="flex items-center gap-1">
                    <span className="text-[11px] text-black/35">TVL</span>
                    <InfoTooltip text="Total Value Locked — the total amount of assets deposited in this market." />
                </div>
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2 shrink-0">
                <span className="sm:hidden font-mono text-sm text-black">
                    {vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : 'TBD'}
                </span>
                <Link href={`/vaults/${vault.id}`}>
                    <button className="h-[var(--touch-target)] sm:h-8 rounded-lg border border-black/10 px-4 sm:px-3 text-sm sm:text-xs text-black/45 hover:border-black/20 hover:text-black transition-colors">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    className="flex h-[var(--touch-target)] sm:h-8 items-center gap-1 rounded-lg bg-black px-4 sm:px-3 text-sm sm:text-xs text-white transition-opacity hover:opacity-75 active:scale-[0.98]"
                >
                    Supply <ArrowUpRight className="h-3 w-3" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Grid card ────────────────────────────────────────────────────────────────

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
                    <TokenIcons vaultName={vault.name} size={32} />
                    <div>
                        <p className="text-sm text-black">{vault.name}</p>
                        <div className="flex items-center gap-1.5 mt-0.5">
                            <span className="text-[11px] text-black/35">{vault.strategy}</span>
                        </div>
                    </div>
                </div>
            </div>

            {/* APY + TVL */}
            <div className="mb-4 flex items-end gap-6">
                <div>
                    <div className="flex items-center gap-1 mb-1">
                        <span className="text-[10px] text-black/35 uppercase tracking-wide">APY</span>
                        <InfoTooltip text="Annual Percentage Yield — the projected yearly return on your supplied assets." />
                    </div>
                    <p className="font-mono text-2xl text-black">
                        {vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : <span className="text-gray-400 text-sm">APY TBD</span>}
                    </p>
                    {vault.apy !== undefined && (
                        <p className="text-[9px] text-black/40 mt-1">APY is variable and based on recent performance. Past performance is not indicative of future results.</p>
                    )}
                </div>
                <div>
                    <div className="flex items-center gap-1 mb-1">
                        <span className="text-[10px] text-black/35 uppercase tracking-wide">TVL</span>
                        <InfoTooltip text="Total Value Locked — the total amount of assets deposited in this market." />
                    </div>
                    <p className="font-mono text-2xl text-black">
                        {vault.tvl !== undefined ? formatTvl(vault.tvl) : <span className="text-gray-400 text-sm">TVL unavailable</span>}
                    </p>
                </div>
            </div>

            <div className="flex-1" />

            {/* Actions */}
            <div className="flex gap-2">
                <Link href={`/vaults/${vault.id}`} className="flex-1">
                    <button className="h-[var(--touch-target)] sm:h-9 w-full rounded-xl border border-black/10 text-sm sm:text-xs text-black/45 hover:border-black/20 hover:text-black transition-colors active:bg-black/5">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    className="flex flex-1 h-[var(--touch-target)] sm:h-9 items-center justify-center gap-1 rounded-xl bg-black text-sm sm:text-xs text-white transition-opacity hover:opacity-75 active:scale-[0.98]"
                >
                    Supply <ArrowUpRight className="h-3.5 w-3.5" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Stats bar ────────────────────────────────────────────────────────────────

function StatsBar({ vaults }: { vaults: VaultType[] }) {
    const totalTvl = vaults.reduce((s, v) => s + (v.tvl || 0), 0);
    const avgApy = vaults.length ? vaults.reduce((s, v) => s + (v.apy || 0), 0) / vaults.length : 0;

    return (
        <div className="mb-7 grid grid-cols-2 gap-3 sm:gap-4">
            {[
                { label: "Total TVL", value: formatTvl(totalTvl), tooltip: "Total Value Locked — the total amount of assets currently deposited across all markets." },
                { label: "Avg APY", value: `${avgApy.toFixed(1)}%`, tooltip: "Average Annual Percentage Yield — the mean return rate across all listed markets." },
            ].map((s) => (
                <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                    <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                    <div className="mt-0.5 flex items-center gap-1.5">
                        <span className="text-[11px] text-black/35">{s.label}</span>
                        <InfoTooltip text={s.tooltip} />
                    </div>
                </div>
            ))}
        </div>
    );
}

// ── Empty state ──────────────────────────────────────────────────────────────

function EmptyState() {
    return (
        <div className="flex flex-col items-center justify-center py-20 text-center">
            <p className="text-sm text-black/40">No markets match this filter</p>
        </div>
    );
}

// ── Main content ─────────────────────────────────────────────────────────────

function VaultsPageContent({ view, onSelect, vaults }: { view: "list" | "grid"; onSelect: (v: VaultType) => void; vaults: VaultType[] }) {
    const { filteredAndSorted } = useVaultFilters(vaults);

    if (filteredAndSorted.length === 0) return <EmptyState />;

    if (view === "grid") {
        return (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
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

function StatsBarWrapper({ vaults }: { vaults: VaultType[] }) {
    const { filteredAndSorted } = useVaultFilters(vaults);
    return <StatsBar vaults={filteredAndSorted} />;
}

// ── Page ─────────────────────────────────────────────────────────────────────

export default function VaultsPage() {
    const { positions } = usePortfolio();
    const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);
    const [view, setView] = useState<"list" | "grid">("list");
    
    const { data: vaults = [], isLoading, isError } = useVaults();

    const MARKET_IDS = ["usdc", "xlm", "xlm-usdc", "defi500"];
    const marketPositions = positions.filter((p) => MARKET_IDS.includes(p.vaultId));

    return (
        <ProtectedRoute>
            <AppShell>
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7"
                >
                    <h1 className="text-2xl text-black sm:text-3xl">Markets</h1>
                    <p className="mt-1 text-sm text-black/40">
                        Supply assets to earn yield across DeFi lending pools, LP positions, and on-chain indexes.
                    </p>
                </motion.div>

                {isLoading && (
                    <div className="space-y-4">
                        <div className="h-20 bg-gray-200 animate-pulse rounded-2xl" />
                        <div className="h-32 bg-gray-200 animate-pulse rounded-2xl" />
                        <div className="h-32 bg-gray-200 animate-pulse rounded-2xl" />
                    </div>
                )}
                {isError && (
                    <div className="text-red-500 py-10 text-center">Failed to load vaults</div>
                )}

                {!isLoading && !isError && (
                    <>
                        {/* Stats */}
                        <Suspense>
                            <StatsBarWrapper vaults={vaults} />
                        </Suspense>

                        {/* Accepted assets */}
                        <div className="mb-7 flex items-center gap-2 flex-wrap">
                            <span className="text-xs text-black/35 mr-1">Supported assets</span>
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

                        {/* Filter bar */}
                        <Suspense>
                            <FilterBar view={view} onViewChange={setView} vaults={vaults} />
                        </Suspense>

                        {/* Market list / grid */}
                        <Suspense>
                            <VaultsPageContent view={view} onSelect={setSelectedVault} vaults={vaults} />
                        </Suspense>
                    </>
                )}

                {/* Open positions */}
                {marketPositions.length > 0 && (
                    <motion.div
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: 0.2 }}
                        className="mt-8"
                    >
                        <h2 className="text-sm text-black mb-3">Your Market Positions</h2>
                        <PositionCards positions={marketPositions} />
                    </motion.div>
                )}

            <DepositModal
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                vault={selectedVault}
            />
        </AppShell>
        </ProtectedRoute>
    );
}
