"use client";

import { notFound } from "next/navigation";
import { ConnectionStatusBadge } from "@/components/connection-status-badge";
import { LiveValue } from "@/components/live-value";
import { useWebSocketContext } from "@/components/websocket-provider";
import { E2E_HARNESS_ENABLED } from "@/lib/e2e-harness";

/**
 * WebSocket connection-state harness (E2E only).
 *
 * The production badge lives in the navbar behind a connected wallet, which
 * a browser-driven test cannot produce without a real wallet extension. This
 * route renders the same components against the same provider so the
 * reconnection behaviour can be asserted end to end.
 *
 * It is inert unless NEXT_PUBLIC_E2E_HARNESS is set, which the Playwright
 * webServer does and no deployed environment does.
 */
export default function ConnectionHarnessPage() {
    const { status, lastUpdatedAt } = useWebSocketContext();

    if (!E2E_HARNESS_ENABLED) notFound();

    return (
        <main className="p-8 space-y-6">
            <h1 className="text-xl font-medium">WebSocket connection harness</h1>

            <ConnectionStatusBadge />

            <p className="text-[42px] font-light leading-none">
                <LiveValue label="Total balance">$1,234.56</LiveValue>
            </p>

            <p data-testid="last-updated-at">
                {lastUpdatedAt === null ? "never" : String(lastUpdatedAt)}
            </p>
            <p data-testid="raw-status">{status}</p>
        </main>
    );
}
