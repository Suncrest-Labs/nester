/**
 * Sentry client-side init (Next.js App Router convention: this file is
 * auto-loaded before hydration when present — no manual import needed).
 *
 * Disabled by default: `Sentry.init` only runs when
 * NEXT_PUBLIC_SENTRY_DSN is set, so local dev and CI stay silent and
 * network-free (nester#791).
 */
import * as Sentry from "@sentry/nextjs";
import {
  getClientDsn,
  getEnvironment,
  getTracesSampleRate,
} from "@/lib/observability/sentry-config";
import { scrubSentryEvent } from "@/lib/observability/sentry-scrub";

const dsn = getClientDsn();

if (dsn) {
  Sentry.init({
    dsn,
    environment: getEnvironment(),
    tracesSampleRate: getTracesSampleRate(),
    // Wallet addresses, balances and tokens must never leave the browser —
    // the same bar lib/observability/sanitize.ts enforces for the console/
    // beacon sink. beforeSend is the last gate before a Sentry event is
    // transmitted.
    beforeSend: scrubSentryEvent,
    beforeSendTransaction: scrubSentryEvent,
  });
}

// Sentry's own hook is a documented no-op when no client is active (i.e. no
// DSN was configured above), so this can be exported unconditionally rather
// than re-implementing that guard here.
export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
