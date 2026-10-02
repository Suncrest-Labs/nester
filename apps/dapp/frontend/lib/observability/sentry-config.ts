/**
 * Shared Sentry configuration, read once and reused by the three runtime
 * entry points (`instrumentation-client.ts`, `instrumentation.ts` for the
 * Node server, and the edge runtime hook inside `instrumentation.ts`).
 *
 * Sentry is opt-in: every entry point calls `Sentry.init` only when a DSN is
 * configured. With no DSN, none of them call `init` at all, so the SDK sits
 * fully idle — no network activity, no console noise, and CI/local dev are
 * unaffected. This mirrors how OpenTelemetry tracing is gated in the Go API
 * (`apps/api/internal/tracing`): opt-in via env, no-op otherwise.
 */

/** Client-side DSN. Must be NEXT_PUBLIC_* to reach the browser bundle. */
export function getClientDsn(): string | undefined {
  return process.env.NEXT_PUBLIC_SENTRY_DSN || undefined;
}

/**
 * Server/edge DSN. Falls back to the public DSN so a deployment only has to
 * set one value; a separate SENTRY_DSN lets server-side events go to a
 * different Sentry project if desired.
 */
export function getServerDsn(): string | undefined {
  return process.env.SENTRY_DSN || process.env.NEXT_PUBLIC_SENTRY_DSN || undefined;
}

/**
 * Fraction of transactions sampled for performance tracing, in [0, 1].
 * Defaults to a modest 10% so tracing has low overhead out of the box.
 * Invalid or out-of-range values fall back to the default rather than
 * silently disabling tracing (0) or sending every transaction (1).
 */
export function getTracesSampleRate(): number {
  const raw = process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE;
  if (!raw) return 0.1;
  const parsed = Number(raw);
  if (!Number.isFinite(parsed) || parsed < 0 || parsed > 1) return 0.1;
  return parsed;
}

export function getEnvironment(): string {
  return process.env.NEXT_PUBLIC_SENTRY_ENVIRONMENT || process.env.NODE_ENV || "development";
}
