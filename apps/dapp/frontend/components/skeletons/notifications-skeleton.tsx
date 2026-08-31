"use client";

import {
  Skeleton,
  SkeletonLine,
  LoadingRegion,
} from "@/components/ui/skeleton/skeleton";

/**
 * Notification rows only — the notification centre already renders its own
 * header and card chrome, so this fills the card body rather than the page.
 */
export function NotificationsListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <LoadingRegion label="Loading your notifications" className="space-y-4">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-start gap-4">
          <Skeleton className="h-10 w-10 rounded-full" />
          <div className="flex-1 space-y-2">
            <SkeletonLine width="65%" height="0.95rem" />
            <SkeletonLine width="88%" height="0.75rem" />
            <SkeletonLine width="90px" height="0.7rem" />
          </div>
          <Skeleton className="h-2 w-2 rounded-full" />
        </div>
      ))}
    </LoadingRegion>
  );
}
