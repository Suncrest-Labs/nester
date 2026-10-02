/**
 * Next.js instrumentation hook (App Router). `register()` runs once per
 * runtime at boot and loads the matching Sentry config; `onRequestError`
 * forwards server-side request failures (API routes, RSC render errors,
 * middleware) to the same scrubbed Sentry pipeline. Both are no-ops when no
 * DSN is configured (nester#791).
 */
export async function register() {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    await import("./sentry.server.config");
  }

  if (process.env.NEXT_RUNTIME === "edge") {
    await import("./sentry.edge.config");
  }
}

export async function onRequestError(
  ...args: Parameters<
    typeof import("@sentry/nextjs").captureRequestError
  >
) {
  const { getServerDsn } = await import("@/lib/observability/sentry-config");
  if (!getServerDsn()) return;

  const Sentry = await import("@sentry/nextjs");
  Sentry.captureRequestError(...args);
}
