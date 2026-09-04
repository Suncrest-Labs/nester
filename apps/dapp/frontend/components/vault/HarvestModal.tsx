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
  Calendar,
  History,
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
    hour: "2-digit",
    minute: "2-digit",
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
              className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-[28px] border border-white/10 bg-[#fafafa] shadow-2xl dark:bg-[#100F0F]"
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
                  className="rounded-full border border-border bg-white p-2 text-muted-foreground transition-colors hover:text-foreground dark:bg-[#181717]"
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
  const [activeTab, setActiveTab] = useState<"harvest" | "history">("harvest");

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
        .filter((harvest) => harvest.vault_id === vaultId) ?? []
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
    if (!Number.isFinite(gross)) return "0";
    return Math.max(0, gross - estimatedGasCost).toString();
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
    setActiveTab("harvest");
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
      await queryClient.invalidateQueries({ queryKey: ["harvest-preview", vaultId] });
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
      subtitle="Review yield generation, estimated gas costs, and past performance."
    >
      <div className="flex border-b border-border px-6 pt-2">
        <button
          type="button"
          onClick={() => setActiveTab("harvest")}
          className={cn(
            "border-b-2 px-4 py-2.5 text-sm font-medium transition-colors",
            activeTab === "harvest"
              ? "border-emerald-600 text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          )}
        >
          Harvest Action
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("history")}
          className={cn(
            "flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors",
            activeTab === "history"
              ? "border-emerald-600 text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground"
          )}
        >
          <History className="h-4 w-4" />
          Harvest History ({vaultHarvests.length})
        </button>
      </div>

      {activeTab === "history" ? (
        <div className="p-6">
          {historyQuery.isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : vaultHarvests.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-center">
              <Calendar className="h-8 w-8 text-muted-foreground/50" />
              <p className="mt-3 text-sm font-medium text-foreground">No past harvests found</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Harvests for this vault will appear here once executed.
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground mb-2">
                Past harvest records including net yield after gas deduction:
              </p>
              {vaultHarvests.map((harvest: any) => {
                const harvestAmount = Number.parseFloat(harvest.amount || "0");
                const gasCost = Number.parseFloat(harvest.gas_cost_usdc || harvest.estimated_gas_cost_usdc || "0");
                const netYield = harvest.net_yield_after_gas_usdc !== undefined
                  ? Number.parseFloat(harvest.net_yield_after_gas_usdc)
                  : Math.max(0, harvestAmount - gasCost);

                return (
                  <div
                    key={harvest.id}
                    className="flex flex-col gap-2 rounded-2xl border border-border bg-card p-4 transition-all hover:border-border/80"
                  >
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>{formatDate(harvest.harvested_at)}</span>
                      {harvest.tx_hash ? (
                        <a
                          href={getExplorerTxUrl(harvest.tx_hash)}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1 font-medium text-emerald-600 hover:underline"
                        >
                          Explorer <ExternalLink className="h-3 w-3" />
                        </a>
                      ) : (
                        <span>{harvest.protocol || "Vault Protocol"}</span>
                      )}
                    </div>
                    <div className="grid grid-cols-3 gap-2 pt-1 border-t border-border/50 text-xs">
                      <div>
                        <span className="block text-muted-foreground">Gross Yield</span>
                        <span className="font-mono font-medium text-foreground">
                          {formatUsdc(harvest.amount)}
                        </span>
                      </div>
                      <div>
                        <span className="block text-muted-foreground">Gas Cost</span>
                        <span className="font-mono font-medium text-amber-600">
                          -{formatUsdc(gasCost)}
                        </span>
                      </div>
                      <div className="text-right">
                        <span className="block text-muted-foreground">Net Yield</span>
                        <span className="font-mono font-semibold text-emerald-600">
                          {formatUsdc(netYield)}
                        </span>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        <div className="p-6 space-y-6">
          {executionState === "success" ? (
            <div className="flex flex-col items-center justify-center py-8 text-center">
              <div className="rounded-full bg-emerald-100 p-3 text-emerald-600 dark:bg-emerald-950/50">
                <CheckCircle2 className="h-8 w-8" />
              </div>
              <h3 className="mt-4 text-xl font-medium text-foreground">
                Harvest Successful!
              </h3>
              <p className="mt-1 text-sm text-muted-foreground">
                Yield has been successfully harvested{compound ? " and compounded" : " to your wallet"}.
              </p>
              {result?.tx_hash && (
                <a
                  href={getExplorerTxUrl(result.tx_hash)}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-4 inline-flex items-center gap-1.5 text-xs font-medium text-emerald-600 hover:underline"
                >
                  View transaction on explorer <ExternalLink className="h-3 w-3" />
                </a>
              )}
              <button
                type="button"
                onClick={resetAndClose}
                className="mt-6 w-full rounded-xl bg-foreground py-3 text-sm font-medium text-background transition-opacity hover:opacity-90"
              >
                Done
              </button>
            </div>
          ) : (
            <div className="space-y-6">
              {/* Compound Toggle */}
              <div className="flex items-center justify-between rounded-2xl border border-border bg-card p-4">
                <div className="flex items-center gap-3">
                  <div className="rounded-xl bg-emerald-500/10 p-2.5 text-emerald-600">
                    <Repeat2 className="h-5 w-5" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-foreground">Auto-compound yield</p>
                    <p className="text-xs text-muted-foreground">
                      Reinvest harvested yield directly back into the vault
                    </p>
                  </div>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={compound}
                  onClick={() => setCompound(!compound)}
                  className={cn(
                    "relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none",
                    compound ? "bg-emerald-600" : "bg-muted"
                  )}
                >
                  <span
                    className={cn(
                      "pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow-lg ring-0 transition duration-200 ease-in-out",
                      compound ? "translate-x-5" : "translate-x-0"
                    )}
                  />
                </button>
              </div>

              {/* Preview Details */}
              <div className="rounded-2xl border border-border bg-card p-5 space-y-4">
                <h3 className="text-xs font-mono uppercase tracking-wider text-muted-foreground">
                  Harvest Breakdown & Gas Evaluation
                Yesterday & Today
                </h3>

                {previewLoading ? (
                  <div className="flex items-center justify-center py-8">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                  </div>
                ) : previewError ? (
                  <div className="rounded-xl bg-destructive/10 p-4 text-sm text-destructive">
                    {previewError}
                  </div>
                ) : hasNoHarvestableYield ? (
                  <div className="rounded-xl bg-amber-500/10 p-4 text-sm text-amber-600 dark:text-amber-400 flex items-start gap-3">
                    <AlertCircle className="h-5 w-5 shrink-0 mt-0.5" />
                    <div>
                      <p className="font-medium">No harvestable yield available</p>
                      <p className="text-xs opacity-90 mt-0.5">
                        This vault currently has no accumulated yield to harvest.
                      </p>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <AmountRow
                      label="Gross Yield"
                      value={formatUsdc(preview?.gross_yield_usdc)}
                    />
                    <AmountRow
                      label="Protocol Fee"
                      value={formatUsdc(preview?.protocol_fee_usdc)}
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
                    {estimatedGasCost > Number.parseFloat(preview?.gross_yield_usdc || "0") && (
                      <div className="mt-3 flex items-start gap-2 rounded-xl bg-amber-500/10 p-3 text-xs text-amber-600 dark:text-amber-400">
                        <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                        <span>
                          Warning: Current gas costs exceed gross yield. Harvesting now will result in a net loss.
                        </span>
                      </div>
                    )}
                  </div>
                )}
              </div>

              {executeError && (
                <div className="rounded-xl bg-destructive/10 p-4 text-sm text-destructive">
                  {executeError}
                </div>
              )}

              {/* Actions */}
              <div className="flex gap-3 pt-2">
                <button
                  type="button"
                  onClick={resetAndClose}
                  disabled={isSubmitting}
                  className="flex-1 rounded-xl border border-border bg-card py-3 text-sm font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-50"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={handleConfirm}
                  disabled={!canContinue || isSubmitting || hasNoHarvestableYield}
                  className="flex-1 inline-flex items-center justify-center gap-2 rounded-xl bg-emerald-600 py-3 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {isSubmitting ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Harvesting...
                    </>
                  ) : (
                    "Confirm Harvest"
                  )}
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </ModalShell>
  );
}
