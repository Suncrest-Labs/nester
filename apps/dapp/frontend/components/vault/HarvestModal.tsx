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
import { useYieldHarvests } from "@/hooks/useYieldHarvests";
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

type PreviewWithGas = HarvestPreview & {
  gas_cost_usdc?: string;
  estimated_gas_cost_usdc?: string;
  net_yield_after_gas_usdc?: string;
};

function formatUsdc(value: string | number | undefined) {
  const amount = Number.parseFloat(String(value ?? "0"));
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

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;

  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
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
                  className="rounded-full border border-border bg-white p-2 text-muted-foreground transition-colors hover:text-foreground dark:bg-[#100F0F]"
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

  const historyQuery = useYieldHarvests(50);
  const preview = previewQuery.data ?? null;
  const previewWithGas = preview as PreviewWithGas | null;
  const previewLoading =
    previewQuery.isLoading || (previewQuery.isFetching && !preview);
  const previewError = previewQuery.error
    ? errorMessage(previewQuery.error)
    : "";

  const vaultHarvests = useMemo(() => {
    return (
      historyQuery.data?.pages
        .flatMap((page) => page.items)
        .filter((harvest) => harvest.vault_id === vaultId)
        .slice(0, 5) ?? []
    );
  }, [historyQuery.data, vaultId]);

  const estimatedGasCost = useMemo(() => {
    const value =
      previewWithGas?.gas_cost_usdc ??
      previewWithGas?.estimated_gas_cost_usdc ??
      "0";
    const amount = Number.parseFloat(value);
    return Number.isFinite(amount) && amount >= 0 ? amount : 0;
  }, [previewWithGas]);

  const netYieldAfterGas = useMemo(() => {
    if (!preview) return "0";
    if (previewWithGas?.net_yield_after_gas_usdc) {
      return previewWithGas.net_yield_after_gas_usdc;
    }
    const gross = Number.parseFloat(preview.gross_yield_usdc);
    const net = Number.isFinite(gross) ? gross - estimatedGasCost : 0;
    return Math.max(0, net).toString();
  }, [preview, previewWithGas, estimatedGasCost]);

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

  const resetFlow = () => {
    setStep("preview");
    setExecutionState("idle");
    setExecuteError("");
    setResult(null);
  };

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
      await queryClient.invalidateQueries({ queryKey: ["yieldHarvests"] });
    } catch (err) {
      setExecutionState("error");
      setExecuteError(errorMessage(err));
    }
  };

  return (
    <ModalShell
      open={open}
      onClose={resetAndClose}
      title={`Harvest Yield — ${vaultName}`}
      subtitle="Claim accumulated yield and review past performance and gas efficiency."
    >
      <div className="p-6 space-y-6">
        {executionState === "success" && result ? (
          <div className="rounded-2xl border border-emerald-500/20 bg-emerald-500/5 p-6 text-center space-y-4">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-600">
              <CheckCircle2 className="h-6 w-6" />
            </div>
            <div className="space-y-1">
              <h3 className="font-heading text-lg font-medium text-foreground">
                Harvest Successful
              </h3>
              <p className="text-sm text-muted-foreground">
                Successfully harvested {formatUsdc(result.amount)} from {vaultName}.
              </p>
            </div>
            {result.tx_hash && (
              <div className="pt-2">
                <a
                  href={getExplorerTxUrl(result.tx_hash)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-600 hover:underline"
                >
                  View on Explorer <ExternalLink className="h-3 w-3" />
                </a>
              </div>
            )}
            <div className="pt-4">
              <button
                type="button"
                onClick={resetAndClose}
                className="w-full rounded-xl bg-foreground px-4 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-90"
              >
                Done
              </button>
            </div>
          </div>
        ) : (
          <> 
            {previewLoading ? (
              <div className="flex flex-col items-center justify-center py-12 gap-3">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                <p className="text-sm text-muted-foreground">Calculating harvest preview and gas costs...</p>
              </div>
            ) : previewError ? (
              <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-4 flex items-start gap-3">
                <AlertCircle className="h-5 w-5 text-rose-500 shrink-0 mt-0.5" />
                <div>
                  <p className="text-sm font-medium text-foreground">Failed to load harvest preview</p>
                  <p className="text-xs text-muted-foreground mt-0.5">{previewError}</p>
                </div>
              </div>
            ) : preview ? (
              <div className="space-y-6">
                {step === "preview" ? (
                  <div className="space-y-6">
                    <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-5 space-y-4 shadow-sm">
                      <h4 className="font-mono text-xs uppercase tracking-wider text-muted-foreground">
                        Current Harvest Estimate
                      </h4>
                      <div className="space-y-3">
                        <AmountRow
                          label="Gross Yield"
                          value={formatUsdc(preview.gross_yield_usdc)}
                        />
                        <AmountRow
                          label="Protocol Performance Fee"
                          value={`-${formatUsdc(preview.fee_usdc)}`}
                        />
                        <AmountRow
                          label="Estimated Gas Cost"
                          value={`-${formatUsdc(estimatedGasCost)}`}
                        />
                        <div className="border-t border-border pt-3">
                          <AmountRow
                            label="Net Yield After Gas"
                            value={formatUsdc(netYieldAfterGas)}
                            highlight
                          />
                        </div>
                      </div>
                    </div>

                    {hasNoHarvestableYield && (
                      <div className="rounded-xl border border-amber-500/20 bg-amber-500/5 p-4 flex items-start gap-3">
                        <AlertCircle className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
                        <div className="text-xs">
                          <p className="font-medium text-foreground">No yield available to harvest yet</p>
                          <p className="text-muted-foreground mt-0.5">
                            This vault has not accumulated enough yield to make harvesting profitable against current gas costs.
                          </p>
                        </div>
                      </div>
                    )}

                    <div className="flex items-center justify-between rounded-xl border border-border bg-white dark:bg-[#100F0F] p-4">
                      <div className="space-y-0.5">
                        <label className="text-sm font-medium text-foreground">Compound automatically</label>
                        <p className="text-xs text-muted-foreground">Reinvest harvested yield directly back into the vault</p>
                      </div>
                      <input
                        type="checkbox"
                        checked={compound}
                        onChange={(e) => setCompound(e.target.checked)}
                        className="h-4 w-4 rounded border-border text-foreground accent-foreground focus:ring-0"
                      />
                    </div>

                    {/* Past Harvests & Net Yield After Gas History */}
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <h4 className="font-mono text-xs uppercase tracking-wider text-muted-foreground">
                          Recent Harvest History & Net Yield
                        </h4>
                        <Link
                          href="/yields/history"
                          className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
                        >
                          View all <ExternalLink className="h-3 w-3" />
                        </Link>
                      </div>
                      <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] overflow-hidden">
                        {vaultHarvests.length === 0 ? (
                          <div className="p-6 text-center text-xs text-muted-foreground">
                            No past harvests recorded for this vault yet.
                          </div>
                        ) : (
                          <div className="divide-y divide-border">
                            {vaultHarvests.map((harvest) => {
                              const amountNum = Number.parseFloat(harvest.amount) || 0;
                              return (
                                <div key={harvest.id} className="flex items-center justify-between p-4 text-xs">
                                  <div className="space-y-0.5">
                                    <span className="font-medium text-foreground">
                                      {formatDate(harvest.harvested_at)}
                                    </span>
                                    <p className="text-muted-foreground">
                                      {harvest.protocol || vaultName}
                                    </p>
                                  </div>
                                  <div className="text-right space-y-0.5">
                                    <span className="font-mono font-medium text-emerald-600">
                                      +{formatUsdc(harvest.amount)}
                                    </span>
                                    {harvest.tx_hash && (
                                      <div className="">
                                        <a
                                          href={getExplorerTxUrl(harvest.tx_hash)}
                                          target="_blank"
                                          rel="noopener noreferrer"
                                          className="text-[10px] text-muted-foreground hover:underline inline-flex items-center gap-0.5"
                                        >
                                          Tx <ExternalLink className="h-2.5 w-2.5" />
                                        </a>
                                      </div>
                                    )}
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </div>

                    <div className="flex justify-end gap-3 pt-2">
                      <button
                        type="button"
                        onClick={resetAndClose}
                        className="rounded-xl border border-border bg-white dark:bg-[#100F0F] px-4 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-muted"
                      >
                        Cancel
                      </button>
                      <button
                        type="button"
                        disabled={!canContinue}
                        onClick={() => setStep("confirm")}
                        className="rounded-xl bg-foreground px-5 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-90 disabled:opacity-50"
                      >
                        Continue
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-6">
                    <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-5 space-y-4 shadow-sm">
                      <h4 className="font-mono text-xs uppercase tracking-wider text-muted-foreground">
                        Confirm Harvest Execution
                      </h4>
                      <div className="space-y-3">
                        <AmountRow
                          label="Vault"
                          value={vaultName}
                        />
                        <AmountRow
                          label="Mode"
                          value={compound ? "Compound (Reinvest)" : "Claim to Wallet"}
                        />
                        <AmountRow
                          label="Estimated Net Yield"
                          value={formatUsdc(netYieldAfterGas)}
                          highlight
                        />
                      </div>
                    </div>

                    {executeError && (
                      <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-4 flex items-start gap-3">
                        <AlertCircle className="h-5 w-5 text-rose-500 shrink-0 mt-0.5" />
                        <div className="text-xs">
                          <p className="font-medium text-foreground">Execution failed</p>
                          <p className="text-muted-foreground mt-0.5">{executeError}</p>
                        </div>
                      </div>
                    )}

                    <div className="flex justify-between gap-3 pt-2">
                      <button
                        type="button"
                        disabled={isSubmitting}
                        onClick={() => setStep("preview")}
                        className="rounded-xl border border-border bg-white dark:bg-[#100F0F] px-4 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-50"
                      >
                        Back
                      </button>
                      <div className="flex gap-3">
                        <button
                          type="button"
                          disabled={isSubmitting}
                          onClick={resetAndClose}
                          className="rounded-xl border border-border bg-white dark:bg-[#100F0F] px-4 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-50"
                        >
                          Cancel
                        </button>
                        <button
                          type="button"
                          disabled={!canSubmit}
                          onClick={handleConfirm}
                          className="inline-flex items-center gap-2 rounded-xl bg-foreground px-5 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-90 disabled:opacity-50"
                        >
                          {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
                          {isSubmitting ? "Harvesting..." : "Confirm Harvest"}
                        </button>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            ) : null}
          </>
        )}
      </div>
    </ModalShell>
  );
}
