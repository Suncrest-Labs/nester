"use client";

import { useEffect } from "react";
import { reportError } from "@/lib/observability/report-error";

/**
 * Last-resort boundary for failures in the root layout itself. It replaces the
 * whole document, so it must render its own <html>/<body> and may not depend on
 * providers from the layout that just crashed. Styling is inline for the same
 * reason.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    reportError({ error, boundary: "global" });
  }, [error]);

  return (
    <html lang="en">
      <body
        style={{
          margin: 0,
          minHeight: "100vh",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: "1rem",
          padding: "2rem",
          textAlign: "center",
          fontFamily:
            "Inter, ui-sans-serif, system-ui, -apple-system, sans-serif",
          background: "#ffffff",
          color: "#100F0F",
        }}
      >
        <div role="alert" aria-live="assertive" data-testid="global-error-fallback">
          <h1 style={{ fontSize: "1.25rem", fontWeight: 600, margin: "0 0 0.75rem" }}>
            Something went wrong
          </h1>
          <p
            style={{
              fontSize: "0.875rem",
              lineHeight: 1.6,
              margin: "0 0 1.5rem",
              maxWidth: "28rem",
              opacity: 0.65,
            }}
          >
            The application ran into an unexpected problem. Trying again usually
            resolves it.
          </p>
          <button
            type="button"
            onClick={reset}
            data-testid="global-error-retry"
            style={{
              height: "2.75rem",
              padding: "0 1.25rem",
              borderRadius: "0.75rem",
              border: "none",
              background: "#100F0F",
              color: "#ffffff",
              fontSize: "0.875rem",
              fontWeight: 600,
              cursor: "pointer",
            }}
          >
            Try again
          </button>
        </div>
      </body>
    </html>
  );
}
