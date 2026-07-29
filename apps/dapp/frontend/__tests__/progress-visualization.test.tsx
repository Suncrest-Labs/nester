import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ProgressVisualization } from "@/components/savings/ProgressVisualization";
import type { GoalProgressData } from "@/lib/types/progress";

const mockUseReducedMotion = vi.fn();
vi.mock("@/hooks/useReducedMotion", () => ({
  useReducedMotion: () => mockUseReducedMotion(),
}));

const baseProgress: GoalProgressData = {
  current_amount: 1200,
  target_amount: 5000,
  currency: "USDC",
  principal_amount: 1000,
  yield_amount: 200,
  locked_positions: [],
  flexible_amount: 1200,
  deadline: "2026-12-31T00:00:00Z",
  status: "active",
};

const progressWithLocks: GoalProgressData = {
  ...baseProgress,
  locked_positions: [
    {
      id: "lock-1",
      amount: 800,
      currency: "USDC",
      locked_at: "2026-01-01T00:00:00Z",
      matures_at: "2026-06-01T00:00:00Z",
      boost_percent: 2,
      yield_earned: 16,
    },
    {
      id: "lock-2",
      amount: 200,
      currency: "USDC",
      locked_at: "2026-03-01T00:00:00Z",
      matures_at: "2026-09-01T00:00:00Z",
      boost_percent: 1.5,
      yield_earned: 3,
    },
  ],
  flexible_amount: 200,
};

const progressWithProjection: GoalProgressData = {
  ...baseProgress,
  projection: {
    vault_id: "vault-1",
    currency: "USDC",
    current_apy: 8.5,
    timeline: [
      { date: "Jan", median: 1200, upper_bound: 1250, lower_bound: 1150 },
      { date: "Feb", median: 1600, upper_bound: 1700, lower_bound: 1500 },
      { date: "Mar", median: 2000, upper_bound: 2200, lower_bound: 1800 },
    ],
    success_probability: 85,
    on_track: true,
  },
};

const atRiskProgress: GoalProgressData = {
  ...baseProgress,
  projection: {
    vault_id: "vault-1",
    currency: "USDC",
    current_apy: 5,
    timeline: [
      { date: "Jan", median: 1200, upper_bound: 1250, lower_bound: 1150 },
      { date: "Feb", median: 1400, upper_bound: 1500, lower_bound: 1300 },
    ],
    success_probability: 35,
    on_track: false,
    monthly_gap: 8000,
  },
};

const completedProgress: GoalProgressData = {
  ...baseProgress,
  current_amount: 5000,
  target_amount: 5000,
  principal_amount: 4500,
  yield_amount: 500,
  status: "completed",
};

const emptyProgress: GoalProgressData = {
  ...baseProgress,
  current_amount: 0,
  principal_amount: 0,
  yield_amount: 0,
  flexible_amount: 0,
};

const progressWithComposition: GoalProgressData = {
  ...baseProgress,
  asset_composition: [
    { asset: "USDC", value: 800, percentage: 67, color: "#2775CA" },
    { asset: "XLM", value: 400, percentage: 33, color: "#000000" },
  ],
};

describe("ProgressVisualization", () => {
  it("renders the component with base progress data", () => {
    render(<ProgressVisualization progress={baseProgress} />);
    expect(screen.getByTestId("progress-visualization")).toBeInTheDocument();
    expect(screen.getByText("24% complete")).toBeInTheDocument();
    expect(screen.getByText(/1,200 USDC/)).toBeInTheDocument();
  });

  it("renders principal and yield breakdown when yield is present", () => {
    render(<ProgressVisualization progress={progressWithLocks} />);
    expect(screen.getByText(/Principal/)).toBeInTheDocument();
    expect(screen.getByText(/Yield earned/)).toBeInTheDocument();
  });

  it("renders locked positions with maturity dates and boost", () => {
    render(<ProgressVisualization progress={progressWithLocks} />);
    expect(screen.getByTestId("maturity-timeline")).toBeInTheDocument();
    expect(screen.getByText("+2%")).toBeInTheDocument();
    expect(screen.getByText("+1.5%")).toBeInTheDocument();
  });

  it("renders locked/flexible segmented progress bar", () => {
    render(<ProgressVisualization progress={progressWithLocks} />);
    expect(screen.getByText(/Flexible/)).toBeInTheDocument();
    expect(screen.getByText(/Locked \d+%/)).toBeInTheDocument();
  });

  it("renders a projection band with success probability", () => {
    render(<ProgressVisualization progress={progressWithProjection} />);
    expect(screen.getByTestId("projection-band")).toBeInTheDocument();
    expect(screen.getByText("85% likely")).toBeInTheDocument();
  });

  it("renders constructive at-risk message for goals not on track", () => {
    render(<ProgressVisualization progress={atRiskProgress} />);
    expect(screen.getByText(/add 8,000 USDC\/month/i)).toBeInTheDocument();
  });

  it("renders celebration state for completed goal", () => {
    render(<ProgressVisualization progress={completedProgress} />);
    expect(screen.getByText("Goal completed!")).toBeInTheDocument();
  });

  it("renders encouraging start state for new goal at 0%", () => {
    render(<ProgressVisualization progress={emptyProgress} />);
    expect(screen.getByText(/Start saving/i)).toBeInTheDocument();
  });

  it("renders multi-asset composition when provided", () => {
    render(<ProgressVisualization progress={progressWithComposition} />);
    expect(screen.getByTestId("multi-asset-composition")).toBeInTheDocument();
    expect(screen.getByText("USDC")).toBeInTheDocument();
    expect(screen.getByText("XLM")).toBeInTheDocument();
  });

  it("renders compact mode without extra breakdowns", () => {
    render(<ProgressVisualization progress={progressWithLocks} compact />);
    expect(screen.getByTestId("progress-visualization")).toBeInTheDocument();
    expect(screen.queryByText("Principal")).not.toBeInTheDocument();
    expect(screen.queryByTestId("maturity-timeline")).not.toBeInTheDocument();
  });

  it("respects reduced motion preferences", () => {
    mockUseReducedMotion.mockReturnValue(true);
    render(<ProgressVisualization progress={baseProgress} />);
    expect(screen.getByTestId("progress-visualization")).toBeInTheDocument();
  });
});
