"use client";

import { RecoverableError } from "@/components/recoverable-error";

export default function RouteError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return <RecoverableError error={error} reset={reset} route="/savings" />;
}

