"use client";

import { Skeleton, SkeletonLine, SkeletonCard, SkeletonTable, SkeletonChart } from "@/components/ui/skeleton/skeleton";

export function DashboardSkeleton() {
  return (
    <div className="space-y-8">
      {/* Greeting header */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="200px" height="2rem" />
        <div className="flex gap-2.5">
          <SkeletonLine width="80px" height="2.5rem" />
          <SkeletonLine width="80px" height="2.5rem" />
        </div>
      </div>

      {/* Balance + Chart card */}
      <div className="rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] overflow-hidden">
        <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] gap-0">
          {/* Left side - balance */}
          <div className="p-8 lg:p-10 flex flex-col justify-between">
            <div className="space-y-3">
              <SkeletonLine width="250px" height="2.5rem" />
              <SkeletonLine width="120px" height="0.75rem" />
              <SkeletonLine width="150px" height="0.75rem" />
            </div>
            <div className="space-y-4 mt-8">
              <div className="flex justify-between">
                <SkeletonLine width="80px" height="0.875rem" />
                <SkeletonLine width="60px" height="0.875rem" />
              </div>
              <div className="flex justify-between">
                <SkeletonLine width="90px" height="0.875rem" />
                <SkeletonLine width="70px" height="0.875rem" />
              </div>
            </div>
          </div>

          {/* Right side - chart */}
          <div className="border-t lg:border-t-0 lg:border-l border-black/[0.06] dark:border-white/[0.06] p-8 lg:p-10 flex flex-col">
            <div className="flex gap-1 mb-6 justify-end">
              {Array.from({ length: 6 }).map((_, i) => (
                <SkeletonLine key={i} width="2rem" height="1.5rem" />
              ))}
            </div>
            <SkeletonChart height="160px" />
            <div className="flex items-center gap-2 mt-4">
              <Skeleton className="h-2 w-2 rounded-full" />
              <SkeletonLine width="50px" height="0.75rem" />
            </div>
          </div>
        </div>
      </div>

      {/* Rebalance suggestions */}
      <div className="space-y-3">
        {Array.from({ length: 2 }).map((_, i) => (
          <SkeletonCard key={`rebalance-${i}`} height="6rem" />
        ))}
      </div>

      {/* Positions table */}
      <SkeletonCard className="p-8">
        <div className="flex items-center justify-between mb-6">
          <SkeletonLine width="80px" height="1rem" />
          <SkeletonLine width="100px" height="0.75rem" />
        </div>
        <SkeletonTable rows={3} columns={6} />
      </SkeletonCard>

      {/* Wallet balance table */}
      <SkeletonCard className="p-8">
        <SkeletonLine width="120px" height="1rem" className="mb-6" />
        <SkeletonTable rows={2} columns={4} />
      </SkeletonCard>

      {/* Recent activity */}
      <SkeletonCard className="p-8">
        <SkeletonLine width="120px" height="1rem" className="mb-6" />
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-xl bg-black/[0.015] dark:bg-white/[0.015]">
              <div className="flex items-center gap-3">
                <Skeleton className="h-8 w-8 rounded-lg" />
                <div className="space-y-2">
                  <SkeletonLine width="100px" height="0.875rem" />
                  <SkeletonLine width="150px" height="0.75rem" />
                </div>
              </div>
              <div className="flex items-center gap-4">
                <div className="text-right space-y-1">
                  <SkeletonLine width="80px" height="0.875rem" />
                  <SkeletonLine width="60px" height="0.75rem" />
                </div>
                <Skeleton className="h-7 w-7 rounded-md" />
              </div>
            </div>
          ))}
        </div>
      </SkeletonCard>
    </div>
  );
}

export function VaultsSkeleton() {
  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="150px" height="2rem" />
        <SkeletonLine width="100px" height="2.5rem" />
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {Array.from({ length: 3 }).map((_, i) => (
          <SkeletonCard key={i} height="8rem" className="p-6">
            <div className="space-y-3">
              <SkeletonLine width="100px" height="0.75rem" />
              <SkeletonLine width="120px" height="1.5rem" />
              <SkeletonLine width="80px" height="0.75rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>

      {/* Vault grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {Array.from({ length: 6 }).map((_, i) => (
          <SkeletonCard key={i} height="12rem" className="p-6">
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <Skeleton className="h-10 w-10 rounded-lg" />
                <div className="space-y-2 flex-1">
                  <SkeletonLine width="80%" height="1rem" />
                  <SkeletonLine width="60%" height="0.75rem" />
                </div>
              </div>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <SkeletonLine width="40px" height="0.75rem" />
                  <SkeletonLine width="60px" height="0.75rem" />
                </div>
                <div className="flex justify-between">
                  <SkeletonLine width="50px" height="0.75rem" />
                  <SkeletonLine width="70px" height="0.75rem" />
                </div>
              </div>
              <SkeletonLine width="100%" height="2rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>
    </div>
  );
}

export function HistorySkeleton() {
  return (
    <div className="space-y-8">
      {/* Header with filters */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="100px" height="2rem" />
        <div className="flex gap-3">
          <SkeletonLine width="120px" height="2.5rem" />
          <SkeletonLine width="100px" height="2.5rem" />
        </div>
      </div>

      {/* Timeline skeleton */}
      <div className="space-y-6">
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="relative">
            {/* Timeline line */}
            {i !== 7 && (
              <div className="absolute left-6 top-12 bottom-0 w-px bg-black/[0.06] dark:bg-white/[0.06]" />
            )}
            
            <div className="flex gap-4">
              <div className="flex flex-col items-center">
                <Skeleton className="h-12 w-12 rounded-full" />
              </div>
              
              <div className="flex-1 rounded-xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-6">
                <div className="flex items-start justify-between mb-4">
                  <div className="space-y-2">
                    <SkeletonLine width="150px" height="1rem" />
                    <SkeletonLine width="200px" height="0.75rem" />
                  </div>
                  <SkeletonLine width="80px" height="0.75rem" />
                </div>
                
                <div className="flex justify-between items-center">
                  <SkeletonLine width="100px" height="1.25rem" />
                  <SkeletonLine width="60px" height="0.75rem" />
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function SettlementsSkeleton() {
  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="150px" height="2rem" />
        <SkeletonLine width="120px" height="2.5rem" />
      </div>

      {/* Settlements table */}
      <SkeletonCard className="p-8">
        <SkeletonTable rows={5} columns={5} />
      </SkeletonCard>
    </div>
  );
}

export function NotificationsSkeleton() {
  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="150px" height="2rem" />
        <SkeletonLine width="100px" height="0.875rem" />
      </div>

      {/* Notification cards */}
      <div className="space-y-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <SkeletonCard key={i} height="5rem" className="p-6">
            <div className="flex items-start gap-4">
              <Skeleton className="h-10 w-10 rounded-full" />
              <div className="flex-1 space-y-2">
                <SkeletonLine width="70%" height="1rem" />
                <SkeletonLine width="90%" height="0.75rem" />
                <SkeletonLine width="100px" height="0.75rem" />
              </div>
              <Skeleton className="h-2 w-2 rounded-full" />
            </div>
          </SkeletonCard>
        ))}
      </div>
    </div>
  );
}

export function SettingsSkeleton() {
  return (
    <div className="space-y-8">
      {/* Header */}
      <SkeletonLine width="150px" height="2rem" />

      {/* Settings sections */}
      <div className="space-y-8">
        {Array.from({ length: 4 }).map((_, sectionIndex) => (
          <SkeletonCard key={sectionIndex} className="p-6">
            <SkeletonLine width="150px" height="1.25rem" className="mb-6" />
            
            <div className="space-y-4">
              {Array.from({ length: 3 }).map((_, itemIndex) => (
                <div key={itemIndex} className="flex items-center justify-between py-3 border-b border-black/[0.04] dark:border-white/[0.04] last:border-0">
                  <div className="space-y-1">
                    <SkeletonLine width="120px" height="0.875rem" />
                    <SkeletonLine width="200px" height="0.75rem" />
                  </div>
                  <SkeletonLine width="60px" height="2rem" />
                </div>
              ))}
            </div>
          </SkeletonCard>
        ))}
      </div>
    </div>
  );
}