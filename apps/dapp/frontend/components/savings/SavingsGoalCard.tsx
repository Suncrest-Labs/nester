"use client";

import { useMemo } from "react";
import { isBefore } from "date-fns";
import { Target, TrendingUp, Lock } from "lucide-react";
import type { SavingsGoal } from "@/lib/api/savings-goals";
import { ProgressVisualization } from "./ProgressVisualization";
import type { GoalProgressData } from "@/lib/types/progress";

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(String(value)) || 0;
}

function buildProgressData(goal: SavingsGoal): GoalProgressData {
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const principal = goal.principal_amount != null ? toNumber(goal.principal_amount) : current;
  const yieldAmt = goal.yield_amount != null ? toNumber(goal.yield_amount) : 0;
  const locked = goal.locked_positions?.map((p) => ({
    id: p.id,
    amount: toNumber(p.amount),
    currency: goal.currency,
    locked_at: p.locked_at,
    matures_at: p.matures_at,
    boost_percent: p.boost_percent,
    yield_earned: toNumber(p.yield_earned),
  })) ?? [];
  const flexible = goal.flexible_amount != null ? toNumber(goal.flexible_amount) : Math.max(0, current - locked.reduce((s, p) => s + p.amount, 0));

  return {
    current_amount: current,
    target_amount: target,
    currency: goal.currency,
    principal_amount: principal,
    yield_amount: yieldAmt,
    locked_positions: locked,
    flexible_amount: flexible,
    projection: goal.projection ? {
      vault_id: goal.vault_id,
      currency: goal.currency,
      current_apy: 0,
      timeline: goal.projection.timeline,
      success_probability: goal.projection.success_probability,
      on_track: goal.projection.on_track,
      monthly_gap: goal.projection.monthly_gap,
    } : undefined,
    asset_composition: goal.asset_composition,
    deadline: goal.deadline,
    status: goal.status,
  };
}

export function SavingsGoalCard({
  goal,
  onDeposit,
  onArchive,
}: {
  goal: SavingsGoal;
  onDeposit: (g: SavingsGoal) => void;
  onArchive: (id: string) => void;
}) {
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const deadlineDate = goal.deadline ? new Date(goal.deadline) : null;
  const { daysLeft, deadlineLabel } = useMemo(() => {
    if (!deadlineDate) return { daysLeft: null, deadlineLabel: "—" } as const;
    const now = Date.now();
    const left = isBefore(deadlineDate, new Date(now)) ? 0 : Math.max(0, Math.ceil((deadlineDate.getTime() - now) / 86400000));
    return {
      daysLeft: left,
      deadlineLabel: left === 0 ? "Due today" : `${left} day${left > 1 ? "s" : ""} left`,
    };
  }, [deadlineDate]);

  const hasRichData = goal.principal_amount != null || goal.locked_positions?.length;

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

      {hasRichData ? (
        <ProgressVisualization progress={buildProgressData(goal)} compact />
      ) : (
        <>
          <div className="mb-2 h-2 overflow-hidden rounded-full bg-black/8 dark:bg-white/8">
            <div
              className="h-full rounded-full bg-black dark:bg-white transition-all"
              style={{ width: `${Math.min(100, Math.max(0, goal.progress_pct ?? (target > 0 ? (current / target) * 100 : 0)))}%` }}
              role="progressbar"
              aria-valuenow={Math.min(100, Math.max(0, goal.progress_pct ?? (target > 0 ? (current / target) * 100 : 0)))}
              aria-valuemin={0}
              aria-valuemax={100}
            />
          </div>

          <div className="flex items-center justify-between text-[11px] text-black/50 dark:text-white/50 font-medium mb-3">
            <span>{Math.floor(Math.min(100, Math.max(0, goal.progress_pct ?? (target > 0 ? (current / target) * 100 : 0))))}% complete</span>
            <span>{deadlineLabel}</span>
          </div>

          <div className="flex items-center gap-3 text-[11px] text-black/60 dark:text-white/60 font-medium mb-3">
            {(goal.yield_amount != null && toNumber(goal.yield_amount) > 0) && (
              <span className="flex items-center gap-1 text-emerald-600">
                <TrendingUp className="h-3 w-3" aria-hidden="true" />
                +{toNumber(goal.yield_amount).toLocaleString()} yield
              </span>
            )}
            {goal.locked_positions && goal.locked_positions.length > 0 && (
              <span className="flex items-center gap-1 text-amber-600">
                <Lock className="h-3 w-3" aria-hidden="true" />
                {goal.locked_positions.length} locked
              </span>
            )}
          </div>
        </>
      )}

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
