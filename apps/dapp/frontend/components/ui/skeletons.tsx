"use client";

import { cn } from "@/lib/utils";

/** Animated shimmer placeholder for a single line. */
export function SkeletonLine({
  className,
}: {
  className?: string;
}) {
  return (
    <div
      className={cn(
        "animate-pulse rounded-md bg-black/[0.06]",
        className
      )}
    />
  );
}

/** Stat card skeleton — mirrors the balance / APY / yield cards. */
export function SkeletonStatCard() {
  return (
    <div className="rounded-2xl border border-black/[0.06] bg-white p-6 space-y-3">
      <SkeletonLine className="h-8 w-32" />
      <SkeletonLine className="h-3 w-20" />
    </div>
  );
}

/** Table row skeleton for the positions table. */
export function SkeletonTableRow({ cols = 5 }: { cols?: number }) {
  return (
    <tr className="border-b border-black/[0.04]">
      {Array.from({ length: cols }).map((_, i) => (
        <td key={i} className="py-4 pr-6">
          <SkeletonLine className="h-4 w-full max-w-[120px]" />
        </td>
      ))}
    </tr>
  );
}

/** Full positions-table skeleton. */
export function SkeletonPositionsTable({ rows = 3 }: { rows?: number }) {
  return (
    <table className="w-full text-left">
      <thead>
        <tr className="border-b border-black/[0.05]">
          {["Vault", "Balance", "APY", "Yield", "Status", ""].map((h) => (
            <th key={h} className="pb-3.5 pr-6">
              <SkeletonLine className="h-3 w-12" />
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {Array.from({ length: rows }).map((_, i) => (
          <SkeletonTableRow key={i} cols={6} />
        ))}
      </tbody>
    </table>
  );
}

/** Activity feed item skeleton. */
export function SkeletonActivityItem() {
  return (
    <div className="flex items-center justify-between rounded-xl bg-black/[0.015] px-5 py-3.5">
      <div className="flex items-center gap-3">
        <SkeletonLine className="h-8 w-8 rounded-lg" />
        <div className="space-y-1.5">
          <SkeletonLine className="h-3.5 w-24" />
          <SkeletonLine className="h-3 w-36" />
        </div>
      </div>
      <div className="space-y-1.5 text-right">
        <SkeletonLine className="h-3.5 w-20 ml-auto" />
        <SkeletonLine className="h-3 w-14 ml-auto" />
      </div>
    </div>
  );
}
