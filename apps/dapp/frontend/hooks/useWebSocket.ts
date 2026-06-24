"use client";

import {
    useCallback,
    useEffect,
    useRef,
    useState,
} from "react";
import { type WSConnectionStatus, type WSEvent } from "@/lib/ws-events";

interface UseWebSocketOptions {
    /** WebSocket server URL, e.g. wss://api.nester.fi/ws */
    url: string;
    /** JWT for authenticating the session on connect */
    token: string;
    /** Channels to subscribe to on connect, then again after reconnection */
    channels: string[];
    /** Called for every event message received */
    onEvent: (event: WSEvent) => void;
    /** How many reconnect attempts before giving up (default: 5) */
    reconnectAttempts?: number;
    /** Base interval for exponential back-off in ms (default: 1000) */
    reconnectInterval?: number;
    /** Interval in ms for REST polling fallback (default: 30 000) */
    pollInterval?: number;
    /** Optional: called to fetch latest snapshot via REST when polling */
    onPoll?: () => Promise<void>;
    /** How often to send a heartbeat ping in ms (default: 30 000) */
    heartbeatInterval?: number;
    /** How long to wait for a pong before assuming the link is dead (default: 10 000) */
    heartbeatTimeout?: number;
}

export interface UseWebSocketReturn {
    isConnected: boolean;
    status: WSConnectionStatus;
    lastEvent: WSEvent | null;
    subscribe: (channel: string) => void;
    unsubscribe: (channel: string) => void;
    disconnect: () => void;
    manualReconnect: () => void;
}

const MAX_RECONNECT_ATTEMPTS = 5;
const BASE_RECONNECT_INTERVAL_MS = 1000;
const POLL_INTERVAL_MS = 30_000;
const HEARTBEAT_INTERVAL_MS = 30_000;
const HEARTBEAT_TIMEOUT_MS = 10_000;

/** Maximum back-off between reconnect attempts (matches the issue spec). */
export const MAX_BACKOFF_MS = 30_000;

/**
 * Exponential back-off schedule for reconnect attempts.
 *
 * delay = base * 2^attempt, capped at MAX_BACKOFF_MS. With the default base of
 * 1000ms this yields the schedule 1s, 2s, 4s, 8s, 16s, 30s, 30s…
 *
 * Exported so the schedule can be unit-tested independently of the socket.
 */
export function getReconnectDelay(
    attempt: number,
    base: number = BASE_RECONNECT_INTERVAL_MS,
): number {
    const safeAttempt = Math.max(0, attempt);
    return Math.min(base * 2 ** safeAttempt, MAX_BACKOFF_MS);
}

export function useWebSocket({
    url,
    token,
    channels,
    onEvent,
    reconnectAttempts = MAX_RECONNECT_ATTEMPTS,
    reconnectInterval = BASE_RECONNECT_INTERVAL_MS,
    pollInterval = POLL_INTERVAL_MS,
    onPoll,
    heartbeatInterval = HEARTBEAT_INTERVAL_MS,
    heartbeatTimeout = HEARTBEAT_TIMEOUT_MS,
}: UseWebSocketOptions): UseWebSocketReturn {
    const [status, setStatus] = useState<WSConnectionStatus>("offline");
    const [lastEvent, setLastEvent] = useState<WSEvent | null>(null);

    // Keep stable references so interval/event callbacks don't go stale.
    const wsRef = useRef<WebSocket | null>(null);
    const attemptsRef = useRef(0);
    const isMountedRef = useRef(true);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const heartbeatTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const pongTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const channelsRef = useRef<string[]>(channels);
    const onEventRef = useRef(onEvent);
    const onPollRef = useRef(onPoll);
    const tokenRef = useRef(token);

    // Keep refs in sync without triggering reconnects.
    useEffect(() => { channelsRef.current = channels; }, [channels]);
    useEffect(() => { onEventRef.current = onEvent; }, [onEvent]);
    useEffect(() => { onPollRef.current = onPoll; }, [onPoll]);
    useEffect(() => { tokenRef.current = token; }, [token]);

    const stopPoll = useCallback(() => {
        if (pollTimerRef.current !== null) {
            clearInterval(pollTimerRef.current);
            pollTimerRef.current = null;
        }
    }, []);

    const startPoll = useCallback(() => {
        if (!onPollRef.current || pollTimerRef.current !== null) return;
        pollTimerRef.current = setInterval(async () => {
            try {
                await onPollRef.current?.();
            } catch {
                // Polling errors are non-fatal; keep trying.
            }
        }, pollInterval);
    }, [pollInterval]);

    const stopHeartbeat = useCallback(() => {
        if (heartbeatTimerRef.current !== null) {
            clearInterval(heartbeatTimerRef.current);
            heartbeatTimerRef.current = null;
        }
        if (pongTimerRef.current !== null) {
            clearTimeout(pongTimerRef.current);
            pongTimerRef.current = null;
        }
    }, []);

    // Send a ping every `heartbeatInterval`; if no pong arrives within
    // `heartbeatTimeout`, assume the link is dead and force a reconnect by
    // closing the socket (which triggers onclose → back-off).
    const startHeartbeat = useCallback((ws: WebSocket) => {
        stopHeartbeat();
        heartbeatTimerRef.current = setInterval(() => {
            if (ws.readyState !== WebSocket.OPEN) return;
            ws.send(JSON.stringify({ type: "ping" }));
            if (pongTimerRef.current !== null) clearTimeout(pongTimerRef.current);
            pongTimerRef.current = setTimeout(() => {
                // No pong in time — drop the connection so onclose reconnects.
                ws.close();
            }, heartbeatTimeout);
        }, heartbeatInterval);
    }, [heartbeatInterval, heartbeatTimeout, stopHeartbeat]);

    const sendSubscriptions = useCallback((ws: WebSocket) => {
        if (ws.readyState !== WebSocket.OPEN) return;
        for (const channel of channelsRef.current) {
            ws.send(JSON.stringify({ type: "subscribe", channel }));
        }
    }, []);

    const connect = useCallback(() => {
        if (!isMountedRef.current) return;

        // Close any existing socket cleanly.
        stopHeartbeat();
        if (wsRef.current) {
            wsRef.current.onopen = null;
            wsRef.current.onclose = null;
            wsRef.current.onerror = null;
            wsRef.current.onmessage = null;
            wsRef.current.close();
            wsRef.current = null;
        }

        let ws: WebSocket;
        try {
            ws = new WebSocket(url);
        } catch {
            // URL may be invalid in dev; fall back to polling.
            setStatus("offline");
            startPoll();
            return;
        }

        wsRef.current = ws;

        ws.onopen = () => {
            if (!isMountedRef.current) return;
            attemptsRef.current = 0;
            setStatus("connected");
            stopPoll();

            // Authenticate, then subscribe to channels.
            ws.send(JSON.stringify({ type: "auth", token: tokenRef.current }));
            sendSubscriptions(ws);

            // Begin liveness pings now that the link is open.
            startHeartbeat(ws);
        };

        ws.onmessage = (evt: MessageEvent) => {
            if (!isMountedRef.current) return;
            try {
                const data = JSON.parse(evt.data as string) as
                    | WSEvent
                    | { type: "pong" | "ping" };

                // Heartbeat frames are protocol-level, not domain events.
                if (data.type === "pong") {
                    if (pongTimerRef.current !== null) {
                        clearTimeout(pongTimerRef.current);
                        pongTimerRef.current = null;
                    }
                    return;
                }
                if (data.type === "ping") {
                    ws.send(JSON.stringify({ type: "pong" }));
                    return;
                }

                setLastEvent(data as WSEvent);
                onEventRef.current(data as WSEvent);
            } catch {
                // Ignore malformed frames.
            }
        };

        ws.onclose = () => {
            stopHeartbeat();
            if (!isMountedRef.current) return;

            if (attemptsRef.current < reconnectAttempts) {
                // Back-off uses the current attempt index (0-based) before
                // incrementing: 1 s, 2 s, 4 s, 8 s, 16 s, 30 s…
                const delay = getReconnectDelay(attemptsRef.current, reconnectInterval);
                attemptsRef.current += 1;
                setStatus("reconnecting");
                reconnectTimerRef.current = setTimeout(connect, delay);
            } else {
                setStatus("offline");
                startPoll();
            }
        };

        ws.onerror = () => {
            // onerror is always followed by onclose; handle backoff there.
            ws.close();
        };
    }, [url, reconnectAttempts, reconnectInterval, sendSubscriptions, startPoll, stopPoll, startHeartbeat, stopHeartbeat]);

    // Cleanup helper — tears down socket and timers without triggering reconnect.
    const teardown = useCallback(() => {
        if (reconnectTimerRef.current !== null) {
            clearTimeout(reconnectTimerRef.current);
            reconnectTimerRef.current = null;
        }
        stopPoll();
        stopHeartbeat();
        if (wsRef.current) {
            wsRef.current.onopen = null;
            wsRef.current.onclose = null;
            wsRef.current.onerror = null;
            wsRef.current.onmessage = null;
            wsRef.current.close();
            wsRef.current = null;
        }
    }, [stopPoll, stopHeartbeat]);

    const disconnect = useCallback(() => {
        attemptsRef.current = reconnectAttempts; // prevent auto-reconnect
        teardown();
        if (isMountedRef.current) setStatus("offline");
    }, [reconnectAttempts, teardown]);

    const manualReconnect = useCallback(() => {
        attemptsRef.current = 0;
        teardown();
        connect();
    }, [connect, teardown]);

    const subscribe = useCallback((channel: string) => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: "subscribe", channel }));
        }
    }, []);

    const unsubscribe = useCallback((channel: string) => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: "unsubscribe", channel }));
        }
    }, []);

    // Establish the connection on mount.
    useEffect(() => {
        isMountedRef.current = true;
        connect();
        return () => {
            isMountedRef.current = false;
            teardown();
        };
        // connect / teardown are stable — only run on mount/unmount.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return {
        isConnected: status === "connected",
        status,
        lastEvent,
        subscribe,
        unsubscribe,
        disconnect,
        manualReconnect,
    };
}
