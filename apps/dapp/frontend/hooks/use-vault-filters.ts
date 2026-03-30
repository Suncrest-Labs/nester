import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import { VAULTS } from "@/lib/mock-vaults";
import type { RiskTier } from "@/lib/types/vault";

export type SortKey = "apy" | "tvl" | "risk";

const RISK_ORDER: Record<RiskTier, number> = {
  Conservative: 0,
  Balanced: 1,
  Growth: 2,
  DeFi500: 3,
};

export function useVaultFilters() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  const sortBy = (searchParams.get("sort") as SortKey) ?? "apy";
  const filterTier = (searchParams.get("filter") as RiskTier | "all") ?? "all";

  function setSort(key: SortKey) {
    const params = new URLSearchParams(searchParams.toString());
    if (key === "apy") params.delete("sort");
    else params.set("sort", key);
    const qs = params.toString();
    router.replace(`${pathname}${qs ? `?${qs}` : ""}`);
  }

  function setFilter(tier: RiskTier | "all") {
    const params = new URLSearchParams(searchParams.toString());
    if (tier === "all") params.delete("filter");
    else params.set("filter", tier);
    const qs = params.toString();
    router.replace(`${pathname}${qs ? `?${qs}` : ""}`);
  }

  const filteredAndSorted = useMemo(() => {
    const vaults =
      filterTier === "all"
        ? VAULTS
        : VAULTS.filter((v) => v.riskTier === filterTier);
    return [...vaults].sort((a, b) => {
      if (sortBy === "apy") return b.currentApy - a.currentApy;
      if (sortBy === "tvl") return b.tvl - a.tvl;
      if (sortBy === "risk") return RISK_ORDER[a.riskTier] - RISK_ORDER[b.riskTier];
      return 0;
    });
  }, [filterTier, sortBy]);

  return { sortBy, filterTier, setSort, setFilter, filteredAndSorted };
}
