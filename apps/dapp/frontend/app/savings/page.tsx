"use client";

import { useRef, useState, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    Unlock,
    Sliders,
    RefreshCcw,
    BarChart2,
    TrendingUp,
    Shield,
    Zap,
    ChevronDown,
    ArrowUpRight,
    Info,
    Clock,
    X,
    Activity,
    Sparkles,
} from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { cn } from "@/lib/utils";
import { PositionCards } from "@/components/position-cards";
import {
    buildDepositTransaction,
    signTransaction,
    submitTransaction,
    UserRejectedError,
    TransactionFailedError,
    TransactionTimeoutError,
    truncateTxHash,
} from "@/lib/stellar/transaction";
import SavingsChart from "@/components/analytics/SavingsChart";
import { type APYHistoryPeriod } from "@/lib/api/vaults";
import { useSavingsChartData } from "@/hooks/useSavingsChartData";
import { useSavingsGoals } from "@/hooks/useSavingsGoals";
import { useYieldOpportunities } from "@/hooks/useYieldOpportunities";
import { intelligenceApi, type SavingsPlanResponse } from "@/lib/api/intelligence";
import {
  SAVINGS_VAULT_DEFINITIONS,
  type SavingsVault,
  type SavingsVaultType,
} from "@/lib/savings/vault-definitions";
import { buildSavingsVaults } from "@/lib/savings/apply-live-apy";
import { SavingsGoalsSection } from "@/components/savings/SavingsGoalsSection";

// ── Types ─────────────────────────────────────────────────────────────────────

const TYPE_ICONS: Record<SavingsVaultType, React.ElementType> = {
    flexible: Unlock,
    "auto-compound": RefreshCcw,
    "stablecoin-yield": BarChart2,
    custom: Sliders,
};

// ── Create Plan Modal ─────────────────────────────────────────────────────────

function CreatePlanModal({
    onClose,
}: {
    onClose: () => void;
}) {
    const [goal, setGoal] = useState("5000");
    const [months, setMonths] = useState("18");
    const [contribution, setContribution] = useState("250");
    const [loading, setLoading] = useState(false);
    const [plan, setPlan] = useState<SavingsPlanResponse | null>(null);

    const handleCreate = async () => {
        setLoading(true);
        try {
            const res = await intelligenceApi.createSavingsPlan({
                goal_usdc: parseFloat(goal),
                time_horizon_months: parseInt(months),
                max_monthly_contribution_usdc: parseFloat(contribution),
            });
            setPlan(res);
        } catch (err) {
            console.error("Failed to create plan:", err);
        } finally {
            setLoading(false);
        }
    };

    return (
        <AnimatePresence>
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="fixed inset-0 z-50 bg-black/25 backdrop-blur-sm"
                onClick={onClose}
            />
            <motion.div
                initial={{ opacity: 0, scale: 0.95 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.95 }}
                className="fixed inset-x-4 top-20 z-50 mx-auto max-w-2xl rounded-3xl bg-white p-8 shadow-2xl sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2"
                role="dialog"
                aria-modal="true"
                aria-labelledby="plan-modal-title"
            >
                <div className="flex items-center justify-between mb-6">
                    <h2 id="plan-modal-title" className="text-xl font-bold text-black">Create a Personalised Savings Plan</h2>
                    <button onClick={onClose} aria-label="Close" className="rounded-full p-2 hover:bg-black/5">
                        <X className="h-5 w-5" />
                    </button>
                </div>

                {!plan ? (
                    <div className="space-y-6">
                        <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
                            <div>
                                <label className="mb-2 block text-xs font-bold text-black/60 uppercase">Savings Goal (USDC)</label>
                                <input
                                    type="number"
                                    value={goal}
                                    onChange={(e) => setGoal(e.target.value)}
                                    className="w-full rounded-xl border border-black/10 bg-black/[0.02] p-3 text-sm outline-none focus:border-black/25 text-black"
                                />
                            </div>
                            <div>
                                <label className="mb-2 block text-xs font-bold text-black/60 uppercase">Time Horizon (Months)</label>
                                <input
                                    type="number"
                                    value={months}
                                    onChange={(e) => setMonths(e.target.value)}
                                    className="w-full rounded-xl border border-black/10 bg-black/[0.02] p-3 text-sm outline-none focus:border-black/25 text-black"
                                />
                            </div>
                            <div>
                                <label className="mb-2 block text-xs font-bold text-black/60 uppercase">Max Monthly Contribution</label>
                                <input
                                    type="number"
                                    value={contribution}
                                    onChange={(e) => setContribution(e.target.value)}
                                    className="w-full rounded-xl border border-black/10 bg-black/[0.02] p-3 text-sm outline-none focus:border-black/25 text-black"
                                />
                            </div>
                        </div>
                        <button
                            onClick={handleCreate}
                            disabled={loading}
                            className="w-full rounded-xl bg-black py-4 text-sm font-bold text-white transition-opacity hover:opacity-75 disabled:opacity-50"
                        >
                            {loading ? "Generating Plan..." : "Generate Personalised Plan"}
                        </button>
                    </div>
                ) : (
                    <div className="space-y-6">
                        <div className={cn(
                            "rounded-2xl p-5 border",
                            plan.achievable ? "bg-emerald-50 border-emerald-100" : "bg-amber-50 border-amber-100"
                        )}>
                            <p className={cn(
                                "text-sm font-medium leading-relaxed",
                                plan.achievable ? "text-emerald-800" : "text-amber-800"
                            )}>
                                {plan.narrative}
                            </p>
                        </div>

                        <div className="grid grid-cols-2 gap-4">
                            <div className="rounded-2xl bg-black/[0.02] p-4">
                                <p className="text-[10px] font-bold text-black/40 uppercase">Required Deposit</p>
                                <p className="text-xl font-bold text-black">${plan.required_monthly_deposit}/mo</p>
                            </div>
                            <div className="rounded-2xl bg-black/[0.02] p-4">
                                <p className="text-[10px] font-bold text-black/40 uppercase">Total Yield Earned</p>
                                <p className="text-xl font-bold text-black text-emerald-600">+${plan.total_yield_earned}</p>
                            </div>
                        </div>

                        <div className="max-h-48 overflow-y-auto rounded-2xl border border-black/5">
                            <table className="w-full text-left text-[11px]">
                                <thead className="sticky top-0 bg-white border-b border-black/5">
                                    <tr>
                                        <th className="px-4 py-2 font-bold text-black/40 uppercase">Month</th>
                                        <th className="px-4 py-2 font-bold text-black/40 uppercase">Deposit</th>
                                        <th className="px-4 py-2 font-bold text-black/40 uppercase">Yield</th>
                                        <th className="px-4 py-2 font-bold text-black/40 uppercase">Balance</th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-black/5">
                                    {plan.monthly_schedule.map((entry) => (
                                        <tr key={entry.month}>
                                            <td className="px-4 py-2 font-medium text-black/60">{entry.month}</td>
                                            <td className="px-4 py-2 text-black">${entry.deposit}</td>
                                            <td className="px-4 py-2 text-emerald-600">+${entry.yield_earned}</td>
                                            <td className="px-4 py-2 font-bold text-black">${entry.expected_balance}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>

                        <div className="flex gap-3">
                            <button
                                onClick={() => setPlan(null)}
                                className="flex-1 rounded-xl border border-black/10 py-3 text-xs font-bold text-black/60 hover:bg-black/5"
                            >
                                Adjust Parameters
                            </button>
                            {plan.achievable && (
                                <button
                                    onClick={onClose}
                                    className="flex-1 rounded-xl bg-black py-3 text-xs font-bold text-white hover:opacity-75"
                                >
                                    Activate This Plan
                                </button>
                            )}
                        </div>
                    </div>
                )}
            </motion.div>
        </AnimatePresence>
    );
}

// ── Info Tooltip ──────────────────────────────────────────────────────────────

function InfoTooltip({ text }: { text: string }) {
    const [show, setShow] = useState(false);
    const ref = useRef<HTMLDivElement>(null);

    return (
        <div
            ref={ref}
            className="relative"
            onMouseEnter={() => setShow(true)}
            onMouseLeave={() => setShow(false)}
        >
            <button
                className="flex h-6 w-6 items-center justify-center rounded-full border border-black/12 text-black/40 hover:border-black/25 hover:text-black/70 transition-colors focus-visible:ring-2 focus-visible:ring-black"
                tabIndex={0}
                aria-label="More info"
                onFocus={() => setShow(true)}
                onBlur={() => setShow(false)}
            >
                <Info className="h-3 w-3" aria-hidden="true" />
            </button>
            <AnimatePresence>
                {show && (
                    <motion.div
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 4 }}
                        transition={{ duration: 0.15 }}
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-60 rounded-xl border border-black/8 bg-white px-3.5 py-3 shadow-lg shadow-black/6 text-xs text-black/60 leading-relaxed pointer-events-none"
                    >
                        {text}
                        <div className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-black/8" />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// ── Vault Card ────────────────────────────────────────────────────────────────

function SavingsVaultCard({
    vault,
    index,
    onDeposit,
    isApyLoading,
}: {
    vault: SavingsVault;
    index: number;
    onDeposit: (v: SavingsVault) => void;
    isApyLoading?: boolean;
}) {
    const Icon = TYPE_ICONS[vault.type];

    return (
        <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, delay: index * 0.07 }}
            className="group flex flex-col rounded-2xl border border-black/8 bg-white px-7 py-6 transition-all hover:border-black/18 hover:shadow-md"
        >
            {/* Header */}
            <div className="mb-6 flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/5 shrink-0">
                        <Icon className="h-4 w-4 text-black/50" aria-hidden="true" />
                    </div>
                    <div>
                        <h3 className="text-sm text-black font-semibold">{vault.name}</h3>
                        <span className="text-[10px] bg-black/5 text-black/60 font-medium rounded-full px-2 py-0.5 mt-0.5 inline-block">
                            {vault.badge}
                        </span>
                    </div>
                </div>
                <div className="flex items-start gap-2 shrink-0">
                    <div className="text-right">
                        {isApyLoading ? (
                            <div className="ml-auto h-8 w-16 animate-pulse rounded bg-black/10" aria-label="Loading APY" />
                        ) : (
                            <p className="font-mono text-2xl text-black leading-none">{vault.apyLabel}</p>
                        )}
                        <p className="text-[10px] text-black/60 font-medium uppercase tracking-wide mt-1">APY</p>
                    </div>
                    <InfoTooltip text={vault.summary} />
                </div>
            </div>

            {/* Description */}
            <p className="mb-6 text-sm leading-relaxed text-black/60 flex-1">
                {vault.description}
            </p>

            {/* Features */}
            <div className="mb-6 space-y-2">
                {vault.features.map((f) => (
                    <div key={f} className="flex items-center gap-2.5">
                        <div className="h-1 w-1 rounded-full bg-black/40 shrink-0" aria-hidden="true" />
                        <span className="text-xs text-black/60">{f}</span>
                    </div>
                ))}
            </div>

            {/* Meta row */}
            <div className="mb-6 grid grid-cols-2 gap-2">
                <div className="rounded-xl bg-black/[0.025] px-3 py-3 text-center">
                    <p className="font-mono text-sm text-black">
                        {vault.lockDays ? `${vault.lockDays}d` : "None"}
                    </p>
                    <p className="text-[10px] text-black/60 font-medium mt-0.5">Lock</p>
                </div>
                <div className="rounded-xl bg-black/[0.025] px-3 py-3 text-center">
                    <p className="font-mono text-sm text-black">{vault.penaltyPct}%</p>
                    <p className="text-[10px] text-black/60 font-medium mt-0.5">Exit fee</p>
                </div>
            </div>

            {/* CTA */}
            <button
                onClick={() => onDeposit(vault)}
                className="flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3 text-sm text-white font-medium transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
            >
                Start Saving
                <ArrowUpRight className="h-4 w-4" aria-hidden="true" />
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
    const { balances, recordDeposit, refreshBalances } = usePortfolio();
    const { address } = useWallet();
    const [amount, setAmount] = useState("");
    const [goalName, setGoalName] = useState("");
    const [selectedAsset, setSelectedAsset] = useState<"USDC" | "XLM">("USDC");
    const [txState, setTxState] = useState<"idle" | "building" | "signing" | "submitting" | "success" | "error">("idle");
    const [errorMsg, setErrorMsg] = useState("");
    const [txHash, setTxHash] = useState("");

    // Reset states if vault becomes null
    const [prevVaultId, setPrevVaultId] = useState(vault?.id);
    if (vault?.id !== prevVaultId) {
        setPrevVaultId(vault?.id);
        if (!vault) {
            setAmount("");
            setGoalName("");
            setSelectedAsset("USDC");
        } else {
            setSelectedAsset(vault.supportedAssets[0] ?? "USDC");
        }
    }

    // Instead of using Date.now() in render, use a ref or state
    const [now] = useState(() => Date.now());
    
    const maturityDate = useMemo(() => {
        if (!vault) return null;
        return vault.lockDays
            ? new Date(now + vault.lockDays * 86400000).toLocaleDateString("en-US", {
                  month: "short",
                  day: "numeric",
                  year: "numeric",
              })
            : null;
    }, [vault, now]);

    if (!vault) return null;

    const supportedAssets = vault.supportedAssets;
    const available = balances[selectedAsset] ?? 0;
    const parsedAmount = parseFloat(amount) || 0;
    const projectedYield = parsedAmount * vault.apy * ((vault.lockDays ?? 365) / 365);
    const overBalance = parsedAmount > available;
    const canSubmit = parsedAmount > 0 && !overBalance;

    const Icon = TYPE_ICONS[vault.type];

    return (
        <AnimatePresence>
            {vault && (
                <>
                    {/* Backdrop */}
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        className="fixed inset-0 z-50 bg-black/25 backdrop-blur-sm"
                        onClick={onClose}
                    />

                    {/* Sheet on mobile, centred dialog on desktop */}
                    <motion.div
                        initial={{ opacity: 0, y: 32 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 32 }}
                        transition={{ duration: 0.22, ease: "easeOut" }}
                        className="fixed inset-x-0 bottom-0 z-50 rounded-t-3xl bg-white shadow-2xl
                                   sm:inset-auto sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2
                                   sm:rounded-3xl sm:w-[680px]"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="deposit-title"
                    >
                        {/* Drag handle (mobile) */}
                        <div className="flex justify-center pt-4 pb-1 sm:hidden">
                            <div className="h-1 w-10 rounded-full bg-black/10" aria-hidden="true" />
                        </div>

                        <div className="px-8 pb-8 pt-6 sm:px-10 sm:pb-10 sm:pt-8">
                            {/* Header */}
                            <div className="mb-8 flex items-start justify-between">
                                <div className="flex items-center gap-4">
                                    <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-black/5 shrink-0">
                                        <Icon className="h-5 w-5 text-black/50" aria-hidden="true" />
                                    </div>
                                    <div>
                                        <h2 id="deposit-title" className="text-lg text-black font-semibold">{vault.name}</h2>
                                        <p className="text-xs text-black/60 font-medium mt-0.5">
                                            <span className="font-mono">{vault.apyLabel}</span> APY
                                            {vault.lockDays
                                                ? ` · ${vault.lockDays}-day lock`
                                                : " · no lockup"}
                                        </p>
                                    </div>
                                </div>
                                <button
                                    onClick={onClose}
                                    aria-label="Close modal"
                                    className="flex h-9 w-9 items-center justify-center rounded-xl border border-black/10 text-black/40 hover:text-black transition-colors shrink-0 focus-visible:ring-2 focus-visible:ring-black"
                                >
                                    <X className="h-4 w-4" aria-hidden="true" />
                                </button>
                            </div>

                            {/* Two-column layout on sm+ */}
                            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
                                {/* Left: inputs */}
                                <div className="space-y-5">
                                    {/* Amount input */}
                                    <div>
                                        <div className="mb-2 flex items-center justify-between">
                                            <label htmlFor="deposit-amount" className="text-xs text-black/60 font-medium">
                                                Amount ({selectedAsset})
                                            </label>
                                            {supportedAssets.length > 1 && (
                                                <div className="flex rounded-full border border-black/10 bg-black/[0.03] p-0.5" role="group" aria-label="Select asset">
                                                    {supportedAssets.map((a) => (
                                                        <button
                                                            key={a}
                                                            type="button"
                                                            onClick={() => {
                                                                setSelectedAsset(a);
                                                                setAmount("");
                                                            }}
                                                            className={cn(
                                                                "rounded-full px-3 py-1 text-[11px] font-medium transition-colors",
                                                                selectedAsset === a
                                                                    ? "bg-black text-white"
                                                                    : "text-black/60 hover:text-black"
                                                            )}
                                                            aria-current={selectedAsset === a ? "true" : "false"}
                                                        >
                                                            {a}
                                                        </button>
                                                    ))}
                                                </div>
                                            )}
                                        </div>
                                        <div className="relative">
                                            <span className="absolute left-4 top-1/2 -translate-y-1/2 font-mono text-sm text-black/40" aria-hidden="true">
                                                {selectedAsset === "XLM" ? "✦" : "$"}
                                            </span>
                                            <input
                                                id="deposit-amount"
                                                type="number"
                                                value={amount}
                                                onChange={(e) => setAmount(e.target.value)}
                                                placeholder="0.00"
                                                className="h-14 w-full rounded-xl border border-black/10 bg-black/[0.02] pl-8 pr-4
                                                           font-mono text-xl text-black outline-none transition-colors
                                                           focus:border-black/25 focus:bg-white
                                                           [appearance:textfield]
                                                           [&::-webkit-outer-spin-button]:appearance-none
                                                           [&::-webkit-inner-spin-button]:appearance-none"
                                            />
                                        </div>
                                        <div className="mt-2 flex items-center justify-end text-xs text-black/60 font-medium">
                                            <button
                                                onClick={() => setAmount(String(available))}
                                                className="hover:text-black transition-colors"
                                            >
                                                Available: <span className="font-mono">{available.toFixed(2)}</span> {selectedAsset}
                                            </button>
                                        </div>
                                        {overBalance && (
                                            <p className="mt-1.5 text-xs text-red-600 font-medium" role="alert">
                                                Exceeds available balance
                                            </p>
                                        )}
                                    </div>

                                    {/* Quick amounts */}
                                    <div>
                                        <p className="mb-2 text-xs text-black/60 font-medium">Quick amounts</p>
                                        <div className="flex gap-2 flex-wrap" role="group" aria-label="Quick amount selection">
                                            {[50, 100, 250, 500].map((v) => (
                                                <button
                                                    key={v}
                                                    onClick={() => setAmount(String(v))}
                                                    className={cn(
                                                        "rounded-lg border px-3 py-1.5 font-mono text-xs transition-colors focus-visible:ring-2 focus-visible:ring-black",
                                                        amount === String(v)
                                                            ? "border-black bg-black text-white"
                                                            : "border-black/10 text-black/60 hover:border-black/20 hover:text-black"
                                                    )}
                                                >
                                                    ${v}
                                                </button>
                                            ))}
                                        </div>
                                    </div>

                                    {/* Goal name (custom only) */}
                                    {vault.type === "custom" && (
                                        <div>
                                            <label htmlFor="goal-name" className="mb-2 block text-xs text-black/60 font-medium">
                                                Goal name
                                            </label>
                                            <input
                                                id="goal-name"
                                                type="text"
                                                value={goalName}
                                                onChange={(e) => setGoalName(e.target.value)}
                                                placeholder="e.g. Holiday fund, House deposit…"
                                                className="h-12 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm text-black outline-none transition-colors focus:border-black/25 text-black"
                                            />
                                        </div>
                                    )}
                                </div>

                                {/* Right: projection + summary */}
                                <div className="flex flex-col gap-4">
                                    {/* Projection block */}
                                    <div className="rounded-2xl border border-black/8 bg-black/[0.018] p-5 flex-1" aria-label="Savings projection">
                                        <p className="mb-4 text-xs text-black/60 font-semibold uppercase tracking-widest">
                                            Projection
                                        </p>
                                        <div className="space-y-3">
                                            <div className="flex justify-between items-baseline">
                                                <span className="text-xs text-black/60">Deposit</span>
                                                <span className="font-mono text-sm text-black font-medium">
                                                    {parsedAmount > 0 ? parsedAmount.toFixed(2) : "—"}
                                                </span>
                                            </div>
                                            <div className="flex justify-between items-baseline">
                                                <span className="text-xs text-black/60">
                                                    Yield ({vault.lockDays ? `${vault.lockDays}d` : "1yr"})
                                                </span>
                                                <span className="font-mono text-sm text-black font-medium">
                                                    {parsedAmount > 0 ? `+${projectedYield.toFixed(4)}` : "—"}
                                                </span>
                                            </div>
                                            <div className="border-t border-black/6 pt-3 flex justify-between items-baseline">
                                                <span className="text-xs text-black/60 font-semibold">Total at maturity</span>
                                                <span className="font-mono text-base text-black font-bold">
                                                    {parsedAmount > 0
                                                        ? (parsedAmount + projectedYield).toFixed(2)
                                                        : "—"}
                                                </span>
                                            </div>
                                            {maturityDate && parsedAmount > 0 && (
                                                <div className="flex justify-between items-baseline pt-1">
                                                    <span className="text-xs text-black/60">Matures</span>
                                                    <span className="text-xs text-black/80 font-medium">{maturityDate}</span>
                                                </div>
                                            )}
                                        </div>
                                    </div>

                                    {/* Vault features summary */}
                                    <div className="space-y-2">
                                        {vault.features.map((f) => (
                                            <div key={f} className="flex items-center gap-2">
                                                <div className="h-1 w-1 rounded-full bg-black/40 shrink-0" aria-hidden="true" />
                                                <span className="text-xs text-black/60">{f}</span>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            </div>

                            {/* Error */}
                            {txState === "error" && errorMsg && (
                                <p className="mt-4 rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-700 font-medium" role="alert">{errorMsg}</p>
                            )}

                            {/* CTA */}
                            <button
                                disabled={!canSubmit || txState !== "idle"}
                                onClick={async () => {
                                    if (!vault || !canSubmit || !address) return;
                                    setErrorMsg("");
                                    try {
                                        const USDC_CONTRACT = process.env.NEXT_PUBLIC_VAULT_CONTRACT_ID ?? "";
                                        const XLM_CONTRACT = process.env.NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID ?? "";
                                        const contractId = selectedAsset === "XLM" ? XLM_CONTRACT : USDC_CONTRACT;

                                        if (!/^C[A-Z0-9]{55}$/.test(contractId)) {
                                            setErrorMsg("Vault contract not configured. Check environment variables.");
                                            setTxState("error");
                                            return;
                                        }

                                        setTxState("building");
                                        const { xdr } = await buildDepositTransaction({
                                            walletAddress: address,
                                            contractId,
                                            amount: parsedAmount,
                                        });

                                        setTxState("signing");
                                        const signedXdr = await signTransaction(xdr);

                                        setTxState("submitting");
                                        const txReceipt = await submitTransaction(signedXdr);

                                        setTxHash(txReceipt.txHash);
                                        setTxState("success");
                                        refreshBalances();
                                        recordDeposit({
                                            vault: {
                                                id: vault.id,
                                                name: vault.name,
                                                asset: selectedAsset,
                                                apy: vault.apy,
                                                lockDays: vault.lockDays,
                                                earlyWithdrawalPenaltyPct: vault.penaltyPct,
                                            },
                                            amount: parsedAmount,
                                            txHash: txReceipt.txHash,
                                            isOnChain: true,
                                        });
                                        setTimeout(onClose, 1500);
                                    } catch (err) {
                                        if (err instanceof UserRejectedError) {
                                            setTxState("idle");
                                        } else if (err instanceof TransactionFailedError) {
                                            setErrorMsg(err.reason);
                                            setTxState("error");
                                        } else if (err instanceof TransactionTimeoutError) {
                                            setErrorMsg("Transaction timed out. Check the explorer for status.");
                                            setTxState("error");
                                        } else {
                                            setErrorMsg(err instanceof Error ? err.message : "Deposit failed");
                                            setTxState("error");
                                        }
                                    }
                                }}
                                className="mt-8 flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3.5 text-sm text-white font-semibold transition-opacity disabled:opacity-35 focus-visible:ring-2 focus-visible:ring-black"
                            >
                                {txState === "building" && "Building transaction…"}
                                {txState === "signing" && "Waiting for signature…"}
                                {txState === "submitting" && "Submitting…"}
                                {txState === "success" && (txHash ? `Deposited · ${truncateTxHash(txHash)}` : "Deposited!")}
                                {(txState === "idle" || txState === "error") && "Confirm Deposit"}
                            </button>
                        </div>
                    </motion.div>
                </>
            )}
        </AnimatePresence>
    );
}

// ── Savings Overview ──────────────────────────────────────────────────────────
const CHART_PERIODS: APYHistoryPeriod[] = ["7d", "30d", "90d"];

function SavingsOverview({ savingsVaults }: { savingsVaults: SavingsVault[] }) {
    const { positions } = usePortfolio();
    const [period, setPeriod] = useState<APYHistoryPeriod>("30d");

    const activeVaultPositions = useMemo(() => {
        const ids = savingsVaults.map((v) => v.id);
        return positions.filter((p) => ids.includes(p.vaultId));
    }, [positions, savingsVaults]);

    // Distinct vaults the user actually holds, used to decide what to chart.
    const userVaults = useMemo(() => {
        const seen = new Map<string, string>();
        for (const p of activeVaultPositions) {
            if (!seen.has(p.vaultId)) seen.set(p.vaultId, p.vaultName);
        }
        return Array.from(seen, ([id, name]) => ({ id, name }));
    }, [activeVaultPositions]);

    // Linked savings goals (#688): prefer a goal-linked vault when present.
    const { data: goals } = useSavingsGoals();
    const linkedVaultId = useMemo(
        () =>
            goals?.find(
                (g) => g.vault_id && userVaults.some((v) => v.id === g.vault_id)
            )?.vault_id,
        [goals, userVaults]
    );

    // Which vault is currently being charted. When the user holds multiple
    // unlinked vaults they pick via the dropdown below.
    const [selectedVaultId, setSelectedVaultId] = useState<string | undefined>(undefined);

    const primaryVaultId = useMemo(() => {
        if (linkedVaultId) return linkedVaultId;
        if (selectedVaultId && userVaults.some((v) => v.id === selectedVaultId)) {
            return selectedVaultId;
        }
        return userVaults[0]?.id;
    }, [linkedVaultId, selectedVaultId, userVaults]);

    const showVaultPicker = !linkedVaultId && userVaults.length > 1;

    const { data: chartData = [], isLoading: chartLoading } = useSavingsChartData(
        primaryVaultId,
        period
    );

    if (activeVaultPositions.length === 0) {
        return (
            <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-10 p-12 text-center rounded-3xl border border-black/8 bg-white"
            >
                <div className="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-2xl bg-black/5">
                    <TrendingUp className="h-8 w-8 text-black/40" aria-hidden="true" />
                </div>
                <h2 className="text-xl text-black font-semibold">Start your savings journey</h2>
                <p className="mx-auto mt-2 max-w-xs text-sm text-black/60 font-medium">
                    Earn up to 12% APY on your USDC. Make your first deposit to start tracking your growth.
                </p>
                <button 
                    onClick={() => document.getElementById("vault-grid")?.scrollIntoView({ behavior: "smooth" })}
                    className="mt-8 rounded-xl bg-black px-8 py-3 text-sm font-semibold text-white transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                >
                    View Savings Plans
                </button>
            </motion.div>
        );
    }

    const totalBalance = activeVaultPositions.reduce((s, p) => s + p.currentValue, 0);
    const totalPrincipal = activeVaultPositions.reduce((s, p) => s + p.principal, 0);
    const totalYield = totalBalance - totalPrincipal;
    
    // Calculate effective APY: (Yield / Principal) * (365 / Days)
    const firstDeposit = Math.min(...activeVaultPositions.map(p => new Date(p.depositedAt).getTime()));
    const daysSaving = Math.max(1, Math.ceil((Date.now() - firstDeposit) / 86400000));
    const effectiveApy = totalPrincipal > 0 ? (totalYield / totalPrincipal) * (365 / daysSaving) * 100 : 0;

    const ArrowDownLeftIcon = ({ className }: { className?: string }) => (
        <svg 
            xmlns="http://www.w3.org/2000/svg" 
            width="24" 
            height="24" 
            viewBox="0 0 24 24" 
            fill="none" 
            stroke="currentColor" 
            strokeWidth="2" 
            strokeLinecap="round" 
            strokeLinejoin="round" 
            className={className}
            aria-hidden="true"
        >
            <line x1="17" y1="7" x2="7" y2="17"></line>
            <polyline points="17 17 7 17 7 7"></polyline>
        </svg>
    );

    const stats = [
        { label: "Total Deposited", value: `$${totalPrincipal.toLocaleString()}`, icon: ArrowDownLeftIcon, sub: "Principal" },
        { label: "Yield Earned", value: `+$${totalYield.toLocaleString(undefined, { minimumFractionDigits: 2 })}`, icon: Zap, sub: "Total profit" },
        { label: "Effective APY", value: `${effectiveApy.toFixed(1)}%`, icon: Activity, sub: "Annualised" },
        { label: "Days Saving", value: String(daysSaving), icon: Clock, sub: "Since first deposit" },
    ];

    return (
        <div className="mb-10 space-y-6">
            {/* Stats Row */}
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                {stats.map((s) => (
                    <div key={s.label} className="rounded-2xl border border-black/8 bg-white p-5">
                        <div className="mb-3 flex items-center justify-between">
                            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/5">
                                <s.icon className="h-3.5 w-3.5 text-black/50" aria-hidden="true" />
                            </div>
                            <span className="text-[10px] text-black/60 uppercase font-bold tracking-wider">{s.sub}</span>
                        </div>
                        <p className="text-xl font-light text-black truncate" aria-live="polite">{s.value}</p>
                        <p className="mt-0.5 text-xs text-black/60 font-medium">{s.label}</p>
                    </div>
                ))}
            </div>

            {/* Chart Block */}
            <div className="rounded-3xl border border-black/8 bg-white p-6 sm:p-8">
                <div className="flex items-center justify-between gap-4 mb-8 flex-wrap">
                    <div>
                        <h3 className="text-sm font-semibold text-black">APY Performance</h3>
                        <p className="text-xs text-black/60 font-medium mt-0.5">Historical vault APY over time</p>
                    </div>
                    <div className="flex items-center gap-3">
                        {showVaultPicker && (
                            <select
                                value={primaryVaultId ?? ""}
                                onChange={(e) => setSelectedVaultId(e.target.value)}
                                aria-label="Select vault to chart"
                                className="rounded-lg border border-black/10 bg-white px-3 py-1.5 text-[11px] font-medium text-black/70 outline-none focus-visible:ring-2 focus-visible:ring-black"
                            >
                                {userVaults.map((v) => (
                                    <option key={v.id} value={v.id}>
                                        {v.name}
                                    </option>
                                ))}
                            </select>
                        )}
                        <div className="flex bg-black/[0.04] p-1 rounded-xl" role="tablist" aria-label="Chart period">
                            {CHART_PERIODS.map((p) => (
                                <button
                                    key={p}
                                    role="tab"
                                    aria-selected={period === p}
                                    onClick={() => setPeriod(p)}
                                    className={cn(
                                        "px-3 py-1.5 text-[11px] rounded-lg transition-all focus-visible:ring-2 focus-visible:ring-black",
                                        period === p ? "bg-white text-black shadow-sm font-bold" : "text-black/60 hover:text-black/80 font-medium"
                                    )}
                                >
                                    {p.toUpperCase()}
                                </button>
                            ))}
                        </div>
                    </div>
                </div>

                <SavingsChart data={chartData} isLoading={chartLoading} />
            </div>
        </div>
    );
}

// ── Filter Tabs ───────────────────────────────────────────────────────────────

const FILTERS: { label: string; value: SavingsVaultType | "all" }[] = [
    { label: "All", value: "all" },
    { label: "Flexible", value: "flexible" },
    { label: "Auto-Compound", value: "auto-compound" },
    { label: "Stablecoin Yield", value: "stablecoin-yield" },
    { label: "Custom", value: "custom" },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export default function SavingsPage() {
    const { isConnected } = useWallet();
    const { positions } = usePortfolio();
    const [filter, setFilter] = useState<SavingsVaultType | "all">("all");
    const [selectedVault, setSelectedVault] = useState<SavingsVault | null>(null);
    const [showHowItWorks, setShowHowItWorks] = useState(false);
    const [planModalOpen, setPlanModalOpen] = useState(false);

    const { data: yieldPools, isLoading: yieldLoading, isError: yieldError } = useYieldOpportunities();
    const savingsVaults = useMemo(
        () => buildSavingsVaults(SAVINGS_VAULT_DEFINITIONS, yieldPools, yieldError),
        [yieldPools, yieldError]
    );

    const filtered =
        filter === "all"
            ? savingsVaults
            : savingsVaults.filter((v) => v.type === filter);

    return (
        <AppShell>

                {/* ── Page header ──────────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7"
                >
                    <div className="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <h1 className="text-2xl text-black sm:text-3xl font-semibold">Savings</h1>
                            <p className="mt-1 text-sm text-black/60 font-medium">
                                Choose a savings plan and start earning yield on your USDC.
                            </p>
                        </div>
                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => setPlanModalOpen(true)}
                                className="flex items-center gap-2 rounded-xl bg-black px-5 py-2.5 text-xs font-bold text-white transition-opacity hover:opacity-75 focus-visible:ring-2 focus-visible:ring-black"
                            >
                                <Sparkles className="h-3.5 w-3.5" aria-hidden="true" />
                                Create Plan
                            </button>
                            <button
                                onClick={() => setShowHowItWorks(!showHowItWorks)}
                                className="flex items-center gap-2 rounded-xl border border-black/10 px-4 py-2 text-xs text-black/60 hover:border-black/20 hover:text-black transition-all shrink-0 focus-visible:ring-2 focus-visible:ring-black"
                                aria-expanded={showHowItWorks}
                                aria-controls="how-it-works-panel"
                            >
                                <Info className="h-3.5 w-3.5" aria-hidden="true" />
                                How it works
                                <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", showHowItWorks && "rotate-180")} aria-hidden="true" />
                            </button>
                        </div>
                    </div>

                    <AnimatePresence>
                        {showHowItWorks && (
                            <motion.div
                                id="how-it-works-panel"
                                initial={{ opacity: 0, height: 0 }}
                                animate={{ opacity: 1, height: "auto" }}
                                exit={{ opacity: 0, height: 0 }}
                                className="overflow-hidden"
                                role="region"
                                aria-label="How savings work"
                            >
                                <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 rounded-2xl border border-black/8 p-5 bg-black/[0.015]">
                                    {[
                                        {
                                            icon: Shield,
                                            title: "Pick a plan",
                                            body: "Choose flexible, auto-compound, stablecoin yield, or a custom goal-based plan.",
                                        },
                                        {
                                            icon: TrendingUp,
                                            title: "Deposit USDC",
                                            body: "Funds are deployed into audited DeFi protocols and start earning yield immediately.",
                                        },
                                        {
                                            icon: Clock,
                                            title: "Earn & withdraw",
                                            body: "Yield accumulates continuously. All plans allow withdrawal at any time with no exit fees.",
                                        },
                                    ].map((s) => (
                                        <div key={s.title} className="flex gap-3">
                                            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-black/6 shrink-0 mt-0.5">
                                                <s.icon className="h-3.5 w-3.5 text-black/60" aria-hidden="true" />
                                            </div>
                                            <div>
                                                <p className="text-xs text-black/70 font-semibold">{s.title}</p>
                                                <p className="mt-0.5 text-xs leading-relaxed text-black/60 font-medium">{s.body}</p>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </motion.div>

                {/* ── Savings goals + portfolio overview ───────────────────── */}
                <SavingsGoalsSection onCreateGoal={() => setPlanModalOpen(true)} />
                {isConnected && <SavingsOverview savingsVaults={savingsVaults} />}

                {/* ── Filter tabs ──────────────────────────────────────────── */}
                <div id="vault-grid" className="mb-6 flex gap-1.5 border-b border-black/8 pb-px overflow-x-auto scrollbar-hide" role="tablist" aria-label="Savings plan filters">
                    {FILTERS.map((f) => (
                        <button
                            key={f.value}
                            role="tab"
                            aria-selected={filter === f.value}
                            onClick={() => setFilter(f.value)}
                            className={cn(
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0 focus-visible:ring-2 focus-visible:ring-black focus-visible:ring-offset-2",
                                filter === f.value
                                    ? "text-black font-semibold"
                                    : "text-black/60 hover:text-black/80 font-medium"
                            )}
                        >
                            {f.label}
                            {filter === f.value && (
                                <motion.div
                                    layoutId="savings-tab"
                                    className="absolute bottom-0 left-0 right-0 h-0.5 bg-black rounded-full"
                                    aria-hidden="true"
                                />
                            )}
                        </button>
                    ))}
                </div>

                {/* ── Vault grid ───────────────────────────────────────────── */}
                <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                    {filtered.map((vault, i) => (
                        <SavingsVaultCard
                            key={vault.id}
                            vault={vault}
                            index={i}
                            onDeposit={setSelectedVault}
                            isApyLoading={yieldLoading}
                        />
                    ))}
                </div>

                {/* ── Open positions ──────────────────────────────────────── */}
                {(() => {
                    const savingsIds = savingsVaults.map((v) => v.id);
                    const savingsPositions = positions.filter((p) => savingsIds.includes(p.vaultId));
                    if (savingsPositions.length === 0) return null;
                    return (
                        <motion.div
                            initial={{ opacity: 0, y: 10 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ delay: 0.2 }}
                            className="mt-8"
                        >
                            <h2 className="text-sm font-semibold text-black mb-3">Your Savings Positions</h2>
                            <PositionCards positions={savingsPositions} />
                        </motion.div>
                    );
                })()}

            <DepositModal vault={selectedVault} onClose={() => setSelectedVault(null)} />
            {planModalOpen && <CreatePlanModal onClose={() => setPlanModalOpen(false)} />}
        </AppShell>
    );
}
