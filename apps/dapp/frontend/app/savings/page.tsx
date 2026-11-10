"use client";

import { useState } from "react";
import { AppShell } from "@/components/app-shell";
import { SavingsGoalsSection } from "@/components/savings/SavingsGoalsSection";
import { CreateGoalModal } from "@/components/savings/CreateGoalModal";
import { BUTTON_PRIMARY, PageHeader } from "@/components/ui/page-kit";

/**
 * Goals are targets tracked against the user's Nester Savings Rate balance.
 * They do not hold funds; deposits happen on /dashboard.
 */
export default function SavingsPage() {
    const [createOpen, setCreateOpen] = useState(false);

    return (
        <AppShell>
            <PageHeader
                title="Goals"
                actions={
                    <button type="button" onClick={() => setCreateOpen(true)} className={BUTTON_PRIMARY}>
                        Create a goal
                    </button>
                }
            />

            <SavingsGoalsSection />

            {createOpen && <CreateGoalModal onClose={() => setCreateOpen(false)} />}
        </AppShell>
    );
}
