"use client";

import { RouteErrorFallback } from "@/components/ui/error-boundary/route-error-fallback";

/**
 * Root application boundary. Catches anything thrown below the root layout that
 * no nested `error.tsx` handled, so no route can degrade to a blank page.
 *
 * Rendered without the app shell because a failure this high up may originate
 * from the shell's own providers.
 */
export default function RootError({
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
      boundary="root"
      section="This page"
      withShell={false}
    />
  );
}
