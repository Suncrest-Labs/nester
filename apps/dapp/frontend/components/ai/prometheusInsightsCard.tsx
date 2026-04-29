'use client'

import { useEffect, useMemo, useState } from 'react'
import { Sparkles } from 'lucide-react'
import { intelligence, type PortfolioInsight } from '@/lib/api/intelligence'
import { useWallet } from '@/components/wallet-provider'

export function PrometheusInsightsCard() {
  const { address } = useWallet()
  const [insights, setInsights] = useState<PortfolioInsight[]>([])
  const [loading, setLoading] = useState(true)

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

  const openChat = (prompt?: string) => {
    if (typeof window === 'undefined') return
    window.dispatchEvent(new CustomEvent('nester:prometheus-open', { detail: { prompt } }))
  }

  if (loading) {
    return (
      <div className="rounded-2xl border border-black/[0.06] bg-white p-6 animate-pulse">
        <div className="h-4 w-36 rounded bg-black/[0.07]" />
        <div className="mt-4 h-3 w-full rounded bg-black/[0.05]" />
        <div className="mt-2 h-3 w-4/5 rounded bg-black/[0.05]" />
      </div>
    )
  }

  return (
    <section className="rounded-2xl border border-black/[0.06] bg-white p-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/[0.05] text-black/60">
            <Sparkles className="h-4 w-4" />
          </div>
          <div>
            <p className="text-[13px] font-semibold text-black">Prometheus Insight</p>
            <p className="text-[11px] text-black/40">AI Intelligence Layer</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => openChat('Which vault should I use for $5,000 with low risk?')}
          className="rounded-full border border-black/[0.12] bg-white px-3 py-1.5 text-[11px] font-medium text-black/70 transition-colors hover:border-black/20 hover:text-black"
        >
          Ask Prometheus
        </button>
      </div>

      {latest ? (
        <div className="rounded-xl border border-black/[0.06] bg-black/[0.015] p-4">
          <p className="text-[12px] font-medium text-black">{latest.title}</p>
          <p className="mt-1.5 text-[12px] leading-relaxed text-black/60">{latest.body}</p>
          <p className="mt-2 text-[10px] text-black/45">Confidence: {Math.round(latest.confidence * 100)}%</p>
        </div>
      ) : (
        <p className="text-[12px] text-black/50">No insight available yet. Ask Prometheus to generate one.</p>
      )}

      <div className="mt-4 rounded-xl border border-black/[0.06] bg-white p-4">
        <p className="text-[11px] font-semibold uppercase tracking-wide text-black/45">Weekly market summary</p>
        <p className="mt-2 text-[12px] leading-relaxed text-black/60">
          {weeklySummary?.body ?? 'Market data is being prepared. Ask Prometheus for a current market read.'}
        </p>
      </div>
    </section>
  )
}
