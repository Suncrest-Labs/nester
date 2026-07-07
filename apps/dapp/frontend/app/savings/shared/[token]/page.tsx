"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { format } from "date-fns";
import { Target } from "lucide-react";
import { savingsGoals, type SharedGoalView } from "@/lib/api/savings-goals";
import { cn } from "@/lib/utils";

function toNumber(value: string | number | undefined): number {
  if (value === undefined) return 0;
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

export default function SharedGoalPage() {
  const { token } = useParams<{ token: string }>();
  const [goal, setGoal] = useState<SharedGoalView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!token) return;
    savingsGoals
      .getShared(token)
      .then(setGoal)
      .catch(() => setError("This savings goal link is invalid or has been revoked."))
      .finally(() => setLoading(false));
  }, [token]);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-black dark:border-white border-t-transparent" />
      </div>
    );
  }

  if (error || !goal) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50 p-6">
        <div className="rounded-3xl border border-red-100 bg-white dark:bg-[#100F0F] p-10 text-center shadow-sm">
          <p className="text-sm font-semibold text-red-700">{error || "Goal not found."}</p>
        </div>
      </div>
    );
  }

  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const progress = Math.min(100, Math.max(0, goal.progress_pct ?? 0));
  const deadline = goal.deadline ? format(new Date(goal.deadline), "MMMM d, yyyy") : "—";
  const displayName = goal.name || (goal.category ?? "Savings Goal");
  const isCompleted = goal.status === "completed";

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 p-6">
      <div className="w-full max-w-md">
        <div className="mb-6 text-center">
          <p className="text-xs font-semibold uppercase tracking-widest text-black/40 dark:text-white/40">
            Shared Savings Goal
          </p>
        </div>

        <div
          className={cn(
            "rounded-3xl border bg-white dark:bg-[#100F0F] p-8 shadow-sm",
            isCompleted ? "border-emerald-200" : "border-black/8 dark:border-white/8"
          )}
        >
          <div className="mb-6 flex items-center gap-4">
            <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl bg-black/5 dark:bg-white/5 text-3xl">
              {goal.emoji ? <span aria-hidden="true">{goal.emoji}</span> : <Target className="h-7 w-7 text-black/40 dark:text-white/40" />}
            </div>
            <div>
              <h1 className="text-xl font-bold text-black dark:text-white">{displayName}</h1>
              {goal.category && (
                <p className="text-sm text-black/50 dark:text-white/50 capitalize">
                  {goal.category.replace(/_/g, " ")}
                </p>
              )}
            </div>
          </div>

          <div className="mb-6">
            <div className="mb-2 flex items-end justify-between">
              <div>
                <p className="font-mono text-2xl font-bold text-black dark:text-white">
                  {current.toLocaleString()}
                </p>
                <p className="text-xs text-black/50 dark:text-white/50">
                  of {target.toLocaleString()} {goal.currency}
                </p>
              </div>
              <div className="text-right">
                <p
                  className={cn(
                    "text-2xl font-bold",
                    isCompleted ? "text-emerald-600" : "text-black dark:text-white"
                  )}
                >
                  {progress.toFixed(0)}%
                </p>
                <p className="text-xs text-black/50 dark:text-white/50">progress</p>
              </div>
            </div>
            <div className="h-3 overflow-hidden rounded-full bg-black/8 dark:bg-white/8">
              <div
                className={cn(
                  "h-full rounded-full transition-all duration-500",
                  isCompleted ? "bg-emerald-500" : "bg-black dark:bg-white"
                )}
                style={{ width: `${progress}%` }}
                role="progressbar"
                aria-valuenow={progress}
                aria-valuemin={0}
                aria-valuemax={100}
              />
            </div>
          </div>

          <div className="flex items-center justify-between rounded-2xl bg-black/[0.03] dark:bg-white/[0.03] px-4 py-3">
            <div className="text-center">
              <p className="text-[10px] font-bold uppercase tracking-wide text-black/40 dark:text-white/40">Deadline</p>
              <p className="mt-0.5 text-sm font-semibold text-black dark:text-white">{deadline}</p>
            </div>
            <div className="text-center">
              <p className="text-[10px] font-bold uppercase tracking-wide text-black/40 dark:text-white/40">Status</p>
              <p className={cn("mt-0.5 text-sm font-semibold capitalize", isCompleted ? "text-emerald-600" : "text-black dark:text-white")}>
                {goal.status ?? "active"}
              </p>
            </div>
          </div>

          {isCompleted && (
            <div className="mt-4 rounded-2xl bg-emerald-50 px-4 py-3 text-center">
              <p className="text-sm font-semibold text-emerald-700">🎉 Goal achieved!</p>
            </div>
          )}
        </div>

        <p className="mt-6 text-center text-xs text-black/30 dark:text-white/30">
          Powered by{" "}
          <a href="/" className="underline hover:text-black dark:hover:text-white">
            Nester
          </a>
        </p>
      </div>
    </div>
  );
}
