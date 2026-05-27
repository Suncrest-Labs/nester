"use client";

/**
 * useSettlements — live settlement history for the authenticated user.
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { api, type ApiSettlement, ApiError } from "@/lib/api/client";

const POLL_INTERVAL = 30_000;

interface UseSettlementsResult {
  settlements: ApiSettlement[];
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useSettlements(userId: string | null): UseSettlementsResult {
  const [settlements, setSettlements] = useState<ApiSettlement[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetch = useCallback(async () => {
    if (!userId) return;
    setIsLoading(true);
    setError(null);
    try {
      const data = await api.settlements.list(userId);
      setSettlements(data);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setSettlements([]);
      } else {
        setError(err instanceof Error ? err.message : "Failed to load settlements");
      }
    } finally {
      setIsLoading(false);
    }
  }, [userId]);

  useEffect(() => {
    fetch();
    timerRef.current = setInterval(fetch, POLL_INTERVAL);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [fetch]);

  return { settlements, isLoading, error, refresh: fetch };
}
