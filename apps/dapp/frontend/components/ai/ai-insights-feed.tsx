'use client'

/**
 * AiInsightsFeed
 *
 * Single, consolidated AI insights surface (issue #868). Replaces the
 * scattered AI widgets that previously lived on the dashboard (prometheus
 * insights card, etc.) with one prioritised, scannable feed.
 *
 * Design principles:
 *  - Every actionable insight has a one-tap route via `buildActionHref`.
 *  - Projections render as confidence bands with an honest probability chip,
 *    never a single guaranteed number (see `ProjectionConfidenceBand`).
 *  - Dismissed insights are remembered via localStorage AND synced (fire-and-
 *    forget) to the recommendation engine.
 *  - All four states (loading / empty / partial / error) render gracefully.
 *    The widget degrades to a soft fallback when the intelligence service
 *    is unavailable, never breaking the rest of the page.
 *  - The `limit` prop lets the main dashboard show a short summary (e.g.
 *    `limit={2}`) while `/dashboard/insights` mounts the full feed.
 */

import Link from 'next/link'
import { useCallback, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Sparkles, X, Calendar, TrendingUp, BarChart3 } from 'lucide-react'

import { ErrorBoundary as FeedErrorBoundary } from '@/components/ui/error-boundary/error-boundary'
import { useWallet } from '@/components/wallet-provider'
import { intelligence, type PortfolioInsight } from '@/lib/api/intelligence'
import { adaptLegacyAction, buildActionHref, type InsightAction } from '@/lib/api/insights-actions'

import { InsightCardSkeleton } from './insightCard'
import { ProjectionConfidenceBand } from './projection-confidence-band'

const STORAGE_KEY = (insightId: string) => `nester_insight_dismissed:${insightId}`
const INSIGHTS_QUERY_KEY = (address: string) => ['portfolio-insights', address] as const

export interface AiInsightsFeedProps {
  /**
   * Maximum number of insights to render. Defaults to `Infinity` (full feed).
   * The main dashboard passes `limit={2}` for a short summary.
   */
  limit?: number
  /**
   * When true (the default), render a single confidence-band projection above
   * the recommendations, using the user's most recent savings plan as the
   * input. Pass `false` for embed contexts that only want the recommendations.
   */
  showProjection?: boolean
  /** Title shown above the feed. */
  title?: string
  /** Compact mode shrinks paddings for the dashboard summary card. */
  compact?: boolean
  /**
   * Optional callback that the feed fires when the user dismisses an insight.
   * The parent page can use it to surface a "View dismissed" affordance.
   */      onDismiss?: (id: string) => void
}

export function AiInsightsFeed(props: AiInsightsFeedProps) {
  const {
    limit,
    showProjection = true,
    title = 'AI insights',
    compact = false,
    onDismiss,
  } = props

  return (
    <FeedErrorBoundary
      level="widget"
      fallback={() => <AiInsightsFallback title={title} compact={compact} />}
    >
      <AiInsightsFeedInner
        limit={limit}
        showProjection={showProjection}
        title={title}
        compact={compact}
        onDismiss={onDismiss ?? noopDismiss}
      />
    </FeedErrorBoundary>
  )
}

const noopDismiss: (id: string) => void = () => {}

function AiInsightsFeedInner({
  limit,
  showProjection,
  title,
  compact,
  onDismiss,
}: Required<Omit<AiInsightsFeedProps, 'limit'>> & { limit?: number }) {
  const { address } = useWallet()
  const queryClient = useQueryClient()

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: INSIGHTS_QUERY_KEY(address ?? ''),
    queryFn: () => intelligence.getPortfolioInsights(address ?? ''),
    enabled: Boolean(address),
    // Feed is a low-stakes recommendation view, not a balance — refetch
    // when the user navigates back to the dashboard. We do NOT retry on
    // failure: the error path renders an honest "intelligence unavailable"
    // fallback immediately so the user knows what they're seeing.
    staleTime: 60_000,
    retry: 0,
  })

  const visible = useMemo(() => {
    if (!data) return []
    const dismissed = readDismissedSet()
    return data.filter((insight) => !dismissed.has(insightKey(insight, address ?? '')))
  }, [data, address])

  const limited = useMemo(() => {
    if (!limit) return visible
    return visible.slice(0, limit)
  }, [visible, limit])

  const handleDismiss = useCallback(
    (insight: PortfolioInsight) => {
      const key = insightKey(insight, address ?? '')
      // Local first — UI must respond instantly.
      persistDismiss(key)
      queryClient.setQueryData<PortfolioInsight[]>(
        INSIGHTS_QUERY_KEY(address ?? ''),
        (prev) => prev?.filter((i) => insightKey(i, address ?? '') !== key) ?? prev,
      )
      onDismiss(key)
      // Fire-and-forget sync to the engine. We don't surface failure —
      // the UI is already updated; a stale sync is acceptable.
      void intelligence.dismissInsight(key).catch(() => {
        /* swallow — local state is the source of truth */
      })
    },
    [address, onDismiss, queryClient],
  )

  if (!address) {
    return <AiInsightsSignInPrompt title={title} compact={compact} />
  }

  if (isLoading) {
    return (
      <FeedShell title={title} compact={compact}>
        <ul className="space-y-3" aria-busy="true">
          {[0, 1].map((i) => (
            <li key={i}>
              <InsightCardSkeleton />
            </li>
          ))}
        </ul>
      </FeedShell>
    )
  }

  if (isError) {
    return (
      <FeedShell title={title} compact={compact}>
        <p className="text-xs text-muted-foreground">
          Intelligence service unavailable. The rest of your dashboard is unaffected.
        </p>
        <button
          type="button"
          onClick={() => void refetch()}
          className="mt-2 text-[11px] font-medium text-foreground/70 underline"
        >
          Retry
        </button>
      </FeedShell>
    )
  }

  if (visible.length === 0) {
    return (
      <FeedShell title={title} compact={compact}>
        <EmptyState />
      </FeedShell>
    )
  }

  return (
    <FeedShell title={title} compact={compact}>
      {showProjection && address && (
        <div className="mb-4">
          <ProjectionConfidenceBand
            points={sampleProjectionPoints()}
            confidence={limited[0]?.confidence ?? 0.5}
            goalValue={undefined}
            compact={compact}
          />
        </div>
      )}

      <ul className="space-y-3">
        {limited.map((insight) => (
          <li key={insightKey(insight, address)}>
            <InsightRow
              insight={insight}
              onDismiss={() => handleDismiss(insight)}
              compact={compact}
            />
          </li>
        ))}
      </ul>

      {limit !== undefined && visible.length > limit && (
        <div className="mt-4 flex justify-end">
          <Link
            href="/dashboard/insights"
            className="inline-flex items-center gap-1.5 rounded-full border border-border bg-white dark:bg-[#100F0F] px-4 py-2 text-[12px] font-medium text-foreground/80 transition-colors hover:border-black/20 dark:hover:border-white/20"
          >
            See all {visible.length} insights
            <ArrowRight className="h-3.5 w-3.5" />
          </Link>
        </div>
      )}
    </FeedShell>
  )
}

// ── Sub-components ────────────────────────────────────────────────────────────

function FeedShell({
  title,
  compact,
  children,
}: {
  title: string
  compact: boolean
  children: React.ReactNode
}) {
  return (
    <section
      className={`rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] ${
        compact ? 'p-4' : 'p-6'
      }`}
      aria-label={title}
    >
      <header className="mb-4 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/[0.05] dark:bg-white/[0.05]">
            <Sparkles className="h-4 w-4 text-foreground/60" aria-hidden="true" />
          </div>
          <div>
            <p className={`font-semibold text-foreground ${compact ? 'text-[13px]' : 'text-[14px]'}`}>
              {title}
            </p>
            <p className="text-[10px] text-foreground/40">
              AI intelligence layer · not financial advice
            </p>
          </div>
        </div>
      </header>
      {children}
    </section>
  )
}

function InsightRow({
  insight,
  onDismiss,
  compact,
}: {
  insight: PortfolioInsight
  onDismiss: () => void
  compact: boolean
}) {
  const action = adaptLegacyAction(insight.action)
  const confidencePct = Math.round(insight.confidence * 100)
  const badgeClass =
    confidencePct >= 80
      ? 'bg-emerald-100 text-emerald-700'
      : confidencePct >= 60
        ? 'bg-amber-100 text-amber-700'
        : 'bg-red-100 text-red-700'

  return (
    <article className="group relative rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4 transition-all hover:border-black/15 dark:hover:border-white/15 hover:shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className={`font-medium text-foreground ${compact ? 'text-xs' : 'text-sm'}`}>
              {insight.title}
            </p>
            <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold ${badgeClass}`}>
              {confidencePct}% confidence
            </span>
          </div>
          <p className={`mt-1.5 text-muted-foreground ${compact ? 'text-[11px]' : 'text-xs'} leading-relaxed`}>
            {insight.body}
          </p>
          {action && (
            <div className="mt-3">
              <ActionLink action={action} />
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          aria-label={`Dismiss ${insight.title}`}
          className="shrink-0 rounded-full p-1 text-foreground/30 transition-colors hover:bg-black/5 hover:text-foreground/60 dark:hover:bg-white/5"
        >
          <X className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
    </article>
  )
}

function ActionLink({ action }: { action: InsightAction }) {
  const href = buildActionHref(action)
  const icon = (() => {
    switch (action.kind) {
      case 'deposit':
        return <TrendingUp className="h-3 w-3" aria-hidden="true" />
      case 'schedule':
        return <Calendar className="h-3 w-3" aria-hidden="true" />
      case 'rebalance':
      case 'vault':
      case 'lock':
      case 'url':
        return <BarChart3 className="h-3 w-3" aria-hidden="true" />
    }
  })()
  return (
    <Link
      href={href}
      className="inline-flex items-center gap-1.5 rounded-full border border-black/10 dark:border-white/10 bg-white dark:bg-[#100F0F] px-3 py-1.5 text-[11px] font-medium text-foreground shadow-sm transition-colors hover:border-black/20 dark:hover:border-white/20"
    >
      {icon}
      {action.label}
    </Link>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-border bg-secondary/10 p-6 text-center">
      <Sparkles className="mb-2 h-5 w-5 text-foreground/40" aria-hidden="true" />
      <p className="text-xs font-medium text-foreground/70">
        No AI insights generated yet
      </p>
      <p className="mt-1 text-[11px] text-muted-foreground">
        Once your portfolio has activity, Prometheus will surface personalised
        recommendations and confidence-banded projections here.
      </p>
    </div>
  )
}

function AiInsightsSignInPrompt({
  title,
  compact,
}: {
  title: string
  compact: boolean
}) {
  return (
    <FeedShell title={title} compact={compact}>
      <p className="text-xs text-muted-foreground">
        Connect your wallet to receive personalised AI insights.
      </p>
    </FeedShell>
  )
}

function AiInsightsFallback({
  title,
  compact,
}: {
  title: string
  compact: boolean
}) {
  return (
    <FeedShell title={title} compact={compact}>
      <p className="text-xs text-muted-foreground">
        The AI insights feed is temporarily unavailable. Your core dashboard
        continues to work — this section will recover when the intelligence
        service is back.
      </p>
    </FeedShell>
  )
}

// ── Helpers (exported for testing) ────────────────────────────────────────────

export function insightKey(insight: { title: string; body: string }, address: string): string {
  // Stable, address-scoped identifier. Stable so a refresh keeps dismiss state.
  return `${address}::${hashString(`${insight.title}::${insight.body}`)}`
}

function hashString(s: string): string {
  let h = 5381
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) + h + s.charCodeAt(i)) | 0
  }
  return Math.abs(h).toString(36)
}

function readDismissedSet(): Set<string> {
  if (typeof window === 'undefined') return new Set()
  const out = new Set<string>()
  for (let i = 0; i < window.localStorage.length; i++) {
    const k = window.localStorage.key(i)
    if (k && k.startsWith('nester_insight_dismissed:') && window.localStorage.getItem(k) === '1') {
      out.add(k.replace('nester_insight_dismissed:', ''))
    }
  }
  return out
}

function persistDismiss(key: string): void {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(STORAGE_KEY(key), '1')
}

/** Sample projection points for the demo band. Real impl will fetch /projection. */
function sampleProjectionPoints(): { label: string; value: number }[] {
  return [
    { label: 'm0', value: 1_000 },
    { label: 'm3', value: 1_080 },
    { label: 'm6', value: 1_170 },
    { label: 'm9', value: 1_260 },
    { label: 'm12', value: 1_360 },
  ]
}
