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
    fetch(`${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1"}/users/savings-goals/${id}`, {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${typeof window !== "undefined" ? localStorage.getItem("nester_token") ?? "" : ""}`,
      },
    }),
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
    fetch(`${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1"}/users/savings-goals/${id}/share`, {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${typeof window !== "undefined" ? localStorage.getItem("nester_token") ?? "" : ""}`,
      },
    }),
  getShared: (token: string) =>
    apiRequest<SharedGoalView>(`/savings-goals/shared/${token}`),
};
