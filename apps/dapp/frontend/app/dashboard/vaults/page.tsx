"use client";

import { useEffect, useMemo, useRef, useState, Suspense } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { motion } from "framer-motion";
import {
    ArrowDown,
    ArrowUpRight,
    PiggyBank,
    ShieldCheck,
    TrendingUp,
    Users,
    Vault as VaultIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { Navbar } from "@/components/navbar";
import { DepositModal } from "@/components/vault/depositModal";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import {
    formatTvl,
    type Vault as VaultType,
    type RiskTier,
} from "@/lib/mock-vaults";
import { useVaultFilters, type SortKey } from "@/hooks/use-vault-filters";
import {
    ErrorBoundary,
    PageError,
    ApiErrorState,
    VaultsPageSkeleton,
    EmptyState as UIEmptyState,
} from "@/components/ui";

// -------------------- RISK STYLES --------------------

const RISK_STYLES: Record<RiskTier, { badge: string; dot: string }> = {
    Conservative: {
        badge: "bg-emerald-100 text-emerald-700",
        dot: "bg-emerald-500",
    },
    Balanced: { badge: "bg-blue-100 text-blue-700", dot: "bg-blue-500" },
    Growth: { badge: "bg-orange-100 text-orange-700", dot: "bg-orange-500" },
    DeFi500: { badge: "bg-purple-100 text-purple-700", dot: "bg-purple-500" },
};

// -------------------- COMPONENTS --------------------

function RiskBadge({ tier }: { tier: RiskTier }) {
    return (
        <span
            className={cn(
                "rounded-full px-2.5 py-1 text-[10px] font-medium uppercase tracking-wider",
                RISK_STYLES[tier].badge
            )}
        >
            {tier === "DeFi500" ? "DeFi500 Index" : tier}
        </span>
    );
}

function VaultCard({
    vault,
    index,
    onSelect,
    currentExposure,
}: {
    vault: VaultType;
    index: number;
    onSelect: (v: VaultType) => void;
    currentExposure: number;
}) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: index * 0.08 }}
            className="group relative flex flex-col overflow-hidden rounded-2xl border border-border bg-white p-6 transition-all hover:border-black/15 hover:shadow-xl sm:rounded-3xl sm:p-8"
        >
            <div className="mb-4 flex items-center justify-between">
                <RiskBadge tier={vault.riskTier} />
                <div className="flex items-center gap-1.5 text-muted-foreground">
                    <Users className="h-3 w-3" />
                    <span className="text-[10px] font-medium">
                        {vault.userCount.toLocaleString()}
                    </span>
                </div>
            </div>

            <div className="mb-6">
                <h3 className="mb-2 font-heading text-xl font-light text-foreground sm:text-2xl">
                    {vault.name}
                </h3>
                <p className="line-clamp-2 text-sm leading-relaxed text-muted-foreground">
                    {vault.description}
                </p>
            </div>

            <div className="mb-6 grid grid-cols-2 gap-4">
                <div>
                    <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Target APY
                    </p>
                    <p className="font-heading text-2xl font-light text-emerald-600 sm:text-3xl">
                        {vault.currentApy.toFixed(1)}%
                    </p>
                </div>
                <div>
                    <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Total Value Locked
                    </p>
                    <p className="font-heading text-xl font-light text-foreground sm:text-2xl">
                        {formatTvl(vault.tvl)}
                    </p>
                </div>
            </div>

            <div className="mb-6 space-y-3 rounded-2xl border border-border bg-secondary/20 p-4">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>Maturity</span>
                    <span className="font-medium text-foreground">
                        {vault.maturityTerms}
                    </span>
                </div>
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>Early Exit</span>
                    <span className="font-medium text-foreground">
                        {vault.earlyWithdrawalPenalty}
                    </span>
                </div>
                {currentExposure > 0 && (
                    <div className="flex items-center justify-between border-t border-border/50 pt-2 text-xs text-muted-foreground">
                        <span>Your Exposure</span>
                        <span className="font-medium text-foreground">
                            ${currentExposure.toLocaleString()}
                        </span>
                    </div>
                )}
            </div>

            <div className="mb-6 flex flex-wrap gap-1.5">
                {vault.allocations.slice(0, 3).map((a) => (
                    <span
                        key={a.protocol}
                        className="rounded-full bg-secondary px-2.5 py-1 text-[10px] font-medium uppercase text-foreground/60"
                    >
                        {a.percentage}% {a.protocol}
                    </span>
                ))}
            </div>

            <div className="mt-auto flex items-center justify-between border-t border-border pt-6">
                <Link
                    href={`/dashboard/vaults/${vault.id}`}
                    className="text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
                >
                    View Details
                </Link>
                <button
                    type="button"
                    onClick={() => onSelect(vault)}
                    className="flex items-center gap-1.5 text-sm font-medium text-foreground transition-all hover:gap-2"
                >
                    Deposit <ArrowUpRight className="h-4 w-4" />
                </button>
            </div>
        </motion.div>
    );
}

// -------------------- FILTER CONFIG --------------------

const SORT_BUTTONS: { key: SortKey; label: string }[] = [
    { key: "apy", label: "Yield" },
    { key: "tvl", label: "TVL" },
    { key: "risk", label: "Risk" },
];

const FILTER_BUTTONS: { key: RiskTier | "all"; label: string }[] = [
    { key: "all", label: "All" },
    { key: "Conservative", label: "Conservative" },
    { key: "Balanced", label: "Balanced" },
    { key: "Growth", label: "Growth" },
    { key: "DeFi500", label: "DeFi500" },
];

function VaultsPageContent({
    onSelect,
    exposureByVault,
}: {
    onSelect: (v: VaultType) => void;
    exposureByVault: Record<string, number>;
}) {
    const { sortBy, filterTier, setSort, setFilter, filteredAndSorted } =
        useVaultFilters();

    return (
        <>
            <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.5 }}
                className="mb-8 md:mb-10"
            >
                <div className="mb-10 flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
                    <div className="flex-1">
                        <div className="mb-2 flex items-center gap-2 text-primary">
                            <VaultIcon className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                            <span className="text-[10px] font-mono font-medium uppercase tracking-wider sm:text-xs">
                                Vaults Engine
                            </span>
                        </div>
                        <h1 className="font-heading text-2xl font-light text-foreground sm:text-3xl md:text-4xl">
                            Optimize your Yield
                        </h1>
                        <p className="mt-2 max-w-2xl text-sm leading-relaxed text-muted-foreground sm:text-base">
                            Choose a vault, review lock terms and penalties, and
                            simulate wallet signing before the live Soroban
                            contracts are deployed.
                        </p>
                    </div>

                    <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
                        <div className="flex items-center gap-2 rounded-2xl border border-border bg-white p-1.5 shadow-sm">
                            {SORT_BUTTONS.map(({ key, label }) => (
                                <button
                                    key={key}
                                    onClick={() => setSort(key)}
                                    className={cn(
                                        "rounded-xl px-3 py-1.5 text-xs font-medium transition-all",
                                        sortBy === key
                                            ? "bg-secondary text-foreground"
                                            : "text-muted-foreground hover:text-foreground"
                                    )}
                                >
                                    {label}
                                </button>
                            ))}
                        </div>
                        <div className="h-4 w-px bg-border hidden sm:block" />
                        <div className="flex items-center gap-2 rounded-2xl border border-border bg-white p-1.5 shadow-sm">
                            {FILTER_BUTTONS.map(({ key, label }) => (
                                <button
                                    key={key}
                                    onClick={() => setFilter(key)}
                                    className={cn(
                                        "rounded-xl px-3 py-1.5 text-xs font-medium transition-all",
                                        filterTier === key
                                            ? "bg-secondary text-foreground"
                                            : "text-muted-foreground hover:text-foreground"
                                    )}
                                >
                                    {label}
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
            </motion.div>

            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
                {filteredAndSorted.length === 0 ? (
                    <div className="col-span-full py-20">
                        <UIEmptyState
                            icon={VaultIcon}
                            title="No vaults match your filter"
                            description="Try changing the filter or sorting parameters."
                        />
                    </div>
                ) : (
                    filteredAndSorted.map((v, i) => (
                        <VaultCard
                            key={v.id}
                            vault={v}
                            index={i}
                            onSelect={onSelect}
                            currentExposure={exposureByVault[v.id] ?? 0}
                        />
                    ))
                )}
            </div>
        </>
    );
}

// -------------------- MAIN PAGE --------------------

export default function VaultsPage() {
    const { isConnected, disconnect } = useWallet();
    const { positions, isLoading, fetchErrorStatus, refetch } = usePortfolio();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    const exposureByVault = useMemo(() => {
        return positions.reduce<Record<string, number>>((acc, position) => {
            acc[position.vaultId ?? ""] =
                (acc[position.vaultId ?? ""] ?? 0) + position.currentValue;
            return acc;
        }, {});
    }, [positions]);

    const hasPositions = positions.length > 0;

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-background">
            <Navbar />

            <main className="mx-auto max-w-[1536px] px-4 pb-24 pt-20 md:px-8 md:pb-16 md:pt-28 lg:px-12 xl:px-16">
                {fetchErrorStatus != null ? (
                    <ApiErrorState
                        status={fetchErrorStatus}
                        onRetry={refetch}
                        onReconnect={() => {
                            disconnect();
                            router.push("/");
                        }}
                    />
                ) : isLoading ? (
                    <VaultsPageSkeleton />
                ) : (
                    <ErrorBoundary
                        fallback={({ reset }) => (
                            <PageError
                                onRetry={() => {
                                    refetch();
                                    reset();
                                }}
                            />
                        )}
                    >
                        {!hasPositions && (
                            <motion.div
                                initial={{ opacity: 0, y: 12 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ duration: 0.4 }}
                                className="mb-12 rounded-3xl border border-border bg-white p-6 sm:p-8"
                            >
                                <UIEmptyState
                                    icon={PiggyBank}
                                    title="You haven't deposited into any vaults yet."
                                    description="Browse curated strategies below and open your first position when you are ready."
                                    className="py-6 sm:py-8"
                                />
                            </motion.div>
                        )}

                        <Suspense fallback={<VaultsPageSkeleton />}>
                            <VaultsPageContent
                                onSelect={setSelectedVault}
                                exposureByVault={exposureByVault}
                            />
                        </Suspense>

                        <motion.div
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ duration: 0.5, delay: 0.6 }}
                            className="mt-12 rounded-3xl border border-border bg-secondary/30 p-6 sm:p-8 lg:mt-16"
                        >
                            <div className="grid grid-cols-1 gap-8 sm:grid-cols-3">
                                <div className="flex flex-col gap-3">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-border bg-white">
                                        <TrendingUp className="h-5 w-5 text-emerald-600" />
                                    </div>
                                    <h4 className="font-heading font-medium text-foreground">
                                        Auto-Rebalancing
                                    </h4>
                                    <p className="text-xs leading-relaxed text-muted-foreground">
                                        The deposit flow previews yield terms
                                        while keeping the signing and
                                        submission steps mockable until
                                        contracts are live.
                                    </p>
                                </div>
                                <div className="flex flex-col gap-3">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-border bg-white">
                                        <ShieldCheck className="h-5 w-5 text-blue-600" />
                                    </div>
                                    <h4 className="font-heading font-medium text-foreground">
                                        Risk Guarded
                                    </h4>
                                    <p className="text-xs leading-relaxed text-muted-foreground">
                                        Maturity dates and early withdrawal
                                        penalties are surfaced before every
                                        deposit for full transparency.
                                    </p>
                                </div>
                                <div className="flex flex-col gap-3">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-border bg-white">
                                        <ArrowDown className="h-5 w-5 text-purple-600" />
                                    </div>
                                    <h4 className="font-heading font-medium text-foreground">
                                        Flexible Liquidity
                                    </h4>
                                    <p className="text-xs leading-relaxed text-muted-foreground">
                                        Deposits mint nVault shares 1:1. Later,
                                        the UI can switch to live Soroban calls
                                        seamlessly.
                                    </p>
                                </div>
                            </div>
                        </motion.div>
                    </ErrorBoundary>
                )}
            </main>

            <DepositModal
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                vault={selectedVault}
            />
        </div>
    );
}
