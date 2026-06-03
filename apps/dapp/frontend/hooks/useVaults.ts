"use client";

/**
 * useVaults — live vault data for the authenticated user.
 *
 * Polls every 30 s so the dashboard stays fresh without WebSocket overhead.
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { api, type ApiVault, type ApiPerformanceSummary, ApiError } from "@/lib/api/client";

const POLL_INTERVAL = 30_000; // 30 s

export interface VaultWithPerf extends ApiVault {
  performance?: ApiPerformanceSummary;
}

interface UseVaultsResult {
  vaults: VaultWithPerf[];
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useVaults(userId?: string | null): UseVaultsResult {
  const [vaults, setVaults] = useState<VaultWithPerf[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchVaults = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const raw = await api.vaults.list(userId || undefined);

      // Enrich with performance summary (best-effort — don't fail if it errors)
      const enriched: VaultWithPerf[] = await Promise.all(
        raw.map(async (v) => {
          try {
            const performance = await api.performance.getSummary(v.id);
            return { ...v, performance };
          } catch {
            return v;
          }
        })
      );

      setVaults(enriched);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        // Token expired — don't surface as a noisy error
        setVaults([]);
      } else {
        setError(err instanceof Error ? err.message : "Failed to load vaults");
      }
    } finally {
      setIsLoading(false);
    }
  }, [userId]);

  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (userId === null) return;
    fetchVaults();
    timerRef.current = setInterval(fetchVaults, POLL_INTERVAL);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [userId, fetchVaults]);

  return { vaults, isLoading, error, refresh: fetchVaults };
}
