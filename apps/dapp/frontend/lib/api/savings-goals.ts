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
  /** Vault this goal is linked to, when set (see #688). */
  vault_id?: string;
  /** Velocity stats (#714). */
  avg_weekly_deposit?: string | number;
  projected_days_to_completion?: number;
  on_track?: boolean;
  /** Completion fields (#716). */
  completed_at?: string;
  completion_action?: string;
}

export interface CreateSavingsGoalInput {
  target_amount: number;
  currency: string;
  deadline: string;
  description?: string;
  category?: string;
}

export const savingsGoals = {
  list: () => apiRequest<SavingsGoal[]>("/users/savings-goals"),
  get: (id: string) => apiRequest<SavingsGoal>(`/users/savings-goals/${id}`),
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
  complete: (id: string, action: "reinvest" | "withdraw") =>
    apiRequest<SavingsGoal>(`/users/savings-goals/${id}/complete`, {
      method: "POST",
      body: JSON.stringify({ action }),
    }),
};
