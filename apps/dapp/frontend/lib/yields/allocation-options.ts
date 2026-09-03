import type { YieldPool } from "@/lib/api/yield-opportunities";
import { settlementAssetFor } from "@/lib/vault-contracts";
import type { ProtocolOption, ProtocolAllocation, RiskLevel } from "@/lib/types/vault-wizard";

/**
 * A protocol groups the pools DeFiLlama reports under one project — Blend
 * alone publishes four. Allocating per protocol is the fast path ("60% Blend,
 * 40% Gami"); expanding one exposes its pools so a specific market can be
 * chosen instead of the whole project.
 */
export interface ProtocolGroup {
  /** DeFiLlama project slug, e.g. "blend-pools-v2". */
  id: string;
  name: string;
  pools: YieldPool[];
  /** Highest pool APY, which is what "best pool" would earn. */
  bestApy: number;
  lowestApy: number;
  totalTvlUsd: number;
  /** Assets the protocol has pools for, deduplicated. */
  assets: string[];
}

/** How a selected protocol spreads across its own pools. */
export type ProtocolSpread = "best" | "even";

export interface AllocationSelection {
  /** Protocol ids selected at the protocol level. */
  protocols: Record<string, ProtocolSpread>;
  /** Pool ids selected individually, overriding their protocol's spread. */
  pools: string[];
}

function titleCase(slug: string): string {
  return slug
    .split("-")
    .filter((part) => part && !/^v\d+$/i.test(part))
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

/**
 * Risk is derived from TVL rather than invented: a thinly funded pool is the
 * one most likely to move against a depositor. The thresholds are coarse on
 * purpose — this labels a bucket, it does not score a protocol.
 */
export function riskFromTvl(tvlUsd: number): RiskLevel {
  if (tvlUsd >= 50_000_000) return "low";
  if (tvlUsd >= 5_000_000) return "medium";
  return "high";
}

/** Groups a flat pool list into protocols, ordered by best APY. */
export function groupPoolsByProtocol(pools: YieldPool[]): ProtocolGroup[] {
  if (!Array.isArray(pools)) return [];

  const byProject = new Map<string, YieldPool[]>();
  for (const pool of pools) {
    if (!pool?.project) continue;
    const list = byProject.get(pool.project) ?? [];
    list.push(pool);
    byProject.set(pool.project, list);
  }

  const groups: ProtocolGroup[] = [];
  for (const [id, group] of byProject) {
    const apys = group.map((p) => (Number.isFinite(p.apy) ? p.apy : 0));
    groups.push({
      id,
      name: titleCase(id),
      pools: [...group].sort((a, b) => (b.apy ?? 0) - (a.apy ?? 0)),
      bestApy: Math.max(...apys),
      lowestApy: Math.min(...apys),
      totalTvlUsd: group.reduce((sum, p) => sum + (p.tvlUsd ?? 0), 0),
      assets: [...new Set(group.map((p) => p.symbol).filter(Boolean))],
    });
  }

  return groups.sort((a, b) => b.bestApy - a.bestApy);
}

/**
 * Resolves a selection to the pools that actually receive funds.
 *
 * An individually selected pool always wins over its protocol's spread, so
 * expanding a protocol and picking one market does what it looks like it does
 * rather than adding to the protocol-level split.
 */
export function resolveSelectedPools(
  groups: ProtocolGroup[],
  selection: AllocationSelection,
): YieldPool[] {
  const chosen: YieldPool[] = [];
  const explicit = new Set(selection.pools);

  for (const group of groups) {
    const explicitHere = group.pools.filter((p) => explicit.has(p.pool));
    if (explicitHere.length > 0) {
      chosen.push(...explicitHere);
      continue;
    }
    const spread = selection.protocols[group.id];
    if (!spread) continue;
    if (spread === "best") {
      if (group.pools[0]) chosen.push(group.pools[0]);
    } else {
      chosen.push(...group.pools);
    }
  }

  return chosen;
}

/** Even split that still sums to exactly 100, remainder on the first legs. */
export function evenWeights(poolIds: string[]): ProtocolAllocation[] {
  if (poolIds.length === 0) return [];
  const base = Math.floor(100 / poolIds.length);
  let remainder = 100 - base * poolIds.length;
  return poolIds.map((id) => {
    const extra = remainder > 0 ? 1 : 0;
    remainder -= extra;
    return { protocolId: id, percentage: base + extra };
  });
}

/** Pools rendered as the options AllocationBuilder already understands. */
export function poolsAsProtocolOptions(pools: YieldPool[]): ProtocolOption[] {
  return pools.map((pool) => ({
    id: pool.pool,
    name: `${titleCase(pool.project)} · ${pool.symbol}`,
    estimatedApy: Number.isFinite(pool.apy) ? pool.apy : 0,
    riskLevel: riskFromTvl(pool.tvlUsd ?? 0),
    description: `${pool.symbol} on ${titleCase(pool.project)}`,
  }));
}

/** Weighted average APY of an allocation, in percent. */
export function blendedApy(
  allocations: ProtocolAllocation[],
  options: ProtocolOption[],
): number {
  return allocations.reduce((sum, alloc) => {
    const option = options.find((o) => o.id === alloc.protocolId);
    return option ? sum + (alloc.percentage / 100) * option.estimatedApy : sum;
  }, 0);
}

/**
 * Splits an allocation by asset. A vault holds one currency, so a mixed
 * selection cannot settle in a single deposit; the caller deposits per asset
 * and shows that rather than silently dropping the other legs.
 */
export function groupAllocationsByAsset(
  allocations: ProtocolAllocation[],
  pools: YieldPool[],
): Record<string, ProtocolAllocation[]> {
  const byAsset: Record<string, ProtocolAllocation[]> = {};
  for (const alloc of allocations) {
    const pool = pools.find((p) => p.pool === alloc.protocolId);
    if (!pool) continue;
    // Group by what the leg settles in, not the wrapper ticker: EARNUSDC and
    // USDC are one vault's currency, and grouping on the raw symbol split them
    // into two "assets" and made every deposit look unsupported.
    const asset = settlementAssetFor(pool.symbol) ?? pool.symbol;
    (byAsset[asset] ??= []).push(alloc);
  }
  return byAsset;
}
