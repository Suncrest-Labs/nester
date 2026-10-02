/**
 * Sentry `beforeSend` / `beforeSendTransaction` gate.
 *
 * Sentry events are a different shape from the internal ClientErrorEvent this
 * app already builds (lib/observability/report-error.ts), but they carry the
 * same risk: exception messages, breadcrumbs, request URLs and extra context
 * routinely interpolate wallet addresses, balances and bearer tokens. Nothing
 * Sentry-bound skips the same scrubbing lib/observability/sanitize.ts applies
 * to the console/beacon sink — this module is the last gate before
 * transmission, mirroring the DENIED_KEYS + pattern redaction rules exactly
 * so the two sinks can never disagree about what counts as sensitive.
 */
import { sanitizeContext, sanitizeRoute, sanitizeText } from "@/lib/observability/sanitize";

/**
 * Minimal structural type for the pieces of a Sentry event this module reads
 * or rewrites, used only internally for the mutation logic below. Deliberately
 * loose (not imported from @sentry/nextjs) so one function body covers both
 * ErrorEvent and TransactionEvent without a union of two large,
 * version-sensitive SDK types — scrubSentryEvent's public signature instead
 * takes and returns a caller-supplied generic type directly (see below), so
 * callers can pass Sentry's real ErrorEvent/TransactionEvent without a
 * structural-assignability fight against their index-signature-free fields.
 */
interface ScrubbableSentryEvent {
  message?: string;
  exception?: {
    values?: Array<{ value?: string }>;
  };
  request?: {
    url?: string;
    headers?: Record<string, string>;
    cookies?: unknown;
    data?: unknown;
  };
  breadcrumbs?: Array<{
    message?: string;
    data?: Record<string, unknown>;
  }>;
  extra?: Record<string, unknown>;
  transaction?: string;
}

/** Header names that must never reach Sentry, regardless of value shape. */
const DENIED_HEADERS = new Set([
  "authorization",
  "cookie",
  "set-cookie",
  "x-api-key",
]);

function scrubHeaders(
  headers: Record<string, string> | undefined
): Record<string, string> | undefined {
  if (!headers) return headers;
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers)) {
    out[key] = DENIED_HEADERS.has(key.toLowerCase())
      ? "[redacted]"
      : sanitizeText(value);
  }
  return out;
}

/**
 * Scrub and return a Sentry event, or `null` to drop it entirely. Never
 * throws: a scrubbing failure drops the event rather than risking an
 * unscrubbed payload reaching Sentry.
 *
 * Generic and unconstrained on purpose: Sentry's real ErrorEvent /
 * TransactionEvent types have no index signature on nested objects like
 * `exception.values[]`, so constraining T to a structural shape here would
 * force callers into unsound casts just to satisfy `beforeSend` /
 * `beforeSendTransaction`. The event is narrowed to ScrubbableSentryEvent
 * internally instead; the mutations below only ever touch fields that, when
 * present, have the shape this module expects.
 */
export function scrubSentryEvent<T extends object>(event: T): T | null {
  try {
    const scrubbable = event as ScrubbableSentryEvent;

    if (scrubbable.message) {
      scrubbable.message = sanitizeText(scrubbable.message);
    }

    if (scrubbable.transaction) {
      scrubbable.transaction = sanitizeRoute(scrubbable.transaction);
    }

    if (scrubbable.exception?.values) {
      for (const value of scrubbable.exception.values) {
        if (value.value) value.value = sanitizeText(value.value);
      }
    }

    if (scrubbable.request) {
      if (scrubbable.request.url) {
        scrubbable.request.url = sanitizeRoute(scrubbable.request.url);
      }
      scrubbable.request.headers = scrubHeaders(scrubbable.request.headers);
      // Cookies and raw request bodies can carry tokens/PII verbatim; there is
      // no safe partial-redaction of either, so they are dropped wholesale.
      delete scrubbable.request.cookies;
      delete scrubbable.request.data;
    }

    if (scrubbable.breadcrumbs) {
      for (const crumb of scrubbable.breadcrumbs) {
        if (crumb.message) crumb.message = sanitizeText(crumb.message);
        if (crumb.data) crumb.data = sanitizeContext(crumb.data);
      }
    }

    if (scrubbable.extra) {
      scrubbable.extra = sanitizeContext(scrubbable.extra);
    }

    return event;
  } catch {
    // Scrubbing must never let an unscrubbed event through.
    return null;
  }
}
