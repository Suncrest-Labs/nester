"use client";

import { AlertTriangle, Loader2 } from "lucide-react";
import type { NetworkFeeEstimate } from "@/lib/stellar/transaction";

interface Props {
  estimate: NetworkFeeEstimate | null;
  loading: boolean;
  amount: number;
  xlmUsdPrice: number;
}

export function NetworkFeeDisplay({ estimate, loading, amount, xlmUsdPrice }: Props) {
  if (loading) {
    return (
      <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        Estimating network fee…
      </div>
    );
  }

  if (!estimate) return null;

  if (!estimate.available) {
    return (
      <div className="mt-3 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
        <p className="font-medium">Fee estimate unavailable</p>
        <p className="mt-0.5 text-amber-700/90">
          {estimate.error ?? "Network RPC is unreachable."} You can still proceed at your own risk.
        </p>
      </div>
    );
  }

  const feeUsd = estimate.feeXlm * xlmUsdPrice;
  const feePct = amount > 0 ? (feeUsd / amount) * 100 : 0;
  const highFee = amount > 0 && feePct > 1;

  return (
    <div className="mt-3 rounded-xl border border-border bg-white px-3 py-2.5 text-xs">
      <div className="flex items-center justify-between text-muted-foreground">
        <span>Estimated network fee</span>
        <span className="font-medium text-foreground">
          {estimate.feeXlm.toFixed(7)} XLM
          {xlmUsdPrice > 0 && (
            <span className="ml-1 text-muted-foreground">
              (~${feeUsd.toFixed(4)} USD)
            </span>
          )}
        </span>
      </div>
      {highFee && (
        <div className="mt-2 flex items-start gap-1.5 text-amber-700">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>
            Network fee exceeds 1% of this transaction amount. Consider waiting for lower network traffic.
          </span>
        </div>
      )}
    </div>
  );
}
