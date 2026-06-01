import { apiRequest } from "@/lib/api/client";

export interface AllocationPct {
  protocol: string;
  percentage: number;
  apy?: number;
}

export interface RebalanceSuggestion {
  vault_id: string;
  has_suggestion: boolean;
  current_allocations: AllocationPct[];
  recommended_allocations: AllocationPct[];
  expected_apy_gain_bps: number;
  expected_apy_gain_pct: number;
  confidence: string;
  reason: string;
}

export const vaultsApi = {
  getRebalanceSuggestion: (vaultId: string) =>
    apiRequest<RebalanceSuggestion>(`/vaults/${vaultId}/rebalance-suggestion`),
  applyRebalance: (vaultId: string, allocations: AllocationPct[]) =>
    apiRequest<unknown>(`/vaults/${vaultId}/rebalance`, {
      method: "POST",
      body: JSON.stringify({ allocations }),
    }),
};
