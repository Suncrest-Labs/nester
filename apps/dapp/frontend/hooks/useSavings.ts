"use client";

import { useQuery } from "@tanstack/react-query";
import {
  fetchSavingsHistory,
  fetchSavingsPosition,
  fetchSavingsSummary,
  type SavingsPeriod,
} from "@/lib/api/savings";
import { getStoredToken } from "@/lib/api/client";
import { useWallet } from "@/components/wallet-provider";

const STALE_TIME_MS = 60_000;

export function useSavings(period: SavingsPeriod = "30d") {
  const { isConnected } = useWallet();
  const isAuthenticated = isConnected && !!getStoredToken();

  const summary = useQuery({
    queryKey: ["savings", "summary"],
    queryFn: fetchSavingsSummary,
    staleTime: STALE_TIME_MS,
  });
  const history = useQuery({
    queryKey: ["savings", "history", period],
    queryFn: () => fetchSavingsHistory(period),
    staleTime: STALE_TIME_MS,
  });
  const position = useQuery({
    queryKey: ["savings", "position"],
    queryFn: fetchSavingsPosition,
    enabled: isAuthenticated,
    staleTime: STALE_TIME_MS,
  });

  return {
    summary: summary.data,
    history: history.data,
    position: position.data,
    isLoading: summary.isLoading || history.isLoading || position.isLoading,
    error: summary.error ?? history.error ?? position.error ?? null,
  };
}
