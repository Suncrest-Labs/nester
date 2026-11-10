import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

vi.mock("@/components/app-shell", () => ({
  AppShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ isConnected: false, address: null }),
}));
vi.mock("@/hooks/useYields", () => ({
  useYields: () => ({
    data: {
      data: [
        { pool: "p1", project: "blend", symbol: "USDC", apy: 5.2, tvlUsd: 2_000_000, riskScore: 10 },
        { pool: "p2", project: "defindex", symbol: "XLM", apy: 8.1, tvlUsd: 500_000, riskScore: 40 },
      ],
      meta: { stale: false },
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    isFetching: false,
  }),
}));

const savingsState: { summary: { rate7d: string | null } | undefined; isLoading: boolean } = {
  summary: { rate7d: "5.35" },
  isLoading: false,
};
vi.mock("@/hooks/useSavings", () => ({
  useSavings: () => savingsState,
}));

import YieldsPage from "@/app/yields/page";
import { retiredRouteRedirects } from "@/lib/retired-routes";

describe("yields page", () => {
  it("lists market pools without a vault deposit action", () => {
    render(<YieldsPage />);

    expect(screen.getAllByTestId("yield-card")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Deposit" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Build allocation/ })).toBeNull();
  });

  it("pins the Nester Savings Rate for comparison and is honest about venues", () => {
    savingsState.summary = { rate7d: "5.35" };
    render(<YieldsPage />);

    const cta = screen.getByTestId("savings-rate-cta");
    expect(cta.textContent).toContain("5.35%");
    expect(screen.getByRole("link", { name: "Save with Nester" }).getAttribute("href")).toBe("/dashboard");
    expect(screen.getByText(/for comparison, not where your deposit goes/)).toBeTruthy();
  });

  it("says when there is not enough rate history", () => {
    savingsState.summary = { rate7d: null };
    render(<YieldsPage />);

    expect(screen.getByText("Not enough history yet")).toBeTruthy();
  });

  it("hides the rate when the savings summary is unavailable", () => {
    savingsState.summary = undefined;
    render(<YieldsPage />);

    expect(screen.getByTestId("savings-rate-cta").textContent).not.toContain("%");
    expect(screen.getByRole("link", { name: "Save with Nester" })).toBeTruthy();
  });

  it("sorts rows by APY and TVL from the column headers", () => {
    savingsState.summary = { rate7d: "5.35" };
    render(<YieldsPage />);

    const names = () => screen.getAllByTestId("yield-card").map((r) => r.textContent ?? "");
    expect(names()[0]).toContain("defindex");

    const tvl = screen.getByRole("button", { name: "TVL" });
    fireEvent.click(tvl);
    expect(tvl.closest("[role=columnheader]")?.getAttribute("aria-sort")).toBe("descending");
    expect(names()[0]).toContain("blend");

    fireEvent.click(tvl);
    expect(tvl.closest("[role=columnheader]")?.getAttribute("aria-sort")).toBe("ascending");
    expect(names()[0]).toContain("defindex");
  });

  it("is not a retired route", () => {
    expect(retiredRouteRedirects.some((r) => r.source.startsWith("/yields"))).toBe(false);
  });
});
