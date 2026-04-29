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
  X,
} from "lucide-react";

import { usePortfolio } from "@/components/portfolio-provider";
import { useWallet } from "@/components/wallet-provider";
import { cn } from "@/lib/utils";
import { type Vault as VaultDefinition } from "@/hooks/useVaults";
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

// ── Transaction steps ─────────────────────────────────────────────────────────

const TX_STEPS: { label: string; activeStates: ActionState[] }[] = [
  {
    label: "Build contract call",
    activeStates: ["building", "signing", "submitting", "success"],
  },
  {
    label: "Sign with wallet",
    activeStates: ["signing", "submitting", "success"],
  },
  { label: "Submit and confirm", activeStates: ["success"] },
];

// ── DepositModal ──────────────────────────────────────────────────────────────

interface DepositModalProps {
  open: boolean;
  onClose: () => void;
  vault: VaultDefinition | null;
}

function getVaultMeta(vault: VaultDefinition) {
  return {
    apy: (vault.apy || 0) / 100,
    apyLabel: vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : "TBD",
    lockDays: 0,
    managementFeePct: 0.5,
    performanceFeePct: 10,
    asset: "USDC" as "USDC",
  };
}

/**
 * DepositModal
 *
 * Full on-chain deposit flow:
 * 1. User enters amount → validation against wallet balance
 * 2. Soroban transaction built + simulated via RPC
 * 3. Freighter signing popup
 * 4. Transaction submitted and polled until confirmed
 * 5. Receipt shown with explorer link
 *
 * Error handling:
 * - Freighter rejection → friendly "you cancelled" message
 * - On-chain failure → reason shown with retry option
 * - Network timeout → retry option with explorer link suggestion
 * - Missing Freighter → install prompt
 */
export function DepositModal({ open, onClose, vault }: DepositModalProps) {
  const { address } = useWallet();
  const { getAvailableBalance, recordDeposit, refreshBalances } = usePortfolio();

  const [amountInput, setAmountInput] = useState("");
  const [state, setState] = useState<ActionState>("input");
  const [errorMsg, setErrorMsg] = useState("");
  const [receipt, setReceipt] = useState<TransactionReceipt | null>(null);
  const [selectedAsset, setSelectedAsset] = useState<"USDC" | "XLM">("USDC");

  // Keep selectedAsset and strategy in sync when vault changes
  const supportedAssets = ["USDC"] as ("USDC" | "XLM")[];

  const amount = Number(amountInput) || 0;
  const meta = vault ? getVaultMeta(vault) : null;
  const balance = getAvailableBalance(selectedAsset);

  const validationError = useMemo(() => {
    if (!vault) return null;
    if (!vault.contractAddress || vault.contractAddress.length !== 56 || !vault.contractAddress.startsWith('C')) return "Vault is not yet live. Deposits are currently disabled.";
    if (!amount) return null;
    if (amount <= 0) return "Amount must be greater than 0.";
    if (vault.minDeposit && amount < vault.minDeposit) return `Minimum deposit is ${vault.minDeposit}.`;
    if (amount > balance)
      return `Insufficient balance. You have ${formatCurrency(balance)} ${selectedAsset} available.`;
    return null;
  }, [amount, balance, vault, selectedAsset]);

  const canSubmit =
    !!vault && !!address && amount > 0 && !validationError && state === "input";

  const effectiveApy = meta ? meta.apy : 0;
  const estimatedYield = amount * effectiveApy;

  const reset = () => {
    setAmountInput("");
    setState("input");
    setErrorMsg("");
    setReceipt(null);
    onClose();
  };

  const handleDeposit = async () => {
    if (!vault || !address || !canSubmit) return;

    setErrorMsg("");

    try {
      setState("building");
      const txReceipt = await executeVaultDeposit({
        walletAddress: address,
        vaultId: vault.id,
        contractId: vault.contractAddress,
        asset: selectedAsset,
        amount,
      });

      // Record in portfolio state
      recordDeposit({
        vault: {
          id: vault.id,
          name: vault.name,
          asset: selectedAsset,
          apy: meta?.apy || 0,
          lockDays: meta?.lockDays || 0,
          earlyWithdrawalPenaltyPct: 0,
        },
        amount,
        txHash: txReceipt.txHash,
      });

      setReceipt(txReceipt);
      setState("success");
      // Re-fetch true on-chain balance so UI reflects what actually happened
      refreshBalances();
    } catch (err) {
      setErrorMsg(humanizeError(err));
      setState("error");
    }
  };

  return (
    <ModalShell
      open={open && !!vault}
      onClose={state === "signing" || state === "submitting" ? () => {} : reset}
      title={`Deposit into ${vault?.name ?? "Vault"}`}
      subtitle={`Build and sign a Soroban transaction to deposit ${selectedAsset} into this vault.`}
    >
      {vault && (
        <>
        <div className="grid gap-0 lg:grid-cols-[1.05fr_0.95fr]">
          {/* ── Left: amount + preview ── */}
          <div className="border-b border-border p-6 lg:border-b-0 lg:border-r">
            <div className="rounded-3xl border border-border bg-white p-5">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                    {vault.name}
                  </p>
                  <p className="mt-2 font-heading text-3xl font-light text-emerald-600">
                    {meta?.apyLabel}
                  </p>
                  {meta?.apy !== undefined && meta?.apy !== 0 && (
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

              {/* Amount input */}
              <div className="mt-5">
                <label className="mb-2 block text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground">
                  Deposit Amount
                </label>
                <div
                  className={cn(
                    "flex items-center gap-3 rounded-2xl border bg-[#fafafa] px-4 py-4 transition-colors",
                    validationError ? "border-destructive/50" : "border-border",
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
                    {supportedAssets.length > 1 ? (
                      <div className="flex rounded-full border border-border bg-white p-0.5 shadow-sm">
                        {supportedAssets.map((a) => (
                          <button
                            key={a}
                            type="button"
                            onClick={() => {
                              setSelectedAsset(a);
                              setAmountInput("");
                              if (state === "error") setState("input");
                            }}
                            disabled={state !== "input" && state !== "error"}
                            className={cn(
                              "rounded-full px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-40",
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
                      onClick={() => setAmountInput(balance.toFixed(2))}
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

              {/* Preview */}
              <div className="mt-5 space-y-3 rounded-2xl border border-border bg-secondary/30 p-4">
                {[
                  {
                    label: "Strategy",
                    value: vault.strategy,
                  },
                  {
                    label: "Strategy APY",
                    value: meta?.apyLabel || "TBD",
                  },
                  {
                    label: "Estimated annual yield",
                    value: `${formatCurrency(estimatedYield)} ${selectedAsset}`,
                  },
                  {
                    label: "nVault shares to receive",
                    value: formatCurrency(amount),
                  },
                  {
                    label: "Lock period",
                    value: meta?.lockDays ? `${meta.lockDays} days` : "Flexible",
                  },
                ].map(({ label, value }) => (
                  <div
                    key={label}
                    className="flex items-center justify-between text-sm"
                  >
                    <span className="text-muted-foreground">{label}</span>
                    <span className="font-medium text-foreground">{value}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* ── Right: transaction flow (lg+ only) ── */}
          <div className="hidden lg:block p-6">
            <div className="rounded-3xl border border-border bg-white p-5 h-full">
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
                              : "border-border bg-secondary/40 text-muted-foreground",
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
              {state === "input" && (
                <div className="mt-5 rounded-2xl border border-border bg-secondary/20 p-4">
                  <div className="flex items-start gap-3">
                    <ShieldCheck className="mt-0.5 h-4 w-4 text-emerald-600" />
                    <p className="text-xs leading-relaxed text-muted-foreground">
                      A Freighter popup will appear to confirm signing. No funds
                      leave your wallet until you approve the transaction.
                    </p>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* ── Bottom: status + actions (always visible) ── */}

        <div className="border-t border-border px-6 pb-6 pt-5">
          {state === "success" && receipt && (
            <div className="mb-4 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
              <div className="flex items-center gap-2 text-emerald-700">
                <CheckCircle2 className="h-4 w-4" />
                <p className="text-sm font-medium">Deposit confirmed</p>
              </div>
              <p className="mt-2 text-sm text-emerald-800/80">
                {formatCurrency(amount)} {selectedAsset} deposited into the {vault?.name} vault.
              </p>
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

          {state === "error" && errorMsg && (
            <div className="mb-4 rounded-2xl border border-destructive/20 bg-destructive/10 p-4">
              <div className="flex items-start gap-2 text-sm text-destructive">
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{errorMsg}</span>
              </div>
            </div>
          )}

          <div className="flex gap-3">
            <button
              onClick={reset}
              disabled={state === "signing" || state === "submitting"}
              className="flex-1 rounded-full border border-border bg-white px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-black/15 disabled:opacity-40"
            >
              {state === "success" ? "Close" : "Cancel"}
            </button>

            {state !== "success" && (
              <button
                onClick={
                  state === "error"
                    ? () => { setState("input"); setErrorMsg(""); }
                    : handleDeposit
                }
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
                {state === "input" && "Confirm Deposit"}
              </button>
            )}
          </div>
        </div>
        </>
      )}
    </ModalShell>
  );
}
