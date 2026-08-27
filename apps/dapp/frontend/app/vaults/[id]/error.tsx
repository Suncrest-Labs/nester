"use client";

import { RouteErrorFallback } from "@/components/ui/error-boundary/route-error-fallback";

export default function VaultDetailError({
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
      boundary="vault-detail"
      section="This market"
    />
  );
}
