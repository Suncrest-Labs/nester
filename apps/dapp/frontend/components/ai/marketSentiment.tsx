'client'

import { useEffect, useRef, useState } from 'react'
import { RefreshCw, TrendingDown, TrendingUp, Minus, AlertCircle } from 'lucide-react'
import { intelligence, type MarketSentiment, type MarketSentimentPoint } from '@/lib/api/intelligence'

/** Refresh the sentiment widget every 5 minutes. */
const REFRESH_INTERVAL_MS = 5 * 60 * 1000

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

export function MarketSentimentWidget() {
  const [data, setData] = useState<MarketSentiment | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [spinning, setSpinning] = useState(false)
  const [historyRange, setHistoryRange] = useState<7 | 30>(7)
  const [historyPoints, setHistoryPoints] = useState<MarketSentimentPoint[]>([])
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchSentiment = async (manual = false) => {
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

  const fetchHistory = async (range: 7 | 30) => {
    try {
      const { points } = await intelligence.getMarketSentimentHistory(range)
      setHistoryPoints(points)
    } catch {
      setHistoryPoints([])
    }
  }

  useEffect(() => {
    fetchSentiment()
    intervalRef.current = setInterval(() => fetchSentiment(), REFRESH_INTERVAL_MS)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [])

  useEffect(() => {
    fetchHistory(historyRange)
  }, [historyRange])

  if (loading) return <MarketSentimentSkeleton />

  if (error || !data) {
    return (
      <div className="rounded-2xl border border-amber-200 bg-amber-50/40 dark:bg-[#100F0F] p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4 text-amber-600" />
            <p className="text-xs font-medium text-foreground/80">Market Sentiment</p>
          </div>
          <button
            type="button"
            onClick={() => fetchSentiment(true)}
            className="inline-flex items-center gap-1.5 rounded-lg border border-amber-300 bg-white dark:bg-zinc-900 px-2.5 py-1 text-xs font-medium text-amber-800 dark:text-amber-200 hover:bg-amber-100 transition-colors"
            aria-label="Retry"
          >
            <RefreshCw className={`h-3 w-3 ${spinning ? 'animate-spin' : ''}`} />
            Retry
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          Intelligence service unavailable. Showing degraded state—cached figures or live feeds are unreachable.
        </p>
      </div>
    )
  }

  const { label, Icon, dot, badge } = SIGNAL_CONFIG[data.signal]
  const confidencePct = Math.round(data.confidence * 100)

  return (
    <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4 transition-all hover:border-black/15 dark:hover:border-white/15 hover:shadow-sm">
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className={`h-2 w-2 rounded-full ${dot}`} />
          <p className="text-xs font-medium text-foreground/60">Market Sentiment</p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[10px] text-muted-foreground">5 min</span>
          <button
            type="button"
            onClick={() => fetchSentiment(true)}
            aria-label="Refresh sentiment"
            className="text-muted-foreground hover:text-foreground transition-colors"
          >
            <RefreshCw className={`h-3.5 w-3.5 transition-transform ${spinning ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <div className="mb-2 flex items-center gap-2">
        <span className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-semibold ${badge}`}>
          <Icon className="h-3 w-3" />
          {label}
        </span>
        <span className="rounded-full bg-secondary px-2 py-0.5 text-[10px] font-medium text-foreground/60">
          {confidencePct}%
        </span>
      </div>

      <SentimentSparkline
        points={historyPoints}
        range={historyRange}
        onRangeChange={setHistoryRange}
      />

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

      <p className="mt-2 text-[10px] text-muted-foreground/50">
        Updated {new Date(data.updatedAt).toLocaleTimeString()}
      </p>
    </div>
  )
}

const SPARKLINE_SIGNAL_COLOR: Record<MarketSentimentPoint['signal'], string> = {
  bull: '#10b981',
  bear: '#ef4444',
  neutral: '#fbbf24',
}

const SPARKLINE_WIDTH = 100
const SPARKLINE_HEIGHT = 24
const SPARKLINE_PADDING_Y = 3

interface SentimentSparklineProps {
  points: MarketSentimentPoint[]
  range: 7 | 30
  onRangeChange: (range: 7 | 30) => void
}

function SentimentSparkline({
  points,
  range,
  onRangeChange,
}: SentimentSparklineProps) {
  return (
    <div className="my-3 flex items-center justify-between">
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={() => onRangeChange(7)}
          className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
            range === 7
              ? 'bg-black text-white dark:bg-white dark:text-black'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          7D
        </button>
        <button
          type="button"
          onClick={() => onRangeChange(30)}
          className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
            range === 30
              ? 'bg-black text-white dark:bg-white dark:text-black'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          30D
        </button>
      </div>
      <div className="text-[10px] text-muted-foreground">
        {points.length} data points
      </div>
    </div>
  )
}

function MarketSentimentSkeleton() {
  return (
    <div className="rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4 animate-pulse">
      <div className="flex items-center justify-between mb-3">
        <div className="h-3 w-28 bg-muted rounded" />
        <div className="h-3 w-6 bg-muted rounded" />
      </div>
      <div className="h-5 w-20 bg-muted rounded-full mb-3" />
      <div className="h-6 w-full bg-muted rounded mb-3" />
      <div className="h-3 w-3/4 bg-muted rounded" />
    </div>
  )
}
