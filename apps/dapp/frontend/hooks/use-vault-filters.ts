import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import { VAULTS } from "@/lib/mock-vaults";
import type { MarketType } from "@/lib/types/vault";

export type SortKey = "apy" | "tvl" | "utilization";
export type FilterType = MarketType | "all";

export function useVaultFilters() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  const sortBy = (searchParams.get("sort") as SortKey) ?? "tvl";
  const filterType = (searchParams.get("filter") as FilterType) ?? "all";

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

  const filteredAndSorted = useMemo(() => {
    const vaults =
      filterType === "all"
        ? VAULTS
        : VAULTS.filter((v) => v.marketType === filterType);
    return [...vaults].sort((a, b) => {
      if (sortBy === "apy") return b.currentApy - a.currentApy;
      if (sortBy === "tvl") return b.tvl - a.tvl;
      if (sortBy === "utilization") return b.utilization - a.utilization;
      return 0;
    });
  }, [filterType, sortBy]);

  return { sortBy, filterType, setSort, setFilter, filteredAndSorted };
}
