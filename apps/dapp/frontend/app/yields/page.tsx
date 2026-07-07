"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import Link from "next/link";
import {
  AlertTriangle,
  Bookmark,
  BookmarkCheck,
  Clock,
  RefreshCw,
  TrendingUp,
} from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { useYields } from "@/hooks/useYields";
import { YieldDepositDrawer } from "@/components/yields/YieldDepositDrawer";
import {
  addYieldBookmark,
  fetchYieldBookmarks,
  protocolSlugFromProject,
  removeYieldBookmark,
  type YieldPool,
} from "@/lib/api/yields";
import { getStoredToken } from "@/lib/api/client";
import { cn } from "@/lib/utils";

type RiskTier = "all" | "low" | "medium" | "high";

const RISK_TABS: { id: RiskTier; label: string }[] = [
  { id: "all", label: "All" },
  { id: "low", label: "Low" },
  { id: "medium", label: "Medium" },
  { id: "high", label: "High" },
];

function riskTier(score: number): Exclude<RiskTier, "all"> {
  if (score <= 25) return "low";
  if (score <= 55) return "medium";
  return "high";
}

function riskBadge(score: number): { label: string; className: string } {
  const tier = riskTier(score);
  if (tier === "low") return { label: "Low", className: "bg-emerald-50 text-emerald-700" };
  if (tier === "medium") return { label: "Medium", className: "bg-amber-50 text-amber-700" };
  return { label: "High", className: "bg-red-50 text-red-700" };
}

function fmtTVL(usd: number): string {
  if (usd >= 1_000_000_000) return `$${(usd / 1_000_000_000).toFixed(1)}B`;
  if (usd >= 1_000_000) return `$${(usd / 1_000_000).toFixed(1)}M`;
  if (usd >= 1_000) return `$${(usd / 1_000).toFixed(0)}K`;
  return `$${usd.toFixed(0)}`;
}

function CardSkeleton() {
  return (
    <div className="animate-pulse rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5">
      <div className="mb-4 flex justify-between">
        <div className="h-10 w-10 rounded-xl bg-black/8 dark:bg-white/8" />
        <div className="h-6 w-16 rounded-full bg-black/8 dark:bg-white/8" />
      </div>
      <div className="mb-2 h-4 w-32 rounded bg-black/8 dark:bg-white/8" />
      <div className="mb-4 h-3 w-24 rounded bg-black/6 dark:bg-white/6" />
      <div className="grid grid-cols-2 gap-3">
        <div className="h-12 rounded-xl bg-black/6 dark:bg-white/6" />
        <div className="h-12 rounded-xl bg-black/6 dark:bg-white/6" />
      </div>
    </div>
  );
}

function YieldCard({
  pool,
  bookmarked,
  onBookmark,
  onDeposit,
}: {
  pool: YieldPool;
  bookmarked: boolean;
  onBookmark: (pool: YieldPool) => void;
  onDeposit: (pool: YieldPool) => void;
}) {
  const risk = riskBadge(pool.riskScore);

  return (
    <motion.div
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      className="flex flex-col rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5 transition-all hover:border-black/18 dark:hover:border-white/18 hover:shadow-sm"
      data-testid="yield-card"
    >
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-black/[0.04] dark:bg-white/[0.04]">
            <span className="text-xs font-semibold uppercase text-black/60 dark:text-white/60">
              {pool.symbol.slice(0, 2)}
            </span>
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold capitalize text-black dark:text-white">{pool.project}</p>
            <p className="text-[11px] font-mono text-black/45 dark:text-white/45">{pool.symbol}</p>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className={cn("rounded-full px-2.5 py-1 text-[10px] font-bold uppercase", risk.className)}>
            {risk.label}
          </span>
          <button
            type="button"
            onClick={() => onBookmark(pool)}
            aria-label={bookmarked ? "Remove bookmark" : "Bookmark protocol"}
            className="flex h-8 w-8 items-center justify-center rounded-lg border border-black/8 dark:border-white/8 text-black/40 dark:text-white/40 hover:text-black dark:hover:text-white transition-colors"
          >
            {bookmarked ? (
              <BookmarkCheck className="h-4 w-4 text-black dark:text-white" />
            ) : (
              <Bookmark className="h-4 w-4" />
            )}
          </button>
        </div>
      </div>

      <div className="mb-4 grid grid-cols-2 gap-3">
        <div className="rounded-xl bg-black/[0.025] dark:bg-white/[0.025] px-3 py-3 text-center">
          <p className="font-mono text-lg text-black dark:text-white">{pool.apy.toFixed(2)}%</p>
          <p className="mt-0.5 text-[10px] font-medium uppercase text-black/45 dark:text-white/45">APY</p>
        </div>
        <div className="rounded-xl bg-black/[0.025] dark:bg-white/[0.025] px-3 py-3 text-center">
          <p className="font-mono text-lg text-black dark:text-white">{fmtTVL(pool.tvlUsd)}</p>
          <p className="mt-0.5 text-[10px] font-medium uppercase text-black/45 dark:text-white/45">TVL</p>
        </div>
      </div>

      <p className="mb-4 text-xs text-black/50 dark:text-white/50">
        Asset: <span className="font-mono text-black/70 dark:text-white/70">{pool.symbol}</span>
      </p>

      <button
        type="button"
        onClick={() => onDeposit(pool)}
        className="mt-auto w-full rounded-xl bg-black dark:bg-blue-600 py-2.5 text-xs font-semibold text-white transition-opacity hover:opacity-80"
      >
        Deposit
      </button>
    </motion.div>
  );
}

export default function YieldsPage() {
  const { isConnected } = useWallet();
  const { data, isLoading, isError, refetch, isFetching } = useYields();
  const [riskFilter, setRiskFilter] = useState<RiskTier>("all");
  const [bookmarks, setBookmarks] = useState<Set<string>>(new Set());
  const [depositPool, setDepositPool] = useState<YieldPool | null>(null);

  useEffect(() => {
    if (!isConnected || !getStoredToken()) {
      setBookmarks(new Set());
      return;
    }
    fetchYieldBookmarks()
      .then((items) => setBookmarks(new Set(items.map((b) => b.protocol_slug))))
      .catch(() => setBookmarks(new Set()));
  }, [isConnected]);

  const pools = data?.data ?? [];
  const isStale = data?.meta?.stale ?? false;

  const filtered = useMemo(() => {
    if (riskFilter === "all") return pools;
    return pools.filter((p) => riskTier(p.riskScore) === riskFilter);
  }, [pools, riskFilter]);

  const handleBookmark = useCallback(
    async (pool: YieldPool) => {
      if (!isConnected || !getStoredToken()) return;
      const slug = protocolSlugFromProject(pool.project);
      const isSaved = bookmarks.has(slug);
      try {
        if (isSaved) {
          await removeYieldBookmark(slug);
          setBookmarks((prev) => {
            const next = new Set(prev);
            next.delete(slug);
            return next;
          });
        } else {
          await addYieldBookmark(slug);
          setBookmarks((prev) => new Set(prev).add(slug));
        }
      } catch {
        // silently ignore bookmark errors
      }
    },
    [bookmarks, isConnected]
  );

  return (
    <AppShell>
      <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8">
        <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold text-black dark:text-white tracking-tight">Yield Opportunities</h1>
            <p className="mt-1 text-sm text-black/55 dark:text-white/55">
              Browse DeFi protocols on Stellar and deposit into your savings goals.
            </p>
          </div>
          <div className="flex items-center gap-3">
            {isConnected && getStoredToken() && (
              <Link href="/yields/history">
                <button className="text-xs font-semibold text-black dark:text-white hover:underline transition-colors">
                  Harvest History
                </button>
              </Link>
            )}
            {isStale && (
              <div
                className="flex items-center gap-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-800"
                data-testid="yield-stale-indicator"
              >
                <Clock className="h-3.5 w-3.5 shrink-0" />
                Cached data — live rates may differ
              </div>
            )}
          </div>
        </div>

        <div className="mb-6 flex flex-wrap items-center gap-2">
          {RISK_TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setRiskFilter(tab.id)}
              className={cn(
                "rounded-full px-4 py-2 text-xs font-semibold transition-colors",
                riskFilter === tab.id
                  ? "bg-black dark:bg-blue-600 text-white"
                  : "border border-black/10 dark:border-white/10 text-black/55 dark:text-white/55 hover:text-black dark:hover:text-white"
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {isLoading ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <CardSkeleton key={i} />
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center gap-3 rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] py-16 text-center">
            <AlertTriangle className="h-6 w-6 text-black/25 dark:text-white/25" />
            <p className="text-sm text-black/50 dark:text-white/50">Yield data unavailable right now.</p>
            <button
              type="button"
              onClick={() => refetch()}
              disabled={isFetching}
              className="flex items-center gap-1.5 rounded-lg border border-black/10 dark:border-white/10 px-3 py-1.5 text-xs text-black/50 dark:text-white/50 hover:text-black dark:hover:text-white"
            >
              <RefreshCw className={cn("h-3 w-3", isFetching && "animate-spin")} />
              Retry
            </button>
          </div>
        ) : filtered.length === 0 ? (
          <div
            className="flex flex-col items-center justify-center rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] py-16 text-center px-6"
            data-testid="yields-empty-state"
          >
            <TrendingUp className="mb-3 h-8 w-8 text-black/25 dark:text-white/25" />
            <p className="text-sm font-medium text-black dark:text-white">No protocols available</p>
            <p className="mt-1 max-w-sm text-xs text-black/50 dark:text-white/50">
              {pools.length === 0
                ? "Protocols below the minimum TVL threshold are hidden. Check back later as liquidity grows."
                : "No protocols match the selected risk tier. Try a different filter."}
            </p>
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {filtered.map((pool) => (
              <YieldCard
                key={pool.pool}
                pool={pool}
                bookmarked={bookmarks.has(protocolSlugFromProject(pool.project))}
                onBookmark={handleBookmark}
                onDeposit={setDepositPool}
              />
            ))}
          </div>
        )}
      </div>

      <YieldDepositDrawer pool={depositPool} onClose={() => setDepositPool(null)} />
    </AppShell>
  );
}
