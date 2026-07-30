"use client";

import { useQuery } from "@tanstack/react-query";
import { getStoredToken } from "@/lib/api/client";
import { savingsGoals } from "@/lib/api/savings-goals";
import { useWallet } from "@/components/wallet-provider";

/** Fetches a single savings goal by id (#732). */
export function useSavingsGoal(id: string | undefined) {
  const { isConnected } = useWallet();
  const isAuthenticated = isConnected && !!getStoredToken();

  return useQuery({
    queryKey: ["savings-goal", id],
    queryFn: () => savingsGoals.get(id as string),
    enabled: isAuthenticated && !!id,
    staleTime: 5 * 60 * 1000,
  });
}

/** Fetches the contribution history for a savings goal (#732). */
export function useSavingsGoalContributions(id: string | undefined) {
  const { isConnected } = useWallet();
  const isAuthenticated = isConnected && !!getStoredToken();

  return useQuery({
    queryKey: ["savings-goal", id, "contributions"],
    queryFn: () => savingsGoals.contributions(id as string),
    enabled: isAuthenticated && !!id,
    staleTime: 60 * 1000,
  });
}
