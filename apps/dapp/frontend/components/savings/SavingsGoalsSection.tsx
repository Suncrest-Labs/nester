"use client";

import { useState } from "react";
import {
  Briefcase,
  GraduationCap,
  Heart,
  Home,
  Plane,
  Plus,
  Shield,
  Target,
  TrendingUp,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { format } from "date-fns";
import { cn } from "@/lib/utils";
import type { SavingsGoal } from "@/lib/api/savings-goals";
import { useSavingsGoals } from "@/hooks/useSavingsGoals";
import { useWallet } from "@/components/wallet-provider";
import { DeadlineBadge } from "@/components/savings/DeadlineBadge";
import { SavingsOnboardingWizard } from "@/components/savings/SavingsOnboardingWizard";
import { ProgressVisualization } from "@/components/savings/ProgressVisualization";
import type { GoalProgressData } from "@/lib/types/progress";

const CATEGORY_ICONS: Record<string, LucideIcon> = {
  emergency_fund: Target,
  education: Target,
  housing: Target,
  travel: Target,
  business: Target,
  health: Target,
  retirement: Target,
  other: Target,
};

function goalDisplayName(goal: SavingsGoal): string {
  if (goal.description?.trim()) return goal.description.trim();
  if (goal.category) {
    return goal.category.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
  return "Savings Goal";
}

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(value) || 0;
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

function GoalsSkeleton() {
  return (
    <div className="space-y-3" data-testid="savings-goals-skeleton">
      {[0, 1].map((i) => (
        <div key={i} className="animate-pulse rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-5">
          <div className="mb-3 h-4 w-40 rounded bg-black/10 dark:bg-white/10" />
          <div className="mb-4 h-2 w-full rounded-full bg-black/10 dark:bg-white/10" />
          <div className="flex justify-between">
            <div className="h-3 w-24 rounded bg-black/10 dark:bg-white/10" />
            <div className="h-3 w-20 rounded bg-black/10 dark:bg-white/10" />
          </div>
        </div>
      ))}
    </div>
  );
}

function GoalCard({ goal }: { goal: SavingsGoal }) {
  const Icon = CATEGORY_ICONS[goal.category ?? "other"] ?? Target;
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const progress = Math.min(100, Math.max(0, goal.progress_pct ?? 0));
  const isPaused = goal.status === "paused";
  const isCompleted = goal.status === "completed";
  const hasRichData = goal.principal_amount != null || goal.locked_positions?.length;

  return (
    <div
      className={cn(
        "rounded-2xl border bg-white dark:bg-[#100F0F] p-5 transition-opacity",
        isPaused && "opacity-60 border-amber-200 bg-amber-50/40",
        isCompleted && "border-emerald-200 bg-emerald-50/30",
        !isPaused && !isCompleted && "border-black/8 dark:border-white/8"
      )}
      data-testid="savings-goal-card"
    >
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-black/5 dark:bg-white/5">
            <Icon className="h-4 w-4 text-black/50 dark:text-white/50" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <p className="truncate text-sm font-semibold text-black dark:text-white">{goalDisplayName(goal)}</p>
              {isPaused && (
                <span className="shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-bold uppercase text-amber-700">
                  Paused
                </span>
              )}
              {isCompleted && (
                <span className="shrink-0 rounded-full bg-emerald-100 px-2 py-0.5 text-[10px] font-bold uppercase text-emerald-700">
                  Done
                </span>
              )}
            </div>
            <p className="text-[11px] text-black/50 dark:text-white/50 font-medium">
              Target {target.toLocaleString()} {goal.currency}
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
        <div className="mb-2 h-2 overflow-hidden rounded-full bg-black/8 dark:bg-white/8">
          <div
            className={cn("h-full rounded-full transition-all", isCompleted ? "bg-emerald-500" : "bg-black dark:bg-white")}
            style={{ width: `${progress}%` }}
            role="progressbar"
            aria-valuenow={progress}
            aria-valuemin={0}
            aria-valuemax={100}
          />
        </div>
      )}

      <div className="flex items-center justify-between text-[11px] text-black/50 dark:text-white/50 font-medium mt-2">
        <span>{progress.toFixed(0)}% complete</span>
        <div className="flex items-center gap-2">
          {goal.on_track != null && !isCompleted && (
            <span
              className={cn(
                "flex items-center gap-1",
                goal.on_track ? "text-emerald-600" : "text-red-500"
              )}
            >
              <TrendingUp className="h-3 w-3" aria-hidden="true" />
              {goal.on_track ? "On track" : "Behind"}
            </span>
          )}
          {goal.deadline && (
            <DeadlineBadge deadline={goal.deadline} status={goal.status} />
          )}
        </div>
      </div>
    </div>
  );
}

export function SavingsGoalsSection({ onCreateGoal }: { onCreateGoal?: () => void }) {
  const { isConnected } = useWallet();
  const { data: goals, isLoading, isError, refetch, isFetching } = useSavingsGoals();
  const [showOnboarding, setShowOnboarding] = useState(false);

  if (!isConnected) {
    return (
      <div
        className="mb-10 rounded-3xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-8 text-center"
        data-testid="savings-goals-connect-prompt"
      >
        <p className="text-sm font-semibold text-black dark:text-white">Connect your wallet to see your savings goals</p>
        <p className="mt-1 text-xs text-black/60 dark:text-white/60 font-medium">
          Link your wallet to track progress toward your personal targets.
        </p>
      </div>
    );
  }

  return (
    <div className="mb-10" data-testid="savings-goals-section">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-black dark:text-white">Your Savings Goals</h2>
          <p className="text-xs text-black/60 dark:text-white/60 font-medium mt-0.5">Progress toward your targets</p>
        </div>
        {onCreateGoal && (
          <button
            type="button"
            onClick={onCreateGoal}
            className="flex items-center gap-1.5 rounded-xl bg-black dark:bg-blue-600 px-3.5 py-2 text-xs font-semibold text-white transition-opacity hover:opacity-75"
          >
            <Plus className="h-3.5 w-3.5" aria-hidden="true" />
            New Goal
          </button>
        )}
      </div>

      {isLoading ? (
        <GoalsSkeleton />
      ) : isError ? (
        <div
          className="rounded-2xl border border-red-100 bg-red-50 p-5 text-center"
          data-testid="savings-goals-error"
        >
          <p className="text-sm font-medium text-red-800">Failed to load savings goals</p>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className="mt-3 rounded-lg bg-black dark:bg-blue-600 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50"
          >
            {isFetching ? "Retrying…" : "Retry"}
          </button>
        </div>
      ) : !goals?.length ? (
        <>
          <div
            className="rounded-3xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-10 text-center"
            data-testid="savings-goals-empty"
          >
            <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5 dark:bg-white/5">
              <Target className="h-6 w-6 text-black/40 dark:text-white/40" aria-hidden="true" />
            </div>
            <p className="text-sm font-semibold text-black dark:text-white">No savings goals yet</p>
            <p className="mx-auto mt-1 max-w-xs text-xs text-black/60 dark:text-white/60 font-medium">
              Set a target amount and deadline to start tracking your progress.
            </p>
            <button
              type="button"
              onClick={() => setShowOnboarding(true)}
              className="mt-5 rounded-xl bg-black dark:bg-blue-600 px-6 py-2.5 text-xs font-semibold text-white transition-opacity hover:opacity-75"
              data-testid="savings-goals-start-onboarding"
            >
              Set up your first goal
            </button>
          </div>
          {showOnboarding && (
            <SavingsOnboardingWizard
              onComplete={() => setShowOnboarding(false)}
              onDismiss={() => setShowOnboarding(false)}
            />
          )}
        </>
      ) : (
        <div className={cn("grid gap-3", goals.length > 1 && "sm:grid-cols-2")}>
          {goals.map((goal) => (
            <Link
              key={goal.id}
              href={`/savings/${goal.id}`}
              aria-label={`View ${goalDisplayName(goal)} details`}
              className="block rounded-2xl transition-transform hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-black"
            >
              <GoalCard goal={goal} />
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
