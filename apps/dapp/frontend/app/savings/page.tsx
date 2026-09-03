"use client";

import { useState } from "react";
import { AppShell } from "@/components/app-shell";
import { SavingsGoalsSection } from "@/components/savings/SavingsGoalsSection";
import { CreateGoalModal } from "@/components/savings/CreateGoalModal";

/**
 * Savings is where a target is set and funded on a schedule.
 *
 * Choosing where the yield comes from lives on /yields, and vault mechanics
 * live on /vaults. This page previously also advertised four "savings vaults"
 * that carried no contract address or vault id and whose deposit button opened
 * a modal that closed again — a mock next to two working pages. It is gone
 * rather than restyled.
 */
export default function SavingsPage() {
    const [createOpen, setCreateOpen] = useState(false);

    return (
        <AppShell>
            <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8">
                <div className="mb-8 flex flex-wrap items-start justify-between gap-4">
                    <div>
                        <h1 className="text-2xl font-semibold tracking-tight text-black dark:text-white">
                            Savings
                        </h1>
                        <p className="mt-1 text-sm text-black/55 dark:text-white/55">
                            Set a target, then fund it automatically on a schedule.
                        </p>
                    </div>
                    <button
                        type="button"
                        onClick={() => setCreateOpen(true)}
                        className="rounded-xl bg-black px-4 py-2.5 text-sm font-semibold text-white transition-opacity hover:opacity-80 dark:bg-blue-600"
                    >
                        Create a goal
                    </button>
                </div>

                <SavingsGoalsSection onCreateGoal={() => setCreateOpen(true)} />
            </div>

            {createOpen && <CreateGoalModal onClose={() => setCreateOpen(false)} />}
        </AppShell>
    );
}
