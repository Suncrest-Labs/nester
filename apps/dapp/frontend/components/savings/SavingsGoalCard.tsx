"use client";

import { formatDistanceToNowStrict, isBefore } from "date-fns";
import { Target } from "lucide-react";
import { cn } from "@/lib/utils";
import type { SavingsGoal } from "@/lib/api/savings-goals";

export function SavingsGoalCard({
  goal,
  onDeposit,
  onArchive,
}: {
  goal: SavingsGoal;
  onDeposit: (g: SavingsGoal) => void;
  onArchive: (id: string) => void;
}) {
  const current = typeof goal.current_amount === "number" ? goal.current_amount : parseFloat(String(goal.current_amount)) || 0;
  const target = typeof goal.target_amount === "number" ? goal.target_amount : parseFloat(String(goal.target_amount)) || 0;
  const progress = Math.min(100, Math.max(0, goal.progress_pct ?? (target > 0 ? (current / target) * 100 : 0)));

  const deadlineDate = goal.deadline ? new Date(goal.deadline) : null;
  const daysLeft = deadlineDate ? (isBefore(deadlineDate, new Date()) ? 0 : Math.max(0, Math.ceil((deadlineDate.getTime() - Date.now()) / 86400000))) : null;
  const deadlineLabel = deadlineDate ? (daysLeft === 0 ? "Due today" : `${daysLeft} day${(daysLeft ?? 0) > 1 ? "s" : ""} left`) : "—";

  return (
    <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5" data-testid="savings-goal-card">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-black/5 dark:bg-white/5">
            <Target className="h-4 w-4 text-black/50 dark:text-white/50" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold text-black dark:text-white">{goal.description ?? (goal.category ?? "Savings Goal")}</p>
            <p className="text-[11px] text-black/50 dark:text-white/50 font-medium">
              {`Target ${target.toLocaleString()} ${goal.currency}`}
            </p>
          </div>
        </div>
        <div className="text-right shrink-0">
          <p className="font-mono text-sm text-black dark:text-white">{current.toLocaleString()}</p>
          <p className="text-[10px] text-black/50 dark:text-white/50 uppercase font-bold tracking-wide">Saved</p>
        </div>
      </div>

      <div className="mb-2 h-2 overflow-hidden rounded-full bg-black/8 dark:bg-white/8">
        <div
          className="h-full rounded-full bg-black dark:bg-white transition-all"
          style={{ width: `${progress}%` }}
          role="progressbar"
          aria-valuenow={progress}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>

      <div className="flex items-center justify-between text-[11px] text-black/50 dark:text-white/50 font-medium mb-3">
        <span>{Math.floor(progress)}% complete</span>
        <span>{deadlineLabel}</span>
      </div>

      <div className="mb-4 text-[11px] text-black/60 dark:text-white/60 font-medium">
        <span>{goal.currency}</span>
        {goal.vault_id && <span className="mx-2">·</span>}
        <span className="truncate">{goal.vault_id ? `Vault ${goal.vault_id.slice(0, 6)}` : "Unlinked"}</span>
      </div>

      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => onDeposit(goal)}
          className="flex-1 rounded-xl border border-black/10 dark:border-white/10 px-3 py-2 text-sm font-semibold text-black dark:text-white hover:bg-black/5 dark:hover:bg-white/5"
        >
          Deposit
        </button>
        <button
          type="button"
          onClick={() => onArchive(goal.id)}
          className="rounded-xl bg-black dark:bg-blue-600 px-3 py-2 text-sm font-semibold text-white hover:opacity-90"
        >
          Archive
        </button>
      </div>
    </div>
  );
}
