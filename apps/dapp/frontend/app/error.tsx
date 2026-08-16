"use client";

import { useEffect } from "react";
import Link from "next/link";
import Image from "next/image";
import { AlertTriangle, RefreshCw, Home } from "lucide-react";
import { reportError } from "@/lib/error-reporting";

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    reportError(error, "/");
  }, [error]);

  return (
    <html>
      <body className="min-h-screen bg-white dark:bg-[#100F0F] flex flex-col items-center justify-center px-4">
        {/* Logo */}
        <Link href="/" className="flex items-center gap-2.5 mb-16">
          <Image
            src="/logo.png"
            alt="Nester"
            width={36}
            height={36}
            className="rounded-xl"
          />
          <span className="font-heading text-[15px] font-medium text-black dark:text-white">
            Nester
          </span>
        </Link>

        {/* Error icon */}
        <div className="mb-6 flex h-16 w-16 items-center justify-center rounded-full bg-red-50 dark:bg-red-900/20">
          <AlertTriangle className="h-8 w-8 text-red-500" />
        </div>

        <h1 className="text-xl sm:text-2xl text-black dark:text-white mb-3 text-center">
          Something went wrong
        </h1>
        <p className="text-sm text-black/40 dark:text-white/40 max-w-md text-center leading-relaxed mb-2">
          An unexpected error occurred. Our team has been notified and we're
          working on it.
        </p>
        {error.digest && (
          <p className="text-xs font-mono text-black/30 dark:text-white/30 mb-6">
            Error ID: {error.digest}
          </p>
        )}

        {/* Actions */}
        <div className="mt-8 flex items-center gap-3">
          <button
            onClick={reset}
            className="inline-flex items-center gap-2 rounded-full bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Try again
          </button>
          <Link
            href="/"
            className="inline-flex items-center gap-2 rounded-full border border-black/10 dark:border-white/10 px-5 py-2.5 text-sm text-black dark:text-white transition-colors hover:bg-black/5 dark:hover:bg-white/5"
          >
            <Home className="h-3.5 w-3.5" />
            Go home
          </Link>
        </div>
      </body>
    </html>
  );
}
