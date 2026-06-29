import { useQuery } from "@tanstack/react-query";
import { fetchYieldOpportunities } from "@/lib/api/yield-opportunities";

export interface VaultMarket {
  id: string;
  protocol: string;
  symbol: string;
  apy: number;
  tvlUsd: number;
  riskScore: number;
  apyPct7d: number | null;
  chain: string;
}

export function useVaultMarkets() {
  return useQuery({
    queryKey: ["vault-markets"],
    queryFn: async () => {
      const data = await fetchYieldOpportunities();
      return data.pools.map((pool): VaultMarket => ({
        id: pool.pool,
        protocol: pool.project,
        symbol: pool.symbol,
        apy: pool.apy,
        tvlUsd: pool.tvlUsd,
        riskScore: pool.riskScore,
        apyPct7d: pool.apyPct7d,
        chain: pool.chain,
      }));
    },
    staleTime: 5 * 60 * 1000,
  });
}
