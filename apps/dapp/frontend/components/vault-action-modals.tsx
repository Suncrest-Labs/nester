"use client";

import Link from "next/link";
import { useMemo, useState, useRef, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { validateAmount } from "@/lib/validation";
import { useFocusTrap } from "@/hooks/useFocusTrap";
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

import { cn } from "@/lib/utils";
import { useVaults, type Vault as VaultDefinition } from "@/hooks/useVaults";
import { useWallet } from "@/components/wallet-provider";
import { executeVaultDeposit, executeVaultWithdraw } from "@/lib/stellar/transaction";

import { useNetwork } from "@/hooks/useNetwork";

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
    const modalRef = useRef<HTMLDivElement>(null);
    useFocusTrap(modalRef, open);

    // ESC to close
    useEffect(() => {
        if (!open) return;
        const handleEsc = (e: KeyboardEvent) => {
            if (e.key === "Escape") onClose();
        };
        document.addEventListener("keydown", handleEsc);
        return () => document.removeEventListener("keydown", handleEsc);
    }, [open, onClose]);

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="fixed inset-0 z-[100] bg-black/45 sm:px-4 sm:py-8 backdrop-blur-sm"
                >
                    <div className="flex h-full sm:min-h-full items-end sm:items-center justify-center">
                        <motion.div
                            ref={modalRef}
                            role="dialog"
                            aria-modal="true"
                            aria-labelledby="modal-title"
                            initial={{ opacity: 0, y: 24, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: 12, scale: 0.98 }}
                            transition={{ duration: 0.2 }}
                            className="w-full h-full sm:h-auto sm:max-w-2xl overflow-hidden sm:rounded-[28px] border border-white/10 bg-[#fafafa] shadow-2xl sm:max-h-[90vh] flex flex-col"
                        >
                            <div className="flex items-start justify-between border-b border-border px-6 py-5">
                                <div>
                                    <p className="text-xs font-mono uppercase tracking-[0.18em] text-muted-foreground">
                                        Vault Action
                                    </p>
                                    <h2 id="modal-title" className="mt-2 font-heading text-2xl font-light text-foreground">
                                        {title}
                                    </h2>
                                    <p className="mt-1 text-sm text-muted-foreground">
                                        {subtitle}
                                    </p>
                                </div>
                                <button
                                    onClick={onClose}
                                    aria-label="Close modal"
                                    className="flex min-h-[44px] min-w-[44px] items-center justify-center rounded-full border border-border bg-white text-muted-foreground transition-colors hover:text-foreground active:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
                                >
                                    <X className="h-5 w-5" />
                                </button>
                            </div>
                            <div className="overflow-y-auto flex-1">
                                {children}
                            </div>
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
    const { currentNetwork } = useNetwork();
    const { address } = useWallet();
    const { getAvailableBalance, recordDeposit } = usePortfolio();
    const [state, setState] = useState<ActionState>("input");
    const [error, setError] = useState("");
    const [receipt, setReceipt] = useState<{
        txHash: string;
        explorerUrl: string;
        walletPopupUsed: boolean;
    } | null>(null);

    const [selectedAsset, setSelectedAsset] = useState<"USDC" | "XLM">("USDC");

    // Reset selected asset when vault changes
    const assets = ["USDC"] as ("USDC" | "XLM")[];
    const balance = getAvailableBalance(selectedAsset);

    const formSchema = useMemo(() => z.object({
        amount: validateAmount({
            min: 0.000001,
            balance: balance,
            maxDecimals: 6,
            minMessage: "Amount must be greater than 0",
            balanceMessage: `Amount exceeds your balance of ${formatCurrency(balance)} USDC`
        })
    }), [balance]);

    type FormValues = z.infer<typeof formSchema>;

    const {
        control,
        handleSubmit,
        watch,
        formState: { errors, isValid, isDirty },
        trigger,
        reset: resetForm
    } = useForm<FormValues>({
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        resolver: zodResolver(formSchema as any),
        mode: "onBlur",
        defaultValues: { amount: "" }
    });

    const amountInput = watch("amount");
    const amount = Number(amountInput) || 0;
    const [showLargeWarning, setShowLargeWarning] = useState(false);
    
    const canSubmit = !!vault && !!address && isValid && amount > 0;
    const estimatedYield = vault && vault.apy !== undefined ? amount * (vault.apy / 100) : 0;
    const sharesReceived = amount;

    const reset = () => {
        resetForm();
        setState("input");
        setError("");
        setReceipt(null);
        setShowLargeWarning(false);
        onClose();
    };

    const processDeposit = async () => {
        if (!vault || !address || !canSubmit) return;

        setError("");
        setState("confirming");
        setShowLargeWarning(false);

        try {
            const submission = await executeVaultDeposit({
                walletAddress: address,
                vaultId: vault.id,
                contractId: vault.contractAddress,
                asset: selectedAsset,
                amount,
            });

            recordDeposit({
                vault: { ...vault, asset: selectedAsset, apy: vault.apy || 0, lockDays: 0, earlyWithdrawalPenaltyPct: 0 },
                amount,
                txHash: submission.txHash,
            });

            await new Promise((resolve) => setTimeout(resolve, 5000));
            setReceipt({
                txHash: submission.txHash,
                explorerUrl: submission.explorerUrl,
                walletPopupUsed: true,
            });
            setState("success");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Deposit failed");
            setState("error");
        }
    };

    const handleDeposit = handleSubmit(() => {
        if (amount > 10000 && !showLargeWarning) {
            setShowLargeWarning(true);
            return;
        }
        processDeposit();
    });

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
                            <div className="mb-4">
                                <span className={cn(
                                    "text-xs font-medium px-2 py-1 rounded-full uppercase tracking-wider",
                                    currentNetwork.id === 'testnet' ? "bg-amber-100 text-amber-700" : "bg-emerald-100 text-emerald-700"
                                )}>
                                    {currentNetwork.id.toUpperCase()} TRANSACTION
                                </span>
                            </div>
                            <div className="flex items-start justify-between">
                                <div>
                                    <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                        {vault.name}
                                    </p>
                                    <p className="mt-2 font-heading text-3xl font-light text-emerald-600">
                                        {vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : "TBD"}
                                    </p>
                                    {vault.apy !== undefined && (
                                        <p className="text-[9px] text-black/40 mt-1 max-w-[200px]">APY is variable and based on recent performance. Past performance is not indicative of future results.</p>
                                    )}
                                </div>
                                <div className="rounded-2xl bg-secondary px-3 py-2 text-right">
                                    <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                                        Balance
                                    </p>
                                    <p className="mt-1 text-sm font-medium text-foreground">
                                        {formatCurrency(balance)} {selectedAsset}
                                    </p>
                                </div>
                            </div>

                            <div className="mt-6">
                                <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                                    Deposit Amount
                                </label>
                                <Controller
                                    name="amount"
                                    control={control}
                                    render={({ field: { onChange, onBlur, value } }) => (
                                        <>
                                            <div className={cn(
                                                "flex items-center gap-3 rounded-2xl border bg-[#fafafa] px-4 py-4",
                                                errors.amount ? "border-red-500" : "border-border"
                                            )}>
                                                <input
                                                    type="text"
                                                    inputMode="decimal"
                                                    value={value}
                                                    onChange={(event) => {
                                                        const next = event.target.value;
                                                        if (/^\d*\.?\d*$/.test(next)) {
                                                            onChange(next);
                                                            if (isDirty) trigger("amount");
                                                            setState("input");
                                                            setShowLargeWarning(false);
                                                        }
                                                    }}
                                                    onBlur={onBlur}
                                                    onPaste={() => setTimeout(() => trigger("amount"), 0)}
                                                    placeholder="0.00"
                                                    className={cn(
                                                        "min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40",
                                                        errors.amount && "text-red-500"
                                                    )}
                                                />
                                                <div className="flex items-center gap-2">
                                                    {assets.length > 1 ? (
                                                        <div className="flex rounded-full border border-border bg-white p-0.5 shadow-sm">
                                                            {assets.map((a) => (
                                                                <button
                                                                    key={a}
                                                                    type="button"
                                                                    onClick={() => {
                                                                        setSelectedAsset(a);
                                                                        onChange("");
                                                                        setShowLargeWarning(false);
                                                                    }}
                                                                    className={cn(
                                                                        "min-h-[var(--touch-target)] min-w-[var(--touch-target)] rounded-full px-3 text-xs font-medium transition-colors",
                                                                        selectedAsset === a
                                                                            ? "bg-foreground text-background"
                                                                            : "text-foreground/60 hover:text-foreground"
                                                                    )}
                                                                >
                                                                    {a}
                                                                </button>
                                                            ))}
                                                        </div>
                                                    ) : (
                                                        <span className="rounded-full bg-white px-3 py-2 text-sm font-medium text-foreground shadow-sm">
                                                            {selectedAsset}
                                                        </span>
                                                    )}
                                                    <button
                                                        type="button"
                                                        onClick={() => {
                                                            onChange(balance.toFixed(2));
                                                            trigger("amount");
                                                            setShowLargeWarning(false);
                                                        }}
                                                        className="min-h-[var(--touch-target)] min-w-[var(--touch-target)] rounded-full border border-border bg-white px-3 text-xs font-medium text-foreground transition-colors hover:border-black/15 active:bg-secondary"
                                                    >
                                                        Max
                                                    </button>
                                                </div>
                                            </div>
                                            <div className="flex justify-between mt-2">
                                                {errors.amount ? (
                                                    <span className="text-xs text-red-500 font-medium">{errors.amount.message}</span>
                                                ) : (
                                                    <span></span>
                                                )}
                                                <p className="text-xs text-muted-foreground">
                                                    Available from connected wallet: {formatCurrency(balance)} USDC
                                                </p>
                                            </div>
                                        </>
                                    )}
                                />
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
                                        Flexible
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Management fee (annual)</span>
                                    <span className="font-medium text-foreground">
                                        0.5%
                                    </span>
                                </div>
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-muted-foreground">Performance fee (on yield)</span>
                                    <span className="font-medium text-foreground">
                                        10%
                                    </span>
                                </div>
                                {currentNetwork.id === 'mainnet' && (
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Estimated Network Fee</span>
                                        <span className="font-medium text-foreground">
                                            ~0.00001 XLM
                                        </span>
                                    </div>
                                )}
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

                            {showLargeWarning && (
                                <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">
                                    <div className="flex items-start gap-2">
                                        <AlertCircle className="mt-0.5 h-4 w-4" />
                                        <span>
                                            You&apos;re about to deposit ${formatCurrency(amount)} — are you sure?
                                        </span>
                                    </div>
                                </div>
                            )}

                            <div className="mt-5 flex gap-3">
                                <button
                                    onClick={reset}
                                    className="flex-1 min-h-[var(--touch-target)] rounded-full border border-border bg-white px-5 text-sm font-medium text-foreground transition-colors hover:border-black/15 active:bg-secondary"
                                >
                                    {state === "success" ? "Close" : "Cancel"}
                                </button>
                                {state !== "success" && (
                                    <button
                                        onClick={handleDeposit}
                                        disabled={!canSubmit || state === "confirming" || state === "submitting"}
                                        className="flex-1 min-h-[var(--touch-target)] rounded-full bg-brand-dark px-5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.98]"
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
                                            (showLargeWarning ? "Yes, confirm deposit" : "Confirm Deposit")}
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
    const { currentNetwork } = useNetwork();
    const { address } = useWallet();
    const { getWithdrawalQuote, recordWithdrawal } = usePortfolio();

    const formSchema = useMemo(() => z.object({
        amount: validateAmount({
            min: 0.000001,
            balance: position?.currentValue || 0,
            maxDecimals: 6,
            minMessage: "Amount must be greater than 0",
            balanceMessage: `Amount exceeds your owned shares of ${formatCurrency(position?.currentValue || 0)}`
        })
    }), [position?.currentValue]);

    type FormValues = z.infer<typeof formSchema>;

    const {
        control,
        handleSubmit,
        watch,
        formState: { errors, isValid, isDirty },
        trigger,
        reset: resetForm
    } = useForm<FormValues>({
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        resolver: zodResolver(formSchema as any),
        mode: "onBlur",
        defaultValues: { amount: "" }
    });

    const amountInput = watch("amount");
    const amount = Number(amountInput) || 0;
    const [showLargeWarning, setShowLargeWarning] = useState(false);
    const [state, setState] = useState<ActionState>("input");
    const [error, setError] = useState("");
    const [receipt, setReceipt] = useState<{
        txHash: string;
        explorerUrl: string;
        walletPopupUsed: boolean;
        penaltyAmount: number;
        netAmount: number;
    } | null>(null);

    const quote = useMemo(
        () => (position ? getWithdrawalQuote(position.id, amount) : null),
        [amount, getWithdrawalQuote, position]
    );

    const canSubmit =
        !!position &&
        !!address &&
        isValid &&
        amount > 0 &&
        !!quote;

    const reset = () => {
        resetForm();
        setState("input");
        setError("");
        setReceipt(null);
        setShowLargeWarning(false);
        onClose();
    };

    const { data: vaults = [] } = useVaults();
    const vault = vaults.find(v => v.id === position?.vaultId);

    const processWithdrawal = async () => {
        if (!position || !address || !quote || !canSubmit || !vault) return;

        setError("");
        setState("confirming");
        setShowLargeWarning(false);

        try {
            const submission = await executeVaultWithdraw({
                walletAddress: address,
                vaultId: position.vaultId,
                contractId: vault.contractAddress,
                asset: position.asset as "USDC" | "XLM",
                shares: amount,
            });

            const result = recordWithdrawal({
                positionId: position.id,
                grossAmount: quote.grossAmount,
                txHash: submission.txHash,
            });

            if (!result) {
                throw new Error("Unable to complete the withdrawal");
            }

            await new Promise((resolve) => setTimeout(resolve, 5000));
            setReceipt({
                txHash: submission.txHash,
                explorerUrl: submission.explorerUrl,
                walletPopupUsed: true,
                penaltyAmount: result.penaltyAmount,
                netAmount: result.netAmount,
            });
            setState("success");
        } catch (err) {
            setError(err instanceof Error ? err.message : "Withdrawal failed");
            setState("error");
        }
    };

    const handleWithdraw = handleSubmit(() => {
        if (amount > 10000 && !showLargeWarning) {
            setShowLargeWarning(true);
            return;
        }
        processWithdrawal();
    });

    return (
        <ModalShell
            open={open && !!position}
            onClose={reset}
            title={`Withdraw from ${position?.vaultName ?? "Vault"}`}
            subtitle="Review maturity, penalty, and expected net proceeds before signing."
        >
            {position && (
                <div className="p-6 space-y-4">
                    {/* Position stats */}
                    <div className="grid gap-3 sm:grid-cols-2">
                        <div className="rounded-2xl border border-border bg-white p-4">
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
                        <div className="rounded-2xl border border-border bg-white p-4">
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

                    {/* Maturity */}
                    {!position.isMatured && (
                        <div className="flex items-start gap-2.5 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3">
                            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                            <div className="text-xs text-amber-800">
                                <p className="font-medium">Early withdrawal penalty applies</p>
                                <p className="mt-0.5">
                                    {position.daysRemaining} day{position.daysRemaining !== 1 ? "s" : ""} until maturity.
                                    A {position.earlyWithdrawalPenaltyPct.toFixed(1)}% fee will be deducted.
                                </p>
                            </div>
                        </div>
                    )}

                    {/* Amount input */}
                    <div>
                        <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                            Withdrawal Amount
                        </label>
                        <Controller
                            name="amount"
                            control={control}
                            render={({ field: { onChange, onBlur, value } }) => (
                                <>
                                    <div className={cn(
                                        "flex items-center gap-3 rounded-2xl border bg-white px-4 py-4",
                                        errors.amount ? "border-red-500" : "border-border"
                                    )}>
                                        <input
                                            type="text"
                                            inputMode="decimal"
                                            value={value}
                                            onChange={(event) => {
                                                const next = event.target.value;
                                                if (/^\d*\.?\d*$/.test(next)) {
                                                    onChange(next);
                                                    if (isDirty) trigger("amount");
                                                    setState("input");
                                                    setShowLargeWarning(false);
                                                }
                                            }}
                                            onBlur={onBlur}
                                            onPaste={() => setTimeout(() => trigger("amount"), 0)}
                                            placeholder="0.00"
                                            className={cn(
                                                "min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40",
                                                errors.amount && "text-red-500"
                                            )}
                                        />
                                        <div className="flex items-center gap-2">
                                            <span className="flex min-h-[var(--touch-target)] min-w-[var(--touch-target)] items-center justify-center rounded-full bg-secondary px-3 text-sm font-medium text-foreground">
                                                {position.asset ?? "USDC"}
                                            </span>
                                            <button
                                                onClick={() => {
                                                    onChange(position.currentValue.toFixed(2));
                                                    trigger("amount");
                                                    setShowLargeWarning(false);
                                                }}
                                                className="min-h-[var(--touch-target)] min-w-[var(--touch-target)] rounded-full border border-border bg-white px-3 text-xs font-medium text-foreground transition-colors hover:border-black/15 active:bg-secondary"
                                            >
                                                Max
                                            </button>
                                        </div>
                                    </div>
                                    {errors.amount && (
                                        <span className="text-xs text-red-500 font-medium mt-2 block">{errors.amount.message}</span>
                                    )}
                                </>
                            )}
                        />
                    </div>

                    {/* Breakdown */}
                    <div className="space-y-2.5 rounded-2xl border border-border bg-white p-4 text-sm">
                        {[
                            { label: "Gross proceeds", value: `${formatCurrency(quote?.grossAmount ?? 0)} ${position.asset ?? "USDC"}` },
                            { label: "Early exit penalty", value: `${formatCurrency(quote ? quote.grossAmount - quote.netAmount : 0)} ${position.asset ?? "USDC"}` },
                            { label: "Net to wallet", value: `${formatCurrency(quote?.netAmount ?? 0)} ${position.asset ?? "USDC"}`, highlight: true },
                            { label: "Shares burned", value: formatCurrency(quote?.sharesBurned ?? 0) },
                        ].map(({ label, value, highlight }) => (
                            <div key={label} className="flex items-center justify-between">
                                <span className="text-muted-foreground">{label}</span>
                                <span className={cn("font-medium", highlight ? "text-emerald-600" : "text-foreground")}>
                                    {value}
                                </span>
                            </div>
                        ))}
                    </div>

                    {/* Success receipt */}
                    {state === "success" && receipt && (
                        <div className="rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                            <div className="flex items-center gap-2 text-emerald-700">
                                <CheckCircle2 className="h-4 w-4" />
                                <p className="text-sm font-medium">Withdrawal confirmed</p>
                            </div>
                            <p className="mt-2 text-sm text-emerald-800/80">
                                {formatCurrency(receipt.netAmount)} {position.asset ?? "USDC"} is on its way back to your wallet.
                            </p>
                            <div className="mt-3 flex flex-wrap gap-2">
                                <Link
                                    href={receipt.explorerUrl}
                                    target="_blank"
                                    className="inline-flex items-center gap-1.5 rounded-full bg-white px-3 py-2 text-xs font-medium text-foreground shadow-sm"
                                >
                                    View on Explorer
                                    <ExternalLink className="h-3.5 w-3.5" />
                                </Link>
                            </div>
                        </div>
                    )}

                    {/* Error */}
                    {error && state === "error" && (
                        <div className="rounded-2xl border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
                            <div className="flex items-start gap-2">
                                <AlertCircle className="mt-0.5 h-4 w-4" />
                                <span>{error}</span>
                            </div>
                        </div>
                    )}

                    {/* Large amount warning */}
                    {showLargeWarning && (
                        <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">
                            <div className="flex items-start gap-2">
                                <AlertCircle className="mt-0.5 h-4 w-4" />
                                <span>
                                    You&apos;re about to withdraw {formatCurrency(amount)} {position.asset ?? "USDC"} — are you sure?
                                </span>
                            </div>
                        </div>
                    )}

                    {/* Action buttons — always visible */}
                    <div className="flex gap-3 pt-2">
                        <button
                            onClick={reset}
                            className="flex-1 min-h-[var(--touch-target)] rounded-full border border-border bg-white px-5 text-sm font-medium text-foreground transition-colors hover:border-black/15 active:bg-secondary"
                        >
                            {state === "success" ? "Close" : "Cancel"}
                        </button>
                        {state !== "success" && (
                            <button
                                onClick={handleWithdraw}
                                disabled={!canSubmit || state === "confirming" || state === "submitting"}
                                className="flex-1 min-h-[var(--touch-target)] rounded-full bg-[#0a0a0a] px-5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.98]"
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
                                    (showLargeWarning ? "Yes, confirm withdrawal" : "Confirm Withdrawal")}
                            </button>
                        )}
                    </div>
                </div>
            )}
        </ModalShell>
    );
}

// ---------------------------------------------------------------------------
// TransferModal — move funds from one vault to another
// ---------------------------------------------------------------------------

export function TransferModal({
    open,
    onClose,
    position,
}: {
    open: boolean;
    onClose: () => void;
    position: PortfolioPosition | null;
}) {
    const { recordTransfer } = usePortfolio();
    const { currentNetwork } = useNetwork();
    const { address } = useWallet();
    const [state, setState] = useState<ActionState>("input");
    const [error, setError] = useState<string | null>(null);
    const [selectedVaultId, setSelectedVaultId] = useState<string>("");
    const [receipt, setReceipt] = useState<{
        amount: number;
        toVaultName: string;
        explorerUrl: string;
        walletPopupUsed: boolean;
    } | null>(null);

    const { data: vaults = [] } = useVaults();
    const destinationVaults = useMemo(
        () => vaults.filter((v) => v.id !== position?.vaultId),
        [position?.vaultId, vaults]
    );

    const selectedVault = destinationVaults.find((v) => v.id === selectedVaultId) ?? null;

    const transferSchema = useMemo(() => z.object({
        amount: validateAmount({
            min: 0.000001,
            balance: position?.currentValue ?? 0,
            maxDecimals: 6,
            minMessage: "Amount must be greater than 0",
            balanceMessage: `Amount exceeds your position value of ${formatCurrency(position?.currentValue ?? 0)}`,
        }),
    }), [position?.currentValue]);

    type TransferFormValues = z.infer<typeof transferSchema>;

    const {
        control,
        handleSubmit,
        formState: { errors, isDirty },
        trigger,
        reset: resetForm,
        watch,
    } = useForm<TransferFormValues>({
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        resolver: zodResolver(transferSchema as any),
        defaultValues: { amount: "" },
        mode: "onBlur",
    });

    const amount = parseFloat(watch("amount") || "0");
    const canSubmit =
        !isNaN(amount) &&
        amount > 0 &&
        selectedVault !== null &&
        position !== null &&
        amount <= position.currentValue;

    function reset() {
        resetForm();
        setState("input");
        setError(null);
        setReceipt(null);
        setSelectedVaultId("");
        onClose();
    }

    const handleTransfer = handleSubmit(async ({ amount: rawAmount }) => {
        if (!position || !selectedVault) return;
        const amt = parseFloat(rawAmount);
        if (isNaN(amt) || amt <= 0) return;

        setError(null);
        setState("confirming");

        try {
            throw new Error("Transfers are not yet live. Transfers are currently disabled.");
        } catch (err) {
            setState("error");
            setError(err instanceof Error ? err.message : "Transfer failed. Please try again.");
        }
    });

    if (!position) return null;

    return (
        <ModalShell
            open={open}
            onClose={reset}
            title="Transfer Funds"
            subtitle={`Move assets from ${position.vaultName}`}
        >
            {(state === "input" || state === "confirming" || state === "submitting" || state === "error" || state === "success") && (
                <div className="grid grid-cols-1 gap-0 md:grid-cols-2">
                    {/* Left — source info + destination picker */}
                    <div className="border-b border-border p-6 md:border-b-0 md:border-r">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                From Vault
                            </p>
                            <div className="mt-4 rounded-2xl border border-border bg-[#fafafa] p-4">
                                <p className="text-sm font-medium text-foreground">{position.vaultName}</p>
                                <p className="mt-1 text-xs text-muted-foreground">
                                    Current value:{" "}
                                    <span className="font-medium text-foreground">
                                        {formatCurrency(position.currentValue)} {position.asset}
                                    </span>
                                </p>
                                <p className="mt-0.5 text-xs text-muted-foreground">
                                    Yield earned:{" "}
                                    <span className="font-medium text-emerald-600">
                                        +{formatCurrency(position.yieldEarned)} {position.asset}
                                    </span>
                                </p>
                            </div>

                            <div className="mt-4">
                                <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                                    To Vault
                                </label>
                                <div className="space-y-2">
                                    {destinationVaults.map((vault) => (
                                        <button
                                            key={vault.id}
                                            type="button"
                                            onClick={() => setSelectedVaultId(vault.id)}
                                            className={cn(
                                                "w-full rounded-2xl border px-4 py-3 text-left transition-colors",
                                                selectedVaultId === vault.id
                                                    ? "border-foreground bg-foreground/5"
                                                    : "border-border bg-white hover:border-black/15"
                                            )}
                                        >
                                            <div className="flex items-center justify-between">
                                                <div>
                                                    <p className="text-sm font-medium text-foreground">{vault.name}</p>
                                                    <p className="text-xs text-muted-foreground">
                                                        {vault.apy !== undefined ? `${vault.apy.toFixed(1)}% APY` : "APY TBD"} · {vault.strategy}
                                                    </p>
                                                </div>
                                                {selectedVaultId === vault.id && (
                                                    <CheckCircle2 className="h-4 w-4 text-foreground" />
                                                )}
                                            </div>
                                        </button>
                                    ))}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Right — amount + confirm */}
                    <div className="p-6">
                        <div className="rounded-3xl border border-border bg-white p-5">
                            <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                                Transfer Amount
                            </p>

                            <div className="mt-4 rounded-2xl border border-border bg-[#fafafa] p-4">
                                <Controller
                                    name="amount"
                                    control={control}
                                    render={({ field: { onChange, onBlur, value } }) => (
                                        <>
                                            <div className={cn(
                                                "flex items-center gap-3 rounded-2xl border bg-white px-4 py-4",
                                                errors.amount ? "border-red-500" : "border-border"
                                            )}>
                                                <input
                                                    type="text"
                                                    inputMode="decimal"
                                                    value={value}
                                                    onChange={(e) => {
                                                        const next = e.target.value;
                                                        if (/^\d*\.?\d*$/.test(next)) {
                                                            onChange(next);
                                                            if (isDirty) trigger("amount");
                                                            setState("input");
                                                        }
                                                    }}
                                                    onBlur={onBlur}
                                                    onPaste={() => setTimeout(() => trigger("amount"), 0)}
                                                    placeholder="0.00"
                                                    className={cn(
                                                        "min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40",
                                                        errors.amount && "text-red-500"
                                                    )}
                                                />
                                                <div className="flex items-center gap-2">
                                                    <span className="rounded-full bg-secondary px-3 py-2 text-sm font-medium text-foreground">
                                                        {position.asset}
                                                    </span>
                                                    <button
                                                        type="button"
                                                        onClick={() => {
                                                            onChange(position.currentValue.toFixed(2));
                                                            trigger("amount");
                                                        }}
                                                        className="rounded-full border border-border bg-white px-3 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15"
                                                    >
                                                        Max
                                                    </button>
                                                </div>
                                            </div>
                                            {errors.amount && (
                                                <span className="mt-2 block text-xs font-medium text-red-500">{errors.amount.message}</span>
                                            )}
                                        </>
                                    )}
                                />

                                <div className="mt-4 space-y-2">
                                    <div className="flex items-center justify-between text-sm">
                                        <span className="text-muted-foreground">Available to transfer</span>
                                        <span className="font-medium text-foreground">
                                            {formatCurrency(position.currentValue)} {position.asset}
                                        </span>
                                    </div>
                                    {selectedVault && (
                                        <div className="flex items-center justify-between text-sm">
                                            <span className="text-muted-foreground">Destination APY</span>
                                            <span className="font-medium text-emerald-600">{selectedVault.apy !== undefined ? `${selectedVault.apy.toFixed(1)}%` : "TBD"}</span>
                                        </div>
                                    )}
                                    {currentNetwork.id === "mainnet" && (
                                        <div className="flex items-center justify-between text-sm">
                                            <span className="text-muted-foreground">Estimated network fee</span>
                                            <span className="font-medium text-foreground">~0.00001 XLM</span>
                                        </div>
                                    )}
                                </div>
                            </div>

                            <div className="mt-4 rounded-2xl border border-border bg-secondary/20 p-4 text-sm text-muted-foreground">
                                <div className="flex items-start gap-3">
                                    <Sparkles className="mt-0.5 h-4 w-4 text-foreground/70" />
                                    <p>
                                        Funds move directly between vaults. No early-exit penalty applies, and a new lock period starts in the destination vault.
                                    </p>
                                </div>
                            </div>

                            {state === "success" && receipt ? (
                                <div className="mt-5 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                                    <div className="flex items-center gap-2 text-emerald-700">
                                        <CheckCircle2 className="h-4 w-4" />
                                        <p className="text-sm font-medium">Transfer confirmed</p>
                                    </div>
                                    <p className="mt-2 text-sm text-emerald-800/80">
                                        {formatCurrency(receipt.amount)} {position.asset} moved to <strong>{receipt.toVaultName}</strong>.
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
                                            {receipt.walletPopupUsed ? "Wallet signature captured" : "Mock signature used"}
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
                            ) : null}

                            <div className="mt-5 flex gap-3">
                                <button
                                    type="button"
                                    onClick={reset}
                                    className="flex-1 min-h-[var(--touch-target)] rounded-full border border-border bg-white px-5 text-sm font-medium text-foreground transition-colors hover:border-black/15 active:bg-secondary"
                                >
                                    {state === "success" ? "Close" : "Cancel"}
                                </button>
                                {state !== "success" && (
                                    <button
                                        onClick={handleTransfer}
                                        disabled={!canSubmit || state === "confirming" || state === "submitting"}
                                        className="flex-1 min-h-[var(--touch-target)] rounded-full bg-brand-dark px-5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40 active:scale-[0.98]"
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
                                        {(state === "input" || state === "error") && "Confirm Transfer"}
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
