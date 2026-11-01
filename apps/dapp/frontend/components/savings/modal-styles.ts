import { INPUT } from "@/components/ui/page-kit";

export const MODAL_BACKDROP = "fixed inset-0 z-50 bg-background/70 backdrop-blur-sm";

export const MODAL_PANEL =
  "fixed inset-x-4 top-16 z-50 mx-auto max-h-[calc(100vh-5rem)] max-w-lg overflow-y-auto rounded-xl border bg-card p-6 sm:inset-auto sm:left-1/2 sm:top-1/2 sm:w-full sm:-translate-x-1/2 sm:-translate-y-1/2";

export const FIELD_LABEL = "mb-1.5 block text-[13px] text-muted-foreground";

export const NUMBER_INPUT = `${INPUT} tabular-nums [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none`;

export const GOAL_CATEGORIES = [
  { id: "other", label: "Other" },
  { id: "emergency_fund", label: "Emergency fund" },
  { id: "education", label: "Education" },
  { id: "housing", label: "Housing" },
  { id: "travel", label: "Travel" },
  { id: "business", label: "Business" },
  { id: "health", label: "Health" },
  { id: "retirement", label: "Retirement" },
] as const;
