import config from "@/lib/config";
import { getStoredToken } from "@/lib/api/client";

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

export interface YieldMeta {
  stale: boolean;
  fetched_at?: string;
}

export interface YieldsResponse {
  data: YieldPool[];
  meta: YieldMeta;
}

export interface YieldBookmark {
  protocol_slug: string;
  apy: number;
  tvl_usd: number;
  symbol?: string;
  created_at: string;
}

type ApiEnvelope<T> = {
  success: boolean;
  data: T;
  error?: { message: string };
};

export function protocolSlugFromProject(project: string): string {
  return project.toLowerCase().trim().replace(/\s+/g, "-");
}

export async function fetchYields(
  chain = "Stellar",
  limit = 50,
  sortBookmarks = false
): Promise<YieldsResponse> {
  const params = new URLSearchParams({
    chain,
    limit: String(limit),
  });
  if (sortBookmarks && getStoredToken()) {
    params.set("sort_bookmarks", "true");
  }
  const res = await fetch(`${config.apiUrl}/yields?${params}`, {
    headers: getStoredToken() ? { Authorization: `Bearer ${getStoredToken()}` } : {},
  });
  const json = (await res.json()) as ApiEnvelope<YieldsResponse>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `yields: ${res.status}`);
  }
  return json.data ?? { data: [], meta: { stale: false } };
}

export async function fetchYieldBookmarks(chain = "Stellar"): Promise<YieldBookmark[]> {
  const res = await fetch(`${config.apiUrl}/yields/bookmarks?chain=${chain}`, {
    headers: { Authorization: `Bearer ${getStoredToken()}` },
  });
  const json = (await res.json()) as ApiEnvelope<YieldBookmark[]>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `yield bookmarks: ${res.status}`);
  }
  return json.data ?? [];
}

export async function addYieldBookmark(protocolSlug: string): Promise<YieldBookmark> {
  const res = await fetch(`${config.apiUrl}/yields/bookmarks`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${getStoredToken()}`,
    },
    body: JSON.stringify({ protocol_slug: protocolSlug }),
  });
  const json = (await res.json()) as ApiEnvelope<YieldBookmark>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `add bookmark: ${res.status}`);
  }
  return json.data;
}

export async function removeYieldBookmark(protocolSlug: string): Promise<void> {
  const res = await fetch(`${config.apiUrl}/yields/bookmarks/${encodeURIComponent(protocolSlug)}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${getStoredToken()}` },
  });
  if (!res.ok && res.status !== 204) {
    const json = (await res.json()) as ApiEnvelope<unknown>;
    throw new Error(json.error?.message ?? `remove bookmark: ${res.status}`);
  }
}
