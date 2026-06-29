"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  ExternalLink,
  Loader2,
  Repeat2,
  Wallet,
  X,
} from "lucide-react";

import {
  type HarvestPreview,
  type HarvestResult,
  vaultsApi,
} from "@/lib/api/vaults";
import { cn } from "@/lib/utils";
import { getExplorerTxUrl } from "@/utils/explorer";

interface HarvestModalProps {
  open: boolean;
  onClose: () => void;
  vaultId: string;
  vaultName?: string;
  onSuccess?: (result: HarvestResult) => void;
}

type Step = "preview" | "confirm";
type ExecutionState = "idle" | "submitting" | "success" | "error";

function formatUsdc(value: string | undefined) {
  const amount = Number.parseFloat(value ?? "0");
  if (!Number.isFinite(amount)) return `${value ?? "0"} USDC`;

  return `${amount.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  })} USDC`;
}

function formatBps(bps: number) {
  const percentage = bps / 100;
  return `${percentage.toLocaleString("en-US", {
    minimumFractionDigits: percentage % 1 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  })}%`;
}

function harvestableYieldIsZero(preview: HarvestPreview | null) {
  if (!preview) return true;
  const grossYield = Number.parseFloat(preview.gross_yield_usdc);
  return !Number.isFinite(grossYield) || grossYield <= 0;
}

function truncateTxHash(txHash: string) {
  if (txHash.length <= 18) return txHash;
  return `${txHash.slice(0, 8)}...${txHash.slice(-8)}`;
}

function errorMessage(error: unknown) {
  return error instanceof Error
    ? error.message
    : "Harvest failed. Please try again.";
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
          className="fixed inset-0 z-100 bg-black/45 px-4 py-8 backdrop-blur-sm"
        >
          <div className="flex min-h-full items-center justify-center">
            <motion.div
              initial={{ opacity: 0, y: 24, scale: 0.98 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 12, scale: 0.98 }}
              transition={{ duration: 0.2 }}
              className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-[28px] border border-white/10 bg-[#fafafa] shadow-2xl"
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
                  type="button"
                  onClick={onClose}
                  className="rounded-full border border-border bg-white p-2 text-muted-foreground transition-colors hover:text-foreground"
                  aria-label="Close harvest modal"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
              <div className="flex-1 overflow-y-auto">{children}</div>
            </motion.div>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function AmountRow({
  label,
  value,
  highlight,
}: {
  label: string;
  value: string;
  highlight?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-4 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={cn(
          "text-right font-medium text-foreground",
          highlight && "text-emerald-600"
        )}
      >
        {value}
      </span>
    </div>
  );
}

export function HarvestModal({
  open,
  onClose,
  vaultId,
  vaultName = "Vault",
  onSuccess,
}: HarvestModalProps) {
  const queryClient = useQueryClient();

  const [compound, setCompound] = useState(true);
  const [step, setStep] = useState<Step>("preview");
  const [executionState, setExecutionState] =
    useState<ExecutionState>("idle");
  const [executeError, setExecuteError] = useState("");
  const [result, setResult] = useState<HarvestResult | null>(null);

  const previewQuery = useQuery({
    queryKey: ["harvest-preview", vaultId, compound],
    enabled: open && Boolean(vaultId),
    staleTime: 0,
    refetchOnMount: "always",
    queryFn: () => vaultsApi.previewHarvest(vaultId, compound),
  });

  const preview = previewQuery.data ?? null;
  const previewLoading =
    previewQuery.isLoading || (previewQuery.isFetching && !preview);
  const previewError = previewQuery.error
    ? errorMessage(previewQuery.error)
    : "";

  const resetFlow = () => {
    setStep("preview");
    setExecutionState("idle");
    setExecuteError("");
    setResult(null);
  };

  const hasNoHarvestableYield = useMemo(
    () => harvestableYieldIsZero(preview),
    [preview]
  );
  const isSubmitting = executionState === "submitting";
  const canContinue =
    !!preview && !previewLoading && !previewError && !hasNoHarvestableYield;
  const canSubmit =
    step === "confirm" &&
    !!preview &&
    !hasNoHarvestableYield &&
    executionState !== "submitting" &&
    executionState !== "success";

  const resetAndClose = () => {
    if (isSubmitting) return;
    resetFlow();
    onClose();
  };

  const handleConfirm = async () => {
    if (!canSubmit) return;

    setExecutionState("submitting");
    setExecuteError("");

    try {
      const harvestResult = await vaultsApi.harvest(vaultId, compound);
      setResult(harvestResult);
      setExecutionState("success");
      onSuccess?.(harvestResult);
      await queryClient.invalidateQueries({ queryKey: ["vault", vaultId] });
    } catch (error) {
      setExecuteError(errorMessage(error));
      setExecutionState("error");
    }
  };

  return (
    <ModalShell
      open={open}
      onClose={resetAndClose}
      title={`Harvest Yield from ${vaultName}`}
      subtitle="Preview the yield, fee, and destination before submitting the Soroban transaction."
    >
      <div className="grid gap-0 lg:grid-cols-[1.05fr_0.95fr]">
        <div className="border-b border-border p-6 lg:border-b-0 lg:border-r">
          <div className="rounded-3xl border border-border bg-white p-5">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                  Harvest Destination
                </p>
                <p className="mt-2 font-heading text-2xl font-light text-foreground">
                  {compound ? "Compound" : "Withdraw"}
                </p>
              </div>
              <div
                className="flex rounded-full border border-border bg-[#fafafa] p-0.5 shadow-sm"
                role="group"
                aria-label="Harvest destination"
              >
                <button
                  type="button"
                  aria-pressed={compound}
                  onClick={() => {
                    setCompound(true);
                    resetFlow();
                  }}
                  disabled={isSubmitting}
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-40",
                    compound
                      ? "bg-foreground text-background"
                      : "text-foreground/60 hover:text-foreground"
                  )}
                >
                  <Repeat2 className="h-3.5 w-3.5" />
                  Compound
                </button>
                <button
                  type="button"
                  aria-pressed={!compound}
                  onClick={() => {
                    setCompound(false);
                    resetFlow();
                  }}
                  disabled={isSubmitting}
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-40",
                    !compound
                      ? "bg-foreground text-background"
                      : "text-foreground/60 hover:text-foreground"
                  )}
                >
                  <Wallet className="h-3.5 w-3.5" />
                  Withdraw
                </button>
              </div>
            </div>

            <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
              {compound
                ? "Net yield will be reinvested into this vault as new shares."
                : "Net yield will be sent back to your connected wallet."}
            </p>

            <div className="mt-5 space-y-3 rounded-2xl border border-border bg-secondary/20 p-4">
              {previewLoading && (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Loading harvest preview
                </div>
              )}

              {!previewLoading && previewError && (
                <div className="space-y-3">
                  <div className="flex items-start gap-2 text-sm text-destructive">
                    <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>{previewError}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => previewQuery.refetch()}
                    className="rounded-full border border-border bg-white px-3 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15"
                  >
                    Retry Preview
                  </button>
                </div>
              )}

              {!previewLoading && preview && (
                <>
                  <AmountRow
                    label="Gross Yield"
                    value={formatUsdc(preview.gross_yield_usdc)}
                  />
                  <AmountRow
                    label={`Performance Fee (${formatBps(
                      preview.performance_fee_bps
                    )})`}
                    value={formatUsdc(preview.performance_fee_usdc)}
                  />
                  <AmountRow
                    label="Net Yield"
                    value={formatUsdc(preview.net_yield_usdc)}
                    highlight
                  />
                  {compound && preview.estimated_new_shares && (
                    <AmountRow
                      label="Estimated New Shares"
                      value={preview.estimated_new_shares}
                    />
                  )}
                </>
              )}
            </div>

            {preview && hasNoHarvestableYield && (
              <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 p-4">
                <div className="flex items-start gap-2.5 text-amber-800">
                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                  <div className="text-sm">
                    <p className="font-medium">Impaired vault warning</p>
                    <p className="mt-1 text-xs leading-relaxed">
                      This vault has no positive harvestable yield right now.
                      Harvesting is disabled until accrued yield is greater than
                      zero.
                    </p>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="p-6">
          <div className="rounded-3xl border border-border bg-white p-5">
            {step === "preview" && (
              <>
                <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                  Step 1
                </p>
                <h3 className="mt-2 font-heading text-xl font-light text-foreground">
                  Preview
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Review the gross yield, performance fee, and net yield before
                  moving to confirmation.
                </p>
              </>
            )}

            {step === "confirm" && (
              <>
                <button
                  type="button"
                  onClick={() => {
                    if (!isSubmitting) setStep("preview");
                  }}
                  disabled={isSubmitting}
                  className="mb-4 inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
                >
                  <ArrowLeft className="h-3.5 w-3.5" />
                  Preview
                </button>

                <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                  Step 2
                </p>
                <h3 className="mt-2 font-heading text-xl font-light text-foreground">
                  Confirm & Execute
                </h3>

                {executionState === "idle" && preview && (
                  <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                    Confirm this harvest to submit the transaction. You will
                    receive {formatUsdc(preview.net_yield_usdc)}{" "}
                    {compound ? "as compounded vault shares" : "to your wallet"}.
                  </p>
                )}

                {executionState === "submitting" && (
                  <div className="mt-5 rounded-2xl border border-blue-200 bg-blue-50 p-4">
                    <div className="flex items-center gap-2 text-sm font-medium text-blue-700">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Submitting harvest
                    </div>
                    <p className="mt-2 text-xs leading-relaxed text-blue-800/70">
                      The Soroban transaction is in-flight. Keep this window
                      open until the result is returned.
                    </p>
                  </div>
                )}

                {executionState === "error" && executeError && (
                  <div className="mt-5 rounded-2xl border border-destructive/20 bg-destructive/10 p-4">
                    <div className="flex items-start gap-2 text-sm text-destructive">
                      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                      <span>{executeError}</span>
                    </div>
                  </div>
                )}

                {executionState === "success" && result && (
                  <div className="mt-5 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
                    <div className="flex items-center gap-2 text-emerald-700">
                      <CheckCircle2 className="h-4 w-4" />
                      <p className="text-sm font-medium">Harvest confirmed</p>
                    </div>
                    <p className="mt-2 text-sm text-emerald-800/80">
                      You received {formatUsdc(result.net_yield_usdc)}{" "}
                      {result.compounded
                        ? "as compounded vault yield."
                        : "to your wallet."}
                    </p>
                    {result.tx_hash && (
                      <>
                        <p className="mt-2 font-mono text-[11px] text-emerald-800/60">
                          {truncateTxHash(result.tx_hash)}
                        </p>
                        <div className="mt-3">
                          <Link
                            href={getExplorerTxUrl(result.tx_hash)}
                            target="_blank"
                            className="inline-flex items-center gap-1.5 rounded-full bg-white px-3 py-2 text-xs font-medium text-foreground shadow-sm hover:shadow"
                          >
                            View on Stellar Explorer
                            <ExternalLink className="h-3.5 w-3.5" />
                          </Link>
                        </div>
                      </>
                    )}
                  </div>
                )}
              </>
            )}

            <div className="mt-6 flex gap-3">
              <button
                type="button"
                onClick={resetAndClose}
                disabled={isSubmitting}
                className="flex-1 rounded-full border border-border bg-white px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-black/15 disabled:opacity-40"
              >
                {executionState === "success" ? "Close" : "Cancel"}
              </button>

              {step === "preview" && (
                <button
                  type="button"
                  onClick={() => setStep("confirm")}
                  disabled={!canContinue}
                  className="flex-1 rounded-full bg-[#0a0a0a] px-5 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Harvest
                </button>
              )}

              {step === "confirm" && executionState !== "success" && (
                <button
                  type="button"
                  onClick={handleConfirm}
                  disabled={!canSubmit}
                  className="flex-1 rounded-full bg-[#0a0a0a] px-5 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {executionState === "submitting" && (
                    <span className="inline-flex items-center justify-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Submitting
                    </span>
                  )}
                  {executionState === "error" && "Retry Harvest"}
                  {executionState === "idle" && "Confirm Harvest"}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </ModalShell>
  );
}
