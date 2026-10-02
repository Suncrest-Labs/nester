/**
 * Sentry init for the Node.js server runtime. Loaded from instrumentation.ts
 * when NEXT_RUNTIME === "nodejs". No-op unless a DSN is configured — see
 * lib/observability/sentry-config.ts.
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
