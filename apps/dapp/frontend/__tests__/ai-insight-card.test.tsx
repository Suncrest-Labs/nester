import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { InsightCard, getInsightDismissKey } from "@/components/ai/insightCard";
import { PrometheusInsightsCard } from "@/components/ai/prometheusInsightsCard";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ address: "GABC123456789" }),
}));

vi.mock("@/lib/api/intelligence", () => ({
  intelligence: {
    getPortfolioInsights: vi.fn().mockResolvedValue([
      {
        id: "insight-101",
        title: "High Yield Opportunity",
        body: "Consider switching to USDC Vault B for +1.5% APY.",
        confidence: 0.85,
      },
      {
        id: "insight-102",
        title: "Weekly Read",
        body: "Market yields remain stable.",
        confidence: 0.9,
      },
    ]),
  },
}));

describe("AI Insight Card Dismissal", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("persists dismissal of InsightCard to localStorage and hides card", () => {
    const key = getInsightDismissKey("insight-1", "Test Title");
    expect(localStorage.getItem(key)).toBeNull();

    const { rerender } = render(
      <InsightCard
        id="insight-1"
        title="Test Title"
        body="Test Body"
        confidence={0.9}
      />
    );

    expect(screen.getByText("Test Title")).toBeInTheDocument();

    const dismissBtn = screen.getByRole("button", { name: /dismiss insight/i });
    fireEvent.click(dismissBtn);

    expect(localStorage.getItem(key)).toBe("true");
    expect(screen.queryByText("Test Title")).not.toBeInTheDocument();

    // Rerender/remount confirms it remains hidden
    rerender(
      <InsightCard
        id="insight-1"
        title="Test Title"
        body="Test Body"
        confidence={0.9}
      />
    );
    expect(screen.queryByText("Test Title")).not.toBeInTheDocument();
  });

  it("persists dismissal of PrometheusInsightsCard to localStorage and hides card", async () => {
    render(<PrometheusInsightsCard />);

    const title = await screen.findByText("High Yield Opportunity");
    expect(title).toBeInTheDocument();

    const dismissBtn = screen.getByRole("button", { name: /dismiss insight/i });
    fireEvent.click(dismissBtn);

    expect(screen.queryByText("High Yield Opportunity")).not.toBeInTheDocument();
    expect(screen.getByText("Insight dismissed.")).toBeInTheDocument();

    const key = getInsightDismissKey("insight-101", "High Yield Opportunity");
    expect(localStorage.getItem(key)).toBe("true");
  });
});
