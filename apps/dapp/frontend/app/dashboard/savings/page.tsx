"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { motion, AnimatePresence } from "framer-motion";
import {
    Lock,
    Unlock,
    Sliders,
    TrendingUp,
    Shield,
    Zap,
    ChevronDown,
    ArrowUpRight,
    Info,
    Clock,
} from "lucide-react";
import { Navbar } from "@/components/navbar";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { cn } from "@/lib/utils";

// ── Types ─────────────────────────────────────────────────────────────────────

type SavingsVaultType = "flexible" | "locked" | "custom";

interface SavingsVault {
    id: string;
    type: SavingsVaultType;
    name: string;
    description: string;
    apy: number;
    apyLabel: string;
    lockDays: number | null;
    minDeposit: number;
    penaltyPct: number;
    badge: string;
    features: string[];
}

// ── Vault Definitions ─────────────────────────────────────────────────────────

const SAVINGS_VAULTS: SavingsVault[] = [
    {
        id: "flexible-savings",
        type: "flexible",
        name: "Flexible Savings",
        description:
            "Earn yield on your USDC with no lockup period. Deposit and withdraw anytime — perfect for an emergency fund or short-term savings.",
        apy: 0.052,
        apyLabel: "4–6%",
        lockDays: null,
        minDeposit: 10,
        penaltyPct: 0,
        badge: "No lockup",
        features: ["Withdraw anytime", "No early exit fee", "Daily yield accrual"],
    },
    {
        id: "locked-30",
        type: "locked",
        name: "30-Day Lock",
        description:
            "Lock your USDC for 30 days and earn a higher fixed APY. Best for funds you won't need in the short term.",
        apy: 0.082,
        apyLabel: "7–9%",
        lockDays: 30,
        minDeposit: 50,
        penaltyPct: 1,
        badge: "30-day lock",
        features: ["Fixed APY", "Higher yield", "1% early exit fee"],
    },
    {
        id: "locked-90",
        type: "locked",
        name: "90-Day Lock",
        description:
            "Commit your USDC for 90 days for our best fixed savings rate. Ideal for funds you're setting aside for a longer goal.",
        apy: 0.115,
        apyLabel: "10–13%",
        lockDays: 90,
        minDeposit: 100,
        penaltyPct: 2,
        badge: "90-day lock",
        features: ["Best fixed APY", "Compounding yield", "2% early exit fee"],
    },
    {
        id: "custom-savings",
        type: "custom",
        name: "Custom Savings Plan",
        description:
            "Choose your own lock period and target amount. Build a savings plan around your goals — holiday fund, house deposit, or anything else.",
        apy: 0.09,
        apyLabel: "Variable",
        lockDays: null,
        minDeposit: 25,
        penaltyPct: 0,
        badge: "Custom",
        features: ["Set your own goal", "Flexible duration", "Goal tracking"],
    },
];

const TYPE_ICONS: Record<SavingsVaultType, React.ElementType> = {
    flexible: Unlock,
    locked: Lock,
    custom: Sliders,
};

// ── Vault Card ────────────────────────────────────────────────────────────────

function SavingsVaultCard({
    vault,
    index,
    onDeposit,
}: {
    vault: SavingsVault;
    index: number;
    onDeposit: (v: SavingsVault) => void;
}) {
    const Icon = TYPE_ICONS[vault.type];

    return (
        <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, delay: index * 0.07 }}
            className="group flex flex-col rounded-2xl border border-black/8 bg-white p-6 transition-all hover:border-black/20 hover:shadow-md"
        >
            {/* Header */}
            <div className="mb-5 flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/5 shrink-0">
                        <Icon className="h-4.5 w-4.5 text-black/60" />
                    </div>
                    <div>
                        <h3 className="font-heading text-base font-medium text-black">
                            {vault.name}
                        </h3>
                        <span className="text-[10px] font-medium bg-black/6 text-black/50 rounded-full px-2 py-0.5">
                            {vault.badge}
                        </span>
                    </div>
                </div>
                <div className="text-right shrink-0">
                    <p className="font-heading text-2xl font-light text-black">
                        {vault.apyLabel}
                    </p>
                    <p className="text-[10px] text-black/40 uppercase tracking-wide">APY</p>
                </div>
            </div>

            {/* Description */}
            <p className="mb-5 text-sm leading-relaxed text-black/55 flex-1">
                {vault.description}
            </p>

            {/* Features */}
            <div className="mb-5 space-y-1.5">
                {vault.features.map((f) => (
                    <div key={f} className="flex items-center gap-2">
                        <div className="h-1 w-1 rounded-full bg-black/40 shrink-0" />
                        <span className="text-xs text-black/50">{f}</span>
                    </div>
                ))}
            </div>

            {/* Meta row */}
            <div className="mb-5 grid grid-cols-3 gap-2">
                <div className="rounded-xl bg-black/[0.025] p-3 text-center">
                    <p className="font-mono text-sm font-semibold text-black">
                        {vault.lockDays ? `${vault.lockDays}d` : "None"}
                    </p>
                    <p className="text-[10px] text-black/40 mt-0.5">Lock</p>
                </div>
                <div className="rounded-xl bg-black/[0.025] p-3 text-center">
                    <p className="font-mono text-sm font-semibold text-black">
                        ${vault.minDeposit}
                    </p>
                    <p className="text-[10px] text-black/40 mt-0.5">Min</p>
                </div>
                <div className="rounded-xl bg-black/[0.025] p-3 text-center">
                    <p className="font-mono text-sm font-semibold text-black">
                        {vault.penaltyPct}%
                    </p>
                    <p className="text-[10px] text-black/40 mt-0.5">Exit fee</p>
                </div>
            </div>

            {/* CTA */}
            <button
                onClick={() => onDeposit(vault)}
                className="flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3 text-sm font-medium text-white transition-opacity hover:opacity-80"
            >
                Start Saving
                <ArrowUpRight className="h-4 w-4" />
            </button>
        </motion.div>
    );
}

// ── Deposit Modal ─────────────────────────────────────────────────────────────

function DepositModal({
    vault,
    onClose,
}: {
    vault: SavingsVault | null;
    onClose: () => void;
}) {
    const { balances } = usePortfolio();
    const [amount, setAmount] = useState("");
    const [goalName, setGoalName] = useState("");

    if (!vault) return null;

    const available = balances.USDC ?? 0;
    const parsedAmount = parseFloat(amount) || 0;
    const projectedYield =
        parsedAmount * vault.apy * ((vault.lockDays ?? 365) / 365);
    const maturityDate = vault.lockDays
        ? new Date(Date.now() + vault.lockDays * 86400000).toLocaleDateString("en-US", {
              month: "short",
              day: "numeric",
              year: "numeric",
          })
        : null;

    return (
        <AnimatePresence>
            {vault && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm"
                        onClick={onClose}
                    />
                    <motion.div
                        initial={{ opacity: 0, y: 24, scale: 0.97 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 24, scale: 0.97 }}
                        transition={{ duration: 0.2 }}
                        className="fixed inset-x-4 bottom-0 z-50 mx-auto max-w-md rounded-t-3xl bg-white p-6 shadow-2xl sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-3xl"
                    >
                        <div className="mb-5 flex items-start justify-between">
                            <div>
                                <h2 className="font-heading text-lg font-medium text-black">
                                    {vault.name}
                                </h2>
                                <p className="text-xs text-black/40 mt-0.5">{vault.apyLabel} APY</p>
                            </div>
                            <button
                                onClick={onClose}
                                className="flex h-8 w-8 items-center justify-center rounded-xl border border-black/10 text-black/40 hover:text-black transition-colors"
                            >
                                ✕
                            </button>
                        </div>

                        {/* Amount */}
                        <div className="mb-4">
                            <label className="mb-1.5 block text-xs font-medium text-black/50">
                                Amount (USDC)
                            </label>
                            <div className="relative">
                                <span className="absolute left-4 top-1/2 -translate-y-1/2 text-sm font-medium text-black/40">
                                    $
                                </span>
                                <input
                                    type="number"
                                    value={amount}
                                    onChange={(e) => setAmount(e.target.value)}
                                    placeholder="0.00"
                                    min={vault.minDeposit}
                                    className="h-12 w-full rounded-xl border border-black/10 bg-black/[0.02] pl-8 pr-4 text-base font-mono font-medium text-black outline-none transition-colors focus:border-black/25"
                                />
                            </div>
                            <div className="mt-1.5 flex justify-between text-[11px] text-black/40">
                                <span>Min: ${vault.minDeposit}</span>
                                <button
                                    onClick={() => setAmount(String(available))}
                                    className="hover:text-black transition-colors"
                                >
                                    Available: {available.toFixed(2)} USDC
                                </button>
                            </div>
                        </div>

                        {/* Goal name (custom only) */}
                        {vault.type === "custom" && (
                            <div className="mb-4">
                                <label className="mb-1.5 block text-xs font-medium text-black/50">
                                    Goal name (optional)
                                </label>
                                <input
                                    type="text"
                                    value={goalName}
                                    onChange={(e) => setGoalName(e.target.value)}
                                    placeholder="e.g. Holiday fund, House deposit…"
                                    className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm text-black outline-none transition-colors focus:border-black/25"
                                />
                            </div>
                        )}

                        {/* Projection */}
                        {parsedAmount >= vault.minDeposit && (
                            <div className="mb-5 rounded-xl border border-black/6 bg-black/[0.02] p-4 space-y-2">
                                <div className="flex justify-between text-xs">
                                    <span className="text-black/50">Projected yield</span>
                                    <span className="font-mono font-medium text-black">
                                        +{projectedYield.toFixed(4)} USDC
                                    </span>
                                </div>
                                {maturityDate && (
                                    <div className="flex justify-between text-xs">
                                        <span className="text-black/50">Matures on</span>
                                        <span className="font-medium text-black">{maturityDate}</span>
                                    </div>
                                )}
                                {vault.penaltyPct > 0 && (
                                    <div className="flex items-start gap-1.5 pt-1 border-t border-black/5">
                                        <Info className="h-3 w-3 text-black/30 mt-0.5 shrink-0" />
                                        <span className="text-[11px] text-black/40">
                                            Withdrawing early incurs a {vault.penaltyPct}% exit fee on the gross amount.
                                        </span>
                                    </div>
                                )}
                            </div>
                        )}

                        <button
                            disabled={parsedAmount < vault.minDeposit || parsedAmount > available}
                            className="flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3 text-sm font-medium text-white transition-opacity disabled:opacity-40"
                        >
                            Confirm Deposit
                        </button>
                        {parsedAmount > available && (
                            <p className="mt-2 text-center text-xs text-red-500">
                                Insufficient USDC balance
                            </p>
                        )}
                    </motion.div>
                </>
            )}
        </AnimatePresence>
    );
}

// ── Summary Bar ───────────────────────────────────────────────────────────────

function SavingsSummary({ positions }: { positions: ReturnType<typeof usePortfolio>["positions"] }) {
    const savingsPositions = positions.filter((p) =>
        ["flexible-savings", "locked-30", "locked-90", "custom-savings"].includes(p.vaultId)
    );
    if (savingsPositions.length === 0) return null;

    const total = savingsPositions.reduce((s, p) => s + p.currentValue, 0);
    const yield_ = savingsPositions.reduce((s, p) => s + p.yieldEarned, 0);

    return (
        <motion.div
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            className="mb-6 grid grid-cols-3 gap-3 sm:gap-4"
        >
            {[
                { label: "Total Saved", value: `$${total.toFixed(2)}`, icon: TrendingUp },
                { label: "Yield Earned", value: `+$${yield_.toFixed(4)}`, icon: Zap },
                { label: "Active Plans", value: String(savingsPositions.length), icon: Shield },
            ].map((stat) => (
                <div
                    key={stat.label}
                    className="rounded-2xl border border-black/8 bg-white p-4 sm:p-5"
                >
                    <div className="mb-3 flex h-7 w-7 items-center justify-center rounded-lg bg-black/5">
                        <stat.icon className="h-3.5 w-3.5 text-black/50" />
                    </div>
                    <p className="font-heading text-xl font-light text-black sm:text-2xl">
                        {stat.value}
                    </p>
                    <p className="mt-0.5 text-[10px] text-black/40 sm:text-xs">{stat.label}</p>
                </div>
            ))}
        </motion.div>
    );
}

// ── Filter Tabs ───────────────────────────────────────────────────────────────

const FILTERS: { label: string; value: SavingsVaultType | "all" }[] = [
    { label: "All", value: "all" },
    { label: "Flexible", value: "flexible" },
    { label: "Locked", value: "locked" },
    { label: "Custom", value: "custom" },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export default function SavingsPage() {
    const { isConnected } = useWallet();
    const { positions } = usePortfolio();
    const router = useRouter();
    const [filter, setFilter] = useState<SavingsVaultType | "all">("all");
    const [selectedVault, setSelectedVault] = useState<SavingsVault | null>(null);
    const [showHowItWorks, setShowHowItWorks] = useState(false);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    const filtered =
        filter === "all"
            ? SAVINGS_VAULTS
            : SAVINGS_VAULTS.filter((v) => v.type === filter);

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-7xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">

                {/* ── Page header ──────────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7"
                >
                    <div className="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <h1 className="font-heading text-2xl font-light text-black sm:text-3xl">
                                Savings
                            </h1>
                            <p className="mt-1 text-sm text-black/45">
                                Choose a savings plan and start earning yield on your USDC.
                            </p>
                        </div>
                        <button
                            onClick={() => setShowHowItWorks(!showHowItWorks)}
                            className="flex items-center gap-2 rounded-xl border border-black/10 px-4 py-2 text-xs font-medium text-black/60 hover:border-black/20 hover:text-black transition-all shrink-0"
                        >
                            <Info className="h-3.5 w-3.5" />
                            How it works
                            <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", showHowItWorks && "rotate-180")} />
                        </button>
                    </div>

                    {/* How it works explainer */}
                    <AnimatePresence>
                        {showHowItWorks && (
                            <motion.div
                                initial={{ opacity: 0, height: 0 }}
                                animate={{ opacity: 1, height: "auto" }}
                                exit={{ opacity: 0, height: 0 }}
                                className="overflow-hidden"
                            >
                                <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-3 rounded-2xl border border-black/8 p-4 sm:p-5 bg-black/[0.015]">
                                    {[
                                        {
                                            step: "01",
                                            icon: Shield,
                                            title: "Pick a plan",
                                            body: "Choose flexible, locked, or a custom goal-based plan based on when you'll need your funds.",
                                        },
                                        {
                                            step: "02",
                                            icon: TrendingUp,
                                            title: "Deposit USDC",
                                            body: "Deposit USDC and it's put to work in audited DeFi protocols, earning yield every day.",
                                        },
                                        {
                                            step: "03",
                                            icon: Clock,
                                            title: "Earn & withdraw",
                                            body: "Watch yield accumulate. Withdraw anytime on flexible plans, or at maturity on locked plans.",
                                        },
                                    ].map((s) => (
                                        <div key={s.step} className="flex gap-3">
                                            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/6 shrink-0 mt-0.5">
                                                <s.icon className="h-3.5 w-3.5 text-black/50" />
                                            </div>
                                            <div>
                                                <p className="text-xs font-semibold text-black/70">{s.title}</p>
                                                <p className="mt-0.5 text-xs leading-relaxed text-black/40">{s.body}</p>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </motion.div>

                {/* ── Active savings summary ────────────────────────────────── */}
                <SavingsSummary positions={positions} />

                {/* ── Filter tabs ──────────────────────────────────────────── */}
                <div className="mb-6 flex gap-1.5 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide">
                    {FILTERS.map((f) => (
                        <button
                            key={f.value}
                            onClick={() => setFilter(f.value)}
                            className={cn(
                                "relative pb-3 px-1 mr-3 text-sm font-medium whitespace-nowrap transition-colors shrink-0",
                                filter === f.value
                                    ? "text-black"
                                    : "text-black/40 hover:text-black/60"
                            )}
                        >
                            {f.label}
                            {filter === f.value && (
                                <motion.div
                                    layoutId="savings-tab"
                                    className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                                />
                            )}
                        </button>
                    ))}
                </div>

                {/* ── Vault grid ───────────────────────────────────────────── */}
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-4">
                    {filtered.map((vault, i) => (
                        <SavingsVaultCard
                            key={vault.id}
                            vault={vault}
                            index={i}
                            onDeposit={setSelectedVault}
                        />
                    ))}
                </div>

                {/* ── Compare table (desktop) ───────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.4 }}
                    className="mt-10 hidden sm:block rounded-2xl border border-black/8 overflow-hidden"
                >
                    <div className="px-5 py-4 border-b border-black/6 flex items-center justify-between">
                        <h2 className="text-sm font-medium text-black">Compare Plans</h2>
                    </div>
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm min-w-[540px]">
                            <thead>
                                <tr className="text-[11px] uppercase tracking-wide text-black/40 border-b border-black/6">
                                    <th className="py-3 pl-5 pr-4 text-left font-medium">Plan</th>
                                    <th className="py-3 pr-4 text-left font-medium">APY</th>
                                    <th className="py-3 pr-4 text-left font-medium">Lock period</th>
                                    <th className="py-3 pr-4 text-left font-medium">Min deposit</th>
                                    <th className="py-3 pr-5 text-left font-medium">Exit fee</th>
                                </tr>
                            </thead>
                            <tbody>
                                {SAVINGS_VAULTS.map((v, i) => {
                                    const Icon = TYPE_ICONS[v.type];
                                    return (
                                        <tr
                                            key={v.id}
                                            className={cn(
                                                "border-b border-black/5 last:border-0 transition-colors hover:bg-black/[0.015]",
                                            )}
                                        >
                                            <td className="py-3.5 pl-5 pr-4">
                                                <div className="flex items-center gap-2.5">
                                                    <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/5 shrink-0">
                                                        <Icon className="h-3.5 w-3.5 text-black/50" />
                                                    </div>
                                                    <span className="font-medium text-black">{v.name}</span>
                                                </div>
                                            </td>
                                            <td className="py-3.5 pr-4">
                                                <span className="font-mono font-semibold text-black">{v.apyLabel}</span>
                                            </td>
                                            <td className="py-3.5 pr-4">
                                                {v.lockDays ? (
                                                    <span className="inline-flex items-center gap-1 text-black/60">
                                                        <Lock className="h-3 w-3" />
                                                        {v.lockDays} days
                                                    </span>
                                                ) : (
                                                    <span className="text-black/40">None</span>
                                                )}
                                            </td>
                                            <td className="py-3.5 pr-4">
                                                <span className="font-mono text-black/70">${v.minDeposit}</span>
                                            </td>
                                            <td className="py-3.5 pr-5">
                                                {v.penaltyPct > 0 ? (
                                                    <span className="font-mono text-black/60">{v.penaltyPct}%</span>
                                                ) : (
                                                    <span className="text-black/30">—</span>
                                                )}
                                            </td>
                                        </tr>
                                    );
                                    void i;
                                })}
                            </tbody>
                        </table>
                    </div>
                </motion.div>
            </main>

            <DepositModal vault={selectedVault} onClose={() => setSelectedVault(null)} />
        </div>
    );
}
