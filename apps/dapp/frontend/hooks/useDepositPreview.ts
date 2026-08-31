"use client";

import { useEffect, useState } from "react";
import { previewDeposit, type DepositParams } from "@/lib/stellar/transaction";

const DEBOUNCE_MS = 500;

interface DepositPreview {
  sharesExpected: bigint;
  available: boolean;
  error?: string;
}

/**
 * Simulated expected shares for a deposit amount (Issue #1129), instead of
 * assuming shares received == amount deposited.
 */
export function useDepositPreview(params: DepositParams | null, enabled: boolean) {
  const [preview, setPreview] = useState<DepositPreview | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!enabled || !params) {
      setPreview(null);
      return;
    }

    let cancelled = false;
    setLoading(true);

    const timer = setTimeout(async () => {
      try {
        const result = await previewDeposit(params);
        if (!cancelled) setPreview(result);
      } catch {
        if (!cancelled) {
          setPreview({
            sharesExpected: BigInt(0),
            available: false,
            error: "Deposit preview unavailable",
          });
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, DEBOUNCE_MS);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [enabled, params]);

  return { preview, loading };
}
