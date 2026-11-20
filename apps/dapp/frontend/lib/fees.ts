/**
 * Vault fee tier helpers matching `packages/contracts/libs/common/src/fees.rs`.
 */

export const SECONDS_PER_DAY = 86_400;

export interface FeeScheduleTierSummary {
  id: string;
  label: string;
  tenureRange: string;
  exitFeeRate: string;
}

/**
 * Default tenure-tiered exit fee schedule summary displayed before deposit.
 */
export const FEE_SCHEDULE_TIERS: FeeScheduleTierSummary[] = [
  { id: "tier-1", label: "Tier 1", tenureRange: "0–89 days", exitFeeRate: "2.00% → 1.00%" },
  { id: "tier-2", label: "Tier 2", tenureRange: "90–364 days", exitFeeRate: "1.00% → 0.25%" },
  { id: "tier-3", label: "Tier 3", tenureRange: "365+ days", exitFeeRate: "0.25%" },
];

export const FULL_FEE_SCHEDULE_HREF = "/vaults#fee-schedule";

/**
 * Format basis points as a percentage string with two decimal places (e.g. 200 -> "2.00%").
 */
export function formatBps(bps: number): string {
  return `${(bps / 100).toFixed(2)}%`;
}

/**
 * Resolve a human-readable tier label from on-chain `fee_schedule_preview` fields.
 */
export function resolveCurrentTierLabel(
  tenureSecs: number,
  currentExitFeeBps: number,
  nextBoundarySecs: number | null
): string {
  if (currentExitFeeBps === 0 && nextBoundarySecs === null) {
    return "No exit fee (Matured)";
  }
  if (nextBoundarySecs === null) {
    return "Tier 3 (365+ days · Lowest tier)";
  }
  if (tenureSecs >= 90 * SECONDS_PER_DAY) {
    return "Tier 2 (90–364 days)";
  }
  return "Tier 1 (< 90 days)";
}

/**
 * Compute the UTC date when the user's position reaches the next lower fee tier.
 * Returns `null` when already at the lowest tier.
 */
export function getNextLowerTierDate(
  tenureSecs: number,
  nextBoundarySecs: number | null,
  nowMs: number = Date.now()
): Date | null {
  if (nextBoundarySecs === null || nextBoundarySecs <= tenureSecs) {
    return null;
  }
  const remainingSecs = nextBoundarySecs - tenureSecs;
  return new Date(nowMs + remainingSecs * 1000);
}

/**
 * Format a tier transition date deterministically in UTC (e.g. "Feb 18, 2027").
 */
export function formatTierDate(date: Date): string {
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}
