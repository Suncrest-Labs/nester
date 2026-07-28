"use client";

import { AppShell } from "@/components/app-shell";
import { AiInsightsFeed } from "@/components/ai/ai-insights-feed";

/**
 * /dashboard/insights
 *
 * Full AI insights surface for issue #868. A single, well-designed page that
 * assembles the user's personalised recommendations, projections, coaching
 * and digest content into a prioritised, scannable feed — each item
 * actionable with a one-tap route and honest about uncertainty.
 *
 * The consolidated `AiInsightsFeed` here is also embedded as a short
 * summary (`limit={2}`) on the main /dashboard page, so the user always
 * sees their top-of-mind insights without leaving their primary view.
 */
export default function InsightsPage() {
  return (
    <AppShell>
      <div className="space-y-6">
        <header>
          <h1 className="text-[28px] font-semibold tracking-[-0.02em] text-black dark:text-white">
            AI insights
          </h1>
          <p className="mt-1 text-[13px] text-black/50 dark:text-white/50">
            Personalised recommendations, projections and coaching from the Prometheus
            intelligence layer. Probabilistic — not guaranteed.
          </p>
        </header>

        <AiInsightsFeed
          title="Your AI feed"
          limit={undefined /* full feed */}
          showProjection
          compact={false}
        />
      </div>
    </AppShell>
  );
}
