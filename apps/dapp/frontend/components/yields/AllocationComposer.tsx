"use client";

import { useMemo, useState } from "react";
import { ChevronDown, Check, AlertCircle } from "lucide-react";
import { AllocationBuilder } from "@/components/vault/AllocationBuilder";
import { getVaultContractByAsset } from "@/lib/vault-contracts";
import type { YieldPool } from "@/lib/api/yield-opportunities";
import type { ProtocolAllocation } from "@/lib/types/vault-wizard";
import {
  groupPoolsByProtocol,
  resolveSelectedPools,
  evenWeights,
  poolsAsProtocolOptions,
  blendedApy,
  groupAllocationsByAsset,
  type AllocationSelection,
  type ProtocolSpread,
} from "@/lib/yields/allocation-options";

function formatTvl(usd: number): string {
  if (usd >= 1_000_000_000) return `$${(usd / 1_000_000_000).toFixed(1)}B`;
  if (usd >= 1_000_000) return `$${(usd / 1_000_000).toFixed(1)}M`;
  if (usd >= 1_000) return `$${(usd / 1_000).toFixed(0)}K`;
  return `$${usd.toFixed(0)}`;
}

interface Props {
  pools: YieldPool[];
  /** Called with the finished split, grouped by the asset each leg settles in. */
  onDeposit: (byAsset: Record<string, ProtocolAllocation[]>, pools: YieldPool[]) => void;
}

export function AllocationComposer({ pools, onDeposit }: Props) {
  const groups = useMemo(() => groupPoolsByProtocol(pools), [pools]);
  const [selection, setSelection] = useState<AllocationSelection>({ protocols: {}, pools: [] });
  const [expanded, setExpanded] = useState<string | null>(null);
  const [weights, setWeights] = useState<ProtocolAllocation[]>([]);

  const selectedPools = useMemo(
    () => resolveSelectedPools(groups, selection),
    [groups, selection],
  );
  const options = useMemo(() => poolsAsProtocolOptions(selectedPools), [selectedPools]);

  // Re-even the split whenever the set of selected pools changes, so the
  // weights always match what is selected and always total 100.
  const syncedWeights = useMemo(() => {
    const ids = selectedPools.map((p) => p.pool);
    const sameSet =
      weights.length === ids.length && weights.every((w) => ids.includes(w.protocolId));
    return sameSet ? weights : evenWeights(ids);
  }, [selectedPools, weights]);

  const total = syncedWeights.reduce((sum, w) => sum + w.percentage, 0);
  const apy = blendedApy(syncedWeights, options);
  const byAsset = useMemo(
    () => groupAllocationsByAsset(syncedWeights, selectedPools),
    [syncedWeights, selectedPools],
  );

  // An asset with no deployed vault cannot be deposited into. Naming them is
  // better than a disabled button with no reason on it.
  const unsupported = Object.keys(byAsset).filter((a) => !getVaultContractByAsset(a));
  const assetCount = Object.keys(byAsset).length;

  const toggleProtocol = (id: string, spread: ProtocolSpread) => {
    setSelection((prev) => {
      const next = { ...prev.protocols };
      if (next[id] === spread) delete next[id];
      else next[id] = spread;
      return { ...prev, protocols: next };
    });
  };

  const togglePool = (poolId: string) => {
    setSelection((prev) => ({
      ...prev,
      pools: prev.pools.includes(poolId)
        ? prev.pools.filter((p) => p !== poolId)
        : [...prev.pools, poolId],
    }));
  };

  return (
    <div className="space-y-6">
      <section>
        <h2 className="text-sm font-semibold text-black dark:text-white">1. Choose protocols</h2>
        <p className="mt-1 text-xs text-black/50 dark:text-white/50">
          Pick the best pool from each, or spread across all of its pools. Expand one to choose
          individual pools instead.
        </p>

        <div className="mt-4 space-y-2">
          {groups.map((group) => {
            const spread = selection.protocols[group.id];
            const isOpen = expanded === group.id;
            const explicitCount = group.pools.filter((p) => selection.pools.includes(p.pool)).length;

            return (
              <div
                key={group.id}
                className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F]"
              >
                <div className="flex flex-wrap items-center gap-3 p-4">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-black dark:text-white">{group.name}</p>
                    <p className="mt-0.5 text-xs text-black/50 dark:text-white/50">
                      {group.pools.length} pool{group.pools.length === 1 ? "" : "s"} ·{" "}
                      {group.lowestApy.toFixed(2)}–{group.bestApy.toFixed(2)}% APY ·{" "}
                      {formatTvl(group.totalTvlUsd)} TVL
                    </p>
                  </div>

                  <div className="flex items-center gap-2">
                    {(["best", "even"] as ProtocolSpread[]).map((mode) => (
                      <button
                        key={mode}
                        type="button"
                        onClick={() => toggleProtocol(group.id, mode)}
                        aria-pressed={spread === mode}
                        className={
                          spread === mode
                            ? "rounded-lg bg-black dark:bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white"
                            : "rounded-lg border border-black/10 dark:border-white/10 px-3 py-1.5 text-xs text-black/60 dark:text-white/60 hover:text-black dark:hover:text-white"
                        }
                      >
                        {mode === "best" ? "Best pool" : "Spread evenly"}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => setExpanded(isOpen ? null : group.id)}
                      aria-expanded={isOpen}
                      aria-label={`${isOpen ? "Hide" : "Show"} ${group.name} pools`}
                      className="rounded-lg border border-black/10 dark:border-white/10 p-1.5 text-black/50 dark:text-white/50"
                    >
                      <ChevronDown
                        className={`h-4 w-4 transition-transform ${isOpen ? "rotate-180" : ""}`}
                      />
                    </button>
                  </div>
                </div>

                {isOpen && (
                  <div className="border-t border-black/6 dark:border-white/6 p-3">
                    {explicitCount > 0 && (
                      <p className="mb-2 px-1 text-[11px] text-black/45 dark:text-white/45">
                        Chosen pools override this protocol&apos;s spread.
                      </p>
                    )}
                    <div className="space-y-1.5">
                      {group.pools.map((p) => {
                        const picked = selection.pools.includes(p.pool);
                        return (
                          <button
                            key={p.pool}
                            type="button"
                            onClick={() => togglePool(p.pool)}
                            aria-pressed={picked}
                            className={`flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-left transition-colors ${
                              picked
                                ? "bg-black/[0.04] dark:bg-white/[0.06]"
                                : "hover:bg-black/[0.02] dark:hover:bg-white/[0.03]"
                            }`}
                          >
                            <span className="flex items-center gap-2 text-sm text-black dark:text-white">
                              <span
                                className={`flex h-4 w-4 items-center justify-center rounded border ${
                                  picked
                                    ? "border-transparent bg-black dark:bg-blue-600"
                                    : "border-black/20 dark:border-white/20"
                                }`}
                              >
                                {picked && <Check className="h-3 w-3 text-white" />}
                              </span>
                              {p.symbol}
                            </span>
                            <span className="text-xs text-black/50 dark:text-white/50">
                              {(p.apy ?? 0).toFixed(2)}% · {formatTvl(p.tvlUsd ?? 0)}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </section>

      {selectedPools.length > 0 && (
        <section>
          <h2 className="text-sm font-semibold text-black dark:text-white">2. Set the split</h2>
          <div className="mt-3">
            <AllocationBuilder
              protocols={options}
              allocations={syncedWeights}
              onChange={setWeights}
            />
          </div>
        </section>
      )}

      {selectedPools.length > 0 && (
        <section className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <span className="text-xs uppercase tracking-wider text-black/45 dark:text-white/45">
              Blended APY
            </span>
            <span className="text-2xl font-light text-black dark:text-white">
              {apy.toFixed(2)}%
            </span>
          </div>

          {assetCount > 1 && (
            <p className="mt-3 flex items-start gap-2 text-xs text-black/55 dark:text-white/55">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              This split spans {assetCount} assets. A vault holds one currency, so it settles as one
              deposit per asset — {Object.keys(byAsset).join(", ")}.
            </p>
          )}

          {unsupported.length > 0 && (
            <p className="mt-3 flex items-start gap-2 text-xs text-amber-600 dark:text-amber-500">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              No vault is deployed for {unsupported.join(", ")}, so those legs cannot be funded yet.
            </p>
          )}

          <button
            type="button"
            disabled={total !== 100 || unsupported.length > 0}
            onClick={() => onDeposit(byAsset, selectedPools)}
            className="mt-4 w-full rounded-xl bg-black dark:bg-blue-600 py-3 text-sm font-semibold text-white disabled:opacity-40"
          >
            {total !== 100 ? `Allocation totals ${total}% — must be 100%` : "Continue to deposit"}
          </button>
        </section>
      )}
    </div>
  );
}
