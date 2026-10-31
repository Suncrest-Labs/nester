import Decimal from "decimal.js";

export const MINUS = "−";

function group(whole: string): string {
  return whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

export function formatUsdc(value: string): string {
  const d = new Decimal(value);
  const abs = d.abs().toDecimalPlaces(2, Decimal.ROUND_HALF_UP);
  const [whole, frac] = abs.toFixed(2).split(".");
  return `${d.isNegative() && !abs.isZero() ? MINUS : ""}$${group(whole)}.${frac}`;
}

export function formatPercent(value: Decimal.Value, places = 0): string {
  const d = new Decimal(value).toDecimalPlaces(places, Decimal.ROUND_HALF_UP);
  return `${d.toFixed(places)}%`;
}

export function percentOf(part: string, total: string): Decimal {
  const t = new Decimal(total);
  if (t.isZero()) return new Decimal(0);
  return new Decimal(part).div(t).times(100);
}

export function formatShares(value: string): string {
  const [whole, frac] = new Decimal(value).toDecimalPlaces(4, Decimal.ROUND_DOWN).toFixed().split(".");
  return frac ? `${group(whole)}.${frac}` : group(whole);
}
