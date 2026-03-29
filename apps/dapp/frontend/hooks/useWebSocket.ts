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

export function useWebSocket({
    url,
    token,
    channels,
    onEvent,
    reconnectAttempts = MAX_RECONNECT_ATTEMPTS,
    reconnectInterval = BASE_RECONNECT_INTERVAL_MS,
    pollInterval = POLL_INTERVAL_MS,
    onPoll,
}: UseWebSocketOptions): UseWebSocketReturn {
    const [status, setStatus] = useState<WSConnectionStatus>("offline");
    const [lastEvent, setLastEvent] = useState<WSEvent | null>(null);

    // Keep stable references so interval/event callbacks don't go stale.
    const wsRef = useRef<WebSocket | null>(null);
    const attemptsRef = useRef(0);
    const isMountedRef = useRef(true);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
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

    const sendSubscriptions = useCallback((ws: WebSocket) => {
        if (ws.readyState !== WebSocket.OPEN) return;
        for (const channel of channelsRef.current) {
            ws.send(JSON.stringify({ type: "subscribe", channel }));
        }
    }, []);

    const connect = useCallback(() => {
        if (!isMountedRef.current) return;

        // Close any existing socket cleanly.
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
        };

        ws.onmessage = (evt: MessageEvent) => {
            if (!isMountedRef.current) return;
            try {
                const data = JSON.parse(evt.data as string) as WSEvent;
                setLastEvent(data);
                onEventRef.current(data);
            } catch {
                // Ignore malformed frames.
            }
        };

        ws.onclose = () => {
            if (!isMountedRef.current) return;

            if (attemptsRef.current < reconnectAttempts) {
                attemptsRef.current += 1;
                // Exponential back-off: 1 s, 2 s, 4 s, 8 s, 16 s
                const delay = reconnectInterval * Math.pow(2, attemptsRef.current - 1);
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
    }, [url, reconnectAttempts, reconnectInterval, sendSubscriptions, startPoll, stopPoll]);

    // Cleanup helper — tears down socket and timers without triggering reconnect.
    const teardown = useCallback(() => {
        if (reconnectTimerRef.current !== null) {
            clearTimeout(reconnectTimerRef.current);
            reconnectTimerRef.current = null;
        }
        stopPoll();
        if (wsRef.current) {
            wsRef.current.onopen = null;
            wsRef.current.onclose = null;
            wsRef.current.onerror = null;
            wsRef.current.onmessage = null;
            wsRef.current.close();
            wsRef.current = null;
        }
    }, [stopPoll]);

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
