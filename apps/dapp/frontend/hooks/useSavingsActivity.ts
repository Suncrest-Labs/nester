"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useWallet } from "@/components/wallet-provider";
import { getContractId } from "@/lib/contracts";
import { fetchSavingsActivity } from "@/lib/stellar/activity";

export function useSavingsActivity() {
  const { address } = useWallet();
  const coreId = getContractId("savingsCore");

  return useInfiniteQuery({
    queryKey: ["savings", "activity", address],
    queryFn: ({ pageParam }) => fetchSavingsActivity(address!, coreId!, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled: !!address && !!coreId,
    staleTime: 30_000,
  });
}
