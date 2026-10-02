import {
  sanitizeContext,
  sanitizeRoute,
  sanitizeText,
} from "@/lib/observability/sanitize";
import { getLastRequestId } from "@/lib/observability/request-id";

export type ErrorCategory =
  | "render"
  | "network"
  | "auth"
  | "not-found"
  | "rate-limit"
  | "server"
  | "unknown";

export interface ReportErrorInput {
  error: unknown;
  /** Route the failure happened on. Defaults to the current pathname. */
  route?: string;
  /** Named section/boundary that caught it, e.g. "vaults", "root". */
  boundary?: string;
  /** Extra non-sensitive context. Sanitized before it is emitted. */
  context?: Record<string, unknown>;
}

export interface ClientErrorEvent {
  route: string;
  boundary: string;
  category: ErrorCategory;
  name: string;
  message: string;
  digest?: string;
  timestamp: string;
  environment: string;
  context: Record<string, unknown>;
  /**
   * X-Request-ID of the most recent API response, when one has been seen.
   * Lets a Sentry event (and the console/beacon sinks) be correlated with the
   * backend log line / trace span for the request that likely caused it.
   */
  requestId?: string;
}

/** Endpoint is opt-in; without it the event still reaches the console sink. */
function getSink(): string {
  return process.env.NEXT_PUBLIC_ERROR_REPORT_URL ?? "";
}

export function categorizeError(error: unknown): ErrorCategory {
  const message =
    error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const status = (error as { status?: number } | null)?.status;
  const lower = message.toLowerCase();

  if (status === 401 || status === 403 || lower.includes("401")) return "auth";
  if (status === 404 || lower.includes("404")) return "not-found";
  if (status === 429 || lower.includes("429")) return "rate-limit";
  if (typeof status === "number" && status >= 500) return "server";
  if (lower.includes("500") || lower.includes("502") || lower.includes("503"))
    return "server";
  if (
    lower.includes("fetch") ||
    lower.includes("network") ||
    lower.includes("timeout") ||
    lower.includes("offline")
  )
    return "network";
  if (error instanceof Error) return "render";
  return "unknown";
}

/**
 * Build the wire payload. Exported so tests can assert on exactly what would be
 * transmitted without stubbing the transport.
 */
export function buildErrorEvent({
  error,
  route,
  boundary = "unknown",
  context,
}: ReportErrorInput): ClientErrorEvent {
  const resolvedRoute =
    route ??
    (typeof window === "undefined" ? "unknown" : window.location.pathname);

  const name = error instanceof Error ? error.name : "NonError";
  const rawMessage =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "Unknown error";

  return {
    route: sanitizeRoute(resolvedRoute),
    boundary,
    category: categorizeError(error),
    name,
    // Messages routinely interpolate addresses and amounts, so they are scrubbed
    // rather than trusted. Stack traces are never transmitted.
    message: sanitizeText(rawMessage).slice(0, 300),
    digest: (error as { digest?: string } | null)?.digest,
    timestamp: new Date().toISOString(),
    environment: process.env.NODE_ENV ?? "development",
    context: sanitizeContext(context ?? {}),
    requestId: getLastRequestId(),
  };
}

/** Guards against a boundary that re-renders and re-reports the same failure. */
const recentlyReported = new Set<string>();

function dedupeKey(event: ClientErrorEvent): string {
  return `${event.route}|${event.boundary}|${event.name}|${event.message}`;
}

export function resetErrorReportDedupe(): void {
  recentlyReported.clear();
}

/**
 * Report a caught UI error. Always writes a structured console entry (the sink
 * Playwright and CI read) and, when an endpoint is configured, beacons the same
 * payload to it. Never throws.
 */
export function reportError(input: ReportErrorInput): ClientErrorEvent {
  const event = buildErrorEvent(input);

  const key = dedupeKey(event);
  if (recentlyReported.has(key)) return event;
  recentlyReported.add(key);
  if (typeof window !== "undefined") {
    window.setTimeout(() => recentlyReported.delete(key), 10_000);
  }

  try {
    console.error("[nester:client-error]", JSON.stringify(event));
  } catch {
    // Serialisation must never mask the original failure.
  }

  const sink = getSink();
  if (sink && typeof navigator !== "undefined" && "sendBeacon" in navigator) {
    try {
      navigator.sendBeacon(
        sink,
        new Blob([JSON.stringify(event)], { type: "application/json" })
      );
    } catch {
      // Telemetry is best-effort.
    }
  }

  reportToSentry(input.error, event);

  return event;
}

/**
 * Forward the same scrubbed event to Sentry, when configured. Dynamically
 * imported so a build with no DSN never even evaluates the SDK from this
 * path, and a failure to load/report never masks the original error — this
 * sink is best-effort exactly like the sendBeacon sink above.
 */
function reportToSentry(error: unknown, event: ClientErrorEvent): void {
  if (!process.env.NEXT_PUBLIC_SENTRY_DSN) return;

  import("@sentry/nextjs")
    .then((Sentry) => {
      Sentry.withScope((scope) => {
        scope.setTag("boundary", event.boundary);
        scope.setTag("category", event.category);
        scope.setTag("route", event.route);
        if (event.requestId) scope.setTag("request_id", event.requestId);
        if (event.digest) scope.setTag("digest", event.digest);
        scope.setContext("client_error", event.context);

        if (error instanceof Error) {
          Sentry.captureException(error);
        } else {
          // Sentry only accepts synthetic exceptions for non-Error values;
          // the (already-scrubbed) message is what actually gets sent since
          // beforeSend re-scrubs it, matching the console/beacon sinks.
          Sentry.captureMessage(event.message, "error");
        }
      });
    })
    .catch(() => {
      // Telemetry is best-effort; a Sentry SDK load failure must not surface
      // as a user-visible error.
    });
}
