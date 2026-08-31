"use client";

import { useEffect } from "react";
import Link from "next/link";
import { AlertTriangle, RefreshCw, Home } from "lucide-react";
import { reportError } from "@/lib/error-reporting";

interface RecoverableErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
  route: string;
  /** Optional custom message shown below the heading. */
  message?: string;
  /** Optional heading override. */
  heading?: string;
}

/**
 * A reusable error-boundary fallback for per-route error.tsx files.
 *
 * Shows a centered error card with a "Try again" button and a "Go home"
 * link.  Reports the error to the logging pipeline with the route context.
 */
export function RecoverableError({
  error,
  reset,
  route,
  message = "We couldn't load this page. Please try again or go back to the dashboard.",
  heading = "Something went wrong",
}: RecoverableErrorProps) {
  useEffect(() => {
    reportError(error, route);
  }, [error, route]);

  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center px-4">
      <div className="mb-6 flex h-14 w-14 items-center justify-center rounded-full bg-red-50 dark:bg-red-900/20">
        <AlertTriangle className="h-7 w-7 text-red-500" />
      </div>

      <h2 className="text-lg sm:text-xl text-black dark:text-white mb-2 text-center">
        {heading}
      </h2>
      <p className="text-sm text-black/40 dark:text-white/40 max-w-sm text-center leading-relaxed mb-2">
        {message}
      </p>
      {error.digest && (
        <p className="text-xs font-mono text-black/30 dark:text-white/30 mb-6">
          Error ID: {error.digest}
        </p>
      )}

      <div className="mt-6 flex items-center gap-3">
        <button
          onClick={reset}
          className="inline-flex items-center gap-2 rounded-full bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75"
        >
          <RefreshCw className="h-3.5 w-3.5" />
          Try again
        </button>
        <Link
          href="/dashboard"
          className="inline-flex items-center gap-2 rounded-full border border-black/10 dark:border-white/10 px-5 py-2.5 text-sm text-black dark:text-white transition-colors hover:bg-black/5 dark:hover:bg-white/5"
        >
          <Home className="h-3.5 w-3.5" />
          Dashboard
        </Link>
      </div>
    </div>
  );
}
