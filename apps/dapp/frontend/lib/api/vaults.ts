// lib/api/vaults.ts
import { apiRequest } from "@/lib/api/client";

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

function newIdempotencyKey(): string {
  return crypto.randomUUID();
}


/** Server-derived position for the signed-in user in a single vault. */
export interface UserVaultPosition {
  vault_id: string;
  user_id: string;
  total_deposited_usdc: string;
  shares_held: string;
  current_value_usdc: string;
  unrealized_pnl_usdc: string;
  unrealized_pnl_pct: string;
  fees_paid_usdc: string;
  first_deposit_at: string | null;
  last_activity_at: string | null;
}

export interface RegisterTransactionInput {
  vault_id: string;
  type: "deposit" | "withdrawal";
  /** Decimal string — the API parses this exactly, so never send a float. */
  amount: string;
  currency: string;
  tx_hash: string;
}

export const vaultsApi = {
  /**
   * Records a signed on-chain transaction against the vault.
   *
   * The row is created as "pending" and stays that way until the API's
   * reconciliation poller confirms the hash against Horizon and verifies the
   * amount actually moved on-chain, so calling this cannot credit a deposit
   * that did not happen. Registering is what makes a position outlive the
   * browser: positions are derived server-side from these rows.
   */
  registerTransaction: (input: RegisterTransactionInput) =>
    apiRequest<Transaction>(`/transactions`, {
      method: "POST",
      headers: { "Idempotency-Key": newIdempotencyKey() },
      body: JSON.stringify(input),
    }),

  /** The signed-in user's position in one vault, derived from indexed rows. */
  getMyPosition: (vaultId: string) =>
    apiRequest<UserVaultPosition>(`/vaults/${vaultId}/my-position`),

  getProjection: (vaultId: string) =>
    apiRequest<Projection>(`/vaults/${vaultId}/projection`),

  getTransactions: async (vaultId?: string): Promise<Transaction[]> => {
    const query = vaultId ? `?vault_id=${encodeURIComponent(vaultId)}` : "";
    const data = await apiRequest<Transaction[]>(`/transactions${query}`);
    return data ?? [];
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
      headers: { "Idempotency-Key": newIdempotencyKey() },
      body: JSON.stringify({ compound }),
    }),

  applyRebalance: (vaultId: string, allocations: AllocationPct[]) =>
    apiRequest<unknown>(`/vaults/${vaultId}/rebalance`, {
      method: "POST",
      headers: { "Idempotency-Key": newIdempotencyKey() },
      body: JSON.stringify({ allocations }),
    }),
}
