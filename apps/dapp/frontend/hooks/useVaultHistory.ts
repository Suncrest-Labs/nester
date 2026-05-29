"use client";

/**
 * useVaultHistory — fetches APY/balance history for the portfolio chart.
 * Merges snapshots from all user vaults into a single time-series.
 */

import { useState, useEffect, useCallback, useMemo } from "react";
import { api, ApiError } from "@/lib/api/client";

export interface ChartPoint {
  date: string;   // ISO date string
  value: number;  // cumulative balance in asset units (sum across vaults)
}

interface UseVaultHistoryResult {
  history: ChartPoint[];
  isLoading: boolean;
}

export function useVaultHistory(
  vaultIds: string[],
  period: string = "30d"
): UseVaultHistoryResult {
  const [history, setHistory] = useState<ChartPoint[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const stableVaultIds = useMemo(
    () => JSON.stringify([...vaultIds].sort()),
    [vaultIds]
  );

  const fetch = useCallback(async () => {
    if (vaultIds.length === 0) {
      setHistory([]);
      return;
    }
    setIsLoading(true);
    try {
      // Fetch history for every vault in parallel
      const allSnapshots = await Promise.all(
        vaultIds.map((id) =>
          api.performance.getHistory(id, period).catch(() => [])
        )
      );

      // Build a date→balance map by summing all vaults
      const map = new Map<string, number>();
      for (const snaps of allSnapshots) {
        for (const s of snaps) {
          const day = s.recorded_at.slice(0, 10); // "YYYY-MM-DD"
          map.set(day, (map.get(day) ?? 0) + s.balance);
        }
      }

      const sorted = Array.from(map.entries())
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([date, value]) => ({ date, value }));

      setHistory(sorted);
    } catch (err) {
      if (!(err instanceof ApiError && err.status === 401)) {
        console.warn("useVaultHistory:", err);
      }
    } finally {
      setIsLoading(false);
    }
  }, [stableVaultIds, period]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  return { history, isLoading };
}
