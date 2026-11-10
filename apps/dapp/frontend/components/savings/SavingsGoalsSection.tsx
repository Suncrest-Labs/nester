"use client";

import { useState } from "react";
import Link from "next/link";
import type { SavingsGoal } from "@/lib/api/savings-goals";
import { useSavingsGoals } from "@/hooks/useSavingsGoals";
import { useWallet } from "@/components/wallet-provider";
import { DeadlineBadge } from "@/components/savings/DeadlineBadge";
import { SavingsOnboardingWizard } from "@/components/savings/SavingsOnboardingWizard";
import { GoalProgressBar, formatGoalAmount } from "@/components/savings/GoalProgressBar";
import {
  BUTTON_SECONDARY,
  BigNumber,
  EmptyPanel,
  PANEL,
  SKELETON,
  Section,
} from "@/components/ui/page-kit";

function goalDisplayName(goal: SavingsGoal): string {
  if (goal.description?.trim()) return goal.description.trim();
  if (goal.category) {
    return goal.category.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase());
  }
  return "Savings goal";
}

function toNumber(value: string | number): number {
  return typeof value === "number" ? value : parseFloat(value) || 0;
}

function goalPercent(goal: SavingsGoal): number {
  if (goal.progress_pct != null) return Math.min(100, Math.max(0, goal.progress_pct));
  const target = toNumber(goal.target_amount);
  return target > 0 ? Math.min(100, (toNumber(goal.current_amount) / target) * 100) : 0;
}

interface CurrencyTotal {
  currency: string;
  saved: number;
  target: number;
}

function totalsByCurrency(goals: SavingsGoal[]): CurrencyTotal[] {
  const map = new Map<string, CurrencyTotal>();
  for (const g of goals) {
    if (g.status === "archived") continue;
    const t = map.get(g.currency) ?? { currency: g.currency, saved: 0, target: 0 };
    t.saved += toNumber(g.current_amount);
    t.target += toNumber(g.target_amount);
    map.set(g.currency, t);
  }
  return [...map.values()].sort((a, b) => b.target - a.target);
}

function GoalsHero({ goals }: { goals: SavingsGoal[] }) {
  const totals = totalsByCurrency(goals);
  if (!totals.length) return null;
  const [main, ...rest] = totals;
  const pct = main.target > 0 ? (main.saved / main.target) * 100 : 0;

  return (
    <section aria-labelledby="goals-total-label" className="py-2">
      <BigNumber className="text-6xl tracking-tight sm:text-7xl">{formatGoalAmount(main.saved)}</BigNumber>
      <p id="goals-total-label" className="mt-3 text-sm text-muted-foreground">
        Saved toward your goals, of{" "}
        <span className="tabular-nums text-foreground/80">
          {formatGoalAmount(main.target, main.currency)}
        </span>
      </p>
      <GoalProgressBar percent={pct} label="Progress across all goals" className="mt-5 max-w-md" />
      {rest.map((t) => (
        <p key={t.currency} className="mt-2 text-sm text-muted-foreground">
          Plus <span className="tabular-nums text-foreground/80">{formatGoalAmount(t.saved, t.currency)}</span>{" "}
          of <span className="tabular-nums">{formatGoalAmount(t.target, t.currency)}</span>
        </p>
      ))}
    </section>
  );
}

function GoalsSkeleton() {
  return (
    <div data-testid="savings-goals-skeleton" aria-busy="true" className="mt-4 space-y-8">
      <div className="h-20 w-56 animate-pulse rounded-lg bg-foreground/[0.06]" />
      <div className="grid gap-6 lg:grid-cols-2">
        <div className={`h-40 ${SKELETON}`} />
        <div className={`h-40 ${SKELETON}`} />
      </div>
    </div>
  );
}

function GoalCard({ goal }: { goal: SavingsGoal }) {
  const current = toNumber(goal.current_amount);
  const target = toNumber(goal.target_amount);
  const pct = goalPercent(goal);
  const isPaused = goal.status === "paused";
  const isCompleted = goal.status === "completed";

  return (
    <div
      className={`${PANEL} h-full transition-colors hover:bg-foreground/[0.02]`}
      data-testid="savings-goal-card"
    >
      <div className="flex items-start justify-between gap-3">
        <p className="min-w-0 truncate text-sm font-medium text-foreground">{goalDisplayName(goal)}</p>
        {isPaused && <span className="shrink-0 text-[13px] text-muted-foreground">Paused</span>}
        {isCompleted && <span className="shrink-0 text-[13px] text-foreground">Reached</span>}
      </div>
      <p className="mt-3 font-heading text-3xl font-light tabular-nums text-foreground">
        {formatGoalAmount(current)}
        <span className="ml-2 text-base text-muted-foreground">
          of {formatGoalAmount(target, goal.currency)}
        </span>
      </p>
      <GoalProgressBar percent={pct} className="mt-4" />
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-[13px] text-muted-foreground">
        <span className="tabular-nums">{pct.toFixed(0)}% there</span>
        <div className="flex items-center gap-3">
          {goal.on_track != null && !isCompleted && (
            <span className={goal.on_track ? undefined : "text-amber-700 dark:text-amber-400"}>
              {goal.on_track ? "On track" : "Behind"}
            </span>
          )}
          {goal.deadline && <DeadlineBadge deadline={goal.deadline} status={goal.status} />}
        </div>
      </div>
    </div>
  );
}

export function SavingsGoalsSection() {
  const { isConnected } = useWallet();
  const { data: goals, isLoading, isError, refetch, isFetching } = useSavingsGoals();
  const [showOnboarding, setShowOnboarding] = useState(false);

  if (!isConnected) {
    return (
      <div className="mt-6" data-testid="savings-goals-connect-prompt">
        <EmptyPanel title="Connect your wallet to see your goals">
          Your goals are tied to your wallet. Connect it to track progress toward your targets.
        </EmptyPanel>
      </div>
    );
  }

  return (
    <div data-testid="savings-goals-section">
      {isLoading ? (
        <GoalsSkeleton />
      ) : isError ? (
        <div className={`mt-6 ${PANEL}`} data-testid="savings-goals-error">
          <p role="alert" className="text-sm text-foreground">
            Your goals couldn&apos;t be loaded. Check your connection and try again.
          </p>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className={`mt-4 ${BUTTON_SECONDARY}`}
          >
            {isFetching ? "Retrying…" : "Try again"}
          </button>
        </div>
      ) : !goals?.length ? (
        <>
          <div className="mt-6" data-testid="savings-goals-empty">
            <EmptyPanel
              title="No goals yet"
              action={
                <button
                  type="button"
                  onClick={() => setShowOnboarding(true)}
                  className={BUTTON_SECONDARY}
                  data-testid="savings-goals-start-onboarding"
                >
                  Set up your first goal
                </button>
              }
            >
              Pick a target amount and a date, then track how close you are.
            </EmptyPanel>
          </div>
          {showOnboarding && (
            <SavingsOnboardingWizard
              onComplete={() => setShowOnboarding(false)}
              onDismiss={() => setShowOnboarding(false)}
            />
          )}
        </>
      ) : (
        <>
          <div className="mt-4">
            <GoalsHero goals={goals} />
          </div>
          <Section
            id="goals-list-heading"
            title="Your goals"
            lede="Goals are targets. They don't hold money: your savings stay in the Nester Savings Rate."
          >
            <div className="grid gap-6 lg:grid-cols-2">
              {goals.map((goal) => (
                <Link
                  key={goal.id}
                  href={`/savings/${goal.id}`}
                  aria-label={`View ${goalDisplayName(goal)} details`}
                  className="block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
                >
                  <GoalCard goal={goal} />
                </Link>
              ))}
            </div>
          </Section>
        </>
      )}
    </div>
  );
}
