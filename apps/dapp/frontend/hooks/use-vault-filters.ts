import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import type { Vault, MarketType } from "@/lib/types/vault";
import { getVaultContractByAsset } from "@/lib/vault-contracts";
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
        // The API already reports APY in percent (6.98 means 6.98%), so the
        // old `apy * 100` rendered a 7% pool as 698% and its range as
        // "558.6-837.9%".
        currentApy: apy,
        apyRange: `${(apy * 0.8).toFixed(2)}-${(apy * 1.2).toFixed(2)}%`,
        tvl,
        utilization: 0, // Not provided by current API
        allocations: [],
        supportedAssets: tokens,
        maturityTerms: "Flexible - withdraw anytime",
        earlyWithdrawalPenalty: "None",
        // Resolved from the pool's settlement asset so a deposit built from
        // this market targets a real deployed vault rather than failing at
        // signing time.
        contractAddress: getVaultContractByAsset(m.symbol)?.contractAddress ?? undefined,
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
