import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@/lib/api/savings-goals", () => ({
  savingsGoals: { create: vi.fn(() => Promise.resolve({ id: "g1" })) },
}));
vi.mock("@/hooks/useYieldOpportunities", () => ({
  useYieldOpportunities: () => ({ data: { pools: [] }, isLoading: false }),
}));

import { SavingsOnboardingWizard } from "@/components/savings/SavingsOnboardingWizard";

async function reachYieldStep(currency?: "XLM") {
  const qc = new QueryClient();
  const onComplete = vi.fn();
  const view = render(
    <QueryClientProvider client={qc}>
      <SavingsOnboardingWizard onComplete={onComplete} onDismiss={vi.fn()} />
    </QueryClientProvider>
  );
  fireEvent.click(screen.getByText("Emergency Fund"));
  fireEvent.change(screen.getByLabelText(/target amount/i), { target: { value: "500" } });
  if (currency) fireEvent.click(screen.getByRole("button", { name: currency }));
  fireEvent.click(screen.getByRole("button", { name: /Create goal & continue/ }));
  await screen.findByText(/don.t add to its progress yet/);
  return { ...view, onComplete };
}

describe("SavingsOnboardingWizard after the goal is created", () => {
  it("says savings deposits don't count toward the goal", async () => {
    const { container } = await reachYieldStep();

    expect(screen.queryByText(/Goal deposits go into/)).toBeNull();
    expect(screen.queryByText(/Link your goal/)).toBeNull();
    expect(screen.getByRole("link", { name: /See the Nester Savings Rate/ }).getAttribute("href")).toBe(
      "/dashboard"
    );
    for (const link of Array.from(container.querySelectorAll("a"))) {
      expect(link.getAttribute("href")).not.toMatch(/^\/vaults/);
    }
  });

  it("presents the savings rate as separate from the goal, not as a first goal deposit", async () => {
    const { onComplete } = await reachYieldStep("XLM");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => expect(screen.getByText(/separate from this goal/)).toBeTruthy());
    expect(screen.queryByText(/Make your first deposit/)).toBeNull();
    expect(screen.queryByText(/Kick-start your goal/)).toBeNull();
    expect(screen.queryByText(/XLM/)).toBeNull();
    expect(screen.getByRole("link", { name: /Open the Savings Rate/ }).getAttribute("href")).toBe("/dashboard");

    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onComplete).toHaveBeenCalled();
  });
});
