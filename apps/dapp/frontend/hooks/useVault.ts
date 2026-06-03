import { useState, useEffect } from "react";
import { api } from "@/lib/api/client";
import type { Vault, MarketType } from "@/lib/types/vault";

export function useVault(vaultId: string) {
    const [vault, setVault] = useState<Vault | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        if (!vaultId) return;
        setIsLoading(true);
        Promise.all([
            api.vaults.getById(vaultId),
            api.performance.getSummary(vaultId).catch(() => null)
        ]).then(([v, perf]) => {
            const apy = perf?.apy_30d ? perf.apy_30d * 100 : 0;
            const isPair = v.currency.includes("-");
            const marketType: MarketType = isPair ? "pair" : "single";
            const tokens = isPair ? v.currency.split("-") : [v.currency];

            const mapped: Vault = {
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
            };
            setVault(mapped);
        }).catch(() => {
            setVault(null);
        }).finally(() => {
            setIsLoading(false);
        });
    }, [vaultId]);

    return { vault, isLoading };
}
