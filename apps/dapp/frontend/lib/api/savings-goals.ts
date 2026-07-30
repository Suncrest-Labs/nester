import { apiRequest } from "@/lib/api/client";

export interface SavingsGoalVelocity {
  avg_weekly_deposit: string | number;
  projected_days_to_completion?: number;
  on_track: boolean;
}

export interface SavingsGoal {
  id: string;
  target_amount: string | number;
  currency: string;
  deadline: string;
  description?: string;
  name?: string;
  category?: string;
  status?: "active" | "completed" | "paused" | "archived";
  current_amount: string | number;
  progress_pct: number;
  /** Whether yield is automatically reinvested toward the goal. */
  auto_compound?: boolean;
  /** Vault this goal is linked to, when set (see #688). */
  vault_id?: string;
  /** Velocity stats (#714). */
  avg_weekly_deposit?: string | number;
  projected_days_to_completion?: number;
  on_track?: boolean;
  /** Completion fields (#716). */
  completed_at?: string;
  completion_action?: string;
  /** Active recurring schedule (#734). */
  active_schedule?: {
    id: string;
    amount: string | number;
    currency: string;
    frequency: "weekly" | "biweekly" | "monthly";
    next_run_at: string;
    status: string;
  };
  /** Sharing. */
  share_token?: string;
  is_shared?: boolean;
  /** Progress visualization fields (#869). */
  principal_amount?: string | number;
  yield_amount?: string | number;
  locked_positions?: Array<{
    id: string;
    amount: string | number;
    locked_at: string;
    matures_at: string;
    boost_percent: number;
    yield_earned: string | number;
  }>;
  flexible_amount?: string | number;
  projection?: {
    success_probability: number;
    on_track: boolean;
    monthly_gap?: number;
    timeline: Array<{
      date: string;
      median: number;
      upper_bound: number;
      lower_bound: number;
    }>;
  };
  asset_composition?: Array<{
    asset: string;
    value: number;
    percentage: number;
    color: string;
  }>;
}

export interface SharedGoalView {
  name: string;
  emoji?: string;
  target_amount: string | number;
  currency: string;
  current_amount: string | number;
  progress_pct: number;
  deadline: string;
  category?: string;
  status?: string;
}

/** A single contribution toward a savings goal (#732). */
export interface SavingsGoalContribution {
  id: string;
  amount: string | number;
  currency: string;
  /** Where the contribution came from, e.g. "deposit" or "yield". */
  source?: string;
  tx_hash?: string;
  created_at: string;
}

export interface CreateSavingsGoalInput {
  target_amount: number;
  currency: string;
  deadline: string;
  description?: string;
  category?: string;
  auto_compound?: boolean;
}

export const savingsGoals = {
  list: () => apiRequest<SavingsGoal[]>("/users/savings-goals"),
  get: (id: string) => apiRequest<SavingsGoal>(`/users/savings-goals/${id}`),
  contributions: (id: string) =>
    apiRequest<SavingsGoalContribution[]>(
      `/users/savings-goals/${id}/contributions`
    ),
  create: (input: CreateSavingsGoalInput) =>
    apiRequest<SavingsGoal>("/users/savings-goals", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  update: (id: string, input: Partial<CreateSavingsGoalInput>) =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
  delete: (id: string) =>
    apiRequest<void>(`/users/savings-goals/${id}`, { method: "DELETE" }),
  pause: (id: string) =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/pause`, { method: "PATCH" }),
  resume: (id: string) =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/resume`, { method: "PATCH" }),
  archive: (id: string) =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/archive`, { method: "PATCH" }),
  complete: (id: string, action: "reinvest" | "withdraw") =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/complete`, {
      method: "POST",
      body: JSON.stringify({ action }),
    }),
  share: (id: string) =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/share`, { method: "POST" }),
  unshare: (id: string) =>
    apiRequest<void>(`/users/savings-goals/${id}/share`, { method: "DELETE" }),
  getShared: (token: string) =>
    apiRequest<SharedGoalView>(`/savings-goals/shared/${token}`),
};
