"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchYieldHarvests, type ListYieldHarvestsResponse } from "@/lib/api/yields";

/**
 * Paginated harvest history for the signed-in user.
 *
 * Pass `vaultId` to scope the query to a single vault server-side. Filtering
 * client-side instead would silently hide a vault's history whenever the
 * user's most recent harvests all belong to other vaults (nester#949).
 */
export function useYieldHarvests(limit = 20, vaultId?: string) {
  return useInfiniteQuery<ListYieldHarvestsResponse, Error>({
    queryKey: ["yieldHarvests", limit, vaultId ?? "all"],
    queryFn: ({ pageParam }) =>
      fetchYieldHarvests(pageParam as string | undefined, limit, vaultId),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    initialPageParam: undefined,
  });
}
