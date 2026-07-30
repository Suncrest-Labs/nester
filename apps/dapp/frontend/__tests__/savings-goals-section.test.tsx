import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { SavingsGoalsSection } from "@/components/savings/SavingsGoalsSection";
import type { SavingsGoal } from "@/lib/api/savings-goals";

const mockUseWallet = vi.fn();
const mockUseSavingsGoals = vi.fn();

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => mockUseWallet(),
}));

vi.mock("@/hooks/useSavingsGoals", () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const sampleGoals: SavingsGoal[] = [
  {
    id: "g1",
    description: "Holiday Fund",
    category: "travel",
    target_amount: 5000,
    current_amount: 1200,
    currency: "USDC",
    deadline: "2026-12-31T00:00:00Z",
    progress_pct: 24,
  },
];

describe("SavingsGoalsSection", () => {
  beforeEach(() => {
    mockUseWallet.mockReset();
    mockUseSavingsGoals.mockReset();
  });

  it("shows connect prompt when wallet is not connected", () => {
    mockUseWallet.mockReturnValue({ isConnected: false });
    mockUseSavingsGoals.mockReturnValue({ data: undefined, isLoading: false, isError: false });

    render(<SavingsGoalsSection />, { wrapper });

    expect(screen.getByTestId("savings-goals-connect-prompt")).toBeInTheDocument();
    expect(screen.queryByTestId("savings-goals-section")).not.toBeInTheDocument();
  });

  it("renders goals when wallet is connected", () => {
    mockUseWallet.mockReturnValue({ isConnected: true });
    mockUseSavingsGoals.mockReturnValue({
      data: sampleGoals,
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
      isFetching: false,
    });

    render(<SavingsGoalsSection />, { wrapper });

    expect(screen.getByTestId("savings-goals-section")).toBeInTheDocument();
    expect(screen.getByText("Holiday Fund")).toBeInTheDocument();
    expect(screen.getByTestId("savings-goal-card")).toBeInTheDocument();
  });

  it("shows empty state when user has no goals", () => {
    mockUseWallet.mockReturnValue({ isConnected: true });
    mockUseSavingsGoals.mockReturnValue({
      data: [],
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
      isFetching: false,
    });

    render(<SavingsGoalsSection />, { wrapper });

    expect(screen.getByTestId("savings-goals-empty")).toBeInTheDocument();
    expect(screen.getByText("No savings goals yet")).toBeInTheDocument();
  });
});
