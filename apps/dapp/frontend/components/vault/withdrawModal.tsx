"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  ExternalLink,
  Loader2,
  Sparkles,
  X,
} from "lucide-react";

import {
  usePortfolio,
  type PortfolioPosition,
} from "@/components/portfolio-provider";
import { useWallet } from "@/components/wallet-provider";
import { cn } from "@/lib/utils";
import {
  buildWithdrawTransaction,
  signTransaction,
  submitTransaction,
  VAULT_CONTRACT_ID,
  USDC_CONTRACT_ID,
  UserRejectedError,
  TransactionFailedError,
  TransactionTimeoutError,
  truncateTxHash,
  type TransactionReceipt,
} from "@/lib/stellar/transaction";

// ── Types ─────────────────────────────────────────────────────────────────────

type ActionState = "input" | "building" | "signing" | "submitting" | "success" | "error";

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatCurrency(n: number) {
  return n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function humanizeError(err: unknown): string {
  if (err instanceof UserRejectedError) {
    return "You cancelled the transaction in your wallet. No funds were moved.";
  }
  if (err instanceof TransactionFailedError) {
    return `Transaction failed on-chain: ${err.reason}`;
  }
  if (err instanceof TransactionTimeoutError) {
    return "Transaction timed out. Check Stellar Explorer for the current status, then retry if needed.";
  }
  if (err instanceof Error) return err.message;
  return "An unexpected error occurred. Please try again.";
}

// ── ModalShell ────────────────────────────────────────────────────────────────

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
                  <p className="font-mono text-xs uppercase tracking-[0.18em] text-muted-foreground">
                    Vault Action
                  </p>
                  <h2 className="mt-2 font-heading text-2xl font-light text-foreground">
                    {title}
                  </h2>
                  <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
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

// ── Transaction steps ─────────────────────────────────────────────────────────

const TX_STEPS: { label: string; activeStates: ActionState[] }[] = [
  { label: "Build contract call", activeStates: ["building", "signing", "submitting", "success"] },
  { label: "Sign with wallet",    activeStates: ["signing", "submitting", "success"] },
  { label: "Submit and confirm",  activeStates: ["success"] },
];

// ── WithdrawModal ─────────────────────────────────────────────────────────────

interface WithdrawModalProps {
  open: boolean;
  onClose: () => void;
  position: PortfolioPosition | null;
}

/**
 * WithdrawModal
 *
 * Full on-chain withdrawal flow:
 * 1. User enters withdrawal amount → preview of shares burned, penalty, net amount
 * 2. Warning shown if vault is still in lock period
 * 3. Soroban transaction built + simulated
 * 4. Freighter signing popup
 * 5. Submit + poll for confirmation
 * 6. Receipt with explorer link
 */
export function WithdrawModal({ open, onClose, position }: WithdrawModalProps) {
  const { address } = useWallet();
  const { getWithdrawalQuote, recordWithdrawal } = usePortfolio();

  const [amountInput, setAmountInput] = useState("");
  const [state, setState] = useState<ActionState>("input");
  const [errorMsg, setErrorMsg] = useState("");
  const [receipt, setReceipt] = useState<(TransactionReceipt & { penaltyAmount: number; netAmount: number }) | null>(null);

  const amount = Number(amountInput) || 0;

  const quote = useMemo(
    () => (position ? getWithdrawalQuote(position.id, amount) : null),
    [amount, getWithdrawalQuote, position]
  );

  const validationError = useMemo(() => {
    if (!amount) return null;
    if (!position) return null;
    if (amount <= 0) return "Amount must be greater than 0.";
    if (amount > position.currentValue)
      return `Maximum withdrawal is ${formatCurrency(position.currentValue)} USDC.`;
    return null;
  }, [amount, position]);

  const canSubmit =
    !!position &&
    !!address &&
    amount > 0 &&
    !validationError &&
    !!quote &&
    state === "input";

  const reset = () => {
    setAmountInput("");
    setState("input");
    setErrorMsg("");
    setReceipt(null);
    onClose();
  };

  const handleWithdraw = async () => {
    if (!position || !address || !quote) return;

    setErrorMsg("");

    try {
      // Step 1 — Build
      setState("building");
      const { xdr } = await buildWithdrawTransaction({
        walletAddress: address,
        contractId: VAULT_CONTRACT_ID || `mock_${position.vaultId}`,
        tokenAddress: USDC_CONTRACT_ID || "mock_usdc",
        shares: quote.sharesBurned,
      });

      // Step 2 — Sign
      setState("signing");
      const signedXdr = await signTransaction(xdr);

      // Step 3 — Submit
      setState("submitting");
      const txReceipt = await submitTransaction(signedXdr);

      const result = recordWithdrawal({
        positionId: position.id,
        grossAmount: quote.grossAmount,
        txHash: txReceipt.txHash,
      });

      if (!result) throw new Error("Unable to record the withdrawal locally.");

      setReceipt({
        ...txReceipt,
        penaltyAmount: result.penaltyAmount,
        netAmount: result.netAmount,
      });
      setState("success");
    } catch (err) {
      setErrorMsg(humanizeError(err));
      setState("error");
    }
  };

  return (
    <ModalShell
      open={open && !!position}
      onClose={state === "signing" || state === "submitting" ? () => {} : reset}
      title={`Withdraw from ${position?.vaultName ?? "Vault"}`}
      subtitle="Review shares, lock period, and net proceeds before signing."
    >
      {position && (
        <div className="grid gap-0 lg:grid-cols-[1.05fr_0.95fr]">
          {/* ── Left: position summary + amount ── */}
          <div className="border-b border-border p-6 lg:border-b-0 lg:border-r">
            <div className="rounded-3xl border border-border bg-white p-5">
              {/* Position stats */}
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
                  <p className="mt-1 text-xs text-muted-foreground">Since deposit</p>
                </div>
              </div>

              {/* Lock period warning */}
              {!position.isMatured && (
                <div className="mt-4 flex items-start gap-2.5 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3">
                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                  <div className="text-xs text-amber-800">
                    <p className="font-medium">Early withdrawal penalty applies</p>
                    <p className="mt-0.5">
                      {position.daysRemaining} day{position.daysRemaining !== 1 ? "s" : ""} until maturity.
                      A {position.earlyWithdrawalPenaltyPct.toFixed(1)}% fee will be deducted from your gross proceeds.
                    </p>
                  </div>
                </div>
              )}

              {/* Amount input */}
              <div className="mt-4">
                <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                  Withdrawal Amount
                </label>
                <div
                  className={cn(
                    "flex items-center gap-3 rounded-2xl border bg-[#fafafa] px-4 py-4 transition-colors",
                    validationError ? "border-destructive/50" : "border-border"
                  )}
                >
                  <input
                    type="text"
                    inputMode="decimal"
                    value={amountInput}
                    onChange={(e) => {
                      const next = e.target.value;
                      if (/^\d*\.?\d*$/.test(next)) {
                        setAmountInput(next);
                        if (state === "error") setState("input");
                      }
                    }}
                    placeholder="0.00"
                    disabled={state !== "input" && state !== "error"}
                    className="min-w-0 flex-1 bg-transparent font-heading text-3xl font-light outline-none placeholder:text-muted-foreground/40 disabled:opacity-50"
                  />
                  <div className="flex items-center gap-2">
                    <span className="rounded-full bg-secondary px-3 py-2 text-sm font-medium text-foreground">
                      USDC
                    </span>
                    <button
                      onClick={() => setAmountInput(position.currentValue.toFixed(2))}
                      disabled={state !== "input" && state !== "error"}
                      className="rounded-full border border-border bg-white px-3 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15 disabled:opacity-40"
                    >
                      Max
                    </button>
                  </div>
                </div>
                {validationError && (
                  <p className="mt-1.5 flex items-center gap-1.5 text-xs text-destructive">
                    <AlertCircle className="h-3 w-3" />
                    {validationError}
                  </p>
                )}
              </div>

              {/* Quote preview */}
              <div className="mt-4 space-y-2.5 rounded-2xl border border-border bg-secondary/20 p-4 text-sm">
                {[
                  { label: "Gross proceeds", value: `${formatCurrency(quote?.grossAmount ?? 0)} USDC` },
                  { label: "Early exit penalty", value: `${formatCurrency(quote ? quote.grossAmount - quote.netAmount : 0)} USDC` },
                  { label: "Net to wallet", value: `${formatCurrency(quote?.netAmount ?? 0)} USDC`, highlight: true },
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
            </div>
          </div>

          {/* ── Right: transaction flow + actions ── */}
          <div className="p-6">
            <div className="rounded-3xl border border-border bg-white p-5">
              <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                Transaction Flow
              </p>

              <div className="mt-4 space-y-3">
                {TX_STEPS.map(({ label, activeStates }) => {
                  const done = activeStates.includes(state);
                  const active =
                    (label === "Build contract call" && state === "building") ||
                    (label === "Sign with wallet" && state === "signing") ||
                    (label === "Submit and confirm" && state === "submitting");
                  return (
                    <div
                      key={label}
                      className="flex items-center gap-3 rounded-2xl border border-border px-4 py-3"
                    >
                      <div
                        className={cn(
                          "flex h-8 w-8 items-center justify-center rounded-full border",
                          done && !active
                            ? "border-emerald-200 bg-emerald-50 text-emerald-600"
                            : active
                              ? "border-blue-200 bg-blue-50 text-blue-600"
                              : "border-border bg-secondary/40 text-muted-foreground"
                        )}
                      >
                        {active ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : done ? (
                          <CheckCircle2 className="h-4 w-4" />
                        ) : (
                          <Clock3 className="h-4 w-4" />
                        )}
                      </div>
                      <span className="text-sm text-foreground/80">{label}</span>
                    </div>
                  );
                })}
              </div>

              {/* Info note */}
              {state === "input" && (
                <div className="mt-5 rounded-2xl border border-border bg-secondary/20 p-4">
                  <div className="flex items-start gap-3">
                    <Sparkles className="mt-0.5 h-4 w-4 text-foreground/50" />
                    <p className="text-xs leading-relaxed text-muted-foreground">
                      Partial withdrawals burn shares proportionally. Full withdrawals
                      close the position entirely and return all remaining funds.
                    </p>
                  </div>
                </div>
              )}

              {/* Success receipt */}
              {state === "success" && receipt && (
                <div className="mt-5 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                  <div className="flex items-center gap-2 text-emerald-700">
                    <CheckCircle2 className="h-4 w-4" />
                    <p className="text-sm font-medium">Withdrawal confirmed</p>
                  </div>
                  <p className="mt-2 text-sm text-emerald-800/80">
                    {formatCurrency(receipt.netAmount)} USDC is on its way to your wallet.
                  </p>
                  {receipt.penaltyAmount > 0 && (
                    <p className="mt-1 text-xs text-emerald-800/60">
                      Penalty applied: {formatCurrency(receipt.penaltyAmount)} USDC
                    </p>
                  )}
                  <p className="mt-1 font-mono text-[11px] text-emerald-800/60">
                    {truncateTxHash(receipt.txHash)}
                  </p>
                  <div className="mt-3">
                    <Link
                      href={receipt.explorerUrl}
                      target="_blank"
                      className="inline-flex items-center gap-1.5 rounded-full bg-white px-3 py-2 text-xs font-medium text-foreground shadow-sm hover:shadow"
                    >
                      View on Stellar Explorer
                      <ExternalLink className="h-3.5 w-3.5" />
                    </Link>
                  </div>
                </div>
              )}

              {/* Error state */}
              {state === "error" && errorMsg && (
                <div className="mt-5 rounded-2xl border border-destructive/20 bg-destructive/10 p-4">
                  <div className="flex items-start gap-2 text-sm text-destructive">
                    <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>{errorMsg}</span>
                  </div>
                </div>
              )}

              {/* Actions */}
              <div className="mt-5 flex gap-3">
                <button
                  onClick={reset}
                  disabled={state === "signing" || state === "submitting"}
                  className="flex-1 rounded-full border border-border bg-white px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-black/15 disabled:opacity-40"
                >
                  {state === "success" ? "Close" : "Cancel"}
                </button>

                {state !== "success" && (
                  <button
                    onClick={state === "error" ? () => { setState("input"); setErrorMsg(""); } : handleWithdraw}
                    disabled={
                      state === "building" ||
                      state === "signing" ||
                      state === "submitting" ||
                      (state === "input" && !canSubmit)
                    }
                    className="flex-1 rounded-full bg-[#0a0a0a] px-5 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    {state === "building" && (
                      <span className="inline-flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Building
                      </span>
                    )}
                    {state === "signing" && (
                      <span className="inline-flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Awaiting Signature
                      </span>
                    )}
                    {state === "submitting" && (
                      <span className="inline-flex items-center justify-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Submitting
                      </span>
                    )}
                    {state === "error" && "Try Again"}
                    {state === "input" && "Confirm Withdrawal"}
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