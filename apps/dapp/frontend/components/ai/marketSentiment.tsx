'use client'

import { useEffect, useRef, useState } from 'react'
import { RefreshCw, TrendingDown, TrendingUp, Minus, AlertCircle } from 'lucide-react'
import { intelligence, type MarketSentiment, type MarketSentimentPoint } from '@/lib/api/intelligence'
import { useRelativeAge } from '@/hooks/useRelativeAge'

/** Refresh the sentiment widget every 5 minutes. */
const REFRESH_INTERVAL_MS = 5 * 60 * 1000

// ── Signal config ─────────────────────────────────────────────────────────────

const SIGNAL_CONFIG = {
  bull: {
    label: 'Bullish',
    Icon: TrendingUp,
    dot: 'bg-emerald-500',
    badge: 'bg-emerald-50 text-emerald-700 border-emerald-100',
  },
  bear: {
    label: 'Bearish',
    Icon: TrendingDown,
    dot: 'bg-red-500',
    badge: 'bg-red-50 text-red-700 border-red-100',
  },
  neutral: {
    label: 'Neutral',
    Icon: Minus,
    dot: 'bg-amber-400',
    badge: 'bg-amber-50 text-amber-700 border-amber-100',
  },
} as const

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * MarketSentiment
 *
 * Shows the current DeFi market signal (Bull / Bear / Neutral), a one-sentence
 * AI summary, and a confidence badge. Refreshes automatically every 5 minutes
 * or on manual click.
 *
 * Degrades gracefully: if the intelligence service is unreachable, shows a
 * subtle error state without breaking the rest of the dashboard.
 */
export function MarketSentimentWidget() {
  const [data, setData] = useState<MarketSentiment | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [spinning, setSpinning] = useState(false)
  const [historyRange, setHistoryRange] = useState<7 | 30>(7)
  const [historyPoints, setHistoryPoints] = useState<MarketSentimentPoint[]>([])
  // When the last successful read happened, so a cached figure can be labelled
  // with its age instead of being presented as current (nester#1126).
  const [fetchedAt, setFetchedAt] = useState<number | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Ticks only while degraded, which is the only time the age is shown.
  const age = useRelativeAge(fetchedAt, error)

  const fetch = async (manual = false) => {
    if (manual) setSpinning(true)
    setError(false)
    try {
      const sentiment = await intelligence.getMarketSentiment()
      setData(sentiment)
      setFetchedAt(Date.now())
    } catch {
      setError(true)
    } finally {
      setLoading(false)
      if (manual) setTimeout(() => setSpinning(false), 600)
    }
  }

  const fetchHistory = async (range: 7 | 30) => {
    try {
      const { points } = await intelligence.getMarketSentimentHistory(range)
      setHistoryPoints(points)
    } catch {
      setHistoryPoints([])
    }
  }

  useEffect(() => {
    fetch()
    intervalRef.current = setInterval(() => fetch(), REFRESH_INTERVAL_MS)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [])

  useEffect(() => {
    fetchHistory(historyRange)
  }, [historyRange])

  if (loading) return <MarketSentimentSkeleton />

  // Unreachable with nothing cached: say so plainly and offer a retry that
  // does not need a page reload. Never a spinner that runs forever.
  if (error && !data) {
    return (
      <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <AlertCircle className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
            <p className="text-xs font-medium text-foreground/60">Market Sentiment</p>
          </div>
          <button
            type="button"
            onClick={() => fetch(true)}
            disabled={spinning}
            className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
            aria-label="Retry loading market sentiment"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${spinning ? 'animate-spin' : ''}`} />
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground" role="status">
          Intelligence service unavailable. Retrying automatically.
        </p>
      </div>
    )
  }

  if (!data) return <MarketSentimentSkeleton />

  const { label, Icon, dot, badge } = SIGNAL_CONFIG[data.signal]
  const confidencePct = Math.round(data.confidence * 100)

  return (
    <div
      data-stale={error ? 'true' : 'false'}
      className={`rounded-2xl border bg-white dark:bg-[#100F0F] p-4 transition-all hover:shadow-sm ${
        error
          ? 'border-amber-300 dark:border-amber-500/40'
          : 'border-border hover:border-black/15 dark:hover:border-white/15'
      }`}
    >
      {/* Stale banner: the service is unreachable but a previous read is still
          on screen. Labelling it with its age is the point — an unlabelled
          cached figure reads as current, which is the failure #1126 describes. */}
      {error && (
        <div
          role="status"
          className="mb-3 flex items-center justify-between gap-2 rounded-lg border border-amber-200 bg-amber-50/60 px-2.5 py-1.5 dark:border-amber-500/30 dark:bg-amber-500/10"
        >
          <span className="flex items-center gap-1.5 text-[11px] text-amber-800 dark:text-amber-200">
            <AlertCircle className="h-3 w-3 shrink-0" aria-hidden="true" />
            {age ? `Cached, last updated ${age}` : 'Cached — not current'}
          </span>
          <button
            type="button"
            onClick={() => fetch(true)}
            disabled={spinning}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium text-amber-900 transition-colors hover:bg-amber-100 disabled:opacity-50 dark:text-amber-100 dark:hover:bg-amber-500/20"
          >
            <RefreshCw className={`h-3 w-3 ${spinning ? 'animate-spin' : ''}`} aria-hidden="true" />
            Retry
          </button>
        </div>
      )}

      {/* Header */}
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className={`h-2 w-2 rounded-full ${dot}`} />
          <p className="text-xs font-medium text-foreground/60">Market Sentiment</p>
        </div>
        <div className="flex items-center gap-2">
          {/* Auto-refresh indicator */}
          <span className="text-[10px] text-muted-foreground">5 min</span>
          <button
            type="button"
            onClick={() => fetch(true)}
            aria-label="Refresh sentiment"
            className="text-muted-foreground hover:text-foreground transition-colors"
          >
            <RefreshCw className={`h-3.5 w-3.5 transition-transform ${spinning ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* Signal badge. Dimmed while stale so the figure itself, not just the
          banner, shows it is not live. */}
      <div className={`mb-2 flex items-center gap-2 ${error ? 'opacity-60' : ''}`}>
        <span className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-semibold ${badge}`}>
          <Icon className="h-3 w-3" />
          {label}
        </span>
        <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] font-medium text-foreground/60">
          {confidencePct}%
        </span>
      </div>

      {/* Historical trend */}
      <SentimentSparkline
        points={historyPoints}
        range={historyRange}
        onRangeChange={setHistoryRange}
      />

      {/* Summary */}
      <p className="text-xs leading-relaxed text-muted-foreground">{data.summary}</p>

      {data.contexts && data.contexts.length > 0 && (
        <div className="mt-3 space-y-2 border-t border-border pt-3">
          <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            Sourced market context · informational only
          </p>
          {data.contexts.map((context) => (
            <div key={`${context.source_url}-${context.observed_at}`} className="text-xs">
              <p className="text-foreground/80">
                <span className="font-medium">{context.protocol}:</span> {context.summary}
              </p>
              <a
                href={context.source_url}
                target="_blank"
                rel="noreferrer noopener"
                className="mt-0.5 inline-block text-[10px] text-muted-foreground underline"
              >
                Source: {context.publisher} · {Math.round(context.confidence * 100)}% confidence
              </a>
            </div>
          ))}
          <p className="text-[10px] leading-relaxed text-muted-foreground">
            {data.disclaimer ??
              'Low-trust context, not financial advice. It cannot trigger fund movements.'}
          </p>
        </div>
      )}

      {/* Timestamp */}
      <p className="mt-2 text-[10px] text-muted-foreground/50">
        Updated {new Date(data.updatedAt).toLocaleTimeString()}
      </p>
    </div>
  )
}

// ── Sparkline ─────────────────────────────────────────────────────────────────

const SPARKLINE_SIGNAL_COLOR: Record<MarketSentimentPoint['signal'], string> = {
  bull: '#10b981', // emerald-500, matches SIGNAL_CONFIG.bull.dot
  bear: '#ef4444', // red-500, matches SIGNAL_CONFIG.bear.dot
  neutral: '#fbbf24', // amber-400, matches SIGNAL_CONFIG.neutral.dot
}

const SPARKLINE_WIDTH = 100
const SPARKLINE_HEIGHT = 24
const SPARKLINE_PADDING_Y = 3

interface SentimentSparklineProps {
  points: MarketSentimentPoint[]
  range: 7 | 30
  onRangeChange: (range: 7 | 30) => void
}

/**
 * SentimentSparkline
 *
 * A small confidence-over-time trend line (7 or 30 day) so users see how
 * sentiment has been moving, not just the current point-in-time read.
 * Line color follows the most recent point's signal.
 */
function SentimentSparkline({ points, range, onRangeChange }: SentimentSparklineProps) {
  const hasEnoughData = points.length >= 2

  const path = hasEnoughData
    ? (() => {
        const xs = points.map((_, i) => (i / (points.length - 1)) * SPARKLINE_WIDTH)
        const usableHeight = SPARKLINE_HEIGHT - SPARKLINE_PADDING_Y * 2
        const ys = points.map(
          (p) => SPARKLINE_HEIGHT - SPARKLINE_PADDING_Y - p.confidence * usableHeight
        )
        return xs.map((x, i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${ys[i].toFixed(2)}`).join(' ')
      })()
    : ''

  const lineColor = hasEnoughData
    ? SPARKLINE_SIGNAL_COLOR[points[points.length - 1].signal]
    : SPARKLINE_SIGNAL_COLOR.neutral

  return (
    <div className="mb-3 flex items-center gap-2">
      <div className="h-6 flex-1">
        {hasEnoughData ? (
          <svg
            viewBox={`0 0 ${SPARKLINE_WIDTH} ${SPARKLINE_HEIGHT}`}
            preserveAspectRatio="none"
            className="h-6 w-full"
            role="img"
            aria-label={`Sentiment confidence trend over the last ${range} days`}
          >
            <path d={path} fill="none" stroke={lineColor} strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
          </svg>
        ) : (
          <p className="text-[10px] leading-6 text-muted-foreground/60">
            Not enough history yet to chart a trend
          </p>
        )}
      </div>
      <div className="flex gap-0.5" role="tablist" aria-label="Sentiment trend period">
        {([7, 30] as const).map((r) => (
          <button
            key={r}
            type="button"
            role="tab"
            aria-selected={range === r}
            onClick={() => onRangeChange(r)}
            className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
              range === r
                ? 'bg-secondary text-foreground'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {r}d
          </button>
        ))}
      </div>
    </div>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

export function MarketSentimentSkeleton() {
  return (
    <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4 animate-pulse">
      <div className="mb-3 flex items-center gap-2">
        <div className="h-2 w-2 rounded-full bg-secondary" />
        <div className="h-3 w-28 rounded bg-secondary" />
      </div>
      <div className="mb-2 h-6 w-20 rounded-full bg-secondary" />
      <div className="space-y-1.5">
        <div className="h-3 w-full rounded bg-secondary" />
        <div className="h-3 w-3/4 rounded bg-secondary" />
      </div>
    </div>
  )
}
