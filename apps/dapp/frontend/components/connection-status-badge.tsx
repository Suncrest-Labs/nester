"use client";

import { useWebSocketContext } from "@/components/websocket-provider";
import { useRelativeAge } from "@/hooks/useRelativeAge";
import { type WSConnectionStatus } from "@/lib/ws-events";
import { cn } from "@/lib/utils";

interface StatusMeta {
    dot: string;
    text: string;
    bg: string;
    label: string;
    title: string;
    pulse: boolean;
}

// Visual + copy for each connection state.
//
// The distinction that matters is "live" vs "not live": in either non-live
// state the number on screen is a remembered value, not a current one, and
// the badge has to say so rather than implying the feed is merely slow.
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
            };
        case "reconnecting":
            return {
                dot: "bg-amber-500",
                text: "text-amber-700",
                bg: "bg-amber-50 border-amber-200",
                label: "Reconnecting…",
                title: "Connection lost — retrying. Values shown are not live.",
                pulse: true,
            };
        case "offline":
        default:
            return {
                dot: "bg-red-500",
                text: "text-red-700",
                bg: "bg-red-50 border-red-200",
                label: "Disconnected",
                title:
                    "Disconnected — values shown are not live. Falling back to periodic refresh.",
                pulse: false,
            };
    }
}

/**
 * Compact connection-status badge for the app header.
 *
 * Reflects the live WebSocket state:
 *   - green "Live" when connected
 *   - amber "Reconnecting…" while backing off between attempts
 *   - red "Disconnected" once retries are exhausted
 *
 * In both non-live states it also reports when the data was last confirmed
 * current, so a frozen view is legible as frozen.
 */
export function ConnectionStatusBadge({ className }: { className?: string }) {
    const { status, lastUpdatedAt, manualReconnect } = useWebSocketContext();
    const meta = statusMeta(status);
    const isStale = status !== "connected";
    const age = useRelativeAge(isStale ? lastUpdatedAt : null, isStale);

    return (
        <div
            role="status"
            aria-live="polite"
            data-testid="connection-status"
            data-status={status}
            title={age ? `${meta.title} Last updated ${age}.` : meta.title}
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
            <span className="hidden sm:inline">{meta.label}</span>
            {age && (
                <span className="hidden sm:inline font-normal opacity-75" data-testid="connection-last-updated">
                    · updated {age}
                </span>
            )}
            {status === "offline" && (
                // Retries are deliberately bounded, so give the user the way
                // back rather than making a page reload the only option.
                <button
                    type="button"
                    onClick={manualReconnect}
                    data-testid="connection-retry"
                    className="ml-0.5 rounded-full px-1.5 underline underline-offset-2 hover:no-underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1"
                >
                    Retry
                </button>
            )}
        </div>
    );
}
