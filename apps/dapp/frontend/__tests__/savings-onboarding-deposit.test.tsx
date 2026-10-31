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

describe("SavingsOnboardingWizard deposit steps", () => {
  it("sends goal deposits to the savings rate dashboard, not the vault menu", async () => {
    const qc = new QueryClient();
    const { container } = render(
      <QueryClientProvider client={qc}>
        <SavingsOnboardingWizard onComplete={vi.fn()} onDismiss={vi.fn()} />
      </QueryClientProvider>
    );

    fireEvent.click(screen.getByText("Emergency Fund"));
    fireEvent.change(screen.getByLabelText(/target amount/i), { target: { value: "500" } });
    fireEvent.click(screen.getByRole("button", { name: /Create goal & continue/ }));

    const explore = await screen.findByRole("link", { name: /Nester Savings Rate/ });
    expect(explore.getAttribute("href")).toBe("/dashboard");

    fireEvent.click(screen.getByRole("button", { name: /Continue/ }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: /Deposit on the Savings Rate page/ }).getAttribute("href")).toBe(
        "/dashboard"
      )
    );
    for (const link of Array.from(container.querySelectorAll("a"))) {
      expect(link.getAttribute("href")).not.toMatch(/^\/vaults/);
    }
  });
});
