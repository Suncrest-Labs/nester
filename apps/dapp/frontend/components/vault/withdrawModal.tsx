"use client";

import Link from "next/link";
import { useMemo, useRef, useState, useEffect } from "react";
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
import { useOfflineStatus } from "@/hooks/useOfflineStatus";
import {
  executeVaultWithdraw,
  UserRejectedError,
  TransactionFailedError,
  TransactionTimeoutError,
  truncateTxHash,
  type TransactionReceipt,
} from "@/lib/stellar/transaction";
import type { Vault as VaultDefinition } from "@/lib/types/vault";
import { getVaultContractById as getVaultById } from "@/lib/vault-contracts";

// ── Types ─────────────────────────────────────────────────────────────────────

type ActionState = "input" | "building" | "signing" | "submitting" | "pending" | "success" | "error";

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
  const modalRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<Element | null>(null);

  useEffect(() => {
    if (open) {
      triggerRef.current = document.activeElement;
      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
          return;
        }
        if (e.key === "Tab" && modalRef.current) {
          const focusableElements = modalRef.current.querySelectorAll(
            'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
          );
          const firstElement = focusableElements[0] as HTMLElement;
          const lastElement = focusableElements[focusableElements.length - 1] as HTMLElement;

          if (e.shiftKey) {
            if (document.activeElement === firstElement) {
              lastElement?.focus();
              e.preventDefault();
            }
          } else {
            if (document.activeElement === lastElement) {
              firstElement?.focus();
              e.preventDefault();
            }
          }
        }
      };
      window.addEventListener("keydown", handleKeyDown);

      const timer = setTimeout(() => {
        const firstInput = modalRef.current?.querySelector(
          'input, button'
        ) as HTMLElement;
        firstInput?.focus();
      }, 50);

      return () => {
        window.removeEventListener("keydown", handleKeyDown);
        clearTimeout(timer);
        if (triggerRef.current && triggerRef.current instanceof HTMLElement) {
          triggerRef.current.focus();
        }
      };
    }
  }, [open, onClose]);

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
              ref={modalRef}
              role="dialog"
              aria-modal="true"
              aria-labelledby="modal-title"
              aria-describedby="modal-desc"
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
                  <h2 id="modal-title" className="mt-2 font-heading text-2xl font-light text-foreground">
                    {title}
                  </h2>
                  <p id="modal-desc" className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
                </div>
                <button
                  onClick={onClose}
                  aria-label="Close modal"
                  className="rounded-full border border-border bg-white dark:bg-[#100F0F] p-2 text-muted-foreground transition-colors hover:text-foreground"
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
  { label: "Build contract call", activeStates: ["building", "signing", "submitting", "pending", "success"] },
  { label: "Sign with wallet",    activeStates: ["signing", "submitting", "pending", "success"] },
  { label: "Submit and confirm",  activeStates: ["submitting", "pending", "success"] },
];

// ── WithdrawModal ─────────────────────────────────────────────────────────────

interface WithdrawModalProps {
  open: boolean;
  onClose: () => void;
  position: PortfolioPosition | null;
}

export function WithdrawModal({ open, onClose, position }: WithdrawModalProps) {
  const { address } = useWallet();
  const { getWithdrawalQuote, recordWithdrawal, refreshBalances } = usePortfolio();
  const { isOffline } = useOfflineStatus();

  const [amountInput, setAmountInput] = useState("");
  const [state, setState] = useState<ActionState>("input");
  const [errorMsg, setErrorMsg] = useState("");
  const [receipt, setReceipt] = useState<(TransactionReceipt & { penaltyAmount: number; netAmount: number }) | null>(null);
  const submittingRef = useRef(false);

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
      return `Maximum withdrawal is ${formatCurrency(position.currentValue)} ${position.asset ?? "USDC"}.`;
    return null;
  }, [amount, position]);

  const canSubmit =
    !!position &&
    !!address &&
    amount > 0 &&
    !validationError &&
    !!quote &&
    state === "input" &&
    !isOffline;

  const reset = () => {
    setAmountInput("");
    setState("input");
    setErrorMsg("");
    setReceipt(null);
    onClose();
  };

  const handleWithdraw = async () => {
    if (!position || !address || quote || submittingRef.current) return;
    submittingRef.current = true;

    setErrorMsg("");

    try {
      const vaultDef = getVaultById(position.vaultId);
      setState("pending");
      const txReceipt = await executeVaultWithdraw({
        walletAddress: address,
        vaultId: position.vaultId,
        contractId: vaultDef?.contractAddress || "",
        asset: position.asset?.toUpperCase() === "XLM" ? "XLM" : "USDC",
        shares: quote?.sharesBurned || 0,
        minAssetsOut: quote?.netAmount || 0,
      });

      recordWithdrawal({
        vaultId: position.vaultId,
        amount: quote?.netAmount || 0,
        sharesBurned: quote?.sharesBurned || 0,
        txHash: txReceipt.txHash,
      });

      setReceipt({
        ...txReceipt,
        penaltyAmount: quote?.penaltyAmount || 0,
        netAmount: quote?.netAmount || 0,
      });
      setState("success");
      refreshBalances();
    } catch (err) {
      setErrorMsg(humanizeError(err));
      setState("error");
    } finally {
      submittingRef.current = false;
    }
  };

  return (
    <ModalShell
      open={open}
      onClose={reset}
      title="Withdraw from Vault"
      subtitle="Redeem shares for underlying assets"
    >
      {/* Screen reader announcer for live status and errors */}
      <div className="sr-only" aria-live="polite">
        {errorMsg && `Error: ${errorMsg}`}
        {validationError && `Validation error: ${validationError}`}
        {state === "success" && "Withdrawal transaction completed successfully."}
        {state === "pending" && "Withdrawal transaction is pending confirmation."}
        {amount > 0 && !validationError && quote && `Entered withdrawal amount ${amount}. Net proceeds: ${quote.netAmount}.`}
      </div>

      <div className="p-6 space-y-6">
        {state === "input" || state === "error" ? (
          <div className="space-y-4">
            <div>
              <div className="flex justify-between text-sm mb-2">
                <span className="text-muted-foreground">Amount</span>
                <span className="text-foreground font-medium">
                  Position: {formatCurrency(position?.currentValue || 0)} {position?.asset ?? "USDC"}
                </span>
              </div>
              <div className="relative">
                <input
                  type="number"
                  step="any"
                  placeholder="0.00"
                  value={amountInput}
                  onChange={(e) => setAmountInput(e.target.value)}
                  className="w-full rounded-2xl border border-border bg-white dark:bg-[#100F0F] px-4 py-3.5 text-2xl font-medium text-foreground outline-none focus:border-primary"
                />
                <button
                  onClick={() => setAmountInput((position?.currentValue || 0).toString())}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded-lg bg-muted px-2.5 py-1 text-xs font-semibold text-muted-foreground hover:text-foreground"
                >
                  MAX
                </button>
              </div>
              {validationError && (
                <p className="mt-2 text-xs text-destructive flex items-center gap-1.5">
                  <AlertCircle className="h-3.5 w-3.5" /> {validationError}
                </p>
              )}
            </div>

            {errorMsg && (
              <div className="rounded-xl bg-destructive/10 p-4 text-sm text-destructive flex items-start gap-3">
                <AlertCircle className="h-5 w-5 shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium">Withdrawal failed</p>
                  <p className="mt-1 text-xs opacity-90">{errorMsg}</p>
                </div>
              </div>
            )}

            <button
              disabled={!canSubmit}
              onClick={handleWithdraw}
              className="w-full rounded-2xl bg-primary py-4 font-medium text-primary-foreground shadow-lg transition-all hover:opacity-90 disabled:opacity-50"
            >
              Confirm Withdrawal
            </button>
          </div>
        ) : (
          <div className="space-y-6 py-4">
            <div className="space-y-3">
              {TX_STEPS.map((step, i) => {
                const isDone = step.activeStates.includes(state);
                return (
                  <div key={i} className="flex items-center gap-3">
                    <div
                      className={cn(
                        "flex h-7 w-7 items-center justify-center rounded-full text-xs font-medium",
                        isDone ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
                      )}
                    >
                      {isDone ? <CheckCircle2 className="h-4 w-4" /> : i + 1}
                    </div>
                    <span className={cn("text-sm font-medium", isDone ? "text-foreground" : "text-muted-foreground")}>
                      {step.label}
                    </span>
                  </div>
                );
              })}
            </div>

            {state === "success" && receipt && (
              <div className="rounded-2xl bg-emerald-500/10 p-4 text-center space-y-3">
                <CheckCircle2 className="mx-auto h-8 w-8 text-emerald-500" />
                <div>
                  <p className="font-semibold text-foreground">Withdrawal Successful!</p>
                  <p className="text-xs text-muted-foreground mt-1">
                    Successfully withdrew {formatCurrency(receipt.netAmount)} {position?.asset ?? "USDC"}
                  </p>
                </div>
                <Link
                  href={receipt.explorerUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:underline"
                >
                  View on Explorer <ExternalLink className="h-3 w-3" />
                </Link>
                <button
                  onClick={reset}
                  className="w-full mt-2 rounded-xl bg-primary py-3 font-medium text-primary-foreground"
                >
                  Done
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </ModalShell>
  );
}
