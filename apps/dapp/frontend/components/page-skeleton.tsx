"use client";

import { Skeleton, SkeletonLine, SkeletonCard, SkeletonTable } from "@/components/ui/skeleton/skeleton";

/**
 * Generic page skeleton shown while a route's async data loads.
 *
 * Matches the shared layout used by most DApp pages: a content header,
 * a stat-card grid, and a table.  Content does not shift when the real
 * data arrives because the skeleton mirrors the final layout.
 */
export default function PageSkeleton() {
  return (
    <div className="mx-auto max-w-7xl px-4 sm:px-6 pt-8 pb-16 space-y-8">
      {/* Greeting header */}
      <div className="flex items-center justify-between">
        <SkeletonLine width="200px" height="1.75rem" />
        <div className="flex gap-2.5">
          <SkeletonLine width="96px" height="2.25rem" />
          <SkeletonLine width="96px" height="2.25rem" />
        </div>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <SkeletonCard key={i} className="p-5">
            <div className="space-y-3">
              <SkeletonLine width="60%" height="0.75rem" />
              <SkeletonLine width="80%" height="1.5rem" />
              <SkeletonLine width="40%" height="0.75rem" />
            </div>
          </SkeletonCard>
        ))}
      </div>

      {/* Main content card */}
      <SkeletonCard className="p-6">
        <div className="flex items-center justify-between mb-6">
          <SkeletonLine width="120px" height="1rem" />
          <SkeletonLine width="96px" height="2rem" />
        </div>
        <SkeletonTable rows={4} columns={6} />
      </SkeletonCard>
    </div>
  );
}