"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchYieldOpportunities } from "@/lib/api/yield-opportunities";

export function useYieldOpportunities(chain = "Stellar", limit = 50) {
  return useQuery({
    queryKey: ["yield-opportunities", chain, limit],
    queryFn: () => fetchYieldOpportunities(chain, limit),
    staleTime: 5 * 60 * 1000,
  });
}
