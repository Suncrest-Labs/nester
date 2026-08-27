"use client";

import {
  Skeleton,
  SkeletonLine,
  SkeletonCard,
  SkeletonChart,
  LoadingRegion,
} from "@/components/ui/skeleton/skeleton";

/**
 * Skeletons for the async views that previously rendered either nothing, a bare
 * "Loading..." string, or an empty state while the request was still in flight.
 * Each one mirrors the real layout so content does not jump when it lands.
 */

/** Portfolio positions tab — one row per position. */
export function PositionsSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <LoadingRegion label="Loading your positions" className="space-y-2">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center justify-between rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] px-5 py-4"
        >
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-10 rounded-full" />
            <div className="space-y-2">
              <SkeletonLine width="120px" height="0.95rem" />
              <SkeletonLine width="80px" height="0.75rem" />
            </div>
          </div>
          <div className="space-y-2 text-right">
            <SkeletonLine width="90px" height="0.95rem" className="ml-auto" />
            <SkeletonLine width="60px" height="0.75rem" className="ml-auto" />
          </div>
        </div>
      ))}
    </LoadingRegion>
  );
}

/** Portfolio activity tab — compact transaction rows. */
export function ActivitySkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <LoadingRegion label="Loading your activity" className="space-y-1.5">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center justify-between rounded-xl bg-black/[0.015] dark:bg-white/[0.015] px-5 py-3.5"
        >
          <div className="flex items-center gap-3">
            <Skeleton className="h-8 w-8 rounded-lg" />
            <div className="space-y-1.5">
              <SkeletonLine width="110px" height="0.85rem" />
              <SkeletonLine width="150px" height="0.7rem" />
            </div>
          </div>
          <div className="space-y-1.5 text-right">
            <SkeletonLine width="80px" height="0.85rem" className="ml-auto" />
            <SkeletonLine width="55px" height="0.7rem" className="ml-auto" />
          </div>
        </div>
      ))}
    </LoadingRegion>
  );
}

/** Portfolio route shell — net-worth card, tabs, then the positions list. */
export function PortfolioSkeleton() {
  return (
    <LoadingRegion label="Loading your portfolio" className="space-y-6">
      <div className="flex items-center justify-between">
        <SkeletonLine width="160px" height="2rem" />
        <SkeletonLine width="120px" height="2.25rem" />
      </div>

      <div className="rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] overflow-hidden">
        <div className="p-8 space-y-3">
          <SkeletonLine width="90px" height="0.75rem" />
          <SkeletonLine width="240px" height="2.6rem" />
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-4 divide-x divide-black/[0.06] dark:divide-white/[0.06] border-t border-black/[0.06] dark:border-white/[0.06]">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="px-5 py-4 space-y-2">
              <SkeletonLine width="70px" height="0.7rem" />
              <SkeletonLine width="90px" height="0.9rem" />
            </div>
          ))}
        </div>
      </div>

      <div className="flex gap-6">
        {Array.from({ length: 3 }).map((_, i) => (
          <SkeletonLine key={i} width="80px" height="1rem" />
        ))}
      </div>

      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <SkeletonCard key={i} height="5.5rem" />
        ))}
      </div>
    </LoadingRegion>
  );
}

/** Analytics dashboard — metric cards above charts. */
export function AnalyticsSkeleton() {
  return (
    <LoadingRegion label="Loading analytics" className="space-y-8 p-6">
      <div className="flex flex-wrap gap-4">
        {Array.from({ length: 3 }).map((_, i) => (
          <SkeletonLine key={i} width="56px" height="1.75rem" />
        ))}
      </div>

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <SkeletonCard key={i} height="7rem" className="p-6">
            <div className="space-y-3">
              <SkeletonLine width="90px" height="0.75rem" />
              <SkeletonLine width="120px" height="1.5rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>

      <SkeletonCard height="20rem" className="p-6">
        <div className="space-y-4">
          <SkeletonLine width="180px" height="1.1rem" />
          <SkeletonChart height="15rem" />
        </div>
      </SkeletonCard>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {Array.from({ length: 2 }).map((_, i) => (
          <SkeletonCard key={i} height="16rem" className="p-6">
            <div className="space-y-4">
              <SkeletonLine width="150px" height="1.1rem" />
              <SkeletonChart height="11rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>
    </LoadingRegion>
  );
}

/** Savings goals grid. */
export function SavingsSkeleton({ cards = 3 }: { cards?: number }) {
  return (
    <LoadingRegion label="Loading your savings goals" className="space-y-6">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: cards }).map((_, i) => (
          <SkeletonCard key={i} height="11rem" className="p-6">
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <Skeleton className="h-9 w-9 rounded-xl" />
                <div className="flex-1 space-y-2">
                  <SkeletonLine width="70%" height="0.95rem" />
                  <SkeletonLine width="45%" height="0.7rem" />
                </div>
              </div>
              <SkeletonLine width="100%" height="0.5rem" />
              <div className="flex justify-between">
                <SkeletonLine width="70px" height="0.75rem" />
                <SkeletonLine width="60px" height="0.75rem" />
              </div>
            </div>
          </SkeletonCard>
        ))}
      </div>
    </LoadingRegion>
  );
}

/** Savings goal detail — summary card plus the contributions list. */
export function SavingsDetailSkeleton() {
  return (
    <LoadingRegion label="Loading this savings goal" className="space-y-6">
      <div className="space-y-3">
        <SkeletonLine width="200px" height="1.75rem" />
        <SkeletonLine width="140px" height="0.85rem" />
      </div>
      <SkeletonCard height="10rem" className="p-8">
        <div className="space-y-4">
          <SkeletonLine width="180px" height="2.2rem" />
          <SkeletonLine width="100%" height="0.5rem" />
          <div className="flex justify-between">
            <SkeletonLine width="90px" height="0.75rem" />
            <SkeletonLine width="90px" height="0.75rem" />
          </div>
        </div>
      </SkeletonCard>
      <div className="space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <SkeletonCard key={i} height="4rem" />
        ))}
      </div>
    </LoadingRegion>
  );
}

/** Market (vault) detail — header, stats strip, chart. */
export function VaultDetailSkeleton() {
  return (
    <LoadingRegion label="Loading this market" className="space-y-6">
      <div className="flex items-center gap-4">
        <Skeleton className="h-12 w-12 rounded-2xl" />
        <div className="space-y-2">
          <SkeletonLine width="180px" height="1.6rem" />
          <SkeletonLine width="120px" height="0.8rem" />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <SkeletonCard key={i} height="6rem" className="p-5">
            <div className="space-y-2">
              <SkeletonLine width="70px" height="0.7rem" />
              <SkeletonLine width="90px" height="1.3rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>
      <SkeletonCard height="18rem" className="p-6">
        <SkeletonChart height="14rem" />
      </SkeletonCard>
    </LoadingRegion>
  );
}

/** Harvest history table. */
export function HarvestHistorySkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <LoadingRegion label="Loading harvest history" className="space-y-2">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center justify-between rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] px-5 py-4"
        >
          <div className="space-y-2">
            <SkeletonLine width="140px" height="0.9rem" />
            <SkeletonLine width="100px" height="0.7rem" />
          </div>
          <SkeletonLine width="80px" height="0.9rem" />
        </div>
      ))}
    </LoadingRegion>
  );
}

/** Offramp settlement history. */
export function OfframpSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <LoadingRegion label="Loading your offramps" className="space-y-2">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center justify-between rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] px-5 py-4"
        >
          <div className="flex items-center gap-3">
            <Skeleton className="h-9 w-9 rounded-full" />
            <div className="space-y-2">
              <SkeletonLine width="130px" height="0.9rem" />
              <SkeletonLine width="90px" height="0.7rem" />
            </div>
          </div>
          <SkeletonLine width="70px" height="0.9rem" />
        </div>
      ))}
    </LoadingRegion>
  );
}
