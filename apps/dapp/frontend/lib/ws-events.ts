// WebSocket event types shared between the hook, provider, and consumers.
// Mirrors the shape the backend service (#146 / #870) will produce.

/**
 * The three states the UI must be able to tell apart:
 *   connected     — socket open and heartbeating; displayed values are live
 *   reconnecting  — link dropped, retrying with jittered back-off
 *   offline       — retries exhausted (or the tab is hidden); values are stale
 *
 * Anything other than "connected" means what is on screen may not reflect
 * the chain, and must not be styled as though it does.
 */
export type WSConnectionStatus = "connected" | "reconnecting" | "offline";

export type WSEventType =
    | "balance_updated"
    | "deposit_confirmed"
    | "withdrawal_confirmed"
    | "yield_accrued"
    | "vault_paused"
    | "vault_unpaused"
    | "emergency_queue_fill"
    | "security_event"
    | "breaker_trip"
    | "goal_milestone"
    | "nudge_alert";

export interface WSEvent {
    type: WSEventType;
    channel: string;
    payload: Record<string, unknown>;
    timestamp: string;
}

// Payloads for each event type. Components can narrow via `event.type`.

export interface BalanceUpdatedPayload {
    asset: string;
    newBalance: number;
    previousBalance: number;
}

export interface DepositConfirmedPayload {
    vaultId: string;
    vaultName: string;
    amount: number;
    asset: string;
    txHash: string;
}

export interface WithdrawalConfirmedPayload {
    vaultId: string;
    vaultName: string;
    netAmount: number;
    asset: string;
    txHash: string;
}

export interface YieldAccruedPayload {
    positionId: string;
    deltaYield: number;
    asset: string;
}

export interface VaultPausedPayload {
    vaultId: string;
    reason?: string;
}

export interface VaultUnpausedPayload {
    vaultId: string;
}

export interface EmergencyQueueFillPayload {
    queueId: string;
    asset: string;
    fillPercentage: number;
    message?: string;
}

export interface SecurityEventPayload {
    eventId: string;
    eventType: string;
    details: string;
    severity?: "info" | "warning" | "high" | "critical";
}

export interface BreakerTripPayload {
    breakerId: string;
    asset?: string;
    reason?: string;
}

export interface GoalMilestonePayload {
    goalId: string;
    goalTitle: string;
    progress: number;
    message?: string;
}

export interface NudgeAlertPayload {
    nudgeId?: string;
    title: string;
    message: string;
    actionUrl?: string;
    actionLabel?: string;
}


// ---------------------------------------------------------------------------
// Wire protocol
// ---------------------------------------------------------------------------
//
// The Go hub (apps/api/internal/ws) speaks a different shape to the one the
// React components consume:
//
//   server -> client   { channel, event, data, timestamp }
//   client -> server   { action: "subscribe" | "unsubscribe" | "ping",
//                        channels: string[] }
//
// Authentication is a `?token=` query parameter on the upgrade request, not
// an in-band frame — the hub authenticates in ServeWs before upgrading.
// Keeping the translation here means the hook and the components both stay
// in the app's own vocabulary.

/** Raw event as it arrives from the Go hub. */
export interface ServerWireEvent {
    channel: string;
    event: string;
    data: unknown;
    timestamp?: string;
}

/** Client -> server frame accepted by Hub.readPump. */
export interface ClientWireMessage {
    action: "subscribe" | "unsubscribe" | "ping";
    channels?: string[];
}

/**
 * Server event names that differ from the name the DApp uses internally.
 */
// A Map rather than an object literal: `rawType` comes off the wire, and a
// plain-object lookup for "constructor" or "__proto__" would resolve up the
// prototype chain and yield a nonsense event type.
const SERVER_EVENT_ALIASES = new Map<string, WSEventType>([]);

/** Heartbeat reply from the hub — see EventPong in apps/api/internal/ws. */
export const PONG_EVENT = "pong";

type MaybeFrame = Partial<ServerWireEvent> & { type?: string };

/**
 * True for the hub's pong reply, in either the current wire shape
 * (`{ event: "pong" }`) or the legacy `{ type: "pong" }` some older builds
 * and test doubles emit. Heartbeat frames are transport bookkeeping and must
 * never reach domain handlers.
 */
export function isPongFrame(frame: unknown): boolean {
    if (typeof frame !== "object" || frame === null) return false;
    const f = frame as MaybeFrame;
    return f.event === PONG_EVENT || f.type === PONG_EVENT;
}

/**
 * Translate a frame from the hub into the WSEvent shape the app consumes.
 *
 * Returns null for anything that is not a domain event (heartbeats,
 * malformed frames) so callers can drop it without a second check.
 */
export function normalizeServerEvent(frame: unknown): WSEvent | null {
    if (typeof frame !== "object" || frame === null) return null;
    const f = frame as MaybeFrame & { payload?: Record<string, unknown> };

    // Accept both the hub shape ({event, data}) and the app shape
    // ({type, payload}) so a mocked or replayed event still flows through.
    const rawType = f.event ?? f.type;
    if (typeof rawType !== "string" || rawType === "" || rawType === PONG_EVENT) {
        return null;
    }

    const type = (SERVER_EVENT_ALIASES.get(rawType) ?? rawType) as WSEventType;
    const data = f.data ?? f.payload;

    return {
        type,
        channel: typeof f.channel === "string" ? f.channel : "",
        payload:
            typeof data === "object" && data !== null
                ? (data as Record<string, unknown>)
                : {},
        timestamp: typeof f.timestamp === "string" ? f.timestamp : new Date().toISOString(),
    };
}

export const WS_PROTOCOL_V1 = "nester.v1";
export const WS_AUTH_PROTOCOL_PREFIX = "nester.auth.";

/**
 * Build the WebSocket upgrade URL without embedding bearer JWTs in the query
 * string (#1519). If an optional short-lived single-use `ticket` is supplied,
 * it is attached as `?ticket=`.
 */
export function buildSocketUrl(url: string, ticket?: string): string {
    if (!url) return url;
    if (!ticket) return url;
    const separator = url.includes("?") ? "&" : "?";
    return `${url}${separator}ticket=${encodeURIComponent(ticket)}`;
}

/**
 * Build the Sec-WebSocket-Protocol list carrying the authentication credential
 * out of the URL (#1519).
 */
export function buildSocketProtocols(credential: string): string[] {
    const trimmed = credential.trim();
    if (!trimmed) return [];
    return [WS_PROTOCOL_V1, `${WS_AUTH_PROTOCOL_PREFIX}${trimmed}`];
}

/**
 * Request a short-lived (<=30s), single-use WebSocket ticket from
 * POST /api/v1/ws/ticket using the caller's Bearer token.
 */
export async function fetchWsTicket(apiBaseUrl: string, bearerToken: string): Promise<string> {
    const base = apiBaseUrl.replace(/\/+$/, "");
    const res = await fetch(`${base}/api/v1/ws/ticket`, {
        method: "POST",
        headers: {
            Authorization: `Bearer ${bearerToken}`,
            "Content-Type": "application/json",
        },
    });
    if (!res.ok) {
        throw new Error(`Failed to obtain WebSocket ticket: HTTP ${res.status}`);
    }
    const body = (await res.json()) as { data?: { ticket?: string } };
    const ticket = body?.data?.ticket;
    if (!ticket) {
        throw new Error("WebSocket ticket response missing ticket");
    }
    return ticket;
}

