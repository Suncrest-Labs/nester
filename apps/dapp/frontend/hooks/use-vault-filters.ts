import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import { Vault } from "./useVaults";

export type SortKey = "apy" | "tvl";
export type FilterType = string | "all";

export function useVaultFilters(vaults: Vault[] = []) {
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
    const filteredVaults =
      filterType === "all"
        ? vaults
        : vaults.filter((v) => v.strategy === filterType);
    return [...filteredVaults].sort((a, b) => {
      if (sortBy === "apy") return (b.apy || 0) - (a.apy || 0);
      if (sortBy === "tvl") return (b.tvl || 0) - (a.tvl || 0);
      return 0;
    });
  }, [filterType, sortBy, vaults]);

  return { sortBy, filterType, setSort, setFilter, filteredAndSorted };
}

