/**
 * Tracks the most recent X-Request-ID the API returned, so a client-side
 * error report can be correlated with the backend log/trace for the request
 * that caused it (nester#791; the header itself is set server-side in
 * apps/api/pkg/response/response.go).
 *
 * Deliberately just "the last one seen" rather than a per-request map: error
 * boundaries report failures asynchronously and disconnected from whichever
 * fetch triggered them, so there is no reliable request handle to key a map
 * by. The most recent request-id is still a useful correlation hint for
 * "what was the API doing right before this broke," which is the case this
 * exists for.
 */
let lastRequestId: string | undefined;

export function setLastRequestId(id: string | null | undefined): void {
  if (id) lastRequestId = id;
}

export function getLastRequestId(): string | undefined {
  return lastRequestId;
}

/** Test-only reset so specs don't leak state into each other. */
export function resetLastRequestId(): void {
  lastRequestId = undefined;
}
