'use client'

import { useEffect, useMemo, useState } from 'react'
import { Sparkles, X } from 'lucide-react'
import { intelligence, type PortfolioInsight } from '@/lib/api/intelligence'
import { useWallet } from '@/components/wallet-provider'
import { getInsightDismissKey } from './insightCard'

export function PrometheusInsightsCard() {
  const { address } = useWallet()
  const [insights, setInsights] = useState<PortfolioInsight[]>([])
  const [loading, setLoading] = useState(true)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      if (!address) {
        setInsights([])
        setLoading(false)
        return
      }
      setLoading(true)
      const data = await intelligence.getPortfolioInsights(address)
      if (!cancelled) {
        setInsights(data)
        setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
    }
  }, [address])

  const latest = useMemo(() => insights[0], [insights])
  const weeklySummary = useMemo(() => insights[1], [insights])

  const storageKey = useMemo(() => {
    return latest ? getInsightDismissKey(latest.id, latest.title) : ''
  }, [latest])

  useEffect(() => {
    if (typeof window !== 'undefined' && storageKey) {
      if (localStorage.getItem(storageKey) === 'true') {
        setDismissed(true)
      } else {
        setDismissed(false)
      }
    }
  }, [storageKey])

  const handleDismiss = () => {
    if (typeof window !== 'undefined' && storageKey) {
      localStorage.setItem(storageKey, 'true')
    }
    setDismissed(true)
  }

  const openChat = (prompt?: string) => {
    if (typeof window === 'undefined') return
    window.dispatchEvent(new CustomEvent('nester:prometheus-open', { detail: { prompt } }))
  }

  if (loading) {
    return (
      <div className="rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-6 animate-pulse">
        <div className="h-4 w-36 rounded bg-black/[0.07] dark:bg-white/[0.07]" />
        <div className="mt-4 h-3 w-full rounded bg-black/[0.05] dark:bg-white/[0.05]" />
        <div className="mt-2 h-3 w-4/5 rounded bg-black/[0.05] dark:bg-white/[0.05]" />
      </div>
    )
  }

  return (
    <section className="rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/[0.05] dark:bg-white/[0.05] text-black/60 dark:text-white/60">
            <Sparkles className="h-4 w-4" />
          </div>
          <div>
            <p className="text-[13px] font-semibold text-black dark:text-white">Prometheus Insight</p>
            <p className="text-[11px] text-black/40 dark:text-white/40">AI Intelligence Layer</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => openChat('Which vault should I use for $5,000 with low risk?')}
          className="rounded-full border border-black/[0.12] dark:border-white/[0.12] bg-white dark:bg-[#100F0F] px-3 py-1.5 text-[11px] font-medium text-black/70 dark:text-white/70 transition-colors hover:border-black/20 dark:hover:border-white/20 hover:text-black dark:hover:text-white"
        >
          Ask Prometheus
        </button>
      </div>

      {latest && !dismissed ? (
        <div className="relative rounded-xl border border-black/[0.06] dark:border-white/[0.06] bg-black/[0.015] dark:bg-white/[0.015] p-4">
          <button
            type="button"
            onClick={handleDismiss}
            aria-label="Dismiss insight"
            className="absolute top-3 right-3 rounded-lg p-1 text-black/40 dark:text-white/40 hover:bg-black/5 dark:hover:bg-white/5 hover:text-black dark:hover:text-white transition-colors"
          >
            <X className="h-3.5 w-3.5" />
          </button>
          <p className="pr-6 text-[12px] font-medium text-black dark:text-white">{latest.title}</p>
          <p className="mt-1.5 text-[12px] leading-relaxed text-black/60 dark:text-white/60">{latest.body}</p>
          <p className="mt-2 text-[10px] text-black/45 dark:text-white/45">Confidence: {Math.round(latest.confidence * 100)}%</p>
        </div>
      ) : (
        <p className="text-[12px] text-black/50 dark:text-white/50">
          {dismissed ? 'Insight dismissed.' : 'No insight available yet. Ask Prometheus to generate one.'}
        </p>
      )}

      <div className="mt-4 rounded-xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-4">
        <p className="text-[11px] font-semibold uppercase tracking-wide text-black/45 dark:text-white/45">Weekly market summary</p>
        <p className="mt-2 text-[12px] leading-relaxed text-black/60 dark:text-white/60">
          {weeklySummary?.body ?? 'Market data is being prepared. Ask Prometheus for a current market read.'}
        </p>
      </div>
    </section>
  )
}
