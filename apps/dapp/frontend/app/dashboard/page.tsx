"use client";

import Link from "next/link";
import Image from "next/image";
import { useWallet } from "@/components/wallet-provider";
import { useAuth } from "@/components/auth-provider";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, useCallback } from "react";
import { motion } from "framer-motion";
import {
    ArrowDownToLine,
    ArrowUpRight,
    BarChart3,
    Layers,
    LogIn,
    PiggyBank,
    RefreshCw,
    Shield,
    TrendingUp,
    Vault,
    Wallet,
} from "lucide-react";
import { WithdrawModal } from "@/components/vault-action-modals";
import { cn } from "@/lib/utils";
import { GuidedTour } from "@/components/onboarding/GuidedTour";
import { OnboardingWizard } from "@/components/onboarding/OnboardingWizard";
import { RebalanceSuggestionCard } from "@/components/dashboard/RebalanceSuggestionCard";
import { profileApi } from "@/lib/api/profile";
import { useTokenPrices } from "@/hooks/useTokenPrices";
import { useNetwork } from "@/hooks/useNetwork";
import { AppShell } from "@/components/app-shell";
import { useOfflineStatus } from "@/hooks/useOfflineStatus";
import { formatDistanceToNow } from "date-fns";
import { useVaults, type VaultWithPerf } from "@/hooks/useVaults";
import { useSettlements } from "@/hooks/useSettlements";
import { useVaultHistory } from "@/hooks/useVaultHistory";
import {
    SkeletonStatCard,
    SkeletonPositionsTable,
    SkeletonActivityItem,
} from "@/components/ui/skeletons";
import { usePortfolio } from "@/components/portfolio-provider";
import type { PortfolioPosition } from "@/components/portfolio-provider";

// ── Constants ─────────────────────────────────────────────────────────────────

const CHART_PERIODS = ["1W", "1M", "3M", "All"] as const;
type ChartPeriod = (typeof CHART_PERIODS)[number];

const PERIOD_API_MAP: Record<ChartPeriod, string> = {
    "1W": "7d",
    "1M": "30d",
    "3M": "90d",
    All: "all",
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function getVaultIcon(currency: string) {
    const c = currency.toLowerCase();
    if (c === "usdc") return <PiggyBank className="h-4 w-4" />;
    if (c === "xlm") return <Layers className="h-4 w-4" />;
    return <Vault className="h-4 w-4" />;
}

function fmtUsd(n: number) {
    return n.toLocaleString("en-US", {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    });
}

// Convert ApiVault → PortfolioPosition shape for the existing WithdrawModal
function vaultToPosition(v: VaultWithPerf): PortfolioPosition {
    const balance = parseFloat(v.current_balance) || 0;
    const apy = v.performance?.apy_30d ?? 0;
    const yieldEarned = parseFloat(v.yield_earned) || 0;
    const depositedAt = v.created_at;
    // No lock for now — assume flexible
    return {
        id: v.id,
        vaultId: v.id,
        vaultName: `${v.currency} Vault`,
        asset: v.currency as "USDC" | "XLM",
        principal: parseFloat(v.total_deposited) || 0,
        shares: balance,
        apy,
        depositedAt,
        maturityAt: depositedAt, // flexible — already matured
        earlyWithdrawalPenaltyPct: 0,
        currentValue: balance,
        yieldEarned,
        isMatured: true,
        daysRemaining: 0,
    };
}

// ── Portfolio chart ───────────────────────────────────────────────────────────

function PortfolioChart({
    vaultIds,
    period,
}: {
    vaultIds: string[];
    period: ChartPeriod;
}) {
    const { history, isLoading } = useVaultHistory(vaultIds, PERIOD_API_MAP[period]);

    if (isLoading || history.length === 0) {
        // Render a static placeholder curve when no data yet
        return (
            <svg viewBox="0 0 400 120" className="w-full h-full" preserveAspectRatio="none">
                <defs>
                    <linearGradient id="chartGradFallback" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor="rgb(99,102,241)" stopOpacity="0.10" />
                        <stop offset="100%" stopColor="rgb(99,102,241)" stopOpacity="0" />
                    </linearGradient>
                </defs>
                <path
                    d="M0,95 C40,90 70,80 110,75 C150,70 190,82 240,60 C290,38 340,42 370,36 L400,32 L400,120 L0,120Z"
                    fill="url(#chartGradFallback)"
                />
                <path
                    d="M0,95 C40,90 70,80 110,75 C150,70 190,82 240,60 C290,38 340,42 370,36 L400,32"
                    fill="none"
                    stroke="rgb(99,102,241)"
                    strokeWidth="2"
                    strokeDasharray={isLoading ? "4 4" : undefined}
                    opacity={isLoading ? 0.4 : 1}
                />
            </svg>
        );
    }

    // Normalise to SVG coords
    const values = history.map((p) => p.value);
    const minV = Math.min(...values);
    const maxV = Math.max(...values);
    const range = maxV - minV || 1;
    const W = 400;
    const H = 120;
    const pad = 10;

    // Handle single-point case
    const isSinglePoint = history.length === 1;
    const pts = history.map((p, i) => {
        const x = isSinglePoint ? pad : (i / (history.length - 1)) * (W - pad * 2) + pad;
        const y = H - pad - ((p.value - minV) / range) * (H - pad * 2);
        return { x, y };
    });

    // For single point, render a horizontal line; otherwise use normal path
    const pathPoints = isSinglePoint
        ? [`${pad},${pts[0].y}`, `${W - pad},${pts[0].y}`]
        : pts.map(({ x, y }) => `${x},${y}`);

    const linePath = `M${pathPoints.join(" L")}`;
    const areaPath = `M${pathPoints[0]} L${pathPoints.join(" L")} L${W - pad},${H} L${pad},${H}Z`;

    return (
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-full" preserveAspectRatio="none">
            <defs>
                <linearGradient id="chartGradLive" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="rgb(99,102,241)" stopOpacity="0.15" />
                    <stop offset="100%" stopColor="rgb(99,102,241)" stopOpacity="0" />
                </linearGradient>
            </defs>
            <path d={areaPath} fill="url(#chartGradLive)" />
            <path d={linePath} fill="none" stroke="rgb(99,102,241)" strokeWidth="2" />
        </svg>
    );
}

// ── Sign-in nudge ─────────────────────────────────────────────────────────────

function SignInBanner({
    onSignIn,
    isLoading,
}: {
    onSignIn: () => void;
    isLoading: boolean;
}) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            className="mb-6 flex items-center justify-between rounded-2xl border border-indigo-100 bg-indigo-50 px-6 py-4"
        >
            <div className="flex items-center gap-3">
                <LogIn className="h-4 w-4 text-indigo-500" />
                <p className="text-[13px] text-indigo-700">
                    Sign in with your wallet to see live vault data.
                </p>
            </div>
            <button
                onClick={onSignIn}
                disabled={isLoading}
                className="flex items-center gap-1.5 rounded-full bg-indigo-600 px-4 py-2 text-[12px] font-medium text-white transition-opacity hover:opacity-80 disabled:opacity-50"
            >
                {isLoading ? (
                    <RefreshCw className="h-3 w-3 animate-spin" />
                ) : (
                    <LogIn className="h-3 w-3" />
                )}
                {isLoading ? "Signing in…" : "Sign in"}
            </button>
        </motion.div>
    );
}

// ── Positions table ───────────────────────────────────────────────────────────

function PositionsTable({
    vaults,
    isLoading,
    onWithdraw,
}: {
    vaults: VaultWithPerf[];
    isLoading: boolean;
    onWithdraw: (v: VaultWithPerf) => void;
}) {
    if (isLoading) {
        return <SkeletonPositionsTable />;
    }

    if (vaults.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-14 text-center">
                <p className="text-[14px] font-medium text-black/50">No Positions</p>
                <p className="mt-1.5 text-[13px] text-black/30">
                    Create a position by depositing an asset from your wallet.
                </p>
            </div>
        );
    }

    return (
        <div className="overflow-x-auto">
            <table className="w-full text-left">
                <thead>
                    <tr className="border-b border-black/[0.05] text-[11px] text-black/35">
                        <th className="pb-3.5 pr-6 font-medium">Vault</th>
                        <th className="pb-3.5 pr-6 font-medium">Balance</th>
                        <th className="pb-3.5 pr-6 font-medium">APY (30d)</th>
                        <th className="pb-3.5 pr-6 font-medium">Yield</th>
                        <th className="pb-3.5 pr-6 font-medium">Status</th>
                        <th className="pb-3.5 font-medium" />
                    </tr>
                </thead>
                <tbody>
                    {vaults.map((v) => {
                        const balance = parseFloat(v.current_balance) || 0;
                        const yieldEarned = parseFloat(v.yield_earned) || 0;
                        const apy = (v.performance?.apy_30d ?? 0) * 100;
                        return (
                            <tr key={v.id} className="border-b border-black/[0.04] last:border-0">
                                <td className="py-4 pr-6">
                                    <div className="flex items-center gap-3">
                                        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-black/[0.04] text-black/40">
                                            {getVaultIcon(v.currency)}
                                        </div>
                                        <div>
                                            <p className="text-[14px] text-black">{v.currency} Vault</p>
                                            <p className="mt-0.5 text-[11px] text-black/30 font-mono">
                                                {v.contract_address.slice(0, 8)}…
                                            </p>
                                        </div>
                                    </div>
                                </td>
                                <td className="py-4 pr-6 font-mono text-[14px] text-black">
                                    {fmtUsd(balance)}
                                </td>
                                <td className="py-4 pr-6 text-[14px] text-black">
                                    {apy.toFixed(1)}%
                                </td>
                                <td className="py-4 pr-6 font-mono text-[14px] text-black/60">
                                    +{fmtUsd(yieldEarned)}
                                </td>
                                <td className="py-4 pr-6">
                                    <span
                                        className={cn(
                                            "inline-flex items-center rounded-full px-2.5 py-1 text-[11px] font-medium",
                                            v.status === "active"
                                                ? "bg-emerald-50 text-emerald-600"
                                                : v.status === "paused"
                                                ? "bg-amber-50 text-amber-600"
                                                : "bg-black/[0.04] text-black/50"
                                        )}
                                    >
                                        {v.status.charAt(0).toUpperCase() + v.status.slice(1)}
                                    </span>
                                </td>
                                <td className="py-4">
                                    <button
                                        onClick={() => onWithdraw(v)}
                                        className="rounded-lg border border-black/[0.08] px-3.5 py-1.5 text-[12px] text-black/50 transition-colors hover:border-black/20 hover:text-black"
                                    >
                                        Withdraw
                                    </button>
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        </div>
    );
}

// ── Recent Activity (settlements) ─────────────────────────────────────────────

const STATUS_LABELS: Record<string, string> = {
    initiated: "Initiated",
    liquidity_matched: "Matched",
    fiat_dispatched: "Dispatched",
    confirmed: "Confirmed",
    failed: "Failed",
};

function ActivityFeed({
    settlements,
    isLoading,
}: {
    settlements: ReturnType<typeof useSettlements>["settlements"];
    isLoading: boolean;
}) {

    if (isLoading) {
        return (
            <div className="space-y-2">
                {[0, 1, 2].map((i) => (
                    <SkeletonActivityItem key={i} />
                ))}
            </div>
        );
    }

    if (settlements.length === 0) return null;

    return (
        <div className="space-y-2">
            {settlements.slice(0, 5).map((s) => (
                <div
                    key={s.id}
                    className="flex items-center justify-between rounded-xl bg-black/[0.015] px-5 py-3.5"
                >
                    <div className="flex items-center gap-3">
                        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-black/[0.04] text-black/40">
                            <ArrowUpRight className="h-4 w-4" />
                        </div>
                        <div>
                            <p className="text-[14px] text-black">Off-ramp</p>
                            <p className="mt-0.5 text-[11px] text-black/30">
                                {s.fiat_currency} · {new Date(s.created_at).toLocaleString()}
                            </p>
                        </div>
                    </div>
                    <div className="flex items-center gap-4">
                        <div className="text-right">
                            <p className="font-mono text-[14px] text-black">
                                {s.amount} {s.currency}
                            </p>
                            <span
                                className={cn(
                                    "inline-block mt-0.5 text-[11px] font-medium",
                                    s.status === "confirmed"
                                        ? "text-black/40"
                                        : s.status === "failed"
                                        ? "text-red-400/70"
                                        : "text-amber-500/70"
                                )}
                            >
                                {STATUS_LABELS[s.status] ?? s.status}
                            </span>
                        </div>
                    </div>
                </div>
            ))}
        </div>
    );
}

// ── Wallet Balance Table ──────────────────────────────────────────────────────

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
        <table className="w-full text-left">
            <thead>
                <tr className="border-b border-black/[0.05] text-[11px] text-black/35">
                    <th className="pb-3.5 pr-6 font-medium">Asset</th>
                    <th className="pb-3.5 pr-6 font-medium text-right">Balance</th>
                    <th className="pb-3.5 pr-6 font-medium text-right">Price</th>
                    <th className="pb-3.5 font-medium text-right">USD Value</th>
                </tr>
            </thead>
            <tbody>
                {assets.map((asset) => (
                    <tr key={asset.code} className="border-b border-black/[0.04] last:border-0">
                        <td className="py-4 pr-6">
                            <div className="flex items-center gap-3">
                                <Image src={asset.logo} alt={asset.code} width={32} height={32} className="rounded-full" />
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
                            ${fmtUsd(asset.balance * asset.price)}
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function Dashboard() {
    const { isConnected, address } = useWallet();
    const { isAuthenticated, userId, isSigningIn, signIn } = useAuth();
    const { prices: tokenPrices } = useTokenPrices();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultWithPerf | null>(null);
    const [chartPeriod, setChartPeriod] = useState<ChartPeriod>("1M");
    const [onboardingOpen, setOnboardingOpen] = useState(false);
    const { isOffline, lastSynced } = useOfflineStatus();

    // Live data
    const { vaults, isLoading: vaultsLoading } = useVaults(userId);
    const { settlements, isLoading: settlementsLoading } = useSettlements(userId);

    // Wallet balances still come from portfolio-provider (Horizon direct)
    const { balances } = usePortfolio();

    const positions = useMemo(() => vaults.map(vaultToPosition), [vaults]);

    useEffect(() => {
        if (!isConnected) return;
        profileApi
            .get()
            .then((p) => {
                if (!p.onboarding_completed) setOnboardingOpen(true);
            })
            .catch(() => {});
    }, [isConnected]);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    // Auto sign-in when wallet connects and we have no token yet
    useEffect(() => {
        if (isConnected && address && !isAuthenticated && !isSigningIn) {
            signIn().catch(() => {}); // non-blocking — banner shows on failure
        }
    }, [isConnected, address, isAuthenticated, isSigningIn, signIn]);

    // Aggregate portfolio metrics from live vaults
    const { totalBalanceUsd, totalYield, avgApy } = useMemo(() => {
        if (vaults.length === 0) return { totalBalanceUsd: 0, totalYield: 0, avgApy: 0 };
        
        // Convert each vault balance to USD using token prices
        const totalBalanceUsd = vaults.reduce((s, v) => {
            const balance = parseFloat(v.current_balance) || 0;
            const currency = v.currency.toUpperCase();
            const price = (tokenPrices as unknown as Record<string, number>)[currency] ?? 0;
            return s + (balance * price);
        }, 0);
        
        const totalYield = vaults.reduce(
            (s, v) => s + (parseFloat(v.yield_earned) || 0),
            0
        );
        const apys = vaults
            .map((v) => v.performance?.apy_30d ?? 0)
            .filter((a) => a > 0);
        const avgApy = apys.length ? apys.reduce((a, b) => a + b, 0) / apys.length : 0;
        return { totalBalanceUsd, totalYield, avgApy };
    }, [vaults, tokenPrices]);

    const vaultIds = useMemo(() => vaults.map((v) => v.id), [vaults]);

    const greeting = useMemo(() => {
        const hour = new Date().getHours();
        if (hour < 12) return "Good morning.";
        if (hour < 18) return "Good afternoon.";
        return "Good evening.";
    }, []);

    if (!isConnected) return null;

    return (
        <AppShell>
            {/* ── Greeting + actions ── */}
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
                        className="flex items-center gap-2 rounded-full border border-black/[0.1] bg-white px-5 py-2.5 text-[13px] font-medium text-black/65 transition-all hover:border-black/20 hover:shadow-sm"
                    >
                        <ArrowDownToLine className="h-3.5 w-3.5" />
                        Deposit
                    </Link>
                    <Link
                        href="/savings"
                        className="flex items-center gap-2 rounded-full border border-black/[0.1] bg-white px-5 py-2.5 text-[13px] font-medium text-black/65 transition-all hover:border-black/20 hover:shadow-sm"
                    >
                        <PiggyBank className="h-3.5 w-3.5" />
                        Save
                    </Link>
                </div>
            </motion.div>

            {/* Sign-in nudge when wallet connected but not yet signed in */}
            {isConnected && !isAuthenticated && (
                <SignInBanner onSignIn={signIn} isLoading={isSigningIn} />
            )}

            {/* ── Balance + Chart ── */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.05 }}
                className="mb-10 grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] gap-0 rounded-2xl border border-black/[0.06] bg-white overflow-hidden"
            >
                {/* Left — balance + stats */}
                <div className="p-8 lg:p-10 flex flex-col justify-between">
                    {vaultsLoading ? (
                        <div className="space-y-3">
                            <div className="h-10 w-40 animate-pulse rounded-md bg-black/[0.06]" />
                            <div className="h-3 w-24 animate-pulse rounded-md bg-black/[0.06]" />
                        </div>
                    ) : (
                        <div>
                            <p className="text-[42px] font-light leading-none text-black tracking-[-0.02em]" aria-live="polite">
                                ${fmtUsd(totalBalanceUsd)}
                            </p>
                            <p className="mt-2 text-[12px] text-black/35 tracking-wide">Protocol Balance</p>
                            {lastSynced && (
                                <p className="mt-1.5 text-[11px] text-black/25">
                                    Last updated {formatDistanceToNow(lastSynced)} ago
                                </p>
                            )}
                        </div>
                    )}
                    <div className="mt-8 space-y-5">
                        <div className="flex items-center justify-between">
                            <span className="text-[13px] text-black/60">Position APY</span>
                            {vaultsLoading ? (
                                <div className="h-4 w-12 animate-pulse rounded bg-black/[0.06]" />
                            ) : (
                                <span className="text-[13px] font-medium text-black">
                                    {(avgApy * 100).toFixed(2)}%
                                </span>
                            )}
                        </div>
                        <div className="flex items-center justify-between">
                            <span className="text-[13px] text-black/40">Total earnings</span>
                            {vaultsLoading ? (
                                <div className="h-4 w-16 animate-pulse rounded bg-black/[0.06]" />
                            ) : (
                                <span className="text-[13px] font-medium text-black">
                                    ${fmtUsd(totalYield)}
                                </span>
                            )}
                        </div>
                    </div>
                </div>

                {/* Right — chart */}
                <div className="border-t lg:border-t-0 lg:border-l border-black/[0.06] p-8 lg:p-10 flex flex-col">
                    <div className="flex items-center justify-end gap-0.5 mb-6" role="tablist" aria-label="Chart period">
                        {CHART_PERIODS.map((p) => (
                            <button
                                key={p}
                                role="tab"
                                aria-selected={chartPeriod === p}
                                onClick={() => setChartPeriod(p)}
                                className={cn(
                                    "rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors",
                                    chartPeriod === p
                                        ? "bg-black/[0.06] text-black"
                                        : "text-black/60 hover:text-black/80"
                                )}
                            >
                                {p}
                            </button>
                        ))}
                    </div>
                    <div className="flex-1 min-h-[160px] flex items-end" role="img" aria-label="Protocol balance growth over time">
                        <PortfolioChart vaultIds={vaultIds} period={chartPeriod} />
                    </div>
                    <div className="flex items-center gap-2 mt-4">
                        <span className="h-2 w-2 rounded-full bg-indigo-600" aria-hidden="true" />
                        <span className="text-[11px] text-black/60">Balance</span>
                    </div>
                </div>
            </motion.div>

            {positions.length > 0 && (
                <div className="mb-6 space-y-3">
                    {positions.slice(0, 3).map((p) => (
                        <RebalanceSuggestionCard
                            key={p.id}
                            vaultId={p.vaultId}
                            vaultName={p.vaultName}
                        />
                    ))}
                </div>
            )}

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
                    <Link
                        href="/vaults"
                        data-tour="deposit-cta"
                        className="text-[12px] text-black/60 transition-colors hover:text-black"
                    >
                        + New Position
                    </Link>
                </div>
                <div className="px-8 pb-8 pt-6">
                    <PositionsTable
                        vaults={vaults}
                        isLoading={vaultsLoading && isAuthenticated}
                        onWithdraw={setSelectedVault}
                    />
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

            {/* ── Recent Activity (settlements) ── */}
            <motion.div
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.3, delay: 0.2 }}
                className="mt-8 rounded-2xl dash-border bg-white"
            >
                <div className="px-8 pt-7">
                    <h2 className="text-[16px] font-semibold text-black">Recent Activity</h2>
                </div>
                <div className="px-8 pb-8 pt-6">
                    <ActivityFeed
                        settlements={settlements}
                        isLoading={settlementsLoading && isAuthenticated}
                    />
                    {!settlementsLoading && settlements.length === 0 && (
                        <div className="flex flex-col items-center justify-center py-10 text-center">
                            <p className="text-[14px] font-medium text-black/50">No recent activity</p>
                            <p className="mt-1.5 text-[13px] text-black/30">
                                Off-ramp settlements will appear here once you initiate a withdrawal.
                            </p>
                        </div>
                    )}
                </div>
            </motion.div>

            {/* Withdraw modal — uses existing PortfolioPosition shape */}
            <WithdrawModal
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                position={selectedVault ? vaultToPosition(selectedVault) : null}
            />
            <OnboardingWizard
                open={onboardingOpen}
                onClose={() => setOnboardingOpen(false)}
                onComplete={() => setOnboardingOpen(false)}
            />
            <GuidedTour />
        </AppShell>
    );
}

