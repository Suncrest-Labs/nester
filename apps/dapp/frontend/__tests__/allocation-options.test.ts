import { describe, it, expect } from "vitest";
import {
  groupPoolsByProtocol,
  resolveSelectedPools,
  evenWeights,
  poolsAsProtocolOptions,
  blendedApy,
  groupAllocationsByAsset,
  riskFromTvl,
} from "@/lib/yields/allocation-options";
import type { YieldPool } from "@/lib/api/yield-opportunities";

// Mirrors what /api/v1/yields returns for Stellar: 3 protocols, 7 pools.
const pool = (over: Partial<YieldPool>): YieldPool => ({
  pool: "id", project: "blend-pools-v2", symbol: "USDC", apy: 1,
  apyBase: 1, apyReward: 0, tvlUsd: 1_000_000, apyPct7d: null,
  chain: "Stellar", riskScore: 1, ...over,
});

const POOLS = [
  pool({ pool: "gami-usdc", project: "gami-labs", symbol: "EARNUSDC", apy: 10, tvlUsd: 24_843_295 }),
  pool({ pool: "gami-xlm", project: "gami-labs", symbol: "EARNXLM", apy: 5, tvlUsd: 3_509_267 }),
  pool({ pool: "blend-usdc", project: "blend-pools-v2", symbol: "USDC", apy: 6.98, tvlUsd: 15_830_780 }),
  pool({ pool: "blend-xlm", project: "blend-pools-v2", symbol: "XLM", apy: 0.01, tvlUsd: 142_580 }),
  pool({ pool: "ondo-usdy", project: "ondo-yield-assets", symbol: "USDY", apy: 3.56, tvlUsd: 535_223_073 }),
];

describe("groupPoolsByProtocol", () => {
  it("groups pools under their protocol, best APY first", () => {
    const groups = groupPoolsByProtocol(POOLS);
    expect(groups.map((g) => g.id)).toEqual(["gami-labs", "blend-pools-v2", "ondo-yield-assets"]);
    expect(groups[0].pools).toHaveLength(2);
    expect(groups[0].bestApy).toBe(10);
  });

  it("sums TVL and lists each protocol's assets", () => {
    const blend = groupPoolsByProtocol(POOLS).find((g) => g.id === "blend-pools-v2")!;
    expect(blend.totalTvlUsd).toBe(15_830_780 + 142_580);
    expect(blend.assets).toEqual(["USDC", "XLM"]);
  });

  it("survives a non-array payload", () => {
    expect(groupPoolsByProtocol(undefined as unknown as YieldPool[])).toEqual([]);
  });
});

describe("resolveSelectedPools", () => {
  const groups = groupPoolsByProtocol(POOLS);

  it("takes only the best pool when a protocol is set to 'best'", () => {
    const chosen = resolveSelectedPools(groups, { protocols: { "gami-labs": "best" }, pools: [] });
    expect(chosen.map((p) => p.pool)).toEqual(["gami-usdc"]);
  });

  it("takes every pool when a protocol is set to 'even'", () => {
    const chosen = resolveSelectedPools(groups, { protocols: { "gami-labs": "even" }, pools: [] });
    expect(chosen).toHaveLength(2);
  });

  it("lets an explicitly picked pool override its protocol's spread", () => {
    const chosen = resolveSelectedPools(groups, {
      protocols: { "gami-labs": "even" },
      pools: ["gami-xlm"],
    });
    expect(chosen.map((p) => p.pool)).toEqual(["gami-xlm"]);
  });

  it("combines protocol-level and pool-level selections across protocols", () => {
    const chosen = resolveSelectedPools(groups, {
      protocols: { "blend-pools-v2": "best" },
      pools: ["gami-usdc"],
    });
    expect(chosen.map((p) => p.pool).sort()).toEqual(["blend-usdc", "gami-usdc"]);
  });

  it("returns nothing when nothing is selected", () => {
    expect(resolveSelectedPools(groups, { protocols: {}, pools: [] })).toEqual([]);
  });
});

describe("evenWeights", () => {
  it("splits evenly when it divides cleanly", () => {
    expect(evenWeights(["a", "b", "c", "d"]).map((w) => w.percentage)).toEqual([25, 25, 25, 25]);
  });

  it("always totals exactly 100, remainder on the first legs", () => {
    const weights = evenWeights(["a", "b", "c"]);
    expect(weights.reduce((s, w) => s + w.percentage, 0)).toBe(100);
    expect(weights.map((w) => w.percentage)).toEqual([34, 33, 33]);
  });

  it("returns nothing for an empty selection", () => {
    expect(evenWeights([])).toEqual([]);
  });
});

describe("blendedApy", () => {
  it("weights each pool's APY by its share", () => {
    const options = poolsAsProtocolOptions(POOLS);
    const apy = blendedApy(
      [{ protocolId: "gami-usdc", percentage: 50 }, { protocolId: "ondo-usdy", percentage: 50 }],
      options,
    );
    expect(apy).toBeCloseTo(6.78, 2);
  });

  it("ignores an allocation with no matching option", () => {
    expect(blendedApy([{ protocolId: "ghost", percentage: 100 }], [])).toBe(0);
  });
});

describe("groupAllocationsByAsset", () => {
  // A vault holds one currency, so a mixed selection needs one deposit each.
  it("separates legs by the asset their pool pays in", () => {
    const byAsset = groupAllocationsByAsset(
      [{ protocolId: "blend-usdc", percentage: 50 }, { protocolId: "blend-xlm", percentage: 50 }],
      POOLS,
    );
    expect(Object.keys(byAsset).sort()).toEqual(["USDC", "XLM"]);
  });

  it("keeps a single-asset selection in one group", () => {
    const byAsset = groupAllocationsByAsset(
      [{ protocolId: "blend-usdc", percentage: 100 }],
      POOLS,
    );
    expect(Object.keys(byAsset)).toEqual(["USDC"]);
  });
});

describe("riskFromTvl", () => {
  it("labels by depth, since a thin pool is the one that moves", () => {
    expect(riskFromTvl(535_223_073)).toBe("low");
    expect(riskFromTvl(15_830_780)).toBe("medium");
    expect(riskFromTvl(142_580)).toBe("high");
  });
});

describe("wrapper tokens map to their settlement asset", () => {
  // Gami pays EARNUSDC for a USDC deposit. Grouping on the raw symbol split
  // one currency into two and made every leg look like it had no vault.
  it("treats a wrapper ticker as its underlying currency", () => {
    const byAsset = groupAllocationsByAsset(
      [
        { protocolId: "gami-usdc", percentage: 50 },
        { protocolId: "blend-usdc", percentage: 50 },
      ],
      POOLS,
    );
    expect(Object.keys(byAsset)).toEqual(["USDC"]);
    expect(byAsset.USDC).toHaveLength(2);
  });

  it("still separates genuinely different currencies", () => {
    const byAsset = groupAllocationsByAsset(
      [
        { protocolId: "gami-usdc", percentage: 50 },
        { protocolId: "gami-xlm", percentage: 50 },
      ],
      POOLS,
    );
    expect(Object.keys(byAsset).sort()).toEqual(["USDC", "XLM"]);
  });

  it("leaves an unrecognised symbol under its own name rather than guessing", () => {
    const byAsset = groupAllocationsByAsset(
      [{ protocolId: "ondo-usdy", percentage: 100 }],
      POOLS,
    );
    expect(Object.keys(byAsset)).toEqual(["USDY"]);
  });
});
