import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { SavingsPosition, SavingsSummary } from "@/lib/api/savings";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  usePathname: () => "/dashboard",
}));
vi.mock("@/components/app-shell", () => ({
  AppShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

const wallet = { isConnected: true, address: "GAWALLET" };
vi.mock("@/components/wallet-provider", () => ({ useWallet: () => wallet }));

const auth = { isAuthenticated: true, isSigningIn: false, signIn: vi.fn(() => Promise.resolve()) };
vi.mock("@/components/auth-provider", () => ({ useAuth: () => auth }));

let savings: {
  summary?: SavingsSummary;
  position?: SavingsPosition;
  isLoading: boolean;
  error: Error | null;
};
vi.mock("@/hooks/useSavings", () => ({ useSavings: () => savings }));

vi.mock("@/lib/stellar/savings", () => ({
  fetchDepositsPaused: vi.fn(() => Promise.resolve(false)),
  quoteMinShares: vi.fn(),
  quoteMinAssets: vi.fn(),
  executeSavingsDeposit: vi.fn(),
  executeSavingsRedeem: vi.fn(),
}));

import Dashboard from "@/app/dashboard/page";

const populated: SavingsSummary = {
  rate7d: "5.35",
  rate30d: "4.90",
  adapters: [
    { address: "CBLEND", name: "Blend", value: "600.00", absCap: "1000.00", relCapBps: 6000, share: "0.6" },
    { address: "CDEFI", name: "DeFindex", value: "300.00", absCap: "500.00", relCapBps: 4000, share: "0.3" },
  ],
  pending: [{ id: 1, kind: "RaiseCap", eta: Math.floor(Date.now() / 1000) + 86_400 }],
  reserve: { shares: "10", value: "20.00" },
  sharePrice: "1.0123",
  totalAssets: "1000.00",
  idle: "100.00",
  feeBps: 1000,
  timelockSeconds: 259_200,
};

const fresh: SavingsSummary = {
  ...populated,
  rate7d: null,
  rate30d: null,
  adapters: [],
  pending: [],
  reserve: { shares: "0", value: "0" },
  sharePrice: "1",
  totalAssets: "0",
  idle: "0",
};

function renderDashboard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Dashboard />
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  wallet.isConnected = true;
  auth.isAuthenticated = true;
  savings = {
    summary: populated,
    position: { shares: "250", value: "253.07", deposited: null },
    isLoading: false,
    error: null,
  };
});

describe("Dashboard", () => {
  it("dashboard renders savings rate layout", () => {
    const { container } = renderDashboard();

    expect(screen.getByRole("heading", { level: 1, name: "Nester Savings Rate" })).toBeTruthy();
    expect(screen.getByText("5.35%")).toBeTruthy();
    expect(screen.getByRole("tablist", { name: "Savings action" })).toBeTruthy();
    expect(screen.getByText("Your savings")).toBeTruthy();
    expect(screen.getByText("$253.07")).toBeTruthy();
    expect(screen.getByText("Where the money sits")).toBeTruthy();
    expect(screen.getByText("Blend")).toBeTruthy();
    expect(screen.getByText("DeFindex")).toBeTruthy();
    expect(screen.getByText("Reserve")).toBeTruthy();
    expect(screen.getByText("Pending changes")).toBeTruthy();
    expect(screen.getByText("10% of gains above the previous high")).toBeTruthy();
    expect(screen.getByText("3 days")).toBeTruthy();

    const goals = screen.getByRole("link", { name: "Set a goal" });
    expect(goals.getAttribute("href")).toBe("/savings");

    for (const link of Array.from(container.querySelectorAll("a"))) {
      expect(link.getAttribute("href")).not.toMatch(/^\/(vaults|portfolio|stocks)/);
    }
  });

  it("renders fresh-deploy empty states", () => {
    savings.summary = fresh;
    savings.position = { shares: "0", value: "0", deposited: null };
    renderDashboard();

    expect(screen.getAllByText("Not enough history yet").length).toBeGreaterThan(0);
    expect(screen.getByText("Idle")).toBeTruthy();
    expect(screen.queryByText("Pending changes")).toBeNull();
    expect(screen.getByText(/No changes are queued/)).toBeTruthy();
  });

  it("asks a signed-out wallet to sign in instead of showing a position", () => {
    auth.isAuthenticated = false;
    savings.position = undefined;
    renderDashboard();

    expect(screen.queryByText("Your savings")).toBeNull();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
    expect(screen.getByText("5.35%")).toBeTruthy();
  });

  it("shows loading placeholders while the summary loads", () => {
    savings = { isLoading: true, error: null };
    renderDashboard();

    expect(screen.getByTestId("savings-loading")).toBeTruthy();
    expect(screen.queryByRole("tablist")).toBeNull();
  });

  it("explains when savings data can't be loaded", () => {
    savings = { isLoading: false, error: new Error("boom") };
    renderDashboard();

    expect(screen.getByRole("alert").textContent).toMatch(/Savings data couldn't be loaded/);
  });

  it("sends a disconnected visitor to the landing page", () => {
    wallet.isConnected = false;
    const { container } = renderDashboard();

    expect(push).toHaveBeenCalledWith("/");
    expect(container.textContent).toBe("");
  });
});
