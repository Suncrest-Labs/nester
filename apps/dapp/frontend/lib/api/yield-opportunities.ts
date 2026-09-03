import config from "@/lib/config";

export interface YieldPool {
  pool: string;
  project: string;
  symbol: string;
  apy: number;
  apyBase: number;
  apyReward: number;
  tvlUsd: number;
  apyPct7d: number | null;
  chain: string;
  riskScore: number;
}

type YieldMeta = { stale?: boolean };

/**
 * The endpoint nests the page under `data`, so the envelope's `data` is
 * `{ data, meta }` rather than the array itself — the same shape
 * lib/api/yields.ts already models. Reading it as an array handed callers an
 * object, and the savings page died on `pools.filter is not a function`. The
 * older array form is still accepted so a rollback of the API cannot break
 * this again.
 */
type ApiEnvelope = {
  success: boolean;
  data?: YieldPool[] | { data?: YieldPool[]; meta?: YieldMeta };
  error?: { message: string };
  meta?: YieldMeta;
};

export async function fetchYieldOpportunities(
  chain = "Stellar",
  limit = 50
): Promise<{ pools: YieldPool[]; meta?: YieldMeta }> {
  const params = new URLSearchParams({ chain, limit: String(limit) });
  const res = await fetch(`${config.apiUrl}/yield-opportunities?${params}`);
  const json = (await res.json()) as ApiEnvelope;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `yield-opportunities: ${res.status}`);
  }

  const payload = json.data;
  const pools = Array.isArray(payload) ? payload : payload?.data;
  const meta = (Array.isArray(payload) ? undefined : payload?.meta) ?? json.meta;

  // Never hand back a non-array: every caller maps or filters this.
  return { pools: Array.isArray(pools) ? pools : [], meta };
}
