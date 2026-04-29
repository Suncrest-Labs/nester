'use client'

import { useEffect, useRef, useState } from 'react'
import { RefreshCw, TrendingDown, TrendingUp, Minus } from 'lucide-react'
import { intelligence, type MarketSentiment } from '@/lib/api/intelligence'

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
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetch = async (manual = false) => {
    if (manual) setSpinning(true)
    setError(false)
    try {
      const sentiment = await intelligence.getMarketSentiment()
      setData(sentiment)
    } catch {
      setError(true)
    } finally {
      setLoading(false)
      if (manual) setTimeout(() => setSpinning(false), 600)
    }
  }

  useEffect(() => {
    fetch()
    intervalRef.current = setInterval(() => fetch(), REFRESH_INTERVAL_MS)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [])

  if (loading) return <MarketSentimentSkeleton />

  if (error || !data) {
    return (
      <div className="rounded-2xl border border-border bg-white p-4">
        <div className="flex items-center justify-between">
          <p className="text-xs font-medium text-foreground/60">Market Sentiment</p>
          <button
            type="button"
            onClick={() => fetch(true)}
            className="text-muted-foreground hover:text-foreground transition-colors"
            aria-label="Retry"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          Intelligence service unavailable. Retrying automatically.
        </p>
      </div>
    )
  }

  const { label, Icon, dot, badge } = SIGNAL_CONFIG[data.signal]
  const confidencePct = Math.round(data.confidence * 100)

  return (
    <div className="rounded-2xl border border-border bg-white p-4 transition-all hover:border-black/15 hover:shadow-sm">
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

      {/* Signal badge */}
      <div className="mb-2 flex items-center gap-2">
        <span className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-semibold ${badge}`}>
          <Icon className="h-3 w-3" />
          {label}
        </span>
        <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] font-medium text-foreground/60">
          {confidencePct}%
        </span>
      </div>

      {/* Summary */}
      <p className="text-xs leading-relaxed text-muted-foreground">{data.summary}</p>

      {/* Timestamp */}
      <p className="mt-2 text-[10px] text-muted-foreground/50">
        Updated {new Date(data.updatedAt).toLocaleTimeString()}
      </p>
    </div>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

export function MarketSentimentSkeleton() {
  return (
    <div className="rounded-2xl border border-border bg-white p-4 animate-pulse">
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