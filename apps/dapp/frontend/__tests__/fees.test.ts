import { describe, it, expect } from "vitest";
import {
  formatBps,
  formatTierDate,
  getNextLowerTierDate,
  resolveCurrentTierLabel,
  SECONDS_PER_DAY,
} from "@/lib/fees";

describe("fees helpers", () => {
  it("formats basis points as percentages", () => {
    expect(formatBps(200)).toBe("2.00%");
    expect(formatBps(100)).toBe("1.00%");
    expect(formatBps(25)).toBe("0.25%");
    expect(formatBps(0)).toBe("0.00%");
  });

  it("resolves tier labels for every tier", () => {
    expect(resolveCurrentTierLabel(10 * SECONDS_PER_DAY, 200, 90 * SECONDS_PER_DAY)).toBe(
      "Tier 1 (< 90 days)"
    );
    expect(resolveCurrentTierLabel(120 * SECONDS_PER_DAY, 100, 365 * SECONDS_PER_DAY)).toBe(
      "Tier 2 (90–364 days)"
    );
    expect(resolveCurrentTierLabel(400 * SECONDS_PER_DAY, 25, null)).toBe(
      "Tier 3 (365+ days · Lowest tier)"
    );
    expect(resolveCurrentTierLabel(400 * SECONDS_PER_DAY, 0, null)).toBe(
      "No exit fee (Matured)"
    );
  });

  it("computes and formats the next lower tier date", () => {
    const nowMs = Date.UTC(2026, 5, 1, 0, 0, 0); // Jun 1, 2026
    const nextDate = getNextLowerTierDate(30 * SECONDS_PER_DAY, 90 * SECONDS_PER_DAY, nowMs);
    expect(nextDate).not.toBeNull();
    expect(formatTierDate(nextDate!)).toBe("Jul 31, 2026");

    expect(getNextLowerTierDate(400 * SECONDS_PER_DAY, null, nowMs)).toBeNull();
  });
});
