import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import type { Vault, MarketType } from "@/lib/types/vault";
import { useVaultMarkets } from "@/hooks/useVaultMarkets";

export type SortKey = "apy" | "tvl" | "utilization";
export type FilterType = MarketType | "all";

export function useVaultFilters() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  const sortBy = (searchParams.get("sort") as SortKey) ?? "tvl";
  const filterType = (searchParams.get("filter") as FilterType) ?? "all";

  // Fetch all vaults from live yield-opportunities API
  const { data: markets = [], isLoading } = useVaultMarkets();

  function setSort(key: SortKey) {
    const params = new URLSearchParams(searchParams.toString());
    if (key === "tvl") params.delete("sort");
    else params.set("sort", key);
    const qs = params.toString();
    router.replace(`${pathname}${qs ? `?${qs}` : ""}`);
  }

  function setFilter(type: FilterType) {
    const params = new URLSearchParams(searchParams.toString());
    if (type === "all") params.delete("filter");
    else params.set("filter", type);
    const qs = params.toString();
    router.replace(`${pathname}${qs ? `?${qs}` : ""}`);
  }

  const vaults: Vault[] = useMemo(() => {
    return markets.map((m) => {
      const apy = m.apy ?? 0;
      const tvl = m.tvlUsd ?? 0;

      // Infer market type from symbol: "A-B" → pair, otherwise single
      const isPair = m.symbol.includes("-");
      const marketType: MarketType = isPair ? "pair" : "single";
      const tokens = isPair ? m.symbol.split("-") : [m.symbol];

      return {
        id: m.id,
        name: `${m.symbol} Market`,
        description: `Automated yield strategies for ${m.symbol} on ${m.protocol}.`,
        marketType,
        tokens,
        currentApy: apy * 100,
        apyRange: `${(apy * 80).toFixed(1)}-${(apy * 120).toFixed(1)}%`,
        tvl,
        utilization: 0, // Not provided by current API
        allocations: [],
        supportedAssets: tokens,
        maturityTerms: "Flexible - withdraw anytime",
        earlyWithdrawalPenalty: "None",
        contractAddress: undefined,
        apyHistory: [],
        strategies: [],
      };
    });
  }, [markets]);

  const filteredAndSorted = useMemo(() => {
    const filtered =
      filterType === "all"
        ? vaults
        : vaults.filter((v) => v.marketType === filterType);
    return [...filtered].sort((a, b) => {
      if (sortBy === "apy") return b.currentApy - a.currentApy;
      if (sortBy === "tvl") return b.tvl - a.tvl;
      if (sortBy === "utilization") return b.utilization - a.utilization;
      return 0;
    });
  }, [vaults, filterType, sortBy]);

  return { sortBy, filterType, setSort, setFilter, filteredAndSorted, isLoading };
}
