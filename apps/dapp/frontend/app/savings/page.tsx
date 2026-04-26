"use client";

import { useState, useEffect } from "react";
import { ProtectedRoute } from "@/components/protected-route";
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
} from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { cn } from "@/lib/utils";
import { PositionCards } from "@/components/position-cards";
import {
    executeVaultDeposit,
    UserRejectedError,
    TransactionFailedError,
    TransactionTimeoutError,
    truncateTxHash,
} from "@/lib/stellar/transaction";

// ── Types ─────────────────────────────────────────────────────────────────────

type SavingsVaultType = "flexible" | "auto-compound" | "stablecoin-yield" | "custom";

interface SavingsVault {
    id: string;
    type: SavingsVaultType;
    name: string;
    description: string;
    summary: string; // short tooltip summary
    apy: number;
    apyLabel: string;
    lockDays: number | null;
    penaltyPct: number;
    badge: string;
    features: string[];
    supportedAssets: ("USDC" | "XLM")[];
    contractAddress: string;
}

// ── Vault Definitions ─────────────────────────────────────────────────────────

const SAVINGS_VAULTS: SavingsVault[] = [
    {
        id: "flexible-savings",
        type: "flexible",
        name: "Flexible Savings",
        description:
            "Earn yield on your USDC with no lockup period. Deposit and withdraw anytime — ideal for an emergency fund or short-term savings.",
        summary:
            "No lock period. Funds sit in audited lending pools and accrue yield daily. Withdraw the full balance at any time with no fees.",
        apy: 0.052,
        apyLabel: "4–6%",
        lockDays: null,

        penaltyPct: 0,
        badge: "No lockup",
        features: ["Withdraw anytime", "No exit fee", "Daily yield accrual"],
        supportedAssets: ["USDC", "XLM"],
        contractAddress: "CINVALIDCONTRACTADDRESS00000000000000000000000000000000000",
    },
    {
        id: "auto-compound",
        type: "auto-compound",
        name: "Auto-Compound",
        description:
            "Yield is automatically reinvested every 24 hours, compounding your returns without any manual action. Set it and grow.",
        summary:
            "Yield harvested daily and re-deposited automatically. Effective APY is higher than the base rate due to continuous compounding. No manual claiming needed.",
        apy: 0.088,
        apyLabel: "8–10%",
        lockDays: null,

        penaltyPct: 0,
        badge: "Auto-reinvest",
        features: ["Daily auto-compounding", "No manual claiming", "No exit fee"],
        supportedAssets: ["USDC", "XLM"],
        contractAddress: "CINVALIDCONTRACTADDRESS00000000000000000000000000000000000",
    },
    {
        id: "stablecoin-yield",
        type: "stablecoin-yield",
        name: "Stablecoin Yield",
        description:
            "Spread across USDC and XLM liquidity pools for diversified, optimised stable yield.",
        summary:
            "Funds are split across USDC/XLM pools. Rebalanced weekly to chase the highest stable yield. Minimises single-protocol risk while keeping APY competitive.",
        apy: 0.105,
        apyLabel: "9–12%",
        lockDays: null,

        penaltyPct: 0,
        badge: "Multi-pool",
        features: ["Multi-stablecoin exposure", "Weekly rebalance", "No exit fee"],
        supportedAssets: ["USDC", "XLM"],
        contractAddress: "CINVALIDCONTRACTADDRESS00000000000000000000000000000000000",
    },
    {
        id: "custom-savings",
        type: "custom",
        name: "Custom Goal",
        description:
            "Name your goal, set a target amount and timeline. Track progress toward a specific savings target — holiday, house deposit, anything.",
        summary:
            "Same underlying yield as Flexible Savings, but wrapped in goal tracking. Set a name, target amount, and target date. See progress in your portfolio.",
        apy: 0.052,
        apyLabel: "4–6%",
        lockDays: null,

        penaltyPct: 0,
        badge: "Goal-based",
        features: ["Named savings goal", "Target amount tracking", "Withdraw anytime"],
        supportedAssets: ["USDC", "XLM"],
        contractAddress: "CINVALIDCONTRACTADDRESS00000000000000000000000000000000000",
    },
];

const TYPE_ICONS: Record<SavingsVaultType, React.ElementType> = {
    flexible: Unlock,
    "auto-compound": RefreshCcw,
    "stablecoin-yield": BarChart2,
    custom: Sliders,
};

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
                className="flex h-6 w-6 items-center justify-center rounded-full border border-black/12 text-black/35 hover:border-black/25 hover:text-black/60 transition-colors"
                tabIndex={-1}
                aria-label="More info"
            >
                <Info className="h-3 w-3" />
            </button>
            <AnimatePresence>
                {show && (
                    <motion.div
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 4 }}
                        transition={{ duration: 0.15 }}
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-60 rounded-xl border border-black/8 bg-white px-3.5 py-3 shadow-lg shadow-black/6 text-xs text-black/55 leading-relaxed pointer-events-none"
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
            className="group flex flex-col rounded-2xl border border-black/8 bg-white px-7 py-6 transition-all hover:border-black/18 hover:shadow-md"
        >
            {/* Header */}
            <div className="mb-6 flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/5 shrink-0">
                        <Icon className="h-4 w-4 text-black/50" />
                    </div>
                    <div>
                        <h3 className="text-sm text-black">{vault.name}</h3>
                        <span className="text-[10px] bg-black/5 text-black/45 rounded-full px-2 py-0.5 mt-0.5 inline-block">
                            {vault.badge}
                        </span>
                    </div>
                </div>
                <div className="flex items-start gap-2 shrink-0">
                    <div className="text-right">
                        <p className="font-mono text-2xl text-black leading-none">{vault.apyLabel}</p>
                        <p className="text-[10px] text-black/35 uppercase tracking-wide mt-1">APY</p>
                    </div>
                    <InfoTooltip text={vault.summary} />
                </div>
            </div>

            {/* Description */}
            <p className="mb-6 text-sm leading-relaxed text-black/50 flex-1">
                {vault.description}
            </p>

            {/* Features */}
            <div className="mb-6 space-y-2">
                {vault.features.map((f) => (
                    <div key={f} className="flex items-center gap-2.5">
                        <div className="h-1 w-1 rounded-full bg-black/30 shrink-0" />
                        <span className="text-xs text-black/45">{f}</span>
                    </div>
                ))}
            </div>

            {/* Meta row */}
            <div className="mb-6 grid grid-cols-2 gap-2">
                <div className="rounded-xl bg-black/[0.025] px-3 py-3 text-center">
                    <p className="font-mono text-sm text-black">
                        {vault.lockDays ? `${vault.lockDays}d` : "None"}
                    </p>
                    <p className="text-[10px] text-black/35 mt-0.5">Lock</p>
                </div>
                <div className="rounded-xl bg-black/[0.025] px-3 py-3 text-center">
                    <p className="font-mono text-sm text-black">{vault.penaltyPct}%</p>
                    <p className="text-[10px] text-black/35 mt-0.5">Exit fee</p>
                </div>
            </div>

            {/* CTA */}
            <button
                onClick={() => onDeposit(vault)}
                className="flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3 text-sm text-white transition-opacity hover:opacity-75"
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
    const { balances, recordDeposit, refreshBalances } = usePortfolio();
    const { address } = useWallet();
    const [amount, setAmount] = useState("");
    const [goalName, setGoalName] = useState("");
    const [selectedAsset, setSelectedAsset] = useState<"USDC" | "XLM">("USDC");
    const [txState, setTxState] = useState<"idle" | "building" | "signing" | "submitting" | "success" | "error">("idle");
    const [errorMsg, setErrorMsg] = useState("");
    const [txHash, setTxHash] = useState("");

    useEffect(() => {
        if (!vault) {
            setAmount("");
            setGoalName("");
            setSelectedAsset("USDC");
        } else {
            setSelectedAsset(vault.supportedAssets[0] ?? "USDC");
        }
    }, [vault]);

    if (!vault) return null;

    const supportedAssets = vault.supportedAssets;
    const available = balances[selectedAsset] ?? 0;
    const parsedAmount = parseFloat(amount) || 0;
    const projectedYield = parsedAmount * vault.apy * ((vault.lockDays ?? 365) / 365);
    const maturityDate = vault.lockDays
        ? new Date(Date.now() + vault.lockDays * 86400000).toLocaleDateString("en-US", {
              month: "short",
              day: "numeric",
              year: "numeric",
          })
        : null;
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
                    >
                        {/* Drag handle (mobile) */}
                        <div className="flex justify-center pt-4 pb-1 sm:hidden">
                            <div className="h-1 w-10 rounded-full bg-black/10" />
                        </div>

                        <div className="px-8 pb-8 pt-6 sm:px-10 sm:pb-10 sm:pt-8">
                            {/* Header */}
                            <div className="mb-8 flex items-start justify-between">
                                <div className="flex items-center gap-4">
                                    <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-black/5 shrink-0">
                                        <Icon className="h-5 w-5 text-black/50" />
                                    </div>
                                    <div>
                                        <h2 className="text-lg text-black">{vault.name}</h2>
                                        <p className="text-xs text-black/40 mt-0.5">
                                            <span className="font-mono">{vault.apyLabel}</span> APY
                                            {vault.lockDays
                                                ? ` · ${vault.lockDays}-day lock`
                                                : " · no lockup"}
                                        </p>
                                    </div>
                                </div>
                                <button
                                    onClick={onClose}
                                    className="flex h-9 w-9 items-center justify-center rounded-xl border border-black/10 text-black/35 hover:text-black transition-colors shrink-0"
                                >
                                    <X className="h-4 w-4" />
                                </button>
                            </div>

                            {/* Two-column layout on sm+ */}
                            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
                                {/* Left: inputs */}
                                <div className="space-y-5">
                                    {/* Amount input */}
                                    <div>
                                        <div className="mb-2 flex items-center justify-between">
                                            <label className="text-xs text-black/45">
                                                Amount ({selectedAsset})
                                            </label>
                                            {supportedAssets.length > 1 && (
                                                <div className="flex rounded-full border border-black/10 bg-black/[0.03] p-0.5">
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
                                                                    : "text-black/50 hover:text-black"
                                                            )}
                                                        >
                                                            {a}
                                                        </button>
                                                    ))}
                                                </div>
                                            )}
                                        </div>
                                        <div className="relative">
                                            <span className="absolute left-4 top-1/2 -translate-y-1/2 font-mono text-sm text-black/35">
                                                {selectedAsset === "XLM" ? "✦" : "$"}
                                            </span>
                                            <input
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
                                        <div className="mt-2 flex items-center justify-end text-xs text-black/35">
                                            <button
                                                onClick={() => setAmount(String(available))}
                                                className="hover:text-black transition-colors"
                                            >
                                                Available: <span className="font-mono">{available.toFixed(2)}</span> {selectedAsset}
                                            </button>
                                        </div>
                                        {overBalance && (
                                            <p className="mt-1.5 text-xs text-red-400">
                                                Exceeds available balance
                                            </p>
                                        )}
                                    </div>

                                    {/* Quick amounts */}
                                    <div>
                                        <p className="mb-2 text-xs text-black/35">Quick amounts</p>
                                        <div className="flex gap-2 flex-wrap">
                                            {[50, 100, 250, 500].map((v) => (
                                                <button
                                                    key={v}
                                                    onClick={() => setAmount(String(v))}
                                                    className={cn(
                                                        "rounded-lg border px-3 py-1.5 font-mono text-xs transition-colors",
                                                        amount === String(v)
                                                            ? "border-black bg-black text-white"
                                                            : "border-black/10 text-black/50 hover:border-black/20 hover:text-black"
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
                                            <label className="mb-2 block text-xs text-black/45">
                                                Goal name
                                            </label>
                                            <input
                                                type="text"
                                                value={goalName}
                                                onChange={(e) => setGoalName(e.target.value)}
                                                placeholder="e.g. Holiday fund, House deposit…"
                                                className="h-12 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm text-black outline-none transition-colors focus:border-black/25"
                                            />
                                        </div>
                                    )}
                                </div>

                                {/* Right: projection + summary */}
                                <div className="flex flex-col gap-4">
                                    {/* Projection block */}
                                    <div className="rounded-2xl border border-black/8 bg-black/[0.018] p-5 flex-1">
                                        <p className="mb-4 text-xs text-black/40 uppercase tracking-widest">
                                            Projection
                                        </p>
                                        <div className="space-y-3">
                                            <div className="flex justify-between items-baseline">
                                                <span className="text-xs text-black/40">Deposit</span>
                                                <span className="font-mono text-sm text-black">
                                                    {parsedAmount > 0 ? parsedAmount.toFixed(2) : "—"}
                                                </span>
                                            </div>
                                            <div className="flex justify-between items-baseline">
                                                <span className="text-xs text-black/40">
                                                    Yield ({vault.lockDays ? `${vault.lockDays}d` : "1yr"})
                                                </span>
                                                <span className="font-mono text-sm text-black">
                                                    {parsedAmount > 0 ? `+${projectedYield.toFixed(4)}` : "—"}
                                                </span>
                                            </div>
                                            <div className="border-t border-black/6 pt-3 flex justify-between items-baseline">
                                                <span className="text-xs text-black/40">Total at maturity</span>
                                                <span className="font-mono text-base text-black">
                                                    {parsedAmount > 0
                                                        ? (parsedAmount + projectedYield).toFixed(2)
                                                        : "—"}
                                                </span>
                                            </div>
                                            {maturityDate && parsedAmount > 0 && (
                                                <div className="flex justify-between items-baseline pt-1">
                                                    <span className="text-xs text-black/40">Matures</span>
                                                    <span className="text-xs text-black/60">{maturityDate}</span>
                                                </div>
                                            )}
                                        </div>
                                    </div>

                                    {/* Vault features summary */}
                                    <div className="space-y-2">
                                        {vault.features.map((f) => (
                                            <div key={f} className="flex items-center gap-2">
                                                <div className="h-1 w-1 rounded-full bg-black/25 shrink-0" />
                                                <span className="text-xs text-black/40">{f}</span>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            </div>

                            {/* Error */}
                            {txState === "error" && errorMsg && (
                                <p className="mt-4 rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-600">{errorMsg}</p>
                            )}

                            {/* CTA */}
                            <button
                                disabled={!canSubmit || txState !== "idle"}
                                onClick={async () => {
                                    if (!vault || !canSubmit || !address) return;
                                    setErrorMsg("");
                                    try {
                                        setTxState("building");
                                        const receipt = await executeVaultDeposit({
                                            walletAddress: address,
                                            vaultId: vault.id,
                                            contractId: vault.contractAddress,
                                            asset: selectedAsset,
                                            amount: parsedAmount,
                                        });

                                        setTxHash(receipt.txHash);
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
                                            txHash: receipt.txHash,
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
                                className="mt-8 flex w-full items-center justify-center gap-2 rounded-xl bg-black py-3.5 text-sm text-white transition-opacity disabled:opacity-35"
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

// ── Summary Bar ───────────────────────────────────────────────────────────────

function SavingsSummary({ positions }: { positions: ReturnType<typeof usePortfolio>["positions"] }) {
    const ids = SAVINGS_VAULTS.map((v) => v.id);
    const savingsPositions = positions.filter((p) => ids.includes(p.vaultId));
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
                <div key={stat.label} className="rounded-2xl border border-black/8 bg-white p-4 sm:p-5">
                    <div className="mb-3 flex h-7 w-7 items-center justify-center rounded-lg bg-black/5">
                        <stat.icon className="h-3.5 w-3.5 text-black/40" />
                    </div>
                    <p className="font-mono text-xl text-black sm:text-2xl">{stat.value}</p>
                    <p className="mt-0.5 text-[10px] text-black/35 sm:text-xs">{stat.label}</p>
                </div>
            ))}
        </motion.div>
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
    const { positions } = usePortfolio();
    const [filter, setFilter] = useState<SavingsVaultType | "all">("all");
    const [selectedVault, setSelectedVault] = useState<SavingsVault | null>(null);
    const [showHowItWorks, setShowHowItWorks] = useState(false);

    const filtered =
        filter === "all"
            ? SAVINGS_VAULTS
            : SAVINGS_VAULTS.filter((v) => v.type === filter);

    return (
        <ProtectedRoute>
            <AppShell>

                {/* ── Page header ──────────────────────────────────────────── */}
                <motion.div
                    initial={{ opacity: 0, y: -8 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="mb-7"
                >
                    <div className="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <h1 className="text-2xl text-black sm:text-3xl">Savings</h1>
                            <p className="mt-1 text-sm text-black/40">
                                Choose a savings plan and start earning yield on your USDC.
                            </p>
                        </div>
                        <button
                            onClick={() => setShowHowItWorks(!showHowItWorks)}
                            className="flex items-center gap-2 rounded-xl border border-black/10 px-4 py-2 text-xs text-black/50 hover:border-black/20 hover:text-black transition-all shrink-0"
                        >
                            <Info className="h-3.5 w-3.5" />
                            How it works
                            <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", showHowItWorks && "rotate-180")} />
                        </button>
                    </div>

                    <AnimatePresence>
                        {showHowItWorks && (
                            <motion.div
                                initial={{ opacity: 0, height: 0 }}
                                animate={{ opacity: 1, height: "auto" }}
                                exit={{ opacity: 0, height: 0 }}
                                className="overflow-hidden"
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
                                                <s.icon className="h-3.5 w-3.5 text-black/40" />
                                            </div>
                                            <div>
                                                <p className="text-xs text-black/60">{s.title}</p>
                                                <p className="mt-0.5 text-xs leading-relaxed text-black/35">{s.body}</p>
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
                                "relative pb-3 px-1 mr-4 text-sm whitespace-nowrap transition-colors shrink-0",
                                filter === f.value
                                    ? "text-black"
                                    : "text-black/35 hover:text-black/55"
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
                <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                    {filtered.map((vault, i) => (
                        <SavingsVaultCard
                            key={vault.id}
                            vault={vault}
                            index={i}
                            onDeposit={setSelectedVault}
                        />
                    ))}
                </div>

                {/* ── Open positions ──────────────────────────────────────── */}
                {(() => {
                    const savingsIds = SAVINGS_VAULTS.map((v) => v.id);
                    const savingsPositions = positions.filter((p) => savingsIds.includes(p.vaultId));
                    if (savingsPositions.length === 0) return null;
                    return (
                        <motion.div
                            initial={{ opacity: 0, y: 10 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ delay: 0.2 }}
                            className="mt-8"
                        >
                            <h2 className="text-sm text-black mb-3">Your Savings Positions</h2>
                            <PositionCards positions={savingsPositions} />
                        </motion.div>
                    );
                })()}

            <DepositModal vault={selectedVault} onClose={() => setSelectedVault(null)} />
        </AppShell>
        </ProtectedRoute>
    );
}
