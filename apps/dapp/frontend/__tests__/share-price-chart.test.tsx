import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const fetchSavingsHistory = vi.fn();
vi.mock("@/lib/api/savings", () => ({
  fetchSavingsHistory: (period: string) => fetchSavingsHistory(period),
}));

import { SharePriceChart } from "@/components/savings-rate/SharePriceChart";

function renderChart() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SharePriceChart />
    </QueryClientProvider>
  );
}

beforeEach(() => {
  fetchSavingsHistory.mockReset();
});

describe("SharePriceChart", () => {
  it("shows a quiet message with fewer than two snapshots", async () => {
    fetchSavingsHistory.mockResolvedValue([{ takenAt: "2026-11-01T10:00:00Z", sharePrice: "1.0001" }]);
    renderChart();

    expect(await screen.findByText("The chart fills in as hourly snapshots arrive.")).toBeTruthy();
    expect(fetchSavingsHistory).toHaveBeenCalledWith("30d");
  });

  it("charts the history and shows the latest share price", async () => {
    fetchSavingsHistory.mockResolvedValue([
      { takenAt: "2026-11-01T11:00:00Z", sharePrice: "1.0002" },
      { takenAt: "2026-11-01T10:00:00Z", sharePrice: "1.0001" },
    ]);
    renderChart();

    expect(await screen.findByTestId("share-price-chart")).toBeTruthy();
    expect(screen.getByText("1.000200")).toBeTruthy();
  });

  it("refetches for the selected period", async () => {
    fetchSavingsHistory.mockResolvedValue([]);
    renderChart();
    await screen.findByText("The chart fills in as hourly snapshots arrive.");

    fireEvent.click(screen.getByRole("button", { name: "7D" }));

    await waitFor(() => expect(fetchSavingsHistory).toHaveBeenCalledWith("7d"));
    expect(screen.getByRole("button", { name: "7D" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("says when history can't be loaded", async () => {
    fetchSavingsHistory.mockRejectedValue(new Error("down"));
    renderChart();

    expect(await screen.findByText(/Share price history couldn't be loaded/)).toBeTruthy();
  });
});
