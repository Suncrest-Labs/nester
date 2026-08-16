"use client";

import { useWebSocketContext } from "@/components/websocket-provider";
import { type WSConnectionStatus } from "@/lib/ws-events";
import { cn } from "@/lib/utils";

interface StatusMeta {
    dot: string;
    text: string;
    bg: string;
    label: string;
    title: string;
    pulse: boolean;
    stale: boolean;
}

// Visual + copy for each connection state. "offline" means every reconnect
// attempt was exhausted and we have fallen back to REST polling, so the user
// is still seeing data — just on a delay.
function statusMeta(status: WSConnectionStatus): StatusMeta {
    switch (status) {
        case "connected":
            return {
                dot: "bg-emerald-500",
                text: "text-emerald-700",
                bg: "bg-emerald-50 border-emerald-200",
                label: "Live",
                title: "Connected and receiving real-time updates",
                pulse: true,
                stale: false,
            };
        case "reconnecting":
            return {
                dot: "bg-amber-500",
                text: "text-amber-700",
                bg: "bg-amber-50 border-amber-200",
                label: "Reconnecting…",
                title: "Connection lost — retrying with back-off",
                pulse: true,
                stale: true,
            };
        case "offline":
        default:
            return {
                dot: "bg-red-500",
                text: "text-red-700",
                bg: "bg-red-50 border-red-200",
                label: "Using delayed updates",
                title: "Disconnected — falling back to polling every 30s",
                pulse: false,
                stale: true,
            };
    }
}

/**
 * Returns a human-readable relative timestamp like "2m ago" or "just now".
 */
function relativeTime(ms: number): string {
    const seconds = Math.floor((Date.now() - ms) / 1000);
    if (seconds < 5) return "just now";
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
}

/**
 * Compact connection-status badge for the app header.
 *
 * Reflects the live WebSocket state:
 *   - green "Live" when connected
 *   - amber "Reconnecting…" while backing off between attempts
 *   - red "Using delayed updates" once retries are exhausted (polling fallback)
 *
 * When not connected, shows the last time data was received so the user
 * can assess how stale the displayed figures may be.
 */
export function ConnectionStatusBadge({ className }: { className?: string }) {
    const { status, lastEventTime } = useWebSocketContext();
    const meta = statusMeta(status);

    return (
        <div
            role="status"
            aria-live="polite"
            title={meta.title}
            className={cn(
                "flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
                meta.bg,
                meta.text,
                className
            )}
        >
            <span className="relative flex h-2 w-2">
                {meta.pulse && (
                    <span
                        className={cn(
                            "absolute inline-flex h-full w-full animate-ping rounded-full opacity-60",
                            meta.dot
                        )}
                    />
                )}
                <span className={cn("relative inline-flex h-2 w-2 rounded-full", meta.dot)} />
            </span>
            <span className="hidden sm:inline">
                {meta.label}
                {meta.stale && lastEventTime !== null && (
                    <span className="ml-1 opacity-70">({relativeTime(lastEventTime)})</span>
                )}
            </span>
        </div>
    );
}
