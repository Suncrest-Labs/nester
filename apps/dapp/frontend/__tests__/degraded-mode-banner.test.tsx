import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import {
    DegradedModeBanner,
    deriveDegradedBannerState,
    type PublicSystemStatus,
} from "@/components/degraded-mode-banner";

const baseHealthyStatus: PublicSystemStatus = {
    status: "ok",
    deposits_paused: false,
    withdrawals_paused: false,
    indexer_lag_seconds: 6,
    indexer_stale: false,
    rpc_healthy: true,
};

describe("deriveDegradedBannerState", () => {
    it("returns null when healthy", () => {
        expect(deriveDegradedBannerState(baseHealthyStatus, false)).toBeNull();
    });

    it("returns api_unreachable banner when API cannot be reached", () => {
        const res = deriveDegradedBannerState(null, true);
        expect(res?.kind).toBe("api_unreachable");
        expect(res?.message).toBe(
            "We're having trouble reaching Nester servers. On-chain funds are unaffected.",
        );
    });

    it("returns deposits_paused banner when deposits are paused", () => {
        const res = deriveDegradedBannerState(
            { ...baseHealthyStatus, status: "paused", deposits_paused: true },
            false,
        );
        expect(res?.kind).toBe("deposits_paused");
        expect(res?.message).toBe(
            "Deposits are temporarily paused for maintenance. Existing funds and withdrawals are unaffected.",
        );
    });

    it("returns withdrawals_paused banner when withdrawals are paused", () => {
        const res = deriveDegradedBannerState(
            { ...baseHealthyStatus, status: "paused", withdrawals_paused: true },
            false,
        );
        expect(res?.kind).toBe("withdrawals_paused");
        expect(res?.message).toBe(
            "Withdrawals are temporarily paused. Your funds remain on-chain.",
        );
    });

    it("returns indexer_lag banner when indexer lag exceeds 120 seconds", () => {
        const res = deriveDegradedBannerState(
            {
                ...baseHealthyStatus,
                status: "degraded",
                indexer_lag_seconds: 240,
                indexer_stale: true,
            },
            false,
        );
        expect(res?.kind).toBe("indexer_lag");
        expect(res?.message).toBe(
            "Displayed balances may be up to 4 minutes behind the chain.",
        );
    });
});

describe("DegradedModeBanner component", () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it("renders nothing when /api/v1/status reports healthy", async () => {
        vi.spyOn(global, "fetch").mockResolvedValueOnce(
            new Response(JSON.stringify({ success: true, data: baseHealthyStatus }), {
                status: 200,
            }),
        );

        const { container } = render(<DegradedModeBanner pollIntervalMs={0} />);
        await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(1));
        expect(container.querySelector("[data-testid='degraded-mode-banner']")).toBeNull();
    });

    it("renders deposits paused banner when API reports deposits_paused", async () => {
        vi.spyOn(global, "fetch").mockResolvedValueOnce(
            new Response(
                JSON.stringify({
                    success: true,
                    data: { ...baseHealthyStatus, status: "paused", deposits_paused: true },
                }),
                { status: 200 },
            ),
        );

        render(<DegradedModeBanner pollIntervalMs={0} />);
        const banner = await screen.findByTestId("degraded-mode-banner");
        expect(banner.getAttribute("data-banner-kind")).toBe("deposits_paused");
        expect(banner.textContent).toContain(
            "Deposits are temporarily paused for maintenance. Existing funds and withdrawals are unaffected.",
        );
    });

    it("renders API unreachable banner when fetch rejects", async () => {
        vi.spyOn(global, "fetch").mockRejectedValueOnce(new Error("network down"));

        render(<DegradedModeBanner pollIntervalMs={0} />);
        const banner = await screen.findByTestId("degraded-mode-banner");
        expect(banner.getAttribute("data-banner-kind")).toBe("api_unreachable");
        expect(banner.textContent).toContain(
            "We're having trouble reaching Nester servers. On-chain funds are unaffected.",
        );
    });
});
