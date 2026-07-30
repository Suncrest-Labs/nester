import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MaturityTimeline } from "@/components/savings/MaturityTimeline";
import type { LockedPosition } from "@/lib/types/progress";

const mockUseReducedMotion = vi.fn();
vi.mock("@/hooks/useReducedMotion", () => ({
  useReducedMotion: () => mockUseReducedMotion(),
}));

const lockedPositions: LockedPosition[] = [
  {
    id: "lock-1",
    amount: 80000,
    currency: "NGN",
    locked_at: "2026-01-15T00:00:00Z",
    matures_at: "2027-03-15T00:00:00Z",
    boost_percent: 2,
    yield_earned: 1600,
  },
  {
    id: "lock-2",
    amount: 40000,
    currency: "NGN",
    locked_at: "2026-02-01T00:00:00Z",
    matures_at: "2027-05-01T00:00:00Z",
    boost_percent: 1.5,
    yield_earned: 600,
  },
];

describe("MaturityTimeline", () => {
  beforeEach(() => {
    mockUseReducedMotion.mockReturnValue(false);
  });

  it("returns null when no positions provided", () => {
    const { container } = render(<MaturityTimeline positions={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders locked positions when provided", () => {
    render(<MaturityTimeline positions={lockedPositions} />);
    expect(screen.getByTestId("maturity-timeline")).toBeInTheDocument();
    expect(screen.getByText("Locked Positions")).toBeInTheDocument();
  });

  it("displays locked amounts and currencies", () => {
    render(<MaturityTimeline positions={lockedPositions} />);
    expect(screen.getByText(/80,000 NGN/)).toBeInTheDocument();
    expect(screen.getByText(/40,000 NGN/)).toBeInTheDocument();
  });

  it("displays boost percentages", () => {
    render(<MaturityTimeline positions={lockedPositions} />);
    expect(screen.getByText("+2%")).toBeInTheDocument();
    expect(screen.getByText("+1.5%")).toBeInTheDocument();
  });

  it("displays yield earned on each position", () => {
    render(<MaturityTimeline positions={lockedPositions} />);
    expect(screen.getByText("+1,600")).toBeInTheDocument();
    expect(screen.getByText("+600")).toBeInTheDocument();
  });

  it("sorts positions by maturity date", () => {
    render(<MaturityTimeline positions={lockedPositions} />);
    const items = screen.getAllByText(/Matures/);
    expect(items.length).toBe(2);
  });
});
