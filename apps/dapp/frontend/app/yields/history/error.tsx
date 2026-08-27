"use client";

import { RouteErrorFallback } from "@/components/ui/error-boundary/route-error-fallback";

export default function YieldsHistoryError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <RouteErrorFallback
      error={error}
      reset={reset}
      boundary="yields-history"
      section="Harvest history"
    />
  );
}
