import { describe, it, expect } from "vitest";
import {
  daysRemaining,
  isPastDeadline,
  getDeadlineBadgeInfo,
} from "@/lib/savings/deadline";

const NOW = new Date("2026-06-24T12:00:00Z");

describe("daysRemaining", () => {
  it("returns the correct number of whole days for a future deadline", () => {
    expect(daysRemaining("2026-07-24T12:00:00Z", NOW)).toBe(30);
  });

  it("rounds up partial days", () => {
    expect(daysRemaining("2026-06-25T01:00:00Z", NOW)).toBe(1);
  });

  it("returns 0 when the deadline is exactly now", () => {
    expect(daysRemaining("2026-06-24T12:00:00Z", NOW)).toBe(0);
  });

  it("never returns a negative number for a past deadline", () => {
    expect(daysRemaining("2026-06-01T12:00:00Z", NOW)).toBe(0);
  });
});

describe("isPastDeadline", () => {
  it("returns false for a future deadline", () => {
    expect(isPastDeadline("2026-07-01T00:00:00Z", NOW)).toBe(false);
  });

  it("returns true for a past deadline", () => {
    expect(isPastDeadline("2026-01-01T00:00:00Z", NOW)).toBe(true);
  });
});

describe("getDeadlineBadgeInfo", () => {
  it("returns a green badge for > 30 days remaining", () => {
    const info = getDeadlineBadgeInfo("2026-08-24T12:00:00Z", "active", NOW);
    expect(info.variant).toBe("green");
    expect(info.label).toBe("61 days left");
  });

  it("returns an amber badge for 7-30 days remaining", () => {
    const info = getDeadlineBadgeInfo("2026-07-10T12:00:00Z", "active", NOW);
    expect(info.variant).toBe("amber");
    expect(info.label).toBe("16 days left");
  });

  it("returns a red badge for 1-6 days remaining", () => {
    const info = getDeadlineBadgeInfo("2026-06-27T12:00:00Z", "active", NOW);
    expect(info.variant).toBe("red");
    expect(info.label).toBe("3 days left");
  });

  it("returns a due-today badge when 0 days remain and not past", () => {
    const info = getDeadlineBadgeInfo("2026-06-24T12:00:00Z", "active", NOW);
    expect(info.variant).toBe("due-today");
    expect(info.label).toBe("Due today");
  });

  it("returns an overdue badge for a past deadline that is not completed", () => {
    const info = getDeadlineBadgeInfo("2026-06-01T00:00:00Z", "active", NOW);
    expect(info.variant).toBe("overdue");
    expect(info.label).toBe("Overdue");
  });

  it("returns a completed badge for a completed goal regardless of deadline", () => {
    const info = getDeadlineBadgeInfo("2026-01-01T00:00:00Z", "completed", NOW);
    expect(info.variant).toBe("completed");
    expect(info.label).toBe("Goal Achieved");
  });

  it("includes a descriptive aria-label conveying the full meaning", () => {
    const info = getDeadlineBadgeInfo("2026-07-15T12:00:00Z", "active", NOW);
    expect(info.ariaLabel).toMatch(/\d+ days remaining until goal deadline/);
  });
});
