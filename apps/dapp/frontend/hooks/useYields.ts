"use client";

import { useQuery } from "@tanstack/react-query";
import { getStoredToken } from "@/lib/api/client";
import { fetchYields } from "@/lib/api/yields";
import { useWallet } from "@/components/wallet-provider";

export function useYields(chain = "Stellar", limit = 50) {
  const { isConnected } = useWallet();
  const sortBookmarks = isConnected && !!getStoredToken();

  return useQuery({
    queryKey: ["yields", chain, limit, sortBookmarks],
    queryFn: () => fetchYields(chain, limit, sortBookmarks),
    staleTime: 5 * 60 * 1000,
  });
}
