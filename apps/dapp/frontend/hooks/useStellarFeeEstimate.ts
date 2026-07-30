"use client";

import { useEffect, useState } from "react";
import {
  estimateDepositFee,
  estimateWithdrawFee,
  type DepositParams,
  type NetworkFeeEstimate,
  type WithdrawParams,
} from "@/lib/stellar/transaction";

const DEBOUNCE_MS = 500;

export function useStellarFeeEstimate(
  kind: "deposit" | "withdraw",
  params: DepositParams | WithdrawParams | null,
  enabled: boolean
) {
  const [estimate, setEstimate] = useState<NetworkFeeEstimate | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!enabled || !params) {
      setEstimate(null);
      return;
    }

    let cancelled = false;
    setLoading(true);

    const timer = setTimeout(async () => {
      try {
        const result =
          kind === "deposit"
            ? await estimateDepositFee(params as DepositParams)
            : await estimateWithdrawFee(params as WithdrawParams);
        if (!cancelled) setEstimate(result);
      } catch {
        if (!cancelled) {
          setEstimate({
            feeStroops: BigInt(0),
            feeXlm: 0,
            available: false,
            error: "Fee estimation unavailable",
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
  }, [kind, enabled, params]);

  return { estimate, loading };
}
