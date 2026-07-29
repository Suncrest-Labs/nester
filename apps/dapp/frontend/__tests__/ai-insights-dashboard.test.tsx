import { describe, it, expect, beforeEach, vi } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { InsightsDashboard } from "@/components/ai/insights-dashboard"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ address: "GABC123456789", isConnected: true }),
}))

vi.mock("@/components/auth-provider", () => ({
  useAuth: () => ({ userId: "user-123", isAuthenticated: true }),
}))

vi.mock("@/lib/api/intelligence", () => ({
  intelligence: {
    getPortfolioInsights: vi.fn().mockResolvedValue([
      {
        id: "insight-1",
        title: "High Yield Opportunity",
        body: "Consider switching to USDC Vault B for +1.5% APY.",
        confidence: 0.85,
        action: { label: "View Vault", href: "/vaults/usdc-b" },
      },
      {
        id: "insight-2",
        title: "Rebalance Suggested",
        body: "Your allocation is overweight in stablecoins.",
        confidence: 0.72,
        action: { label: "Review Allocation", href: "/dashboard" },
      },
    ]),
    getMarketSentiment: vi.fn().mockResolvedValue({
      signal: "bull" as const,
      summary: "Market conditions are favorable.",
      confidence: 0.78,
      updatedAt: new Date().toISOString(),
    }),
    coaching: vi.fn().mockRejectedValue(new Error("Not implemented")),
  },
}))

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe("InsightsDashboard", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("renders prioritized insights with actions", async () => {
    renderWithQuery(<InsightsDashboard />)

    await waitFor(() => {
      expect(screen.getByText("High Yield Opportunity")).toBeInTheDocument()
    })

    expect(screen.getByText("Consider switching to USDC Vault B for +1.5% APY.")).toBeInTheDocument()
    const actionLink = screen.getByText("View Vault")
    expect(actionLink).toBeInTheDocument()
    expect(actionLink.closest("a")).toHaveAttribute("href", "/vaults/usdc-b")
  })

  it("renders loading state initially", () => {
    renderWithQuery(<InsightsDashboard />)
    const skeletons = document.querySelectorAll(".animate-pulse")
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it("renders empty state when no insights", async () => {
    const { intelligence } = await import("@/lib/api/intelligence")
    vi.mocked(intelligence.getPortfolioInsights).mockResolvedValueOnce([])

    renderWithQuery(<InsightsDashboard />)

    await waitFor(() => {
      expect(screen.getByText("No insights yet")).toBeInTheDocument()
    })
  })

  it("renders error state when API fails", async () => {
    const { intelligence } = await import("@/lib/api/intelligence")
    vi.mocked(intelligence.getPortfolioInsights).mockRejectedValueOnce(new Error("API Error"))

    renderWithQuery(<InsightsDashboard />)

    await waitFor(() => {
      expect(screen.getByText("AI service temporarily unavailable")).toBeInTheDocument()
    })
  })

  it("dismisses an insight and does not immediately reappear", async () => {
    renderWithQuery(<InsightsDashboard />)

    await waitFor(() => {
      expect(screen.getByText("High Yield Opportunity")).toBeInTheDocument()
    })

    const dismissButtons = screen.getAllByRole("button", { name: /dismiss insight/i })
    fireEvent.click(dismissButtons[0])

    await waitFor(() => {
      expect(screen.queryByText("High Yield Opportunity")).not.toBeInTheDocument()
    })
  })

  it("shows connect wallet prompt when no address", () => {
    vi.mocked(require("@/components/wallet-provider").useWallet).mockReturnValueOnce({
      address: null,
      isConnected: false,
    })

    renderWithQuery(<InsightsDashboard />)
    expect(screen.getByText("Connect your wallet to see AI insights")).toBeInTheDocument()
  })

  it("renders market sentiment when available", async () => {
    renderWithQuery(<InsightsDashboard />)

    await waitFor(() => {
      expect(screen.getByText("High Yield Opportunity")).toBeInTheDocument()
    })
  })
})
