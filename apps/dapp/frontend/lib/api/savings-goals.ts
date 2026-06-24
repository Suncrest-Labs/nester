import { apiRequest } from "@/lib/api/client";

export interface SavingsGoal {
  id: string;
  target_amount: string | number;
  currency: string;
  deadline: string;
  description?: string;
  current_amount: string | number;
  progress_pct: number;
  /** Vault this goal is linked to, when set (see #688). */
  vault_id?: string;
}

export interface CreateSavingsGoalInput {
  target_amount: number;
  currency: string;
  deadline: string;
  description?: string;
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
};
