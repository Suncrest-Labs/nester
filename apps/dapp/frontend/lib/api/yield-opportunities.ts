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

type ApiEnvelope<T> = {
  success: boolean;
  data: T;
  error?: { message: string };
};

export async function fetchYieldOpportunities(
  chain = "Stellar",
  limit = 50
): Promise<YieldPool[]> {
  const params = new URLSearchParams({ chain, limit: String(limit) });
  const res = await fetch(`${config.apiUrl}/yield-opportunities?${params}`);
  const json = (await res.json()) as ApiEnvelope<YieldPool[]>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `yield-opportunities: ${res.status}`);
  }
  return json.data ?? [];
}
