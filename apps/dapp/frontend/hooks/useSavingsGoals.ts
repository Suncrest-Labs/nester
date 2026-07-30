"use client";

import { useQuery } from "@tanstack/react-query";
import { getStoredToken } from "@/lib/api/client";
import { savingsGoals } from "@/lib/api/savings-goals";
import { useWallet } from "@/components/wallet-provider";

export function useSavingsGoals() {
  const { isConnected } = useWallet();
  const isAuthenticated = isConnected && !!getStoredToken();

  return useQuery({
    queryKey: ["savings-goals"],
    queryFn: () => savingsGoals.list(),
    enabled: isAuthenticated,
    staleTime: 5 * 60 * 1000,
  });
}
