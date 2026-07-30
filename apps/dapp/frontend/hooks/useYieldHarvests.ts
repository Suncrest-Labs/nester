"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchYieldHarvests, type ListYieldHarvestsResponse } from "@/lib/api/yields";

export function useYieldHarvests(limit = 20) {
  return useInfiniteQuery<ListYieldHarvestsResponse, Error>({
    queryKey: ["yieldHarvests", limit],
    queryFn: ({ pageParam }) =>
      fetchYieldHarvests(pageParam as string | undefined, limit),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    initialPageParam: undefined,
  });
}
