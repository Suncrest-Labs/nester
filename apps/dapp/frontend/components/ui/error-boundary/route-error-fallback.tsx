"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { AlertTriangle, Home, RefreshCw } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { reportError, categorizeError } from "@/lib/observability/report-error";

export interface RouteErrorFallbackProps {
  error: Error & { digest?: string };
  /** Next.js boundary reset — re-renders the segment without a page reload. */
  reset: () => void;
  /** Name of the section this boundary guards, used as log context. */
  boundary: string;
  /** Human title for the section, e.g. "Markets". */
  section: string;
  /** Render inside AppShell so the navigation survives the failure. */
  withShell?: boolean;
}

const MESSAGES: Record<string, string> = {
  network: "We could not reach the network. Check your connection and try again.",
  auth: "Your session expired. Reconnect your wallet to continue.",
  "not-found": "We could not find what you were looking for.",
  "rate-limit": "Too many requests. Give it a moment and try again.",
  server: "Our service had a problem handling that request.",
  render: "Something went wrong while loading this page.",
  unknown: "Something went wrong while loading this page.",
};

/**
 * Shared fallback for every route-level `error.tsx`. Deliberately shows no stack
 * trace, no digest and no raw exception message: the user gets a plain
 * explanation and a recovery action, the details go to the logging pipeline.
 */
export function RouteErrorFallback({
  error,
  reset,
  boundary,
  section,
  withShell = true,
}: RouteErrorFallbackProps) {
  const headingRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    reportError({ error, boundary, context: { section } });
  }, [error, boundary, section]);

  useEffect(() => {
    // Move focus to the fallback so keyboard and screen-reader users are not
    // left on a control that no longer exists.
    headingRef.current?.focus();
  }, []);

  const description = MESSAGES[categorizeError(error)] ?? MESSAGES.unknown;

  const body = (
    <div
      role="alert"
      aria-live="assertive"
      data-testid="route-error-fallback"
      className="flex min-h-[50vh] flex-col items-center justify-center px-6 py-16 text-center"
    >
      <div className="mb-6 flex h-14 w-14 items-center justify-center rounded-full bg-red-50 dark:bg-red-500/10">
        <AlertTriangle className="h-7 w-7 text-red-600 dark:text-red-400" aria-hidden="true" />
      </div>

      <h2
        ref={headingRef}
        tabIndex={-1}
        className="mb-3 text-xl font-semibold text-black outline-none dark:text-white"
      >
        {section} could not be loaded
      </h2>

      <p className="mb-8 max-w-md text-sm leading-relaxed text-black/60 dark:text-white/60">
        {description}
      </p>

      <div className="flex flex-wrap items-center justify-center gap-3">
        <button
          type="button"
          onClick={reset}
          data-testid="route-error-retry"
          className="inline-flex h-11 items-center gap-2 rounded-xl bg-black px-5 text-sm font-semibold text-white transition-opacity hover:opacity-90 dark:bg-blue-600"
        >
          <RefreshCw className="h-4 w-4" aria-hidden="true" />
          Try again
        </button>

        <Link
          href="/dashboard"
          data-testid="route-error-home"
          className="inline-flex h-11 items-center gap-2 rounded-xl border border-black/10 px-5 text-sm font-medium text-black/70 transition-colors hover:border-black/25 hover:text-black dark:border-white/10 dark:text-white/70 dark:hover:border-white/25 dark:hover:text-white"
        >
          <Home className="h-4 w-4" aria-hidden="true" />
          Go to dashboard
        </Link>
      </div>
    </div>
  );

  if (!withShell) return body;

  return <AppShell>{body}</AppShell>;
}
