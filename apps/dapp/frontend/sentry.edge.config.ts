/**
 * Sentry init for the Edge runtime (middleware, edge routes). Loaded from
 * instrumentation.ts when NEXT_RUNTIME === "edge". No-op unless a DSN is
 * configured — see lib/observability/sentry-config.ts.
 */
import * as Sentry from "@sentry/nextjs";
import {
  getEnvironment,
  getServerDsn,
  getTracesSampleRate,
} from "@/lib/observability/sentry-config";
import { scrubSentryEvent } from "@/lib/observability/sentry-scrub";

const dsn = getServerDsn();

if (dsn) {
  Sentry.init({
    dsn,
    environment: getEnvironment(),
    tracesSampleRate: getTracesSampleRate(),
    beforeSend: scrubSentryEvent,
    beforeSendTransaction: scrubSentryEvent,
  });
}
