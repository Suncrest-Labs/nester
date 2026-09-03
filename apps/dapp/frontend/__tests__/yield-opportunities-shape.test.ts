import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchYieldOpportunities } from "@/lib/api/yield-opportunities";

function mockJson(body: unknown, ok = true) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({ ok, json: async () => body }),
  );
}

const pool = {
  pool: "p1",
  project: "blend-pools-v2",
  symbol: "USDC",
  apy: 5.48,
  apyBase: 5.48,
  apyReward: 0,
  tvlUsd: 15830780,
  apyPct7d: null,
  chain: "Stellar",
  riskScore: 1,
};

afterEach(() => vi.unstubAllGlobals());

describe("fetchYieldOpportunities response shape", () => {
  // The endpoint nests the page under data, which is what broke the savings
  // page: pools came back as an object and every caller filters or maps it.
  it("reads pools from the nested {data, meta} envelope", async () => {
    mockJson({ success: true, data: { data: [pool], meta: { stale: false } } });
    const { pools, meta } = await fetchYieldOpportunities();
    expect(Array.isArray(pools)).toBe(true);
    expect(pools).toHaveLength(1);
    expect(meta?.stale).toBe(false);
  });

  it("still accepts a bare array, so an API rollback cannot break callers", async () => {
    mockJson({ success: true, data: [pool] });
    const { pools } = await fetchYieldOpportunities();
    expect(pools).toHaveLength(1);
  });

  it("returns an array when the payload is an unexpected shape", async () => {
    mockJson({ success: true, data: { unexpected: true } });
    const { pools } = await fetchYieldOpportunities();
    expect(Array.isArray(pools)).toBe(true);
    expect(pools).toHaveLength(0);
  });

  it("returns an array when data is missing entirely", async () => {
    mockJson({ success: true });
    const { pools } = await fetchYieldOpportunities();
    expect(pools).toEqual([]);
  });

  it("throws when the API reports failure", async () => {
    mockJson({ success: false, error: { message: "upstream down" } }, false);
    await expect(fetchYieldOpportunities()).rejects.toThrow("upstream down");
  });
});
