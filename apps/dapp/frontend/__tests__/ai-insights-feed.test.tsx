import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AiInsightsFeed, insightKey } from "@/components/ai/ai-insights-feed";
import { buildActionHref, adaptLegacyAction } from "@/lib/api/insights-actions";

// ── Mocks ─────────────────────────────────────────────────────────────────────

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GABC123MOCKADDRESS",
    isConnected: true,
  }),
}));

const dismissInsightMock = vi.fn().mockResolvedValue({ ok: true });
const getInsightsMock = vi.fn();

vi.mock("@/lib/api/intelligence", () => ({
  intelligence: {
    getPortfolioInsights: (...args: unknown[]) => getInsightsMock(...args),
    dismissInsight: (...args: unknown[]) => dismissInsightMock(...args),
  },
}));

// jsdom doesn't ship URLSearchParams en/decodeURIComponent behaviour for some
// edge cases — the production code uses these, so we polyfill defensively.

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderFeed(ui: React.ReactElement) {
  const qc = makeQueryClient();
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const sampleInsights = [
  {
    title: "Move $500 from Flexible to Locked 90d",
    body: "Doing so would lift your realised APY from 4.1% to 6.8% with no liquidity cost over your goal horizon.",
    confidence: 0.82,
    action: { label: "Lock 90d", href: "/vaults/v-1/lock" },
  },
  {
    title: "Top up your emergency goal",
    body: "You are 2 weeks behind the schedule Prometheus set last month.",
    confidence: 0.66,
    action: { label: "Deposit $50", href: "/vaults/v-2/deposit?amount=50" },
  },
  {
    title: "Consider a rebalance",
    body: "Your Stellar allocation has drifted 8% above target.",
    confidence: 0.74,
    action: { label: "Review", href: "/vaults/v-3/rebalance" },
  },
];

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("AiInsightsFeed", () => {
  beforeEach(() => {
    getInsightsMock.mockReset();
    dismissInsightMock.mockClear();
    window.localStorage.clear();
  });

  it("renders skeletons during loading state", () => {
    getInsightsMock.mockImplementation(
      () => new Promise(() => { /* never resolves */ }),
    );
    renderFeed(<AiInsightsFeed limit={2} />);
    // aria-busy wrapper + 2 skeleton rows
    expect(screen.getAllByRole("listitem").length).toBeGreaterThanOrEqual(2);
  });

  it("renders insights and generates correct localised action URLs", async () => {
    getInsightsMock.mockResolvedValueOnce(sampleInsights);
    renderFeed(<AiInsightsFeed />);

    expect(await screen.findByText("Move $500 from Flexible to Locked 90d")).toBeInTheDocument();
    expect(screen.getByText("Top up your emergency goal")).toBeInTheDocument();

    const lockLink = screen.getByRole("link", { name: /Lock 90d/ });
    expect(lockLink.getAttribute("href")).toBe("/vaults/v-1/lock");

    const depositLink = screen.getByRole("link", { name: /Deposit \$50/ });
    expect(depositLink.getAttribute("href")).toBe("/vaults/v-2/deposit?amount=50");

    const rebalanceLink = screen.getByRole("link", { name: /Review/ });
    expect(rebalanceLink.getAttribute("href")).toBe("/vaults/v-3/rebalance");
  });

  it("dismissing an insight removes it from view and calls localStorage + engine", async () => {
    getInsightsMock.mockResolvedValueOnce(sampleInsights);
    renderFeed(<AiInsightsFeed />);

    const title = await screen.findByText("Move $500 from Flexible to Locked 90d");
    const article = title.closest("article")!;
    const dismissBtn = article.querySelector('button[aria-label^="Dismiss"]')!;
    fireEvent.click(dismissBtn);

    await waitFor(() => {
      expect(screen.queryByText("Move $500 from Flexible to Locked 90d")).not.toBeInTheDocument();
    });

    // localStorage was written
    const stored = Object.keys(window.localStorage).filter((k) =>
      k.startsWith("nester_insight_dismissed:"),
    );
    expect(stored.length).toBeGreaterThanOrEqual(1);

    // Engine round-trip was attempted (fire-and-forget)
    await waitFor(() => {
      expect(dismissInsightMock).toHaveBeenCalledTimes(1);
    });
  });

  it("shows empty state when AI returns no insights", async () => {
    getInsightsMock.mockResolvedValueOnce([]);
    renderFeed(<AiInsightsFeed />);
    expect(
      await screen.findByText(/No AI insights generated yet/i),
    ).toBeInTheDocument();
  });

  it("renders AI-down fallback when the intelligence API fails", async () => {
    getInsightsMock.mockRejectedValueOnce(new Error("network down"));
    renderFeed(<AiInsightsFeed />);
    expect(
      await screen.findByText(/Intelligence service unavailable/i),
    ).toBeInTheDocument();
  });

  it("limits the feed to the requested number of insights on the dashboard", async () => {
    getInsightsMock.mockResolvedValueOnce(sampleInsights);
    renderFeed(<AiInsightsFeed limit={1} />);
    await screen.findByText("Move $500 from Flexible to Locked 90d");
    // Second insight should NOT appear, and a 'See all' link should.
    expect(screen.queryByText("Top up your emergency goal")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /See all 3 insights/ })).toBeInTheDocument();
  });

  it("respects dismissed insights on a remount", async () => {
    // Compute the actual insight key for the second insight so the test
    // exercises the same hash that the Feed uses internally.
    const dismissedKey = insightKey(
      sampleInsights[1],
      "GABC123MOCKADDRESS",
    );
    window.localStorage.setItem(
      `nester_insight_dismissed:${dismissedKey}`,
      "1",
    );
    getInsightsMock.mockResolvedValueOnce(sampleInsights);
    renderFeed(<AiInsightsFeed />);
    await screen.findByText("Move $500 from Flexible to Locked 90d");
    expect(screen.queryByText("Top up your emergency goal")).not.toBeInTheDocument();
  });
});

// ── Action URL builder (pure) ────────────────────────────────────────────────

describe("buildActionHref", () => {
  it("deposit with amount", () => {
    expect(
      buildActionHref({
        kind: "deposit",
        label: "Deposit $100",
        vaultId: "v1",
        amount: "100",
      }),
    ).toBe("/vaults/v1/deposit?amount=100");
  });

  it("lock without params", () => {
    expect(
      buildActionHref({ kind: "lock", label: "Lock", vaultId: "v2" }),
    ).toBe("/vaults/v2/lock");
  });

  it("schedule with target + horizon", () => {
    expect(
      buildActionHref({
        kind: "schedule",
        label: "Plan",
        targetUsdc: "5000",
        horizonMonths: 12,
      }),
    ).toBe("/savings-plan?target=5000&horizon=12");
  });

  it("vault link", () => {
    expect(
      buildActionHref({ kind: "vault", label: "Open", vaultId: "v3" }),
    ).toBe("/vaults/v3");
  });

  it("rebalance link", () => {
    expect(
      buildActionHref({
        kind: "rebalance",
        label: "Rebalance",
        vaultId: "v4",
      }),
    ).toBe("/vaults/v4/rebalance");
  });

  it("generic url fallback", () => {
    expect(
      buildActionHref({
        kind: "url",
        label: "External",
        href: "https://docs.example.com",
      }),
    ).toBe("https://docs.example.com");
  });

  it("encodes special characters in vaultId", () => {
    expect(
      buildActionHref({
        kind: "vault",
        label: "Open",
        vaultId: "vault with spaces",
      }),
    ).toBe("/vaults/vault%20with%20spaces");
  });
});

// ── Legacy adapter ────────────────────────────────────────────────────────────

describe("adaptLegacyAction", () => {
  it("parses /vaults/{id}/lock", () => {
    expect(
      adaptLegacyAction({ label: "Lock", href: "/vaults/v-1/lock" }),
    ).toEqual({ kind: "lock", label: "Lock", vaultId: "v-1" });
  });

  it("parses /vaults/{id}/deposit with amount", () => {
    expect(
      adaptLegacyAction({
        label: "Deposit $50",
        href: "/vaults/v-2/deposit?amount=50",
      }),
    ).toEqual({ kind: "deposit", label: "Deposit $50", vaultId: "v-2", amount: "50" });
  });

  it("parses /vaults/{id}/rebalance", () => {
    expect(
      adaptLegacyAction({ label: "Review", href: "/vaults/v-3/rebalance" }),
    ).toEqual({ kind: "rebalance", label: "Review", vaultId: "v-3" });
  });

  it("falls back to url for unknown shapes", () => {
    expect(
      adaptLegacyAction({ label: "Docs", href: "https://docs.example.com" }),
    ).toEqual({ kind: "url", label: "Docs", href: "https://docs.example.com" });
  });

  it("returns undefined when no action is provided", () => {
    expect(adaptLegacyAction(undefined)).toBeUndefined();
  });
});
