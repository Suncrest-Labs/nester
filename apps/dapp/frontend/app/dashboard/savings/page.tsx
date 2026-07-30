"use client";

import { SavingsCalculator } from "@/components/compound-interest/savings-calculator";
import { AppShell } from "@/components/app-shell";
import { ErrorBoundary } from "@/components/ui/error-boundary/error-boundary";

export default function SavingsPage() {
  return (
    <ErrorBoundary level="page">
      <AppShell>
        <div className="space-y-8">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-[30px] font-semibold text-black dark:text-white tracking-[-0.02em]">
                Savings Planner
              </h1>
              <p className="text-black/60 dark:text-white/60 mt-2">
                Calculate your savings growth with compound interest
              </p>
            </div>
          </div>

          <SavingsCalculator />
        </div>
      </AppShell>
    </ErrorBoundary>
  );
}