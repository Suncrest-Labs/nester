/**
 * Slippage tolerance math and quote expiry helpers (#1503).
 *
 * Default tolerance: 0.5% (50 bps)
 * Maximum tolerance: 3.0% (300 bps)
 * Default quote freshness window: 30 seconds
 */

export const DEFAULT_SLIPPAGE_PCT = 0.5;
export const MIN_SLIPPAGE_PCT = 0;
export const MAX_SLIPPAGE_PCT = 3;
export const SLIPPAGE_PRESETS_PCT = [0.5, 1, 3] as const;
export const QUOTE_EXPIRY_MS = 30_000;

/**
 * Clamp a user-supplied slippage percentage to [0%, 3%].
 */
export function clampSlippagePct(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_SLIPPAGE_PCT;
  if (value < MIN_SLIPPAGE_PCT) return MIN_SLIPPAGE_PCT;
  if (value > MAX_SLIPPAGE_PCT) return MAX_SLIPPAGE_PCT;
  return Number(value.toFixed(2));
}

/**
 * Apply slippage tolerance (%) to an integer stroop amount (`min_shares_out` or `min_assets_out`).
 */
export function applySlippageToStroops(
  expectedStroops: bigint,
  slippagePct: number = DEFAULT_SLIPPAGE_PCT
): bigint {
  if (expectedStroops <= BigInt(0)) return BigInt(0);
  const clampedPct = clampSlippagePct(slippagePct);
  const bps = Math.round(clampedPct * 100);
  return (expectedStroops * BigInt(10_000 - bps)) / BigInt(10_000);
}

/**
 * Apply slippage tolerance (%) to a human-readable amount (`min_assets_out` / `min_shares_out`).
 */
export function applySlippageToAmount(
  expectedAmount: number,
  slippagePct: number = DEFAULT_SLIPPAGE_PCT
): number {
  if (!Number.isFinite(expectedAmount) || expectedAmount <= 0) return 0;
  const clampedPct = clampSlippagePct(slippagePct);
  const bps = Math.round(clampedPct * 100);
  return (expectedAmount * (10_000 - bps)) / 10_000;
}

/**
 * Returns true if the quote timestamp is older than `maxAgeMs`.
 */
export function isQuoteStale(
  quotedAtMs: number,
  nowMs: number = Date.now(),
  maxAgeMs: number = QUOTE_EXPIRY_MS
): boolean {
  if (!Number.isFinite(quotedAtMs) || quotedAtMs <= 0) return true;
  return nowMs - quotedAtMs > maxAgeMs;
}

/**
 * Ensure a quote is fresh before signing; if older than `maxAgeMs`, re-fetches via `refetch`.
 */
export async function ensureFreshQuote<T>(
  quoteState: { value: T; quotedAtMs: number },
  refetch: () => Promise<T> | T,
  nowMs: number = Date.now(),
  maxAgeMs: number = QUOTE_EXPIRY_MS
): Promise<{ value: T; refreshed: boolean; quotedAtMs: number }> {
  if (isQuoteStale(quoteState.quotedAtMs, nowMs, maxAgeMs)) {
    const freshValue = await refetch();
    return {
      value: freshValue,
      refreshed: true,
      quotedAtMs: nowMs,
    };
  }

  return {
    value: quoteState.value,
    refreshed: false,
    quotedAtMs: quoteState.quotedAtMs,
  };
}
