"use client";

import { useQuery } from "@tanstack/react-query";
import {
    api,
    type ApiPerformanceSummary,
    type ApiVault,
} from "@/lib/api/client";
import type { Vault, MarketType } from "@/lib/types/vault";

function mapApiVault(v: ApiVault, perf: ApiPerformanceSummary | null): Vault {
    const apy = perf?.apy_30d ? perf.apy_30d * 100 : 0;
    const isPair = v.currency.includes("-");
    const marketType: MarketType = isPair ? "pair" : "single";
    const tokens = isPair ? v.currency.split("-") : [v.currency];

    return {
        id: v.id,
        name: `${v.currency} Market`,
        description: `Automated yield strategies for ${v.currency}.`,
        marketType,
        tokens,
        currentApy: apy,
        apyRange: `${(apy * 0.8).toFixed(1)}-${(apy * 1.2).toFixed(1)}%`,
        tvl: parseFloat(v.total_deposited) || 0,
        utilization: 0,
        allocations: [],
        supportedAssets: tokens,
        maturityTerms: "Flexible - withdraw anytime",
        earlyWithdrawalPenalty: "None",
        contractAddress: v.contract_address,
        apyHistory: [],
        strategies: [],
        status: v.status,
        totalDeposited: parseFloat(v.total_deposited) || 0,
        currentBalance: parseFloat(v.current_balance) || 0,
        yieldEarned: parseFloat(v.yield_earned) || 0,
        feesPaid: parseFloat(v.fees_paid) || 0,
    };
}

export function useVault(vaultId: string) {
    const query = useQuery({
        queryKey: ["vault", vaultId],
        enabled: Boolean(vaultId),
        queryFn: async () => {
            const [vault, performance] = await Promise.all([
                api.vaults.getById(vaultId),
                api.performance.getSummary(vaultId).catch(() => null),
            ]);
            return mapApiVault(vault, performance);
        },
    });

    return {
        vault: query.data ?? null,
        isLoading: query.isLoading,
        error: query.error instanceof Error ? query.error.message : null,
        refetch: query.refetch,
    };
}
