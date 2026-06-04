"use client";

import { useEffect, Suspense, useState } from "react";
import { useRouter } from "next/navigation";
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
    Plus,
    Layers,
    TrendingUp,
    BarChart3,
} from "lucide-react";
import { formatTvl, type Vault as VaultType, type MarketType } from "@/lib/mock-vaults";
import { useVaultFilters, type FilterType } from "@/hooks/use-vault-filters";

// ── Market type labels ───────────────────────────────────────────────────────

const MARKET_LABELS: Record<MarketType, string> = {
    single: "Single Token",
    pair:   "Token Pair",
    index:  "Index",
};

// ── Filter tabs ──────────────────────────────────────────────────────────────

const TYPE_FILTERS: { label: string; value: FilterType }[] = [
    { label: "All Markets",   value: "all" },
    { label: "Single Token",  value: "single" },
    { label: "Token Pairs",   value: "pair" },
    { label: "Indexes",       value: "index" },
];

// ── Token icons helper ───────────────────────────────────────────────────────

function TokenIcons({ tokens, size = 24 }: { tokens: string[]; size?: number }) {
    return (
        <div className="flex items-center" aria-label={`Assets: ${tokens.join(", ")}`}>
            {tokens.map((t, i) => (
                <Image
                    key={t}
                    src={`/${t.toLowerCase()}.png`}
                    alt=""
                    width={size}
                    height={size}
                    className={cn("rounded-full border-2 border-white", i > 0 && "-ml-2")}
                />
            ))}
        </div>
    );
}

// ── Utilization bar ──────────────────────────────────────────────────────────

function UtilizationBar({ value }: { value: number }) {
    return (
        <div className="flex items-center gap-2" role="meter" aria-label="Utilization" aria-valuenow={value} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-1.5 flex-1 rounded-full bg-black/[0.06] overflow-hidden">
                <div
                    className={cn(
                        "h-full rounded-full transition-all",
                        value >= 80 ? "bg-black/70" : value >= 50 ? "bg-black/45" : "bg-black/25"
                    )}
                    style={{ width: `${value}%` }}
                />
            </div>
            <span className="font-mono text-[11px] text-black/60 w-8 text-right">{value}%</span>
        </div>
    );
}

// ── Filter bar ───────────────────────────────────────────────────────────────

function FilterBar({ view, onViewChange }: { view: "list" | "grid"; onViewChange: (v: "list" | "grid") => void }) {
    const { filterType, sortBy, setFilter, setSort } = useVaultFilters();

    return (
        <div className="mb-6 space-y-3">
            <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex gap-1 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide" role="tablist" aria-label="Market type filters">
                    {TYPE_FILTERS.map((f) => (
                        <button
                            key={f.value}
                            role="tab"
                            aria-selected={filterType === f.value}
                            onClick={() => setFilter(f.value)}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                                filterType === f.value ? "text-black" : "text-black/60 hover:text-black/80"
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
                    <span className="text-xs text-black/60">Sort:</span>
                    <div className="flex gap-1" role="radiogroup" aria-label="Sort markets by">
                        {(["tvl", "apy", "utilization"] as const).map((key) => (
                            <button
                                key={key}
                                role="radio"
                                aria-checked={sortBy === key}
                                onClick={() => setSort(key)}
                                className={cn(
                                    "rounded-lg border px-3 py-1.5 text-xs transition-colors",
                                    sortBy === key
                                        ? "border-black bg-black text-white"
                                        : "border-black/10 text-black/60 hover:border-black/20 hover:text-black"
                                )}
                            >
                                {key === "apy" ? "APY" : key === "tvl" ? "TVL" : "Utilization"}
                            </button>
                        ))}
                    </div>

                    <div className="ml-1 flex items-center rounded-lg border border-black/10 overflow-hidden" role="group" aria-label="View mode">
                        <button
                            onClick={() => onViewChange("list")}
                            className={cn(
                                "flex h-8 w-8 items-center justify-center transition-colors",
                                view === "list" ? "bg-black text-white" : "text-black/60 hover:text-black"
                            )}
                            aria-label="List view"
                            aria-current={view === "list" ? "true" : "false"}
                        >
                            <LayoutList className="h-3.5 w-3.5" />
                        </button>
                        <button
                            onClick={() => onViewChange("grid")}
                            className={cn(
                                "flex h-8 w-8 items-center justify-center transition-colors",
                                view === "grid" ? "bg-black text-white" : "text-black/60 hover:text-black"
                            )}
                            aria-label="Grid view"
                            aria-current={view === "grid" ? "true" : "false"}
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
                className="flex h-5 w-5 items-center justify-center rounded-full border border-black/12 text-black/40 hover:border-black/25 hover:text-black/60 transition-colors"
                tabIndex={0}
                aria-label="More info"
                onFocus={() => setShow(true)}
                onBlur={() => setShow(false)}
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
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-56 rounded-xl border border-black/8 bg-white px-3 py-2.5 shadow-lg text-xs text-black/60 leading-relaxed pointer-events-none"
                    >
                        {text}
                        <div className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-black/8" />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// ── Market type icon ─────────────────────────────────────────────────────────

function MarketTypeIcon({ type }: { type: MarketType }) {
    switch (type) {
        case "single":
            return <TrendingUp className="h-3.5 w-3.5" aria-hidden="true" />;
        case "pair":
            return <BarChart3 className="h-3.5 w-3.5" aria-hidden="true" />;
        case "index":
            return <Layers className="h-3.5 w-3.5" aria-hidden="true" />;
    }
}

// ── List row ─────────────────────────────────────────────────────────────────

function VaultRow({ vault, index, onSelect }: { vault: VaultType; index: number; onSelect: (v: VaultType) => void }) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, delay: index * 0.05 }}
            className="grid grid-cols-[1fr_auto] items-center gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4 transition-all hover:border-black/18 hover:shadow-sm sm:grid-cols-[2fr_1fr_1fr_1fr_auto]"
        >
            {/* Market name + tokens */}
            <div className="flex items-center gap-3 min-w-0">
                <TokenIcons tokens={vault.tokens} size={28} />
                <div className="min-w-0">
                    <p className="truncate text-sm text-black font-medium">{vault.name}</p>
                    <div className="flex items-center gap-1.5 mt-0.5">
                        <MarketTypeIcon type={vault.marketType} />
                        <span className="text-[11px] text-black/60">{MARKET_LABELS[vault.marketType]}</span>
                    </div>
                </div>
            </div>

            {/* APY */}
            <div className="hidden sm:block">
                <p className="font-mono text-lg text-black">{vault.currentApy.toFixed(1)}%</p>
                <div className="flex items-center gap-1">
                    <span className="text-[11px] text-black/60">APY</span>
                    <InfoTooltip text="Annual Percentage Yield — the projected yearly return on your supplied assets." />
                </div>
            </div>

            {/* TVL */}
            <div className="hidden sm:block">
                <p className="font-mono text-sm text-black">{formatTvl(vault.tvl)}</p>
                <div className="flex items-center gap-1">
                    <span className="text-[11px] text-black/60">TVL</span>
                    <InfoTooltip text="Total Value Locked — the total amount of assets deposited in this market." />
                </div>
            </div>

            {/* Utilization */}
            <div className="hidden sm:block w-28">
                <UtilizationBar value={vault.utilization} />
                <div className="flex items-center gap-1 mt-1">
                    <span className="text-[11px] text-black/60">Utilization</span>
                    <InfoTooltip text="The percentage of supplied assets currently borrowed. Higher utilization means more demand and often higher yields." />
                </div>
            </div>

            {/* Actions */}
            <div className="flex items-center gap-2 shrink-0">
                <span className="sm:hidden font-mono text-sm text-black">{vault.currentApy.toFixed(1)}%</span>
                <Link href={`/vaults/${vault.id}`}>
                    <button className="h-8 rounded-lg border border-black/10 px-3 text-xs text-black/60 hover:border-black/20 hover:text-black transition-colors focus-visible:ring-2 focus-visible:ring-black">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    aria-label={`Supply assets to ${vault.name}`}
                    className="flex h-8 items-center gap-1 rounded-lg bg-black px-3 text-xs text-white transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                >
                    Supply <ArrowUpRight className="h-3 w-3" aria-hidden="true" />
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
                    <TokenIcons tokens={vault.tokens} size={32} />
                    <div>
                        <p className="text-sm text-black font-medium">{vault.name}</p>
                        <div className="flex items-center gap-1.5 mt-0.5">
                            <MarketTypeIcon type={vault.marketType} />
                            <span className="text-[11px] text-black/60">{MARKET_LABELS[vault.marketType]}</span>
                        </div>
                    </div>
                </div>
                <InfoTooltip text={vault.description} />
            </div>

            {/* APY + TVL */}
            <div className="mb-4 flex items-end gap-6">
                <div>
                    <div className="flex items-center gap-1 mb-1">
                        <span className="text-[10px] text-black/60 uppercase tracking-wide">APY</span>
                        <InfoTooltip text="Annual Percentage Yield — the projected yearly return on your supplied assets." />
                    </div>
                    <p className="font-mono text-2xl text-black">{vault.currentApy.toFixed(1)}%</p>
                    <p className="text-[11px] text-black/60 mt-0.5">{vault.apyRange} range</p>
                </div>
                <div>
                    <div className="flex items-center gap-1 mb-1">
                        <span className="text-[10px] text-black/60 uppercase tracking-wide">TVL</span>
                        <InfoTooltip text="Total Value Locked — the total amount of assets deposited in this market." />
                    </div>
                    <p className="font-mono text-2xl text-black">{formatTvl(vault.tvl)}</p>
                </div>
            </div>

            {/* Utilization */}
            <div className="mb-5">
                <div className="flex items-center gap-1 mb-2">
                    <span className="text-[10px] text-black/60 uppercase tracking-wide">Utilization</span>
                    <InfoTooltip text="The percentage of supplied assets currently borrowed. Higher utilization means more demand and often higher yields." />
                </div>
                <UtilizationBar value={vault.utilization} />
            </div>

            {/* Maturity */}
            <p className="mb-5 text-xs text-black/50 leading-relaxed flex-1">{vault.maturityTerms}</p>

            {/* Actions */}
            <div className="flex gap-2">
                <Link href={`/vaults/${vault.id}`} className="flex-1">
                    <button className="h-9 w-full rounded-xl border border-black/10 text-xs text-black/60 hover:border-black/20 hover:text-black transition-colors focus-visible:ring-2 focus-visible:ring-black">
                        Details
                    </button>
                </Link>
                <button
                    onClick={() => onSelect(vault)}
                    aria-label={`Supply assets to ${vault.name}`}
                    className="flex flex-1 h-9 items-center justify-center gap-1 rounded-xl bg-black text-xs text-white transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                >
                    Supply <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
                </button>
            </div>
        </motion.div>
    );
}

// ── Stats bar ────────────────────────────────────────────────────────────────

function StatsBar({ vaults }: { vaults: VaultType[] }) {
    const totalTvl = vaults.reduce((s, v) => s + v.tvl, 0);
    const avgApy = vaults.length ? vaults.reduce((s, v) => s + v.currentApy, 0) / vaults.length : 0;
    const avgUtil = vaults.length ? vaults.reduce((s, v) => s + v.utilization, 0) / vaults.length : 0;

    return (
        <div className="mb-7 grid grid-cols-3 gap-3 sm:gap-4">
            {[
                { label: "Total TVL", value: formatTvl(totalTvl), tooltip: "Total Value Locked — the total amount of assets currently deposited across all markets." },
                { label: "Avg APY", value: `${avgApy.toFixed(1)}%`, tooltip: "Average Annual Percentage Yield — the mean return rate across all listed markets." },
                { label: "Avg Utilization", value: `${avgUtil.toFixed(0)}%`, tooltip: "Average utilization rate — the percentage of supplied assets that are currently being borrowed or actively deployed." },
            ].map((s) => (
                <div key={s.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                    <p className="font-mono text-xl text-black sm:text-2xl">{s.value}</p>
                    <div className="mt-0.5 flex items-center gap-1.5">
                        <span className="text-[11px] text-black/60">{s.label}</span>
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
            <p className="text-sm text-black/50">No markets match this filter</p>
        </div>
    );
}

// ── Main content ─────────────────────────────────────────────────────────────

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

// ── Page ─────────────────────────────────────────────────────────────────────

export default function VaultsPage() {
    const { isConnected } = useWallet();
    const { positions } = usePortfolio();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);
    const [view, setView] = useState<"list" | "grid">("list");

    const MARKET_IDS = ["usdc", "xlm", "xlm-usdc", "defi500"];
    const marketPositions = positions.filter((p) => MARKET_IDS.includes(p.vaultId));

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    return (
        <AppShell>
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"
                >
                    <div>
                        <h1 className="text-2xl text-black dark:text-white sm:text-3xl">Markets</h1>
                        <p className="mt-1 text-sm text-black/40 dark:text-white/50">
                            Supply assets to earn yield across DeFi lending pools, LP positions, and on-chain indexes.
                        </p>
                    </div>
                    <Link
                        href="/dashboard/vaults/create"
                        className="flex h-[var(--touch-target)] sm:h-10 items-center gap-2 rounded-xl bg-primary px-5 text-sm font-semibold text-primary-foreground hover:opacity-90 transition-opacity shrink-0"
                    >
                        <Plus className="h-4 w-4" />
                        Create Vault
                    </Link>
                </motion.div>

                {/* Stats */}
                <Suspense>
                    <StatsBarWrapper />
                </Suspense>

                {/* Accepted assets */}
                <div className="mb-7 flex items-center gap-2 flex-wrap" aria-label="Accepted assets">
                    <span className="text-xs text-black/60 mr-1">Supported assets</span>
                    {["USDC", "XLM"].map((a) => (
                        <div key={a} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1">
                            <Image
                                src={`/${a.toLowerCase()}.png`}
                                alt=""
                                width={16}
                                height={16}
                                className="rounded-full"
                            />
                            <span className="text-xs text-black/70 font-medium">{a}</span>
                        </div>
                    ))}
                </div>

                {/* Filter bar */}
                <Suspense>
                    <FilterBar view={view} onViewChange={setView} />
                </Suspense>

                {/* Market list / grid */}
                <Suspense>
                    <VaultsPageContent view={view} onSelect={setSelectedVault} />
                </Suspense>

                {/* Open positions */}
                {marketPositions.length > 0 && (
                    <motion.div
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: 0.2 }}
                        className="mt-8"
                    >
                        <h2 className="text-sm font-semibold text-black mb-3">Your Market Positions</h2>
                        <PositionCards positions={marketPositions} />
                    </motion.div>
                )}

            <DepositModal
                key={selectedVault?.id ?? "none"}
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                vault={selectedVault}
            />
        </AppShell>
    );
}
