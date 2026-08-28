"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  ExternalLink,
  Loader2,
  ShieldCheck,
  X,
} from "lucide-react";

import { usePortfolio } from "@/components/portfolio-provider";
import { useWallet } from "@/components/wallet-provider";
import { cn } from "@/lib/utils";
import { useOfflineStatus } from "@/hooks/useOfflineStatus";
import type { Vault as VaultDefinition, MarketStrategy } from "@/lib/types/vault";
import {
  executeVaultDeposit,
  UserRejectedError,
  TransactionFailedError,
  TransactionTimeoutError,
  truncateTxHash,
  type TransactionReceipt,
} from "@/lib/stellar/transaction";

// ── Types ─────────────────────────────────────────────────────────────────────

type ActionState =
  | "input"
  | "building"
  | "signing"
  | "submitting"
  | "pending"
  | "success"
  | "error";

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatCurrency(amount: number) {
  return amount.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

function humanizeError(err: unknown): string {
  if (err instanceof UserRejectedError) {
    return "You cancelled the transaction in your wallet. No funds were moved.";
  }
  if (err instanceof TransactionFailedError) {
    return `Transaction failed on-chain: ${err.reason}`;
  }
  if (err instanceof TransactionTimeoutError) {
    return "Transaction timed out. Check Stellar Explorer for the current status.";
  }
  if (err instanceof Error) {
    return err.message;
  }
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
                  <p id="modal-desc" className="mt-1 text-sm text-muted-foreground">
                    {subtitle}
                  </p>
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

const TX_STEPS: {
  label: string;
  activeStates: ActionState[];
  currentState: ActionState;
}[] = [
  {
    label: "Build and sign",
    activeStates: ["building", "pending", "success"],
    currentState: "building",
  },
  {
    label: "Submit to network",
    activeStates: ["pending", "success"],
    currentState: "pending",
  },
  {
    label: "Confirmed on-chain",
    activeStates: ["success"],
    currentState: "success",
  },
];

// ── DepositModal ──────────────────────────────────────────────────────────────

interface DepositModalProps {
  open: boolean;
  onClose: () => void;
  vault: VaultDefinition | null;
}

function getVaultMeta(vault: VaultDefinition) {
  const lockMatch = vault.maturityTerms.match(/(\d+)/);
  return {
    apy: vault.currentApy / 100,
    apyLabel: vault.apyRange,
    lockDays: lockMatch ? Number(lockMatch[1]) : 0,
    managementFeePct: 0.5,
    performanceFeePct: 10,
    asset: (vault.supportedAssets[0] ?? "USDC") as "USDC" | "XLM",
  };
}

export function DepositModal({ open, onClose, vault }: DepositModalProps) {
  const { address } = useWallet();
  const { getAvailableBalance, recordPendingDeposit, confirmPendingDeposit, failPendingDeposit, refreshBalances } = usePortfolio();
  const { isOffline } = useOfflineStatus();

  const [amountInput, setAmountInput] = useState("");
  const [state, setState] = useState<ActionState>("input");
  const [errorMsg, setErrorMsg] = useState("");
  const [receipt, setReceipt] = useState<TransactionReceipt | null>(null);
  const [userAssetOverride, setUserAssetOverride] = useState<"USDC" | "XLM" | null>(null);
  const [selectedStrategy, setSelectedStrategy] = useState<MarketStrategy | null>(
    vault?.strategies?.[0] ?? null
  );
  const submittingRef = useRef(false);

  const supportedAssets = (vault?.supportedAssets ?? ["USDC"]) as ("USDC" | "XLM")[];
  const strategies = vault?.strategies ?? [];

  const vaultDefaultAsset = (vault?.supportedAssets?.[0] as "USDC" | "XLM") ?? "USDC";
  const selectedAsset =
    userAssetOverride && supportedAssets.includes(userAssetOverride)
      ? userAssetOverride
      : vaultDefaultAsset;

  const balance = getAvailableBalance(selectedAsset);
  const amount = Number(amountInput) || 0;

  const validationError = useMemo(() => {
    if (!amount) return null;
    if (amount <= 0) return "Amount must be greater than 0.";
    if (amount > balance)
      return `Amount exceeds your balance of ${formatCurrency(balance)} ${selectedAsset}.`;
    return null;
  }, [amount, balance, selectedAsset]);

  const canSubmit =
    !!vault &&
    !!address &&
    amount > 0 &&
    !validationError &&
    state === "input" &&
    !isOffline;

  const reset = () => {
    setAmountInput("");
    setState("input");
    setErrorMsg("");
    setReceipt(null);
    onClose();
  };

  const handleDeposit = async () => {
    if (!vault || !address || submittingRef.current) return;
    submittingRef.current = true;
    setErrorMsg("");

    try {
      const contractId =
        selectedAsset === "XLM"
          ? vault.contractXlmAddress || vault.contractAddress
          : vault.contractAddress;

      setState("building");
      const pendingId = recordPendingDeposit({
        vaultId: vault.id,
        vaultName: vault.name,
        amount,
        asset: selectedAsset,
      });

      setState("pending");
      const txReceipt = await executeVaultDeposit({
        walletAddress: address,
        vaultId: vault.id,
        contractId,
        asset: selectedAsset,
        amount,
      });

      confirmPendingDeposit(pendingId, txReceipt.txHash);
      setReceipt(txReceipt);
      setState("success");
      refreshBalances();
    } catch (err) {
      const friendly = humanizeError(err);
      setErrorMsg(friendly);
      setState("error");
      failPendingDeposit(vault.id, amount);
    } finally {
      submittingRef.current = false;
    }
  };

  const meta = vault ? getVaultMeta(vault) : null;

  return (
    <ModalShell
      open={open}
      onClose={reset}
      title={`Deposit to ${vault?.name ?? "Vault"}`}
      subtitle="Supply assets to earn automated yield"
    >
      {/* Screen reader announcer for live status and errors */}
      <div className="sr-only" aria-live="polite">
        {errorMsg && `Error: ${errorMsg}`}
        {validationError && `Validation error: ${validationError}`}
        {state === "success" && "Transaction completed successfully."}
        {state === "pending" && "Transaction is pending confirmation on the network."}
        {state === "building" && "Building vault deposit transaction."}
        {amount > 0 && !validationError && `Entered deposit amount ${amount} ${selectedAsset}. Estimated yield preview available.`}
      </div>

      <div className="p-6 space-y-6">
        {state === "input" || state === "error" ? (
          <div className="space-y-4">
            <div>
              <div className="flex justify-between text-sm mb-2">
                <span className="text-muted-foreground">Amount</span>
                <span className="text-foreground font-medium">
                  Balance: {formatCurrency(balance)} {selectedAsset}
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
                  onClick={() => setAmountInput(balance.toString())}
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
                  <p className="font-medium">Deposit failed</p>
                  <p className="mt-1 text-xs opacity-90">{errorMsg}</p>
                </div>
              </div>
            )}

            <button
              disabled={!canSubmit}
              onClick={handleDeposit}
              className="w-full rounded-2xl bg-primary py-4 font-medium text-primary-foreground shadow-lg transition-all hover:opacity-90 disabled:opacity-50"
            >
              Confirm Deposit
            </button>
          </div>
        ) : (
          <div className="space-y-6 py-4">
            <div className="space-y-3">
              {TX_STEPS.map((step, i) => {
                const isDone = step.activeStates.includes(state);
                const isCurrent = state === step.currentState;
                return (
                  <div key={i} className="flex items-center gap-3">
                    <div
                      className={cn(
                        "flex h-7 w-7 items-center justify-center rounded-full text-xs font-medium",
                        isDone
                          ? "bg-primary text-primary-foreground"
                          : "bg-muted text-muted-foreground"
                      )}
                    >
                      {isDone ? <CheckCircle2 className="h-4 w-4" /> : i + 1}
                    </div>
                    <span
                      className={cn(
                        "text-sm font-medium",
                        isCurrent ? "text-foreground" : "text-muted-foreground"
                      )}
                    >
                      {step.label}
                    </span>
                    {isCurrent && state !== "success" && (
                      <Loader2 className="ml-auto h-4 w-4 animate-spin text-primary" />
                    )}
                  </div>
                );
              })}
            </div>

            {state === "success" && receipt && (
              <div className="rounded-2xl bg-emerald-500/10 p-4 text-center space-y-3">
                <CheckCircle2 className="mx-auto h-8 w-8 text-emerald-500" />
                <div>
                  <p className="font-semibold text-foreground">Deposit Successful!</p>
                  <p className="text-xs text-muted-foreground mt-1">
                    Successfully deposited {amount} {selectedAsset} into {vault?.name}
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
