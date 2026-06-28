import { apiRequest } from "@/lib/api/client";

export type ScheduleFrequency = "weekly" | "biweekly" | "monthly";

export interface SavingsSchedule {
  id: string;
  goal_id: string;
  vault_id: string;
  amount: string | number;
  currency: string;
  frequency: ScheduleFrequency;
  next_run_at: string;
  last_run_at?: string;
  status: string;
  created_at: string;
}

export interface CreateScheduleInput {
  amount: number;
  currency: string;
  frequency: ScheduleFrequency;
  vault_id: string;
}

export interface UpdateScheduleInput {
  amount?: number;
  currency?: string;
  frequency?: ScheduleFrequency;
  vault_id?: string;
}

export const savingsSchedules = {
  create: (goalId: string, input: CreateScheduleInput) =>
    apiRequest<SavingsSchedule>(`/users/savings-goals/${goalId}/schedule`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  update: (goalId: string, input: UpdateScheduleInput) =>
    apiRequest<SavingsSchedule>(`/users/savings-goals/${goalId}/schedule`, {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
  delete: (goalId: string) =>
    fetch(
      `${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1"}/users/savings-goals/${goalId}/schedule`,
      {
        method: "DELETE",
        headers: {
          Authorization: `Bearer ${typeof window !== "undefined" ? localStorage.getItem("nester_token") ?? "" : ""}`,
        },
      }
    ),
};
