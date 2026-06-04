"use client";

import { useCallback, useEffect, useState } from "react";
import { ArrowRightLeft, X, Loader2 } from "lucide-react";
import { vaultsApi, type RebalanceSuggestion } from "@/lib/api/vaults";
import { cn } from "@/lib/utils";

const DISMISS_KEY = "nester_rebalance_dismissed";

function dismissKey(vaultId: string) {
  return `${DISMISS_KEY}:${vaultId}`;
}

interface Props {
  vaultId: string;
  vaultName: string;
}

export function RebalanceSuggestionCard({ vaultId, vaultName }: Props) {
  const [suggestion, setSuggestion] = useState<RebalanceSuggestion | null>(null);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [applying, setApplying] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  const load = useCallback(async () => {
    if (typeof window !== "undefined" && localStorage.getItem(dismissKey(vaultId))) {
      setDismissed(true);
      setLoading(false);
      return;
    }
    try {
      const data = await vaultsApi.getRebalanceSuggestion(vaultId);
      setSuggestion(data);
    } catch {
      setSuggestion(null);
    } finally {
      setLoading(false);
    }
  }, [vaultId]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleDismiss = () => {
    localStorage.setItem(dismissKey(vaultId), "1");
    setDismissed(true);
    setModalOpen(false);
  };

  const handleApply = async () => {
    if (!suggestion) return;
    setApplying(true);
    try {
      await vaultsApi.applyRebalance(vaultId, suggestion.recommended_allocations);
      handleDismiss();
    } finally {
      setApplying(false);
    }
  };

  if (loading || dismissed || !suggestion?.has_suggestion) return null;

  return (
    <>
      <div className="rounded-2xl border border-violet-200 bg-violet-50/80 p-5">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2 text-violet-800">
            <ArrowRightLeft className="h-4 w-4" />
            <h3 className="text-sm font-semibold">Rebalance suggestion</h3>
          </div>
          <button type="button" onClick={handleDismiss} className="text-violet-600/70 hover:text-violet-900">
            <X className="h-4 w-4" />
          </button>
        </div>
        <p className="mt-2 text-sm text-violet-900/80">
          {vaultName}: expected APY gain ~{suggestion.expected_apy_gain_pct.toFixed(2)}% (
          {suggestion.confidence} confidence)
        </p>
        <div className="mt-3 grid grid-cols-2 gap-3 text-xs">
          <div>
            <p className="font-medium text-violet-800">Current</p>
            {suggestion.current_allocations.map((a) => (
              <p key={a.protocol} className="text-violet-900/75">
                {a.protocol}: {a.percentage.toFixed(0)}%
              </p>
            ))}
          </div>
          <div>
            <p className="font-medium text-violet-800">Recommended</p>
            {suggestion.recommended_allocations.map((a) => (
              <p key={a.protocol} className="text-violet-900/75">
                {a.protocol}: {a.percentage.toFixed(0)}%
              </p>
            ))}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setModalOpen(true)}
          className="mt-4 rounded-full bg-violet-700 px-4 py-2 text-xs font-medium text-white hover:bg-violet-800"
        >
          Review & Confirm
        </button>
      </div>

      {modalOpen && (
        <div className="fixed inset-0 z-[110] flex items-center justify-center bg-black/45 px-4">
          <div className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl">
            <h3 className="font-heading text-xl font-light">Confirm rebalance</h3>
            <p className="mt-2 text-sm text-muted-foreground">{vaultName}</p>
            <div className="mt-4 grid grid-cols-2 gap-4 text-sm">
              <div className="rounded-xl border border-border p-3">
                <p className="text-xs font-medium uppercase text-muted-foreground">Before</p>
                {suggestion.current_allocations.map((a) => (
                  <p key={a.protocol} className="mt-1">
                    {a.protocol}: {a.percentage.toFixed(1)}%
                  </p>
                ))}
              </div>
              <div className="rounded-xl border border-emerald-200 bg-emerald-50/50 p-3">
                <p className="text-xs font-medium uppercase text-emerald-800">After</p>
                {suggestion.recommended_allocations.map((a) => (
                  <p key={a.protocol} className="mt-1 text-emerald-900">
                    {a.protocol}: {a.percentage.toFixed(1)}%
                  </p>
                ))}
              </div>
            </div>
            <div className="mt-6 flex gap-2">
              <button
                type="button"
                onClick={handleDismiss}
                className="flex-1 rounded-full border border-border py-2.5 text-sm"
              >
                Dismiss
              </button>
              <button
                type="button"
                disabled={applying}
                onClick={() => void handleApply()}
                className={cn(
                  "flex-1 rounded-full bg-foreground py-2.5 text-sm font-medium text-background",
                  applying && "opacity-60"
                )}
              >
                {applying ? <Loader2 className="mx-auto h-4 w-4 animate-spin" /> : "Apply Rebalance"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
