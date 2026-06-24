import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useSavingsChartData } from "@/hooks/useSavingsChartData";
import { vaultsApi } from "@/lib/api/vaults";

vi.mock("@/lib/api/vaults", () => ({
    vaultsApi: { getApyHistory: vi.fn() },
}));

const getApyHistory = vi.mocked(vaultsApi.getApyHistory);

function wrapper({ children }: { children: ReactNode }) {
    const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useSavingsChartData", () => {
    beforeEach(() => {
        getApyHistory.mockReset();
    });

    it("is disabled and returns no data when vaultId is undefined", () => {
        const { result } = renderHook(() => useSavingsChartData(undefined), { wrapper });
        expect(result.current.fetchStatus).toBe("idle");
        expect(getApyHistory).not.toHaveBeenCalled();
    });

    it("maps API points into chart-ready points (fraction -> percentage)", async () => {
        getApyHistory.mockResolvedValue({
            vault_id: "v1",
            period: "30d",
            points: [
                { timestamp: "2026-06-15T00:00:00Z", apy: 0.0823 },
                { timestamp: "2026-06-16T00:00:00Z", apy: 0.091 },
            ],
        });

        const { result } = renderHook(() => useSavingsChartData("v1", "30d"), { wrapper });
        await waitFor(() => expect(result.current.isSuccess).toBe(true));

        expect(getApyHistory).toHaveBeenCalledWith("v1", "30d");
        expect(result.current.data).toEqual([
            { date: "Jun 15", apy: "8.23", value: 8.23 },
            { date: "Jun 16", apy: "9.10", value: 9.1 },
        ]);
    });

    it("returns an empty array when the vault has no snapshots", async () => {
        getApyHistory.mockResolvedValue({ vault_id: "v1", period: "7d", points: [] });
        const { result } = renderHook(() => useSavingsChartData("v1", "7d"), { wrapper });
        await waitFor(() => expect(result.current.isSuccess).toBe(true));
        expect(result.current.data).toEqual([]);
    });

    it("re-fetches when the period changes", async () => {
        getApyHistory.mockResolvedValue({ vault_id: "v1", period: "90d", points: [] });
        const { rerender } = renderHook(
            ({ p }: { p: "7d" | "30d" | "90d" }) => useSavingsChartData("v1", p),
            { wrapper, initialProps: { p: "30d" as const } }
        );
        await waitFor(() => expect(getApyHistory).toHaveBeenCalledWith("v1", "30d"));

        rerender({ p: "90d" });
        await waitFor(() => expect(getApyHistory).toHaveBeenCalledWith("v1", "90d"));
    });
});
