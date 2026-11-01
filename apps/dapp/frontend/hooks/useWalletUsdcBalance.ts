"use client";

import { useQuery } from "@tanstack/react-query";

import { fetchWalletUsdcBalance } from "@/lib/stellar/wallet-balance";

export function walletUsdcQueryKey(address: string | null | undefined) {
  return ["wallet", "usdc", address] as const;
}

export function useWalletUsdcBalance(address: string | null | undefined) {
  return useQuery({
    queryKey: walletUsdcQueryKey(address),
    queryFn: () => fetchWalletUsdcBalance(address as string),
    enabled: !!address,
    staleTime: 15_000,
    retry: 1,
  });
}
