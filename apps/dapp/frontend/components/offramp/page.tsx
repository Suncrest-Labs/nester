"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import Image from "next/image";
import { motion, AnimatePresence } from "framer-motion";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
    AlertCircle,
    ArrowDownUp,
    ArrowRight,
    Building2,
    CheckCircle2,
    ChevronDown,
    Clock,
    Info,
    Loader2,
    ShieldCheck,
    Tag,
    Zap,
} from "lucide-react";

import { AppShell } from "@/components/app-shell";
import { BankCombobox } from "@/components/offramp/BankCombobox";
import { AccountNameField } from "@/components/offramp/AccountNameField";
import { useBankResolver } from "@/hooks/useBankResolver";
import { useWallet } from "@/hooks/useWallet";
import { useNetwork } from "@/hooks/useNetwork";
import { useNotifications } from "@/hooks/useNotifications";
import { cn } from "@/lib/utils";
import { getExplorerTxUrl } from "@/lib/networks";
import {
    LP_NODES,
    SEND_ASSETS,
    RECEIVE_CURRENCIES,
    MOCK_BALANCE,
    buildQuotes,
    type QuoteResult,
    type QuotePhase,
} from "@/lib/settlement-data";
import { validateAmount } from "@/lib/validation";

// ---------------------------------------------------------------------------
// Form schema
// ---------------------------------------------------------------------------

const offrampSchema = z.object({
    amount: validateAmount({ min: 1, balance: MOCK_BALANCE, maxDecimals: 6 }),
    bankCode: z.string().min(1, "Please select a bank"),
    accountNumber: z
        .string()
        .length(10, "Account number must be exactly 10 digits")
        .regex(/^\d{10}$/, "Account number must contain only digits"),
});

type OfframpFormValues = z.infer<typeof offrampSchema>;

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function OfframpPage() {
    const router = useRouter();
    const { isConnected } = useWallet();
    const { currentNetwork } = useNetwork();
    const { addNotification } = useNotifications();

    const {
        control,
        handleSubmit,
        watch,
        trigger,
        formState: { errors, isValid, isDirty },
    } = useForm<OfframpFormValues>({
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        resolver: zodResolver(offrampSchema as any),
        mode: "onBlur",
        defaultValues: { amount: "", bankCode: "", accountNumber: "" },
    });

    const sendAmount = watch("amount");
    const bankCode = watch("bankCode");
    const accountNumber = watch("accountNumber");

    const [sendAsset, setSendAsset] = useState(SEND_ASSETS[0]);
    const [receiveCurrency, setReceiveCurrency] = useState(RECEIVE_CURRENCIES[0]);
    const [showSendDropdown, setShowSendDropdown] = useState(false);
    const [showReceiveDropdown, setShowReceiveDropdown] = useState(false);

    // Manual account name used only when both providers are down.
    const [manualAccountName, setManualAccountName] = useState("");

    const [quotePhase, setQuotePhase] = useState<QuotePhase>("idle");
    const [scannedCount, setScannedCount] = useState(0);
    const [quotes, setQuotes] = useState<QuoteResult[]>([]);
    const [selectedQuote, setSelectedQuote] = useState<QuoteResult | null>(null);
    const [showLargeWarning, setShowLargeWarning] = useState(false);
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const refreshRef = useRef<ReturnType<typeof setInterval> | null>(null);

    // ── Account name auto-resolve ──────────────────────────────────────────
    // Fires 400ms after both accountNumber (10 digits) and bankCode are set.
    const { resolveState, accountInfo } = useBankResolver(accountNumber, bankCode);

    // The effective account name — verified or manually entered on outage.
    const effectiveAccountName =
        resolveState === "success"
            ? (accountInfo?.account_name ?? "")
            : resolveState === "provider_error"
            ? manualAccountName
            : "";

    // ── Auth redirect ──────────────────────────────────────────────────────
    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    // ── Quote scanning ─────────────────────────────────────────────────────
    const numericAmount = parseFloat(sendAmount) || 0;

    // We need a Bank object for buildQuotes — derive from bankCode.
    const selectedBankObj = bankCode
        ? { code: bankCode, name: "", country: "NG" }
        : null;

    const allFieldsFilled =
        isValid &&
        (resolveState === "success" || resolveState === "provider_error") &&
        (resolveState === "success" || manualAccountName.trim().length > 0);

    const runQuoteScan = useCallback(
        (amount: number, currency: typeof RECEIVE_CURRENCIES[0]) => {
            setQuotePhase("scanning");
            setScannedCount(0);
            setQuotes([]);
            setSelectedQuote(null);

            let count = 0;
            const scanInterval = setInterval(() => {
                count++;
                setScannedCount(count);
                if (count >= LP_NODES.length) {
                    clearInterval(scanInterval);
                    setQuotePhase("comparing");
                    setTimeout(() => {
                        setQuotePhase("ranking");
                        setTimeout(() => {
                            const results = buildQuotes(amount, selectedBankObj!, currency);
                            setQuotes(results);
                            setSelectedQuote(results[0]);
                            setQuotePhase("done");
                        }, 400);
                    }, 600);
                }
            }, 120);
        },
        [selectedBankObj]
    );

    const silentRefresh = useCallback(
        (amount: number, currency: typeof RECEIVE_CURRENCIES[0]) => {
            const results = buildQuotes(amount, selectedBankObj!, currency);
            setQuotes(results);
            setSelectedQuote((prev) => {
                if (!prev) return results[0];
                const same = results.find((q) => q.node.id === prev.node.id);
                return same || results[0];
            });
        },
        [selectedBankObj]
    );

    useEffect(() => {
        if (refreshRef.current) {
            clearInterval(refreshRef.current);
            refreshRef.current = null;
        }
        if (!allFieldsFilled) {
            setQuotePhase("idle");
            setQuotes([]);
            setSelectedQuote(null);
            return;
        }
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
            runQuoteScan(numericAmount, receiveCurrency);
            refreshRef.current = setInterval(() => {
                silentRefresh(numericAmount, receiveCurrency);
            }, 8000);
        }, 500);
        return () => {
            if (debounceRef.current) clearTimeout(debounceRef.current);
            if (refreshRef.current) clearInterval(refreshRef.current);
        };
    }, [allFieldsFilled, numericAmount, receiveCurrency, runQuoteScan, silentRefresh]);

    if (!isConnected) return null;

    const displayReceive = selectedQuote
        ? selectedQuote.receiveAmount
        : numericAmount > 0
        ? numericAmount * receiveCurrency.rate * 0.995
        : 0;

    const handleWithdraw = handleSubmit(() => {
        if (!isValid || quotePhase !== "done" || !selectedQuote) return;
        if (!effectiveAccountName) return;

        if (numericAmount > 10000 && !showLargeWarning) {
            setShowLargeWarning(true);
            return;
        }

        setShowLargeWarning(false);
        addNotification(
            {
                type: "withdrawal_processed",
                title: "Withdrawal Submitted",
                message: `Withdrew ${numericAmount.toLocaleString("en-US", {
                    maximumFractionDigits: 2,
                })} ${sendAsset.symbol} to ${effectiveAccountName} — ${accountNumber.slice(-4)}.`,
                actionUrl: getExplorerTxUrl(`mock-settlement-${selectedQuote.node.id}`),
                actionLabel: "View Transaction",
            },
            { showToast: true }
        );
    });

    return (
        <AppShell>
            <div className="mx-auto max-w-xl">
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4 }}
                    className="text-center mb-6 sm:mb-8"
                >
                    <h1 className="font-heading text-xl sm:text-2xl font-semibold text-foreground">
                        Cash Out
                    </h1>
                    <p className="mt-1 text-sm text-muted-foreground">
                        Convert crypto to fiat, directly to your bank account
                    </p>
                </motion.div>

                {/* Main Card */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4, delay: 0.1 }}
                    className="rounded-2xl border border-border bg-white shadow-sm overflow-hidden"
                >
                    {/* ── Send amount ── */}
                    <div className="p-4 sm:p-5">
                        <label className="text-xs text-muted-foreground font-medium mb-2 block">
                            You&apos;ll send
                        </label>
                        <div className="flex flex-col gap-1">
                            <div className={cn(
                                "flex items-center gap-3 rounded-2xl border px-3 py-2 transition-colors",
                                errors.amount
                                    ? "border-red-500 bg-red-50/30"
                                    : "border-transparent bg-transparent hover:border-border"
                            )}>
                                <Controller
                                    name="amount"
                                    control={control}
                                    render={({ field: { onChange, onBlur, value } }) => (
                                        <input
                                            type="text"
                                            inputMode="decimal"
                                            placeholder="0.00"
                                            value={value}
                                            onChange={(e) => {
                                                const val = e.target.value;
                                                if (/^\d*\.?\d*$/.test(val)) {
                                                    onChange(val);
                                                    if (isDirty) trigger("amount");
                                                }
                                            }}
                                            onBlur={onBlur}
                                            onPaste={() => setTimeout(() => trigger("amount"), 0)}
                                            className={cn(
                                                "flex-1 text-2xl sm:text-3xl font-heading font-light text-foreground",
                                                "bg-transparent outline-none placeholder:text-muted-foreground/40 min-w-0 min-h-[44px]",
                                                errors.amount && "text-red-500"
                                            )}
                                        />
                                    )}
                                />
                                <div className="relative">
                                    <button
                                        type="button"
                                        onClick={() => setShowSendDropdown((o) => !o)}
                                        className="flex items-center gap-2 px-3 py-2 rounded-full bg-secondary hover:bg-secondary/80 transition-colors min-h-[44px]"
                                    >
                                        <Image
                                            src={sendAsset.image}
                                            alt={sendAsset.symbol}
                                            width={20}
                                            height={20}
                                            className="rounded-full"
                                        />
                                        <span className="text-sm font-medium text-foreground">
                                            {sendAsset.symbol}
                                        </span>
                                        <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                                    </button>
                                    {showSendDropdown && (
                                        <div className="absolute right-0 top-full mt-1 w-48 rounded-xl border border-border bg-white shadow-lg py-1 z-10">
                                            {SEND_ASSETS.map((asset) => (
                                                <button
                                                    key={asset.symbol}
                                                    type="button"
                                                    onClick={() => {
                                                        setSendAsset(asset);
                                                        setShowSendDropdown(false);
                                                    }}
                                                    className="w-full flex items-center gap-2.5 px-3 py-3 text-sm hover:bg-secondary/50 transition-colors min-h-[44px]"
                                                >
                                                    <Image
                                                        src={asset.image}
                                                        alt={asset.symbol}
                                                        width={18}
                                                        height={18}
                                                        className="rounded-full"
                                                    />
                                                    <span className="font-medium">{asset.symbol}</span>
                                                    <span className="text-muted-foreground text-xs ml-auto">
                                                        {asset.name}
                                                    </span>
                                                </button>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </div>
                            <div className="flex justify-between mt-1 px-1">
                                {errors.amount ? (
                                    <span className="text-xs text-red-500 font-medium">
                                        {errors.amount.message}
                                    </span>
                                ) : (
                                    <span />
                                )}
                                <div className="text-xs text-muted-foreground">
                                    Balance: {MOCK_BALANCE.toLocaleString()} {sendAsset.symbol}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* ── Swap divider ── */}
                    <div className="relative px-4 sm:px-5">
                        <div className="border-t border-border" />
                        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2">
                            <div className="h-9 w-9 rounded-full border border-border bg-white flex items-center justify-center shadow-sm">
                                <ArrowDownUp className="h-4 w-4 text-muted-foreground" />
                            </div>
                        </div>
                    </div>

                    {/* ── Receive amount ── */}
                    <div className="p-4 sm:p-5">
                        <label className="text-xs text-muted-foreground font-medium mb-2 block">
                            You&apos;ll receive
                        </label>
                        <div className="flex items-center gap-3">
                            <div className="flex-1 text-2xl sm:text-3xl font-heading font-light text-foreground min-w-0 truncate min-h-[44px] flex items-center">
                                {allFieldsFilled && selectedQuote ? (
                                    displayReceive.toLocaleString("en-US", {
                                        minimumFractionDigits: 2,
                                        maximumFractionDigits: 2,
                                    })
                                ) : numericAmount > 0 ? (
                                    <span className="text-muted-foreground/60">
                                        ≈{" "}
                                        {(numericAmount * receiveCurrency.rate * 0.995).toLocaleString(
                                            "en-US",
                                            { minimumFractionDigits: 2, maximumFractionDigits: 2 }
                                        )}
                                    </span>
                                ) : (
                                    <span className="text-muted-foreground/40">0.00</span>
                                )}
                            </div>
                            <div className="relative">
                                <button
                                    type="button"
                                    onClick={() => setShowReceiveDropdown((o) => !o)}
                                    className="flex items-center gap-2 px-3 py-2 rounded-full bg-secondary hover:bg-secondary/80 transition-colors min-h-[44px]"
                                >
                                    <Image
                                        src={receiveCurrency.image}
                                        alt={receiveCurrency.symbol}
                                        width={20}
                                        height={20}
                                        className="rounded-full"
                                    />
                                    <span className="text-sm font-medium text-foreground">
                                        {receiveCurrency.symbol}
                                    </span>
                                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                                </button>
                                {showReceiveDropdown && (
                                    <div className="absolute right-0 top-full mt-1 w-48 rounded-xl border border-border bg-white shadow-lg py-1 z-10">
                                        {RECEIVE_CURRENCIES.map((currency) => (
                                            <button
                                                key={currency.symbol}
                                                type="button"
                                                onClick={() => {
                                                    setReceiveCurrency(currency);
                                                    setShowReceiveDropdown(false);
                                                }}
                                                className="w-full flex items-center gap-2.5 px-3 py-3 text-sm hover:bg-secondary/50 transition-colors min-h-[44px]"
                                            >
                                                <Image
                                                    src={currency.image}
                                                    alt={currency.symbol}
                                                    width={18}
                                                    height={18}
                                                    className="rounded-full"
                                                />
                                                <span className="font-medium">{currency.symbol}</span>
                                                <span className="text-muted-foreground text-xs ml-auto">
                                                    {currency.name}
                                                </span>
                                            </button>
                                        ))}
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>

                    {/* ── Bank details ── */}
                    <div className="border-t border-border p-4 sm:p-5 space-y-4">
                        {/* Searchable bank dropdown — live from API */}
                        <div>
                            <label className="text-xs text-muted-foreground font-medium mb-2 block">
                                Select bank
                            </label>
                            <Controller
                                name="bankCode"
                                control={control}
                                render={({ field: { onChange, value } }) => (
                                    <BankCombobox
                                        value={value}
                                        onChange={(code) => {
                                            onChange(code);
                                            trigger("bankCode");
                                        }}
                                        error={errors.bankCode?.message}
                                        country="NG"
                                    />
                                )}
                            />
                        </div>

                        {/* Account number */}
                        <div>
                            <label className="text-xs text-muted-foreground font-medium mb-2 block">
                                Account number
                            </label>
                            <Controller
                                name="accountNumber"
                                control={control}
                                render={({ field: { onChange, onBlur, value } }) => (
                                    <>
                                        <input
                                            type="text"
                                            inputMode="numeric"
                                            maxLength={10}
                                            placeholder="Enter 10-digit account number"
                                            value={value}
                                            onChange={(e) => {
                                                const val = e.target.value.replace(/\D/g, "");
                                                onChange(val);
                                                if (isDirty) trigger("accountNumber");
                                            }}
                                            onBlur={onBlur}
                                            onPaste={() => setTimeout(() => trigger("accountNumber"), 0)}
                                            className={cn(
                                                "w-full px-4 py-3 rounded-xl border border-border bg-white",
                                                "text-sm text-foreground placeholder:text-muted-foreground/60",
                                                "outline-none focus:border-foreground/20 transition-colors min-h-[52px]",
                                                errors.accountNumber && "border-red-500"
                                            )}
                                        />
                                        {errors.accountNumber && (
                                            <span className="text-xs text-red-500 font-medium mt-1 block">
                                                {errors.accountNumber.message}
                                            </span>
                                        )}
                                    </>
                                )}
                            />
                        </div>

                        {/* Account name auto-resolve field */}
                        <AccountNameField
                            resolveState={resolveState}
                            accountName={accountInfo?.account_name ?? null}
                            onManualName={setManualAccountName}
                            manualName={manualAccountName}
                        />
                    </div>

                    {/* ── Rate info ── */}
                    {selectedQuote && allFieldsFilled && (
                        <div className="border-t border-border px-4 sm:px-5 py-4 space-y-2">
                            <div className="flex items-center justify-between text-xs gap-2">
                                <span className="text-muted-foreground flex items-center gap-1 shrink-0">
                                    Rate via {selectedQuote.node.name}
                                    <Info className="h-3 w-3" />
                                </span>
                                <span className="text-foreground font-medium text-right">
                                    1 {sendAsset.symbol} ≈{" "}
                                    {selectedQuote.effectiveRate.toLocaleString(undefined, {
                                        minimumFractionDigits: 2,
                                        maximumFractionDigits: 2,
                                    })}{" "}
                                    {receiveCurrency.symbol}
                                </span>
                            </div>
                            <div className="flex items-center justify-between text-xs">
                                <span className="text-muted-foreground">
                                    Node fee ({selectedQuote.node.fee}%)
                                </span>
                                <span className="text-foreground font-medium">
                                    {selectedQuote.fee.toFixed(4)} {sendAsset.symbol}
                                </span>
                            </div>
                            <div className="flex items-center justify-between text-xs">
                                <span className="text-muted-foreground">Estimated time</span>
                                <span className="text-foreground font-medium flex items-center gap-1">
                                    <Clock className="h-3 w-3" />
                                    {selectedQuote.estimatedTime}
                                    {selectedQuote.isSameBank && (
                                        <span className="text-emerald-600 font-medium ml-1">
                                            Same bank
                                        </span>
                                    )}
                                </span>
                            </div>
                        </div>
                    )}

                    {/* ── Large amount warning ── */}
                    {showLargeWarning && (
                        <div className="mx-4 sm:mx-5 mb-3 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">
                            <div className="flex items-start gap-2">
                                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                                <span>
                                    You&apos;re about to withdraw $
                                    {numericAmount.toLocaleString("en-US", {
                                        maximumFractionDigits: 2,
                                    })}{" "}
                                    — are you sure?
                                </span>
                            </div>
                        </div>
                    )}

                    {/* ── CTA button ── */}
                    <div className="p-4 sm:p-5 pt-0">
                        <button
                            disabled={
                                !isValid ||
                                quotePhase !== "done" ||
                                !effectiveAccountName ||
                                resolveState === "loading"
                            }
                            onClick={handleWithdraw}
                            className="w-full rounded-xl bg-foreground text-background py-4 text-sm font-medium transition-all hover:opacity-90 disabled:opacity-40 disabled:cursor-not-allowed"
                        >
                            {!numericAmount
                                ? "Enter an amount"
                                : !bankCode
                                ? "Select a bank"
                                : accountNumber.length !== 10
                                ? "Enter account number"
                                : resolveState === "loading"
                                ? "Verifying account…"
                                : resolveState === "not_found"
                                ? "Account not found"
                                : !effectiveAccountName
                                ? "Account name required"
                                : quotePhase !== "done"
                                ? "Finding best rate..."
                                : showLargeWarning
                                ? "Yes, confirm withdrawal"
                                : `Withdraw ${displayReceive.toLocaleString("en-US", {
                                      minimumFractionDigits: 2,
                                  })} ${receiveCurrency.symbol}`}
                        </button>
                    </div>
                </motion.div>

                {/* ── LP Node Quotes ── */}
                <AnimatePresence>
                    {allFieldsFilled && quotePhase !== "idle" && (
                        <motion.div
                            initial={{ opacity: 0, y: 16 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: 8 }}
                            transition={{ duration: 0.35, delay: 0.05 }}
                            className="mt-4 rounded-2xl border border-border bg-white shadow-sm overflow-hidden"
                        >
                            <div className="px-4 sm:px-5 py-3.5 flex items-center justify-between border-b border-border gap-2">
                                <div className="flex items-center gap-2 shrink-0">
                                    <Tag className="h-3.5 w-3.5 text-muted-foreground" />
                                    <span className="text-sm font-medium text-foreground">Quotes</span>
                                </div>
                                <div className="flex items-center gap-1.5 text-xs text-muted-foreground overflow-hidden">
                                    {quotePhase === "done" ? (
                                        <span className="flex items-center gap-1 flex-wrap justify-end">
                                            <span className="flex items-center gap-1">
                                                <span className="relative flex h-1.5 w-1.5 shrink-0">
                                                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                                                    <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500" />
                                                </span>
                                                Live
                                            </span>
                                            <span className="text-muted-foreground/40">·</span>
                                            <span>{quotes.length} nodes</span>
                                        </span>
                                    ) : (
                                        <span className="flex items-center gap-1.5">
                                            <Loader2 className="h-3 w-3 animate-spin shrink-0" />
                                            <span className="truncate">
                                                {quotePhase === "scanning" &&
                                                    `Scanning (${scannedCount}/${LP_NODES.length})`}
                                                {quotePhase === "comparing" && "Comparing rates..."}
                                                {quotePhase === "ranking" && "Ranking..."}
                                            </span>
                                        </span>
                                    )}
                                </div>
                            </div>

                            {quotePhase !== "done" && (
                                <div className="h-0.5 bg-secondary overflow-hidden">
                                    <motion.div
                                        className="h-full bg-foreground/70"
                                        initial={{ width: "0%" }}
                                        animate={{
                                            width:
                                                quotePhase === "scanning"
                                                    ? `${(scannedCount / LP_NODES.length) * 60}%`
                                                    : quotePhase === "comparing"
                                                    ? "80%"
                                                    : "95%",
                                        }}
                                        transition={{ duration: 0.2, ease: "easeOut" }}
                                    />
                                </div>
                            )}

                            {quotePhase === "done" && quotes.length > 0 && (
                                <div className="divide-y divide-border">
                                    {quotes.slice(0, 5).map((quote, i) => (
                                        <motion.button
                                            key={quote.node.id}
                                            initial={{ opacity: 0, x: -8 }}
                                            animate={{ opacity: 1, x: 0 }}
                                            transition={{ duration: 0.25, delay: i * 0.06 }}
                                            onClick={() => setSelectedQuote(quote)}
                                            className={cn(
                                                "w-full flex items-center gap-3 px-4 sm:px-5 py-3 text-left transition-colors min-h-[56px]",
                                                selectedQuote?.node.id === quote.node.id
                                                    ? "bg-secondary/60"
                                                    : "hover:bg-secondary/30"
                                            )}
                                        >
                                            <div className={cn(
                                                "h-7 w-7 rounded-full flex items-center justify-center shrink-0",
                                                quote.isBest
                                                    ? "bg-emerald-100 text-emerald-700"
                                                    : "bg-secondary text-muted-foreground"
                                            )}>
                                                {quote.isBest ? (
                                                    <Zap className="h-3.5 w-3.5" />
                                                ) : (
                                                    <Building2 className="h-3.5 w-3.5" />
                                                )}
                                            </div>
                                            <div className="flex-1 min-w-0">
                                                <div className="flex items-center gap-1.5 flex-wrap">
                                                    <span className="text-sm font-medium text-foreground truncate">
                                                        {quote.node.name}
                                                    </span>
                                                    {quote.isBest && (
                                                        <span className="shrink-0 px-1.5 py-0.5 rounded text-[10px] font-semibold bg-emerald-100 text-emerald-700">
                                                            Best
                                                        </span>
                                                    )}
                                                    {quote.isSameBank && (
                                                        <span className="shrink-0 px-1.5 py-0.5 rounded text-[10px] font-semibold bg-blue-50 text-blue-600">
                                                            Same Bank
                                                        </span>
                                                    )}
                                                </div>
                                                <div className="flex items-center gap-1.5 mt-0.5 text-[11px] text-muted-foreground">
                                                    <span>{quote.node.fee}% fee</span>
                                                    <span className="text-muted-foreground/30">·</span>
                                                    <span>{quote.estimatedTime}</span>
                                                </div>
                                            </div>
                                            <div className="text-right shrink-0">
                                                <div className="text-sm font-medium text-foreground tabular-nums">
                                                    {quote.receiveAmount.toLocaleString("en-US", {
                                                        minimumFractionDigits: 2,
                                                        maximumFractionDigits: 2,
                                                    })}
                                                </div>
                                                <div className="text-[10px] text-muted-foreground">
                                                    {receiveCurrency.symbol}
                                                </div>
                                            </div>
                                            {selectedQuote?.node.id === quote.node.id && (
                                                <CheckCircle2 className="h-4 w-4 text-foreground shrink-0" />
                                            )}
                                        </motion.button>
                                    ))}
                                </div>
                            )}

                            {quotePhase === "done" && selectedQuote && (
                                <div className="px-4 sm:px-5 py-3 bg-secondary/30 flex items-center gap-2 text-[11px] text-muted-foreground">
                                    <ArrowRight className="h-3 w-3 shrink-0" />
                                    <span>
                                        Routing through{" "}
                                        <strong className="text-foreground font-medium">
                                            {selectedQuote.node.name}
                                        </strong>
                                    </span>
                                </div>
                            )}
                        </motion.div>
                    )}
                </AnimatePresence>

                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ duration: 0.4, delay: 0.3 }}
                    className="mt-4 flex items-center justify-center gap-2"
                >
                    <ShieldCheck className="h-3.5 w-3.5 text-emerald-500 shrink-0" />
                    <span className="text-[11px] text-muted-foreground text-center">
                        Secured by Soroban smart contract escrow — auto-refund if settlement fails
                    </span>
                </motion.div>

                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4, delay: 0.4 }}
                    className="mt-8 rounded-2xl border border-border bg-white p-5"
                >
                    <h3 className="font-heading text-sm font-medium text-foreground mb-3">
                        Recent Offramps
                    </h3>
                    <div className="flex flex-col items-center justify-center py-6 text-center">
                        <Clock className="h-5 w-5 text-muted-foreground/30 mb-2" />
                        <p className="text-xs text-muted-foreground">No offramps yet</p>
                    </div>
                </motion.div>
            </div>
        </AppShell>
    );
}