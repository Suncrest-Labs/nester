import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
      ],
      meta: { stale: false },
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    isFetching: false,
  }),
}));

import YieldsPage from "@/app/yields/page";
import { retiredRouteRedirects } from "@/lib/retired-routes";

describe("yields page", () => {
  it("lists market pools without a vault deposit action", () => {
    render(<YieldsPage />);

    expect(screen.getAllByTestId("yield-card")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Deposit" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Build allocation/ })).toBeNull();
  });

  it("points to the Savings Rate and is honest about venues", () => {
    render(<YieldsPage />);

    expect(screen.getByText("Earn with the Nester Savings Rate")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open the Savings Rate" }).getAttribute("href")).toBe("/dashboard");
    expect(screen.getByText(/market data, not where your deposit goes/)).toBeTruthy();
  });

  it("is not a retired route", () => {
    expect(retiredRouteRedirects.some((r) => r.source.startsWith("/yields"))).toBe(false);
  });
});
