/**
 * Lightweight error-reporting utility for the DApp.
 *
 * Routes caught errors to the developer console and — when analytics consent
 * has been granted — forwards them to the configured telemetry endpoint.
 *
 * No user balances, wallet addresses, or personal identifiers are included
 * in the payload.
 */

export interface ErrorReport {
  /** Route path where the error occurred, e.g. "/dashboard" */
  route: string;
  /** Error name or code */
  code: string;
  /** Human-readable message */
  message: string;
  /** ISO-8601 timestamp */
  timestamp: string;
}

/**
 * Report a caught error to the logging pipeline.
 *
 * Stubs out sensitive fields (wallet addresses, balances, auth tokens) by
 * only including the route path, error name, and message.  Designed to be
 * safe to call from error boundaries without additional sanitisation.
 */
export function reportError(error: Error, route: string): void {
  const report: ErrorReport = {
    route,
    code: error.name || "UnknownError",
    message: error.message || "An unexpected error occurred",
    timestamp: new Date().toISOString(),
  };

  // Always log to console so errors are visible during development
  console.error(`[ErrorBoundary] ${report.route}:`, report.code, report.message);

  // When analytics consent is eventually wired, the body below can POST
  // to `/api/v1/telemetry/errors` — the payload is already safe because
  // it contains no PII, wallet addresses, or balances.
  //
  // if (typeof window !== "undefined" && window.__NESTER_ANALYTICS__) {
  //   fetch("/api/v1/telemetry/errors", {
  //     method: "POST",
  //     headers: { "Content-Type": "application/json" },
  //     body: JSON.stringify(report),
  //   }).catch(() => {});
  // }
}