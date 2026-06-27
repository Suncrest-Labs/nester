import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { DeadlineBadge } from "@/components/savings/DeadlineBadge";

const FUTURE = new Date(Date.now() + 1000 * 60 * 60 * 24 * 10).toISOString();

describe("DeadlineBadge", () => {
  it("renders an aria-label conveying days remaining", () => {
    render(<DeadlineBadge deadline={FUTURE} status="active" />);
    const badge = screen.getByRole("status");
    expect(badge.getAttribute("aria-label")).toMatch(/remaining until goal deadline/);
  });

  it("shows a tooltip with the full deadline date on focus", () => {
    render(<DeadlineBadge deadline={FUTURE} status="active" />);
    fireEvent.focus(screen.getByRole("status"));
    expect(screen.getByRole("tooltip").textContent).toMatch(/Goal deadline:/);
  });

  it("renders 'Goal Achieved' for completed goals", () => {
    render(<DeadlineBadge deadline="2026-01-01T00:00:00Z" status="completed" />);
    expect(screen.getByText("Goal Achieved")).toBeInTheDocument();
  });

  it("renders 'Overdue' for past, incomplete goals", () => {
    const past = new Date(Date.now() - 1000 * 60 * 60 * 24 * 5).toISOString();
    render(<DeadlineBadge deadline={past} status="active" />);
    expect(screen.getByText("Overdue")).toBeInTheDocument();
  });
});
