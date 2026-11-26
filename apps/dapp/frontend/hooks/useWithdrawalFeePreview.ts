"use client";

import { useEffect, useState } from "react";
import {
  previewWithdrawalFee,
  previewFeeSchedule,
  type WithdrawalFeePreviewParams,
  type WithdrawalFeePreviewResult,
  type FeeSchedulePreviewResult,
} from "@/lib/stellar/transaction";

const DEBOUNCE_MS = 300;

export function useWithdrawalFeePreview(
  params: WithdrawalFeePreviewParams | null,
  enabled: boolean
) {
  const [feePreview, setFeePreview] = useState<WithdrawalFeePreviewResult | null>(null);
  const [previewedShares, setPreviewedShares] = useState<number | bigint | null>(null);
  const [schedulePreview, setSchedulePreview] = useState<FeeSchedulePreviewResult | null>(null);
  const [loading, setLoading] = useState(false);

  const walletAddress = params?.walletAddress ?? "";
  const contractId = params?.contractId ?? "";
  const shares = params?.shares ?? 0;

  useEffect(() => {
    if (!enabled || !walletAddress || !contractId || Number(shares) <= 0) {
      setFeePreview(null);
      setPreviewedShares(null);
      setSchedulePreview(null);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);

    const timer = setTimeout(async () => {
      try {
        const [feeRes, scheduleRes] = await Promise.all([
          previewWithdrawalFee({ walletAddress, contractId, shares }),
          previewFeeSchedule({ walletAddress, contractId }),
        ]);
        if (!cancelled) {
          setFeePreview(feeRes);
          setPreviewedShares(shares);
          setSchedulePreview(scheduleRes);
        }
      } catch {
        if (!cancelled) {
          setFeePreview(null);
          setPreviewedShares(null);
          setSchedulePreview(null);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, DEBOUNCE_MS);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [enabled, walletAddress, contractId, shares]);

  return { feePreview, previewedShares, schedulePreview, loading };
}
