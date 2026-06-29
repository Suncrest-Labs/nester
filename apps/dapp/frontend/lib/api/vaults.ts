// lib/api/vaults.ts
import { apiRequest } from "@/lib/api/client";

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export interface ProjectionPoint {
  date: string;
  balance: number;
}

export interface Projection {
  vault_id: string;
  currency: string;
  current_apy: number;
  timeline: ProjectionPoint[];
}

export interface Transaction {
  id: string;
  vault_id: string;
  user_id: string;
  type: "deposit" | "withdrawal";
  amount: number;
  transaction_hash: string;
  created_at: string;
}

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

export type APYHistoryPeriod = "7d" | "30d" | "90d";

export interface APYHistoryPoint {
  /** ISO timestamp of the snapshot */
  timestamp: string;
  /** APY as a fraction, e.g. 0.0823 for 8.23% */
  apy: number;
}

export interface APYHistoryResponse {
  vault_id: string;
  period: APYHistoryPeriod;
  points: APYHistoryPoint[];
}

export interface HarvestPreview {
  vault_id: string;
  gross_yield_usdc: string;
  performance_fee_usdc: string;
  net_yield_usdc: string;
  compounded: boolean;
  estimated_new_shares?: string;
  performance_fee_bps: number;
  impaired?: boolean;
}

export interface HarvestResult {
  gross_yield_usdc: string;
  performance_fee_usdc: string;
  net_yield_usdc: string;
  compounded: boolean;
  new_shares_minted?: string;
  tx_hash?: string;
}

export const vaultsApi = {
  getProjection: async (vaultId: string): Promise<Projection> => {
    const res = await fetch(`${API_BASE}/api/v1/vaults/${vaultId}/projection`, {
      headers: {
        Authorization: `Bearer ${getStoredToken()}`,
      },
    });
    if (!res.ok) throw new Error("Failed to fetch projection");
    const json = await res.json();
    return json.data;
  },

  getTransactions: async (vaultId?: string): Promise<Transaction[]> => {
    const url = new URL(`${API_BASE}/api/v1/transactions`);
    if (vaultId) url.searchParams.append("vault_id", vaultId);
    
    const res = await fetch(url.toString(), {
      headers: {
        Authorization: `Bearer ${getStoredToken()}`,
      },
    });
    if (!res.ok) throw new Error("Failed to fetch transactions");
    const json = await res.json();
    return json.data ?? [];
  },

  getApyHistory: (vaultId: string, period: APYHistoryPeriod = "30d") =>
    apiRequest<APYHistoryResponse>(
      `/vaults/${vaultId}/apy-history?period=${period}`
    ),

  getRebalanceSuggestion: (vaultId: string) =>
    apiRequest<RebalanceSuggestion>(`/vaults/${vaultId}/rebalance-suggestion`),

  previewHarvest: (vaultId: string, compound: boolean) =>
    apiRequest<HarvestPreview>(
      `/vaults/${vaultId}/harvest/preview?compound=${compound}`
    ),

  harvest: (vaultId: string, compound: boolean) =>
    apiRequest<HarvestResult>(`/vaults/${vaultId}/harvest`, {
      method: "POST",
      body: JSON.stringify({ compound }),
    }),

  applyRebalance: (vaultId: string, allocations: AllocationPct[]) =>
    apiRequest<unknown>(`/vaults/${vaultId}/rebalance`, {
      method: "POST",
      body: JSON.stringify({ allocations }),
    }),
}

function getStoredToken(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem("nester_token") ?? "";
}
