"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    AlertCircle,
    CheckCircle2,
    Clock3,
    ExternalLink,
    Loader2,
    ShieldCheck,
    Sparkles,
    X,
} from "lucide-react";

import {
    usePortfolio,
    type PortfolioPosition,
} from "@/components/portfolio-provider";
import {
    buildMockTransactionXdr,
    signWithWalletOrMock,
    simulateSubmission,
} from "@/lib/mock-soroban";
import { cn } from "@/lib/utils";
import { type VaultDefinition } from "@/lib/vault-data";
import { useWallet } from "@/components/wallet-provider";

type ActionState = "input" | "confirming" | "submitting" | "success" | "error";

function formatCurrency(amount: number) {
    return amount.toLocaleString("en-US", {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    });
}

function ModalShell({
    open,
    onClose,
    title,
    subtitle,
    children,
}: {
    open: boolean;
    onClose: () => void;
    title: string;
    subtitle: string;
    children: React.ReactNode;
}) {
    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="fixed inset-0 z-[100] bg-black/45 px-4 py-8 backdrop-blur-sm"
                >
                    <div className="flex min-h-full items-center justify-center">
                        <motion.div
                            initial={{ opacity: 0, y: 24, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: 12, scale: 0.98 }}
                            transition={{ duration: 0.2 }}
                            className="w-full max-w-2xl overflow-hidden rounded-[28px] border border-white/10 bg-[#fafafa] shadow-2xl"
                        >
                            <div className="flex items-start justify-between border-b border-border px-6 py-5">
                                <div>
                                    <p className="text-xs font-mono uppercase tracking-[0.18em] text-muted-foreground">
                                        Vault Action
                                    </p>
                                    <h2 className="mt-2 font-heading text-2xl font-light text-foreground">
                                        {title}
                                    </h2>
                                    <p className="mt-1 text-sm text-muted-foreground">
                                        {subtitle}
                                    </p>
                                </div>
                                <button
                                    onClick={onClose}
                                    className="rounded-full border border-border bg-white p-2 text-muted-foreground transition-colors hover:text-foreground"
                                >
                                    <X className="h-4 w-4" />
                                </button>
                            </div>
                            {children}
                        </motion.div>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

export function DepositModal({
    open,
    onClose,
    vault,
}: {
    open: boolean;
    onClose: () => void;
    vault: VaultDefinition | null;
}) {
    const { address } = useWallet();
    const { getAvailableBalance, recordDeposit } = usePortfolio();
    const [amountInput, setAmountInput] = useState("");
    const [state, setState] = useState<ActionState>("input");
    const [error, setError] = useState("");
    const [receipt, setReceipt] = useState<{
        txHash: string;
        explorerUrl: string;
        walletPopupUsed: boolean;
    } | null>(null);

    const amount = Number(amountInput) || 0;
    const balance = getAvailableBalance(vault?.asset ?? "USDC");
    const canSubmit = !!vault && !!address && amount > 0 && amount <= balance;
    const estimatedYield = vault ? amount * vault.apy : 0;
    const sharesReceived = amount;

    const reset = () => {
        setAmountInput("");
        setState("input");
        setError("");
        setReceipt(null);
        onClose();
    };

    const handleDeposit = async () => {
        if (!vault || !address || !canSubmit) return;

        setError("");
        setState("confirming");

        try {
            const txXdr = await buildMockTransactionXdr(
                address,
                `deposit:${vault.id}:${amount.toFixed(2)}`
            );
            const { walletPopupUsed } = await signWithWalletOrMock(txXdr);

            setState("submitting");
            const submission = await simulateSubmission();

            recordDeposit({
                vault,
                amount,
                txHash: submission.txHash,
            });

            setReceipt({
                txHash: submission.txHash,
                explorerUrl: submission.explorerUrl,
                walletPopupUsed,
            });
            setState("success");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Deposit failed");
            setState("error");
        }
    };

    return (
        <ModalShell
            open={open && !!vault}
            onClose={reset}
            title={`Deposit into ${vault?.name ?? "Vault"}`}
            subtitle="Review expected yield, lock terms, and the signing flow before committing funds."
        >
            {vault && (
                <div className="grid gap-0 lg:grid-cols-[1.05fr_0.95fr]">
                    <div className="border-b border-border p-6 lg:border-b-0 lg:border-r">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <div className="flex items-start justify-between">
                                <div>
                                    <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                        {vault.name}
                                    </p>
                                    <p className="mt-2 font-heading text-3xl font-light text-emerald-600">
                                        {vault.apyLabel}
                                    </p>
                                </div>
                                <div className="rounded-2xl bg-secondary px-3 py-2 text-right">
                                    <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                                        Balance
                                    </p>
                                    <p className="mt-1 text-sm font-medium text-foreground">
                                        {formatCurrency(balance)} USDC
                                    </p>
                                </div>
                            </div>

                            <div className="mt-6">
                                <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                                    Deposit Amount
                                </label>
                                <div className="flex items-center gap-3 rounded-2xl border border-border bg-[#fafafa] px-4 py-4">
                                    <input
                                        type="text"
                                        inputMode="decimal"
                                        value={amountInput}
                                        onChange={(event) => {
                                            const next = event.target.value;
                                            if (/^\d*\.?\d*$/.test(next)) {
                                                setAmountInput(next);
                                                setState("input");
                                            }
                                        }}
                                        placeholder="0.00"
                                        className="min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40"
                                    />
                                    <div className="flex items-center gap-2">
                                        <span className="rounded-full bg-white px-3 py-2 text-sm font-medium text-foreground shadow-sm">
                                            USDC
                                        </span>
                                        <button
                                            onClick={() => setAmountInput(balance.toFixed(2))}
                                            className="rounded-full border border-border bg-white px-3 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15"
                                        >
                                            Max
                                        </button>
                                    </div>
                                </div>
                                <p className="mt-2 text-xs text-muted-foreground">
                                    Available from connected wallet: {formatCurrency(balance)} USDC
                                </p>
                            </div>

                            <div className="mt-6 space-y-3 rounded-2xl border border-border bg-secondary/30 p-4">
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Estimated annual yield</span>
                                    <span className="font-medium text-foreground">
                                        {formatCurrency(estimatedYield)} USDC
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">nVault shares to receive</span>
                                    <span className="font-medium text-foreground">
                                        {formatCurrency(sharesReceived)}
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Lock period</span>
                                    <span className="font-medium text-foreground">
                                        {vault.lockDays} days
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Management fee (annual)</span>
                                    <span className="font-medium text-foreground">
                                        {vault.managementFeePct}%
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Performance fee (on yield)</span>
                                    <span className="font-medium text-foreground">
                                        {vault.performanceFeePct}%
                                    </span>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div className="p-6">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                Transaction Flow
                            </p>
                            <div className="mt-4 space-y-3">
                                {[
                                    {
                                        label: "Prepare contract call",
                                        done: state !== "input",
                                    },
                                    {
                                        label: "Request wallet signature",
                                        done: state === "submitting" || state === "success",
                                    },
                                    {
                                        label: "Submit and confirm",
                                        done: state === "success",
                                    },
                                ].map((step) => (
                                    <div
                                        key={step.label}
                                        className="flex items-center gap-3 rounded-2xl border border-border px-4 py-3"
                                    >
                                        <div
                                            className={cn(
                                                "flex h-8 w-8 items-center justify-center rounded-full border",
                                                step.done
                                                    ? "border-emerald-200 bg-emerald-50 text-emerald-600"
                                                    : "border-border bg-secondary/40 text-muted-foreground"
                                            )}
                                        >
                                            {step.done ? (
                                                <CheckCircle2 className="h-4 w-4" />
                                            ) : (
                                                <Clock3 className="h-4 w-4" />
                                            )}
                                        </div>
                                        <span className="text-sm text-foreground/80">
                                            {step.label}
                                        </span>
                                    </div>
                                ))}
                            </div>

                            {state === "success" && receipt ? (
                                <div className="mt-5 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                                    <div className="flex items-center gap-2 text-emerald-700">
                                        <CheckCircle2 className="h-4 w-4" />
                                        <p className="text-sm font-medium">
                                            Deposit confirmed
                                        </p>
                                    </div>
                                    <p className="mt-2 text-sm text-emerald-800/80">
                                        {formatCurrency(amount)} USDC was deposited into the {vault.name} vault.
                                    </p>
                                    <div className="mt-4 flex flex-wrap gap-2">
                                        <Link
                                            href={receipt.explorerUrl}
                                            target="_blank"
                                            className="inline-flex items-center gap-1.5 rounded-full bg-white px-3 py-2 text-xs font-medium text-foreground shadow-sm"
                                        >
                                            View on Explorer
                                            <ExternalLink className="h-3.5 w-3.5" />
                                        </Link>
                                        <span className="inline-flex items-center rounded-full border border-emerald-200 bg-white px-3 py-2 text-xs text-emerald-700">
                                            {receipt.walletPopupUsed
                                                ? "Wallet signature captured"
                                                : "Mock signature used"}
                                        </span>
                                    </div>
                                </div>
                            ) : (
                                <div className="mt-5 rounded-2xl border border-border bg-secondary/20 p-4">
                                    <div className="flex items-start gap-3">
                                        <ShieldCheck className="mt-0.5 h-4 w-4 text-emerald-600" />
                                        <div className="space-y-2 text-sm text-muted-foreground">
                                            <p>
                                                This flow uses a mock Soroban transaction envelope until the live vault contracts are ready on testnet.
                                            </p>
                                            <p>
                                                If your wallet supports signing this mock transaction, you will still get a real wallet popup before the simulated confirmation step.
                                            </p>
                                        </div>
                                    </div>
                                </div>
                            )}

                            {error && (
                                <div className="mt-4 rounded-2xl border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
                                    <div className="flex items-start gap-2">
                                        <AlertCircle className="mt-0.5 h-4 w-4" />
                                        <span>{error}</span>
                                    </div>
                                </div>
                            )}

                            <div className="mt-5 flex gap-3">
                                <button
                                    onClick={reset}
                                    className="flex-1 rounded-full border border-border bg-white px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-black/15"
                                >
                                    {state === "success" ? "Close" : "Cancel"}
                                </button>
                                {state !== "success" && (
                                    <button
                                        onClick={handleDeposit}
                                        disabled={!canSubmit || state === "confirming" || state === "submitting"}
                                        className="flex-1 rounded-full bg-brand-dark px-5 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                                    >
                                        {state === "confirming" && (
                                            <span className="inline-flex items-center gap-2">
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                                Awaiting Signature
                                            </span>
                                        )}
                                        {state === "submitting" && (
                                            <span className="inline-flex items-center gap-2">
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                                Submitting
                                            </span>
                                        )}
                                        {(state === "input" || state === "error") &&
                                            "Confirm Deposit"}
                                    </button>
                                )}
                            </div>
                        </div>
                    </div>
                </div>
            )}
        </ModalShell>
    );
}

export function WithdrawModal({
    open,
    onClose,
    position,
}: {
    open: boolean;
    onClose: () => void;
    position: PortfolioPosition | null;
}) {
    const { address } = useWallet();
    const { getWithdrawalQuote, recordWithdrawal } = usePortfolio();
    const [amountInput, setAmountInput] = useState("");
    const [state, setState] = useState<ActionState>("input");
    const [error, setError] = useState("");
    const [receipt, setReceipt] = useState<{
        txHash: string;
        explorerUrl: string;
        walletPopupUsed: boolean;
        penaltyAmount: number;
        netAmount: number;
    } | null>(null);

    const amount = Number(amountInput) || 0;
    const quote = useMemo(
        () => (position ? getWithdrawalQuote(position.id, amount) : null),
        [amount, getWithdrawalQuote, position]
    );

    const canSubmit =
        !!position &&
        !!address &&
        amount > 0 &&
        amount <= position.currentValue &&
        !!quote;

    const reset = () => {
        setAmountInput("");
        setState("input");
        setError("");
        setReceipt(null);
        onClose();
    };

    const handleWithdraw = async () => {
        if (!position || !address || !quote) return;

        setError("");
        setState("confirming");

        try {
            const txXdr = await buildMockTransactionXdr(
                address,
                `withdraw:${position.vaultId}:${amount.toFixed(2)}`
            );
            const { walletPopupUsed } = await signWithWalletOrMock(txXdr);

            setState("submitting");
            const submission = await simulateSubmission();
            const result = recordWithdrawal({
                positionId: position.id,
                grossAmount: quote.grossAmount,
                txHash: submission.txHash,
            });

            if (!result) {
                throw new Error("Unable to complete the withdrawal");
            }

            setReceipt({
                txHash: submission.txHash,
                explorerUrl: submission.explorerUrl,
                walletPopupUsed,
                penaltyAmount: result.penaltyAmount,
                netAmount: result.netAmount,
            });
            setState("success");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Withdrawal failed");
            setState("error");
        }
    };

    return (
        <ModalShell
            open={open && !!position}
            onClose={reset}
            title={`Withdraw from ${position?.vaultName ?? "Vault"}`}
            subtitle="Review maturity, penalty, and expected net proceeds before signing."
        >
            {position && (
                <div className="grid gap-0 lg:grid-cols-[1.05fr_0.95fr]">
                    <div className="border-b border-border p-6 lg:border-b-0 lg:border-r">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <div className="grid gap-3 sm:grid-cols-2">
                                <div className="rounded-2xl border border-border bg-secondary/20 p-4">
                                    <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                                        Current value
                                    </p>
                                    <p className="mt-2 font-heading text-3xl font-light text-foreground">
                                        {formatCurrency(position.currentValue)}
                                    </p>
                                    <p className="mt-1 text-xs text-muted-foreground">
                                        {formatCurrency(position.shares)} nVault shares
                                    </p>
                                </div>
                                <div className="rounded-2xl border border-border bg-secondary/20 p-4">
                                    <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                                        Yield earned
                                    </p>
                                    <p className="mt-2 font-heading text-3xl font-light text-emerald-600">
                                        {formatCurrency(position.yieldEarned)}
                                    </p>
                                    <p className="mt-1 text-xs text-muted-foreground">
                                        Since deposit
                                    </p>
                                </div>
                            </div>

                            <div className="mt-4 rounded-2xl border border-border bg-[#fafafa] p-4">
                                <div className="flex items-start justify-between gap-4">
                                    <div>
                                        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                            Maturity
                                        </p>
                                        <p className="mt-2 text-sm font-medium text-foreground">
                                            {position.isMatured
                                                ? "Matured - no penalty applies"
                                                : `${position.daysRemaining} day${position.daysRemaining === 1 ? "" : "s"} remaining`}
                                        </p>
                                    </div>
                                    <span
                                        className={cn(
                                            "rounded-full px-3 py-2 text-xs font-medium",
                                            position.isMatured
                                                ? "bg-emerald-50 text-emerald-700"
                                                : "bg-amber-50 text-amber-700"
                                        )}
                                    >
                                        {position.isMatured
                                            ? "Penalty free"
                                            : `${position.earlyWithdrawalPenaltyPct.toFixed(1)}% early exit`}
                                    </span>
                                </div>

                                <div className="mt-4">
                                    <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                                        Withdrawal Amount
                                    </label>
                                    <div className="flex items-center gap-3 rounded-2xl border border-border bg-white px-4 py-4">
                                        <input
                                            type="text"
                                            inputMode="decimal"
                                            value={amountInput}
                                            onChange={(event) => {
                                                const next = event.target.value;
                                                if (/^\d*\.?\d*$/.test(next)) {
                                                    setAmountInput(next);
                                                    setState("input");
                                                }
                                            }}
                                            placeholder="0.00"
                                            className="min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40"
                                        />
                                        <div className="flex items-center gap-2">
                                            <span className="rounded-full bg-secondary px-3 py-2 text-sm font-medium text-foreground">
                                                USDC
                                            </span>
                                            <button
                                                onClick={() =>
                                                    setAmountInput(position.currentValue.toFixed(2))
                                                }
                                                className="rounded-full border border-border bg-white px-3 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15"
                                            >
                                                Max
                                            </button>
                                        </div>
                                    </div>
                                </div>

                                <div className="mt-4 space-y-3">
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Gross withdrawal</span>
                                        <span className="font-medium text-foreground">
                                            {formatCurrency(quote?.grossAmount ?? 0)} USDC
                                        </span>
                                    </div>
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Performance Fee (10% of yield)</span>
                                        <span className="font-medium text-foreground">
                                            {formatCurrency(Math.max(0, (quote?.grossAmount ?? 0) - (quote?.sharesBurned ?? 0)) * 0.1)} USDC
                                        </span>
                                    </div>
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Net amount to wallet</span>
                                        <span className="font-medium text-foreground">
                                            {formatCurrency(quote?.netAmount ?? 0)} USDC
                                        </span>
                                    </div>
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Shares burned</span>
                                        <span className="font-medium text-foreground">
                                            {formatCurrency(quote?.sharesBurned ?? 0)}
                                        </span>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>

                    <div className="p-6">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                Confirmation
                            </p>
                            <div className="mt-4 rounded-2xl border border-border bg-secondary/20 p-4 text-sm text-muted-foreground">
                                <div className="flex items-start gap-3">
                                    <Sparkles className="mt-0.5 h-4 w-4 text-foreground/70" />
                                    <div className="space-y-2">
                                        <p>
                                            Partial withdrawals burn shares proportionally and leave the rest of the position invested.
                                        </p>
                                        <p>
                                            Full withdrawals burn all shares, release the full net balance, and remove the position from your dashboard.
                                        </p>
                                    </div>
                                </div>
                            </div>

                            {state === "success" && receipt ? (
                                <div className="mt-5 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                                    <div className="flex items-center gap-2 text-emerald-700">
                                        <CheckCircle2 className="h-4 w-4" />
                                        <p className="text-sm font-medium">
                                            Withdrawal confirmed
                                        </p>
                                    </div>
                                    <p className="mt-2 text-sm text-emerald-800/80">
                                        {formatCurrency(receipt.netAmount)} USDC is on its way back to your wallet.
                                    </p>
                                    <p className="mt-1 text-xs text-emerald-800/70">
                                        Penalty applied: {formatCurrency(receipt.penaltyAmount)} USDC
                                    </p>
                                    <div className="mt-4 flex flex-wrap gap-2">
                                        <Link
                                            href={receipt.explorerUrl}
                                            target="_blank"
                                            className="inline-flex items-center gap-1.5 rounded-full bg-white px-3 py-2 text-xs font-medium text-foreground shadow-sm"
                                        >
                                            View on Explorer
                                            <ExternalLink className="h-3.5 w-3.5" />
                                        </Link>
                                        <span className="inline-flex items-center rounded-full border border-emerald-200 bg-white px-3 py-2 text-xs text-emerald-700">
                                            {receipt.walletPopupUsed
                                                ? "Wallet signature captured"
                                                : "Mock signature used"}
                                        </span>
                                    </div>
                                </div>
                            ) : error ? (
                                <div className="mt-5 rounded-2xl border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
                                    <div className="flex items-start gap-2">
                                        <AlertCircle className="mt-0.5 h-4 w-4" />
                                        <span>{error}</span>
                                    </div>
                                </div>
                            ) : (
                                <div className="mt-5 rounded-2xl border border-border bg-white p-4">
                                    <div className="space-y-3">
                                        <div className="flex items-center justify-between text-sm">
                                            <span className="text-muted-foreground">Current position</span>
                                            <span className="font-medium text-foreground">
                                                {formatCurrency(position.currentValue)} USDC
                                            </span>
                                        </div>
                                        <div className="flex items-center justify-between text-sm">
                                            <span className="text-muted-foreground">Available now</span>
                                            <span className="font-medium text-foreground">
                                                {formatCurrency(quote?.netAmount ?? position.currentValue)} USDC
                                            </span>
                                        </div>
                                    </div>
                                </div>
                            )}

                            <div className="mt-5 flex gap-3">
                                <button
                                    onClick={reset}
                                    className="flex-1 rounded-full border border-border bg-white px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-black/15"
                                >
                                    {state === "success" ? "Close" : "Cancel"}
                                </button>
                                {state !== "success" && (
                                    <button
                                        onClick={handleWithdraw}
                                        disabled={!canSubmit || state === "confirming" || state === "submitting"}
                                        className="flex-1 rounded-full bg-brand-dark px-5 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                                    >
                                        {state === "confirming" && (
                                            <span className="inline-flex items-center gap-2">
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                                Awaiting Signature
                                            </span>
                                        )}
                                        {state === "submitting" && (
                                            <span className="inline-flex items-center gap-2">
                                                <Loader2 className="h-4 w-4 animate-spin" />
                                                Submitting
                                            </span>
                                        )}
                                        {(state === "input" || state === "error") &&
                                            "Confirm Withdrawal"}
                                    </button>
                                )}
                            </div>
                        </div>
                    </div>
                </div>
            )}
        </ModalShell>
    );
}
