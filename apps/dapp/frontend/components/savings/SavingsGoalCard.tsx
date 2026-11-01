"use client";

import { isBefore } from "date-fns";
import { cn } from "@/lib/utils";
import type { SavingsGoal } from "@/lib/api/savings-goals";
import { BUTTON_SECONDARY, PANEL } from "@/components/ui/page-kit";
import { ProgressVisualization } from "./ProgressVisualization";
import { GoalProgressBar, formatGoalAmount } from "./GoalProgressBar";
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

function daysLeftLabel(deadline?: string): string {
  if (!deadline) return "No date set";
  const deadlineDate = new Date(deadline);
  const now = Date.now();
  const left = isBefore(deadlineDate, new Date(now)) ? 0 : Math.max(0, Math.ceil((deadlineDate.getTime() - now) / 86400000));
  return left === 0 ? "Due today" : `${left} day${left > 1 ? "s" : ""} left`;
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
  const pct = Math.min(100, Math.max(0, goal.progress_pct ?? (target > 0 ? (current / target) * 100 : 0)));
  const deadlineLabel = daysLeftLabel(goal.deadline);

  const hasRichData = goal.principal_amount != null || goal.locked_positions?.length;

  return (
    <div className={PANEL} data-testid="savings-goal-card">
      <p className="truncate text-sm font-medium text-foreground">
        {goal.description ?? goal.category ?? "Savings goal"}
      </p>
      <p className="mt-3 font-heading text-3xl font-light tabular-nums text-foreground">
        {formatGoalAmount(current)}
        <span className="ml-2 text-base text-muted-foreground">of {formatGoalAmount(target, goal.currency)}</span>
      </p>

      {hasRichData ? (
        <div className="mt-4">
          <ProgressVisualization progress={buildProgressData(goal)} compact />
        </div>
      ) : (
        <>
          <GoalProgressBar percent={pct} className="mt-4" />
          <div className="mt-3 flex items-center justify-between text-[13px] text-muted-foreground">
            <span className="tabular-nums">{Math.floor(pct)}% complete</span>
            <span>{deadlineLabel}</span>
          </div>
          {goal.yield_amount != null && toNumber(goal.yield_amount) > 0 && (
            <p className="mt-2 text-[13px] tabular-nums text-muted-foreground">
              +{toNumber(goal.yield_amount).toLocaleString()} yield
            </p>
          )}
          {goal.locked_positions && goal.locked_positions.length > 0 && (
            <p className="mt-1 text-[13px] text-muted-foreground">{goal.locked_positions.length} locked</p>
          )}
        </>
      )}

      <div className="mt-5 flex gap-3">
        <button type="button" onClick={() => onDeposit(goal)} className={BUTTON_SECONDARY}>
          Deposit
        </button>
        <button
          type="button"
          onClick={() => onArchive(goal.id)}
          className={cn(BUTTON_SECONDARY, "text-red-600 dark:text-red-400")}
        >
          Archive
        </button>
      </div>
    </div>
  );
}
