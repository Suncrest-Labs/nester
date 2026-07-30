import { apiRequest } from "@/lib/api/client";

export interface ProjectionPoint {
  month: number;
  principal: string;
  yield: string;
  total: string;
}

export interface ProjectionSummary {
  total_deposited: string;
  total_yield: string;
  final_balance: string;
  effective_apy: string;
}

export interface ProjectionInput {
  initial_deposit: string;
  monthly_contribution: string;
  apy: string;
  period_months: number;
  compound_frequency: "daily" | "monthly";
}

export interface ProjectionOutput {
  vault_id?: string;
  currency: string;
  current_apy: number;
  input: ProjectionInput;
  timeline: ProjectionPoint[];
  summary: ProjectionSummary;
  calculated_at: string;
}

export interface VaultProjectionParams {
  deposit: string;
  period: string; // e.g., "12m"
  compound?: "daily" | "monthly";
  apy?: string; // Optional APY override
}

// ── Monte Carlo savings forecast (#843) ────────────────────────────────────
//
// POST /tools/simulation runs many randomized paths over the horizon
// (varying yield and contribution behavior within modeled distributions —
// see apps/api/internal/domain/projection/README.md for the full write-up
// of every distributional assumption) and returns a P10/P50/P90 band per
// month, plus, when a target amount and deadline are known, the probability
// of hitting the goal in time and a small deposit/deadline sensitivity grid.

export interface SimulationInput {
  vault_id?: string;
  goal_id?: string;
  initial_deposit: string;
  monthly_contribution: string;
  apy?: string; // optional override/explicit mean APY
  period_months?: number;
  compound_frequency?: "daily" | "monthly";
  target_amount?: string;
  deadline_months?: number;
  path_count?: number;
}

export interface PercentileTimelinePoint {
  month: number;
  p10: string;
  p50: string;
  p90: string;
}

export interface PercentileBand {
  p10: string;
  p50: string;
  p90: string;
}

export interface GoalSuccessProbability {
  target_amount: string;
  deadline_months: number;
  probability: number; // 0..1
}

export interface SensitivityGridPoint {
  monthly_contribution_delta: string;
  deadline_months_delta: number;
  success_probability: number; // 0..1
}

export interface SimulationOutput {
  vault_id?: string;
  currency: string;
  input: SimulationInput;
  expected_apy: number;
  apy_std_dev: number;
  contribution_skip_probability: number;
  volatility_source: "historical" | "default_prior";
  contribution_source: "schedule" | "default_prior";
  path_count: number;
  seed: number;
  timeline: PercentileTimelinePoint[];
  final_band: PercentileBand;
  goal_success?: GoalSuccessProbability;
  sensitivity_grid?: SensitivityGridPoint[];
  calculated_at: string;
}

export const projectionApi = {
  // Generic projection calculation
  calculateProjection: (input: ProjectionInput) =>
    apiRequest<ProjectionOutput>("/tools/projection", {
      method: "POST",
      body: JSON.stringify(input),
    }),

  // Vault-specific projection
  calculateVaultProjection: (vaultId: string, params: VaultProjectionParams) => {
    const query = new URLSearchParams({
      deposit: params.deposit,
      period: params.period,
      ...(params.compound && { compound: params.compound }),
      ...(params.apy && { apy: params.apy }),
    });

    return apiRequest<ProjectionOutput>(`/vaults/${vaultId}/projection?${query}`);
  },

  // Monte Carlo savings forecast (#843): P10/P50/P90 band + goal-success
  // probability + sensitivity grid.
  simulateProjection: (input: SimulationInput) =>
    apiRequest<SimulationOutput>("/tools/simulation", {
      method: "POST",
      body: JSON.stringify(input),
    }),
};

export function formatSuccessProbability(probability: number): string {
  return `${Math.round(probability * 100)}%`;
}

// Helper functions for working with projection data

export function formatProjectionAmount(amount: string): string {
  const num = parseFloat(amount);
  return num.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function formatProjectionAPY(apy: string | number): string {
  const num = typeof apy === "string" ? parseFloat(apy) : apy;
  return (num * 100).toFixed(2) + "%";
}

export function calculateMonthlyGrowthRate(timeline: ProjectionPoint[]): number {
  if (timeline.length < 2) return 0;
  
  const firstMonth = parseFloat(timeline[0].total);
  const lastMonth = parseFloat(timeline[timeline.length - 1].total);
  const months = timeline.length;
  
  if (firstMonth === 0) return 0;
  
  return Math.pow(lastMonth / firstMonth, 1 / months) - 1;
}

export function getProjectionMilestones(timeline: ProjectionPoint[]): ProjectionPoint[] {
  // Return milestone points (every 3 months or at key intervals)
  const milestones: ProjectionPoint[] = [];
  
  timeline.forEach((point, index) => {
    // Always include first and last
    if (index === 0 || index === timeline.length - 1) {
      milestones.push(point);
      return;
    }
    
    // Include every 3rd month for shorter timelines, every 6th for longer
    const interval = timeline.length > 24 ? 6 : 3;
    if ((point.month - 1) % interval === 0) {
      milestones.push(point);
    }
  });
  
  return milestones;
}