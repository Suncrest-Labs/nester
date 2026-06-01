import { apiRequest } from "@/lib/api/client";

export type RiskProfile = "conservative" | "moderate" | "aggressive";

export interface UserProfile {
  id: string;
  wallet_address: string;
  display_name: string;
  risk_profile?: RiskProfile;
  savings_goal?: string;
  onboarding_completed: boolean;
}

export interface UpdateProfileInput {
  risk_profile?: RiskProfile;
  savings_goal?: string;
  onboarding_completed?: boolean;
}

export const profileApi = {
  get: () => apiRequest<UserProfile>("/users/profile"),
  update: (input: UpdateProfileInput) =>
    apiRequest<UserProfile>("/users/profile", {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
};
