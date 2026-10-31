"use client";

import { useQuery } from "@tanstack/react-query";
import {
  fetchSavingsHistory,
  fetchSavingsPosition,
  fetchSavingsSummary,
  type SavingsPeriod,
} from "@/lib/api/savings";
import { ApiError } from "@/lib/api/client";
import { getAccessToken } from "@/lib/auth/token-store";
import { useWallet } from "@/components/wallet-provider";

const STALE_TIME_MS = 60_000;
const DEFAULT_RETRIES = 3;

/** 503 means the savings core isn't configured on this deployment; retrying can't fix it. */
function retryUnlessUnconfigured(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.status === 503) return false;
  return failureCount < DEFAULT_RETRIES;
}

export function useSavings(period: SavingsPeriod = "30d") {
  const { address } = useWallet();

  const summary = useQuery({
    queryKey: ["savings", "summary"],
    queryFn: fetchSavingsSummary,
    retry: retryUnlessUnconfigured,
    staleTime: STALE_TIME_MS,
  });
  const history = useQuery({
    queryKey: ["savings", "history", period],
    queryFn: () => fetchSavingsHistory(period),
    retry: retryUnlessUnconfigured,
    staleTime: STALE_TIME_MS,
  });
  const position = useQuery({
    queryKey: ["savings", "position", address],
    queryFn: fetchSavingsPosition,
    enabled: !!address && !!getAccessToken(),
    staleTime: STALE_TIME_MS,
  });

  return {
    summary: summary.data,
    history: history.data,
    position: address ? position.data : undefined,
    positionLoading: !!address && position.isLoading,
    positionError: address ? position.error : null,
    isLoading: summary.isLoading || history.isLoading || position.isLoading,
    error: summary.error ?? history.error ?? position.error ?? null,
  };
}
