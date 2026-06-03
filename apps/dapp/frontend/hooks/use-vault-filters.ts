import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import type { Vault, MarketType } from "@/lib/types/vault";
import { useVaults } from "@/hooks/useVaults";

export type SortKey = "apy" | "tvl" | "utilization";
export type FilterType = MarketType | "all";

export function useVaultFilters() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  const sortBy = (searchParams.get("sort") as SortKey) ?? "tvl";
  const filterType = (searchParams.get("filter") as FilterType) ?? "all";

  // Fetch all vaults from live API
  const { vaults: apiVaults, isLoading } = useVaults(undefined);

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
    return apiVaults.map((v) => {
      const apy = v.performance?.apy_30d ? v.performance.apy_30d * 100 : 0;
      const tvl = parseFloat(v.total_deposited) || 0;
      
      // We map the backend API vaults to the catalog structure.
      // If currency contains "-", assume it's a pair, otherwise single.
      const isPair = v.currency.includes("-");
      const marketType: MarketType = isPair ? "pair" : "single";
      const tokens = isPair ? v.currency.split("-") : [v.currency];

      return {
        id: v.id,
        name: `${v.currency} Market`,
        description: `Automated yield strategies for ${v.currency}.`,
        marketType,
        tokens,
        currentApy: apy,
        apyRange: `${(apy * 0.8).toFixed(1)}-${(apy * 1.2).toFixed(1)}%`,
        tvl,
        utilization: 0, // Not provided by current API
        allocations: [],
        supportedAssets: tokens,
        maturityTerms: "Flexible - withdraw anytime",
        earlyWithdrawalPenalty: "None",
        contractAddress: v.contract_address,
        apyHistory: [],
        strategies: [],
      };
    });
  }, [apiVaults]);

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
