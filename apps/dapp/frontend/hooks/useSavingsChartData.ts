import { useQuery } from "@tanstack/react-query";
import { vaultsApi, type APYHistoryPeriod } from "@/lib/api/vaults";
import { type SavingsChartPoint } from "@/components/analytics/SavingsChart";

const STALE_TIME_MS = 15 * 60 * 1000; // 15 minutes

/**
 * Fetches real APY history for a vault from `/vaults/{id}/apy-history` and maps
 * it into chart-ready points for <SavingsChart />.
 *
 * Disabled until a `vaultId` is known, so callers can pass `undefined` while the
 * primary vault is still being resolved.
 */
export function useSavingsChartData(
    vaultId: string | undefined,
    period: APYHistoryPeriod = "30d"
) {
    return useQuery<SavingsChartPoint[]>({
        queryKey: ["savings-chart", vaultId, period],
        queryFn: async () => {
            if (!vaultId) return [];
            const res = await vaultsApi.getApyHistory(vaultId, period);
            return (res.points ?? []).map((p) => {
                const pct = p.apy * 100;
                return {
                    date: new Date(p.timestamp).toLocaleDateString("en-US", {
                        month: "short",
                        day: "numeric",
                    }),
                    apy: pct.toFixed(2),
                    value: pct,
                };
            });
        },
        enabled: !!vaultId,
        staleTime: STALE_TIME_MS,
    });
}
